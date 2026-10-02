package launcher

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	sd "github.com/coreos/go-systemd/v22/dbus"

	"github.com/Dzobash/apptrol/internal/mixer"
)

// fakeSystemd records the units it is asked to start.
type fakeSystemd struct {
	mu      sync.Mutex
	units   []string
	props   [][]sd.Property
	result  string // the start job's result; "" = "done"
	refuse  string // a property name it does not know, like systemd < 250
	callErr error
	active  []string // names of active units, for the running check
}

// ListUnitsByPatternsContext matches like systemd: globs, "\" escapes.
func (f *fakeSystemd) ListUnitsByPatternsContext(_ context.Context, _, patterns []string) ([]sd.UnitStatus, error) {
	var out []sd.UnitStatus
	for _, name := range f.active {
		for _, p := range patterns {
			if ok, _ := path.Match(p, name); ok {
				out = append(out, sd.UnitStatus{Name: name, ActiveState: "active"})
				break
			}
		}
	}
	return out, nil
}

func (f *fakeSystemd) StartTransientUnitContext(_ context.Context, name, _ string, props []sd.Property, ch chan<- string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.units = append(f.units, name)
	f.props = append(f.props, props)
	if f.callErr != nil {
		return 0, f.callErr
	}
	for _, p := range props {
		if p.Name == f.refuse && f.refuse != "" {
			return 0, errors.New("Cannot set property " + f.refuse + ", or unknown property.")
		}
	}
	result := f.result
	if result == "" {
		result = "done"
	}
	ch <- result
	return 1, nil
}

func (f *fakeSystemd) prop(i int, name string) any {
	for _, p := range f.props[i] {
		if p.Name == name {
			return p.Value.Value()
		}
	}
	return nil
}

func newTestStarter(apps Apps, sys *fakeSystemd) (*Starter, *bytes.Buffer, *[]string) {
	var log bytes.Buffer
	var activated []string
	s := NewStarter(slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug})))
	s.apps = func() Apps { return apps }
	s.connect = func(context.Context) (systemd, error) { return sys, nil }
	s.activate = func(_ context.Context, id string) error { activated = append(activated, id); return nil }
	return s, &log, &activated
}

var testApps = Apps{
	"firefox_firefox": {ID: "firefox_firefox", Name: "Firefox", Exec: "/snap/bin/firefox %u"},
	"My Game":         {ID: "My Game", Name: "My Game", Exec: `steam steam://rungameid/1`},
	"org.example.Bus": {ID: "org.example.Bus", Name: "Bus app", DBusActivatable: true},
	"broken":          {ID: "broken", Name: "Broken", Exec: `app "open`, Path: "/x/broken.desktop"},
}

func launchOf(id string, cmd ...string) mixer.LaunchApp {
	return mixer.LaunchApp{Button: mixer.LED{Transport: mixer.Record}, Launch: mixer.Launch{DesktopID: id, Command: cmd}}
}

// execArgs reads the arguments of go-systemd's ExecStart value, whose type
// is not exported.
func execArgs(v any) []string {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice || rv.Len() != 1 {
		return nil
	}
	args, _ := rv.Index(0).FieldByName("Args").Interface().([]string)
	return args
}

var unitPattern = regexp.MustCompile(`^app-apptrol-.+@[0-9a-f]{8}\.service$`)

func TestLAUNCH04_AnAppStartsInATransientUnit(t *testing.T) {
	sys := &fakeSystemd{}
	s, log, _ := newTestStarter(testApps, sys)
	s.start(launchOf("firefox_firefox"))
	if len(sys.units) != 1 || !unitPattern.MatchString(sys.units[0]) || !strings.Contains(sys.units[0], "firefox_firefox") {
		t.Fatalf("units = %v", sys.units)
	}
	if got := execArgs(sys.prop(0, "ExecStart")); strings.Join(got, " ") != "/snap/bin/firefox" {
		t.Errorf("ExecStart = %+v, want the parsed Exec line (LAUNCH-03)", sys.prop(0, "ExecStart"))
	}
	for name, want := range map[string]string{"CollectMode": "inactive-or-failed", "Type": "exec", "ExitType": "cgroup"} {
		if got := sys.prop(0, name); got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	for _, want := range []string{"starting app", "apptrol.launcher.unit=app-apptrol-firefox_firefox@", "apptrol.component=launcher"} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, log.String())
		}
	}
}

func TestLAUNCH04_UnitNamesAreEscaped(t *testing.T) {
	name := UnitName("My Game")
	if !unitPattern.MatchString(name) || strings.Contains(name, " ") || !strings.Contains(name, `My\x20Game`) {
		t.Errorf("UnitName(My Game) = %q", name)
	}
	if first, second := UnitName("x"), UnitName("x"); first == second {
		t.Error("two starts of one app got the same unit name")
	}
}

func TestLAUNCH05_CommandsRunWithoutAShellAndExpandHome(t *testing.T) {
	sys := &fakeSystemd{}
	s, _, _ := newTestStarter(testApps, sys)
	home, _ := os.UserHomeDir()
	s.start(launchOf("", "~/bin/tool", "--file", "~/x", "a~/b"))
	want := []string{home + "/bin/tool", "--file", home + "/x", "a~/b"}
	if got := execArgs(sys.prop(0, "ExecStart")); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("ExecStart = %q, want %q", got, want)
	}
	if !strings.Contains(sys.units[0], "app-apptrol-tool@") {
		t.Errorf("unit = %s, want the program's name", sys.units[0])
	}
}

func TestLAUNCH03_DBusActivatableAppsAreActivated(t *testing.T) {
	sys := &fakeSystemd{}
	s, _, activated := newTestStarter(testApps, sys)
	s.start(launchOf("org.example.Bus"))
	if len(*activated) != 1 || (*activated)[0] != "org.example.Bus" || len(sys.units) != 0 {
		t.Errorf("activated %v, units %v", *activated, sys.units)
	}
}

func TestLAUNCH11_FailuresAreLogged(t *testing.T) {
	for _, tc := range []struct {
		name   string
		launch mixer.LaunchApp
		sys    *fakeSystemd
		want   string
	}{
		{"not installed", launchOf("com.example.Gone"), &fakeSystemd{}, "is not installed"},
		{"bad Exec", launchOf("broken"), &fakeSystemd{}, "a quote is not closed"},
		{"job failed", launchOf("firefox_firefox"), &fakeSystemd{result: "failed"}, "start job"},
		{"systemd refuses", launchOf("firefox_firefox"), &fakeSystemd{callErr: errors.New("access denied")}, "access denied"},
	} {
		s, log, _ := newTestStarter(testApps, tc.sys)
		s.start(tc.launch)
		for _, want := range []string{"app could not be started", "error.type=app_start_failed", tc.want} {
			if !strings.Contains(log.String(), want) {
				t.Errorf("%s: log lacks %q:\n%s", tc.name, want, log.String())
			}
		}
	}
}

func TestLAUNCH04_OldSystemdWithoutExitType(t *testing.T) {
	sys := &fakeSystemd{refuse: "ExitType"}
	s, log, _ := newTestStarter(testApps, sys)
	s.start(launchOf("firefox_firefox"))
	if len(sys.units) != 2 || sys.prop(1, "ExitType") != nil {
		t.Errorf("units %v; want a second try without ExitType", sys.units)
	}
	if strings.Contains(log.String(), "could not be started") {
		t.Errorf("the second try failed:\n%s", log.String())
	}
}
