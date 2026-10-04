package service

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Dzobash/apptrol/examples"
	"github.com/Dzobash/apptrol/internal/config"
	"github.com/Dzobash/apptrol/internal/launcher"
	"github.com/Dzobash/apptrol/internal/logattr"
	"github.com/Dzobash/apptrol/internal/mixer"
	"github.com/Dzobash/apptrol/internal/state"
)

// fakeAudio is an audio server that reports a fixed set of streams and
// devices and records what it was asked to do.
type fakeAudio struct {
	snapshot mixer.AudioSnapshot

	mu      sync.Mutex
	applied []mixer.Action
}

func (f *fakeAudio) Run(ctx context.Context, out chan<- mixer.Event) error {
	out <- f.snapshot
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeAudio) Apply(a mixer.Action) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied = append(f.applied, a)
	return nil
}

// has reports whether the action was applied.
func (f *fakeAudio) has(a mixer.Action) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, x := range f.applied {
		if x == a {
			return true
		}
	}
	return false
}

// fakeController is a controller driven by the test through in.
type fakeController struct {
	port string
	in   chan mixer.Event

	mu   sync.Mutex
	leds map[mixer.LED]bool
	sets map[mixer.LED]int // how often each LED was set
}

func (f *fakeController) Run(ctx context.Context, out chan<- mixer.Event) error {
	out <- mixer.ControllerConnected{}
	for {
		select {
		case ev := <-f.in:
			out <- ev
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (f *fakeController) SetLED(l mixer.LED, on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.leds[l] = on
	if f.sets == nil {
		f.sets = map[mixer.LED]int{}
	}
	f.sets[l]++
	return nil
}

func (f *fakeController) setCount(l mixer.LED) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sets[l]
}

func (f *fakeController) HasLED(l mixer.LED) bool { return l.Transport != mixer.TrackPrev }

func (f *fakeController) led(l mixer.LED) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.leds[l]
}

func (f *fakeController) anyLit() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, on := range f.leds {
		if on {
			return true
		}
	}
	return false
}

type fakeLogs struct {
	mu     sync.Mutex
	levels []string
}

func (f *fakeLogs) Reconfigure(cfg config.Log) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.levels = append(f.levels, cfg.Level)
	return nil
}

func (f *fakeLogs) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.levels)
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// env is a running service with fakes around it.
type env struct {
	t      *testing.T
	dir    string
	cfg    string
	audio  *fakeAudio
	ctl    *fakeController
	logs   *fakeLogs
	out    *syncBuf
	cancel context.CancelFunc
	done   chan error

	logLevelFlag string // Options.LogLevelFlag
	launcher     fakeLauncher
	session      fakeSession
}

// fakeSession passes on the wake-ups a test sends (internal/session).
type fakeSession struct{ in chan mixer.Event }

func (f *fakeSession) Run(ctx context.Context, out chan<- mixer.Event) error {
	for {
		select {
		case ev := <-f.in:
			out <- ev
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// fakeLauncher records the apps it is asked to start.
type fakeLauncher struct {
	mu  sync.Mutex
	got []mixer.LaunchApp
}

func (f *fakeLauncher) Launch(l mixer.LaunchApp) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, l)
}

func (f *fakeLauncher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.got)
}

var (
	spotify = mixer.Stream{ID: 10, AppName: "Spotify"}
	firefox = mixer.Stream{ID: 11, AppName: "Firefox", Binary: "firefox"}
	goxlr   = mixer.Device{Name: "alsa_input.goxlr", Description: "GoXLR Mini Mic"}
)

const testConfig = `
[log]
level = "debug"

[apps.spotify]
name  = "Spotify"
match = ["spotify"]

[apps.browser]
name  = "Browser"
match = ["firefox"]

[apps.mic]
name  = "Microphone"
type  = "input"
match = ["goxlr"]

[layouts.default]
slider1 = "spotify"
slider2 = "browser"
slider8 = "mic"
`

