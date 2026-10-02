package launcher

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Dzobash/apptrol/internal/mixer"
)

// if_running = "skip" (LAUNCH-06, LAUNCH-07). The unit names below follow
// what the reference system showed for KDE, Flatpak, Snap and a Steam game;
// IDs and numbers are made up.

// fakeProc writes a /proc-like folder with one process of the current user.
func fakeProc(t *testing.T, argv0 string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, strconv.Itoa(os.Getpid()+1000))
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "cmdline"), []byte(argv0+"\x00--flag\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func skipLaunch(id string) mixer.LaunchApp {
	l := launchOf(id)
	l.Launch.SkipIfRunning = true
	return l
}

var skipApps = Apps{
	"discord":                {ID: "discord", Name: "Discord", Exec: "/usr/bin/discord"},
	"vivaldi_vivaldi-stable": {ID: "vivaldi_vivaldi-stable", Name: "Vivaldi", Exec: "/snap/bin/vivaldi_vivaldi-stable %U"},
	"org.example.Flat":       {ID: "org.example.Flat", Name: "Flat", Exec: "/usr/bin/flatpak run org.example.Flat"},
	"Some Game":              {ID: "Some Game", Name: "Some Game", Exec: "steam steam://rungameid/12345"},
	"org.example.Bus":        {ID: "org.example.Bus", Name: "Bus app", DBusActivatable: true},
}

func TestLAUNCH07_FoundByUnit(t *testing.T) {
	for id, unitName := range map[string]string{
		"discord":                `app-discord@0123abcd.service`,                   // KDE
		"vivaldi_vivaldi-stable": `app-vivaldi_vivaldi\x2dstable@0123abcd.service`, // KDE escapes "-"
		"org.example.Flat":       `app-flatpak-org.example.Flat-1234.scope`,        // Flatpak's scope
		"org.example.Bus":        `app-org.example.Bus-5678.scope`,                 // D-Bus activated
	} {
		sys := &fakeSystemd{active: []string{"app-other@1.service", unitName}}
		s, log, _ := newTestStarter(skipApps, sys)
		s.start(skipLaunch(id))
		if len(sys.units) != 0 {
			t.Errorf("%s: started although %s runs", id, unitName)
		}
		for _, want := range []string{"app already running; not started", "running_found_by=unit", "running_unit=" + unitName} {
			if !strings.Contains(log.String(), want) {
				t.Errorf("%s: log lacks %q:\n%s", id, want, log.String())
			}
		}
	}
}

func TestLAUNCH07_SnapScopesAreFound(t *testing.T) {
	sys := &fakeSystemd{active: []string{"snap.vivaldi.vivaldi-stable-0b1c2d3e-aaaa-bbbb-cccc-0123456789ab.scope"}}
	s, _, _ := newTestStarter(skipApps, sys)
	s.start(skipLaunch("vivaldi_vivaldi-stable"))
	if len(sys.units) != 0 {
		t.Error("started although Snap's scope runs")
	}
}

func TestLAUNCH07_FoundByProcess(t *testing.T) {
	sys := &fakeSystemd{}
	s, log, _ := newTestStarter(skipApps, sys)
	s.procDir = fakeProc(t, "/usr/bin/discord") // started from a terminal: no unit
	s.start(skipLaunch("discord"))
	if len(sys.units) != 0 || !strings.Contains(log.String(), "running_found_by=process") {
		t.Errorf("units %v; log:\n%s", sys.units, log.String())
	}
}

func TestLAUNCH06_NotRunningStarts(t *testing.T) {
	sys := &fakeSystemd{active: []string{"app-discordant@1.service", "app-org.example.discord@1.service"}} // other apps
	s, log, _ := newTestStarter(skipApps, sys)
	s.procDir = fakeProc(t, "/usr/bin/other")
	s.start(skipLaunch("discord"))
	if len(sys.units) != 1 {
		t.Errorf("not started; log:\n%s", log.String())
	}
	if !strings.Contains(log.String(), "app not running") || !strings.Contains(log.String(), "processes named discord") {
		t.Errorf("log does not say what was checked:\n%s", log.String())
	}
}

func TestLAUNCH07_WrappersAreNotLookedForAsProcesses(t *testing.T) {
	sys := &fakeSystemd{}
	s, log, _ := newTestStarter(skipApps, sys)
	s.procDir = fakeProc(t, "/usr/bin/flatpak") // some other Flatpak app runs
	s.start(skipLaunch("org.example.Flat"))
	if len(sys.units) != 1 {
		t.Errorf("a running flatpak process stopped another Flatpak app; log:\n%s", log.String())
	}
	if !strings.Contains(log.String(), "no process check: flatpak only starts another program") {
		t.Errorf("log:\n%s", log.String())
	}
}

func TestLAUNCH07_SteamGamesArePassedToSteam(t *testing.T) {
	// Steam lives in the game's unit after the game has ended, and its own
	// process is called steam: neither says whether the game runs.
	sys := &fakeSystemd{active: []string{`app-Some\x20Game@0123abcd.service`}}
	s, log, _ := newTestStarter(skipApps, sys)
	s.procDir = fakeProc(t, "/home/you/.local/share/Steam/ubuntu12_32/steam")
	s.start(skipLaunch("Some Game"))
	if len(sys.units) != 1 {
		t.Errorf("a Steam game was not passed to Steam; log:\n%s", log.String())
	}
	if !strings.Contains(log.String(), "Steam itself does not start a running game twice") {
		t.Errorf("log:\n%s", log.String())
	}
}

func TestLAUNCH06_StartIgnoresRunningApps(t *testing.T) {
	sys := &fakeSystemd{active: []string{"app-discord@0123abcd.service"}}
	s, _, _ := newTestStarter(skipApps, sys)
	s.start(launchOf("discord")) // if_running = "start" (default)
	if len(sys.units) != 1 {
		t.Error("if_running = start must always start")
	}
}

func TestLAUNCH07_UnitPatterns(t *testing.T) {
	got := strings.Join(unitPatterns("vivaldi_vivaldi-stable"), " ")
	for _, want := range []string{
		`app-vivaldi_vivaldi\\x2dstable@*.service`, // escaped, with the "\" protected for the glob
		`app-vivaldi_vivaldi-stable@*.service`,
		`app-*-vivaldi_vivaldi-stable-*.scope`,
		`snap.vivaldi.vivaldi-stable-*.scope`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("patterns lack %q: %s", want, got)
		}
	}
}