// newEnv prepares a service; config is written unless it is empty.
func newEnv(t *testing.T, cfg string) *env {
	e := &env{t: t, dir: t.TempDir(), out: &syncBuf{}, logs: &fakeLogs{}, done: make(chan error, 1),
		session: fakeSession{in: make(chan mixer.Event, 4)}}
	e.cfg = filepath.Join(e.dir, "config", "config.toml")
	if cfg != "" {
		e.write(cfg)
	}
	e.audio = &fakeAudio{snapshot: mixer.AudioSnapshot{Streams: []mixer.Stream{spotify, firefox}, Devices: []mixer.Device{goxlr}}}
	e.ctl = &fakeController{in: make(chan mixer.Event, 16), leds: map[mixer.LED]bool{}}
	return e
}

func (e *env) write(cfg string) {
	e.t.Helper()
	if err := os.MkdirAll(filepath.Dir(e.cfg), 0o755); err != nil {
		e.t.Fatal(err)
	}
	// Rename into place, as editors do, so the watcher never sees half a file.
	tmp := e.cfg + ".tmp"
	if err := os.WriteFile(tmp, []byte(cfg), 0o644); err != nil {
		e.t.Fatal(err)
	}
	if err := os.Rename(tmp, e.cfg); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) statePath() string { return filepath.Join(e.dir, "state", "state.json") }

func (e *env) start() *env {
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	o := Options{
		ConfigPath:    e.cfg,
		StatePath:     e.statePath(),
		Log:           slog.New(&checkHandler{t: e.t, inner: slog.NewTextHandler(e.out, &slog.HandlerOptions{Level: slog.LevelDebug})}),
		Logs:          e.logs,
		LogLevelFlag:  e.logLevelFlag,
		Audio:         e.audio,
		NewController: func(port string) Controller { e.ctl.port = port; return e.ctl },
		WatchInterval: 10 * time.Millisecond,
		SaveDelay:     10 * time.Millisecond,
		LEDResync:     20 * time.Millisecond,
		// Only Discord is installed in the tests, whatever the machine has.
		InstalledApps: func() launcher.Apps { return launcher.Apps{"discord": {ID: "discord", Name: "Discord"}} },
		Launcher:      &e.launcher,
		Session:       &e.session,
	}
	go func() { e.done <- Run(ctx, o) }()
	e.t.Cleanup(e.stop)
	e.waitLog("Apptrol running")
	return e
}

func (e *env) stop() {
	if e.cancel == nil {
		return
	}
	e.cancel()
	e.cancel = nil
	select {
	case err := <-e.done:
		if err != nil {
			e.t.Errorf("Run = %v", err)
		}
	case <-time.After(5 * time.Second):
		e.t.Fatal("Run did not stop")
	}
}

func (e *env) send(ev mixer.Event) { e.ctl.in <- ev }

func (e *env) eventually(what string, cond func() bool) {
	e.t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); time.Sleep(2 * time.Millisecond) {
		if time.Now().After(deadline) {
			e.t.Fatalf("timed out waiting for %s\nlog:\n%s", what, e.out.String())
		}
	}
}

func (e *env) waitLog(s string) {
	e.t.Helper()
	e.eventually(fmt.Sprintf("log %q", s), func() bool { return strings.Contains(e.out.String(), s) })
}

func (e *env) waitApplied(a mixer.Action) {
	e.t.Helper()
	e.eventually(fmt.Sprintf("%+v", a), func() bool { return e.audio.has(a) })
}

func slider(col, v int) mixer.ControlMoved {
	return mixer.ControlMoved{Control: mixer.Control{Kind: mixer.Slider, Column: col}, Value: v}
}

func TestSliderMuteAndState(t *testing.T) {
	e := newEnv(t, testConfig).start()
	e.waitLog("stream matched")
	if e.logs.count() != 1 {
		t.Errorf("log settings applied %d times, want 1", e.logs.count())
	}
	if e.ctl.port != DefaultPort {
		t.Errorf("controller port %q", e.ctl.port)
	}

	e.send(slider(1, 127))
	e.waitApplied(mixer.SetStreamVolume{StreamID: spotify.ID, Volume: 1})

	e.send(mixer.ButtonPressed{Button: mixer.ButtonM, Column: 1})
	e.waitApplied(mixer.SetStreamMute{StreamID: spotify.ID, Muted: true})
	e.eventually("M1 lit", func() bool { return e.ctl.led(mixer.LED{Button: mixer.ButtonM, Column: 1}) })
	e.waitLog("msg=muted")

	// The input column's LEDs are lit (LED-04).
	if !e.ctl.led(mixer.LED{Button: mixer.ButtonS, Column: 8}) {
		t.Error("input column LEDs not lit")
	}

	// State is saved (STATE-02).
	e.eventually("state saved", func() bool {
		st, _, err := state.Load(e.statePath())
		return err == nil && st.Positions[mixer.Control{Kind: mixer.Slider, Column: 1}] == 127 &&
			st.Muted[mixer.Control{Kind: mixer.Slider, Column: 1}]
	})
}

func TestSTATE03_StateRestoredOnStart(t *testing.T) {
	e := newEnv(t, testConfig)
	st := mixer.State{
		Positions: map[mixer.Control]int{{Kind: mixer.Slider, Column: 2}: 0},
		Muted:     map[mixer.Control]bool{{Kind: mixer.Slider, Column: 1}: true},
	}
	if err := state.Save(e.statePath(), st); err != nil {
		t.Fatal(err)
	}
	e.start()
	e.waitLog("state restored")
	e.waitApplied(mixer.SetStreamVolume{StreamID: firefox.ID, Volume: 0})
	e.waitApplied(mixer.SetStreamMute{StreamID: spotify.ID, Muted: true})
}

func TestSTATE05_DamagedState(t *testing.T) {
	e := newEnv(t, testConfig)
	if err := os.MkdirAll(filepath.Dir(e.statePath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.statePath(), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.start()
	e.waitLog("cannot use the saved state")
	e.send(slider(1, 0))
	e.waitApplied(mixer.SetStreamVolume{StreamID: spotify.ID, Volume: 0})
}

func TestCFG09_ExampleCreatedOnFirstStart(t *testing.T) {
	e := newEnv(t, "").start()
	e.waitLog("created an example configuration")
	got, err := os.ReadFile(e.cfg)
	if err != nil || !bytes.Equal(got, examples.Config) {
		t.Fatalf("example not written: %v", err)
	}
	e.waitLog("configuration loaded")
	// The example assigns Spotify to slider 1.
	e.send(slider(1, 0))
	e.waitApplied(mixer.SetStreamVolume{StreamID: spotify.ID, Volume: 0})
}

func TestCFG09_ExampleCannotBeCreated(t *testing.T) {
	e := newEnv(t, "")
	// A dangling link: reading finds no file, and creating one must not
	// follow the link.
	if err := os.MkdirAll(filepath.Dir(e.cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(e.dir, "nowhere", "x.toml"), e.cfg); err != nil {
		t.Fatal(err)
	}
	e.start()
	e.waitLog("the example could not be created")
}

func TestCFG07_UnreadableConfig(t *testing.T) {
	e := newEnv(t, "")
	if err := os.WriteFile(filepath.Dir(e.cfg), nil, 0o644); err != nil { // a file where the directory should be
		t.Fatal(err)
	}
	e.start()
	e.waitLog("cannot read the configuration")
}

func TestCFG06_Reload(t *testing.T) {
	e := newEnv(t, testConfig).start()
	e.waitLog("stream matched")

	// Move Spotify to slider 3.
	e.write(strings.Replace(testConfig, "slider1 = \"spotify\"", "slider3 = \"spotify\"", 1))
	e.waitLog("configuration reloaded")
	e.send(slider(3, 0))
	e.waitApplied(mixer.SetStreamVolume{StreamID: spotify.ID, Volume: 0})
	if e.logs.count() != 2 {
		t.Errorf("log settings applied %d times, want 2 (LOG-08)", e.logs.count())
	}
}

func TestCFG13_ButtonsFromTheConfiguration(t *testing.T) {
	e := newEnv(t, testConfig+`
[layouts.default.buttons]
record = { app = "com.obsproject.Studio" }
m8     = { mode = "hold_to_talk", talk_over = true }
`).start()
	// Each configured button is logged with what it does (LOG-15).
	e.waitLog("button configured")
	for _, want := range []string{"apptrol.button=M8", "apptrol.button_mode=hold_to_talk", "apptrol.button_talk_over=true",
		"apptrol.button=●", "apptrol.button_mode=launcher", "apptrol.launcher.desktop_id=com.obsproject.Studio"} {
		e.waitLog(want)
	}
	// Hold-to-talk reaches the mixer: muted from the start, live while M8 is held (INPUT-03).
	e.waitApplied(mixer.SetDeviceMute{Device: goxlr.Name, Muted: true})
	e.send(mixer.ButtonPressed{Button: mixer.ButtonM, Column: 8})
	e.waitApplied(mixer.SetDeviceMute{Device: goxlr.Name, Muted: false})
}

func TestLAUNCH08_RecordStartsItsAppAndFlashes(t *testing.T) {
	e := newEnv(t, testConfig+`
[layouts.default.buttons]
record = { app = "discord" }
`).start()
	rec := mixer.LED{Transport: mixer.Record}
	e.eventually("Record LED off at start", func() bool { return e.ctl.setCount(rec) > 0 && !e.ctl.led(rec) })
	before := e.ctl.setCount(rec)
	e.send(mixer.TransportPressed{Button: mixer.Record})
	e.eventually("launch", func() bool { return e.launcher.count() == 1 })
	e.eventually("flash on", func() bool { return e.ctl.setCount(rec) > before })
	e.eventually("flash off", func() bool { return e.ctl.setCount(rec) >= before+2 && !e.ctl.led(rec) })
}

func TestLED09_WakeUpSendsEveryLEDAgain(t *testing.T) {
	e := newEnv(t, testConfig).start()
	s8 := mixer.LED{Button: mixer.ButtonS, Column: 8}
	// Sent on connect, on the audio snapshot and on the resync after it.
	e.eventually("startup LEDs", func() bool { return e.ctl.setCount(s8) >= 3 })
	e.ctl.mu.Lock()
	e.ctl.leds = map[mixer.LED]bool{} // the controller lost power while the computer slept
	e.ctl.mu.Unlock()
	e.session.in <- mixer.SystemResumed{}
	e.eventually("mic column lit again", func() bool {
		return e.ctl.led(s8) && e.ctl.led(mixer.LED{Button: mixer.ButtonM, Column: 8}) &&
			e.ctl.led(mixer.LED{Button: mixer.ButtonR, Column: 8})
	})
}

func TestLAUNCH10_LaunchersOfMissingAppsAreWarnedAbout(t *testing.T) {
	e := newEnv(t, testConfig+`
[layouts.default.buttons]
record     = { app = "com.obsproject.Studio" }
marker_set = { app = "discord" }
r1         = { command = ["konsole"] }
`).start()
	e.waitLog("desktop ID not installed")
	for _, want := range []string{"apptrol.button=●", "apptrol.launcher.desktop_id=com.obsproject.Studio"} {
		e.waitLog(want)
	}
	if n := strings.Count(e.out.String(), "desktop ID not installed"); n != 1 {
		t.Errorf("warned %d times, want once (Discord is installed, commands are not checked)", n)
	}
}

func TestCFG07_InvalidReloadKeepsSettings(t *testing.T) {
	e := newEnv(t, testConfig).start()
	e.waitLog("stream matched")
	e.write(testConfig + "\n[layouts.default.extra]\n") // invalid
	e.waitLog("the changed configuration is invalid")
	e.send(slider(1, 0))
	e.waitApplied(mixer.SetStreamVolume{StreamID: spotify.ID, Volume: 0})
	// Two problems: one record each, on one line, with the component and the
	// error type (LOG-10, LOG-12, LOG-13).
	e.write(testConfig + "\n[apps.y]\nname = \"Y\"\ntype = \"bogus\"\nmatch = [\"y\"]\n[apps.z]\nname = \"Z\"\ntype = \"app\"\n")
	e.eventually("two problem records", func() bool {
		return strings.Count(e.out.String(), "error.type=config_invalid") >= 3
	})
	for _, l := range strings.Split(e.out.String(), "\n") {
		if strings.Contains(l, "config_invalid") &&
			(!strings.Contains(l, "apptrol.component=config") || !strings.Contains(l, "exception.message=")) {
			t.Errorf("config problem record lacks component or message: %s", l)
		}
	}
	e.send(slider(1, 64))
	e.waitApplied(mixer.SetStreamVolume{StreamID: spotify.ID, Volume: 64.0 / 127})
	e.send(slider(1, 0))
	e.waitApplied(mixer.SetStreamVolume{StreamID: spotify.ID, Volume: 0})

	if err := os.Remove(e.cfg); err != nil {
		t.Fatal(err)
	}
	e.waitLog("the configuration file was removed")
}

func TestCFG07_InvalidAtStartThenFixed(t *testing.T) {
	e := newEnv(t, "[apps.x]\nname = 1\n").start()
	e.waitLog("invalid configuration; waiting for a valid one")
	e.send(slider(1, 0))
	time.Sleep(30 * time.Millisecond)
	if e.audio.has(mixer.SetStreamVolume{StreamID: spotify.ID, Volume: 0}) {
		t.Fatal("volume changed without a configuration")
	}
	e.write(testConfig)
	e.waitLog("configuration reloaded")
	e.send(slider(1, 0))
	e.waitApplied(mixer.SetStreamVolume{StreamID: spotify.ID, Volume: 0})
}

func TestPortChangeNeedsRestart(t *testing.T) {
	e := newEnv(t, "[controller]\nport = \"first\"\n"+testConfig).start()
	if e.ctl.port != "first" {
		t.Errorf("port = %q", e.ctl.port)
	}
	e.write("[controller]\nport = \"second\"\n" + testConfig)
	e.waitLog("restart Apptrol to use it")
}

func TestLOG16_StartRecordNamesLevelSource(t *testing.T) {
	tests := []struct {
		name string
		flag string
		want string
	}{
		{"from the configuration", "", "apptrol.log.level_source=config"},
		{"from --log-level", "debug", "apptrol.log.level=debug apptrol.log.level_source=flag"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, testConfig)
			e.logLevelFlag = tt.flag
			e.start()
			first := strings.SplitN(e.out.String(), "\n", 2)[0]
			if !strings.Contains(first, "Apptrol starting") || !strings.Contains(first, tt.want) {
				t.Errorf("first record = %q, want the start record with %q", first, tt.want)
			}
		})
	}
}

func TestSVC05_SVC07_Shutdown(t *testing.T) {
	e := newEnv(t, testConfig).start()
	e.waitLog("stream matched")
	e.send(slider(2, 64))
	e.send(mixer.ButtonPressed{Button: mixer.ButtonS, Column: 1}) // solo Spotify: Firefox is silenced
	e.waitApplied(mixer.SetStreamMute{StreamID: firefox.ID, Muted: true})

	e.stop()
	if !e.audio.has(mixer.SetStreamMute{StreamID: firefox.ID, Muted: false}) {
		t.Error("solo not ended on shutdown (SVC-07)")
	}
	if e.ctl.anyLit() {
		t.Error("LEDs still lit after shutdown (LED-08)")
	}
	st, _, err := state.Load(e.statePath())
	if err != nil || st.Positions[mixer.Control{Kind: mixer.Slider, Column: 2}] != 64 {
		t.Errorf("state not saved on shutdown: %v %+v", err, st)
	}
	if st.Solo != 1 {
		t.Errorf("saved solo = %d, want 1: the next start restores it (STATE-04)", st.Solo)
	}
	for _, want := range []string{"Apptrol stopping", "Apptrol stopped"} {
		if !strings.Contains(e.out.String(), want) {
			t.Errorf("log lacks %q", want)
		}
	}
}

func TestCTRL07_Coalesce(t *testing.T) {
	s1, s2 := mixer.Control{Kind: mixer.Slider, Column: 1}, mixer.Control{Kind: mixer.Slider, Column: 2}
	m := mixer.ButtonPressed{Button: mixer.ButtonM, Column: 1}
	in := []mixer.Event{
		mixer.ControlMoved{Control: s1, Value: 1},
		mixer.ControlMoved{Control: s2, Value: 5},
		mixer.ControlMoved{Control: s1, Value: 2},
		m,
		mixer.ControlMoved{Control: s1, Value: 3},
	}
	want := []mixer.Event{
		mixer.ControlMoved{Control: s2, Value: 5},
		m,
		mixer.ControlMoved{Control: s1, Value: 3},
	}
	if got := Coalesce(in); !reflect.DeepEqual(got, want) {
		t.Errorf("Coalesce = %v, want %v", got, want)
	}
	if in[0] != (mixer.ControlMoved{Control: s1, Value: 1}) {
		t.Error("Coalesce changed its input")
	}
}

func TestLED07_LEDsResentAfterAudioReconnect(t *testing.T) {
	e := newEnv(t, testConfig).start()
	s1 := mixer.LED{Button: mixer.ButtonS, Column: 1}
	e.send(mixer.ButtonPressed{Button: mixer.ButtonS, Column: 1})
	e.eventually("S1 lit", func() bool { return e.ctl.led(s1) })
	before := e.ctl.setCount(s1)
	// The audio server comes back: the LEDs are sent at once and once more later.
	e.send(e.audio.snapshot)
	e.eventually("S1 sent twice more", func() bool { return e.ctl.setCount(s1) >= before+2 })
	if !e.ctl.led(s1) {
		t.Error("S1 dark after the audio server came back")
	}
}

// checkHandler fails the test for any record that breaks ADR 0016: every
// record names its component (LOG-10), attribute names follow the naming rules
// (LOG-11), errors carry error.type (LOG-12), and the message is one line
// (LOG-13).
// kinds remembers the value type of every attribute name seen in any test.
var (
	kindsMu sync.Mutex
	kinds   = map[string]slog.Kind{}
)

type checkHandler struct {
	t     *testing.T
	inner slog.Handler
	attrs []slog.Attr
}

func (h *checkHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *checkHandler) Handle(ctx context.Context, r slog.Record) error {
	keys := map[string]bool{}
	var walk func(a slog.Attr)
	walk = func(a slog.Attr) {
		if a.Key == "" && a.Value.Kind() == slog.KindGroup {
			for _, g := range a.Value.Group() {
				walk(g)
			}
			return
		}
		keys[a.Key] = true
		if !logattr.ValidKey(a.Key) {
			h.t.Errorf("record %q: attribute name %q breaks LOG-11", r.Message, a.Key)
		}
		// One type per name, so log stores such as Elasticsearch can map it (LOG-11).
		kindsMu.Lock()
		if k, seen := kinds[a.Key]; seen && k != a.Value.Kind() {
			h.t.Errorf("record %q: %s is %v here but %v elsewhere", r.Message, a.Key, a.Value.Kind(), k)
		}
		kinds[a.Key] = a.Value.Kind()
		kindsMu.Unlock()
	}
	for _, a := range h.attrs {
		walk(a)
	}
	r.Attrs(func(a slog.Attr) bool { walk(a); return true })
	if !keys[logattr.KeyComponent] {
		h.t.Errorf("record %q has no %s (LOG-10)", r.Message, logattr.KeyComponent)
	}
	if r.Level >= slog.LevelError && !keys[logattr.KeyErrorType] {
		h.t.Errorf("error record %q has no %s (LOG-12)", r.Message, logattr.KeyErrorType)
	}
	if strings.ContainsAny(r.Message, "\r\n") {
		h.t.Errorf("record %q is not one line (LOG-13)", r.Message)
	}
	return h.inner.Handle(ctx, r)
}

func (h *checkHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &checkHandler{t: h.t, inner: h.inner.WithAttrs(attrs), attrs: append(append([]slog.Attr(nil), h.attrs...), attrs...)}
}

func (h *checkHandler) WithGroup(name string) slog.Handler {
	h.t.Errorf("WithGroup(%q): groups would change attribute names", name)
	return h
}
