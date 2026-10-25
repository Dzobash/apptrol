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
	"github.com/Dzobash/apptrol/internal/version"
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

	mu    sync.Mutex
	leds  map[mixer.LED]bool
	sets  map[mixer.LED]int // how often each LED was set
	ons   map[mixer.LED]int // how often each LED was turned on
	offs  map[mixer.LED]int // how often each LED was turned off
	calls []string          // "release" (with "lit" if an LED was still on) and "take", in order
}

func (f *fakeController) Release() {
	f.mu.Lock()
	defer f.mu.Unlock()
	call := "release"
	for _, on := range f.leds {
		if on {
			call = "release lit"
		}
	}
	f.calls = append(f.calls, call)
}

func (f *fakeController) Take() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "take")
}

func (f *fakeController) callList() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, ", ")
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
	if on {
		if f.ons == nil {
			f.ons = map[mixer.LED]int{}
		}
		f.ons[l]++
	} else {
		if f.offs == nil {
			f.offs = map[mixer.LED]int{}
		}
		f.offs[l]++
	}
	return nil
}

func (f *fakeController) setCount(l mixer.LED) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sets[l]
}

// onCount is how often l was turned on. Unlike setCount, it does not change
// when the startup writes that set every LED off arrive late (#111).
func (f *fakeController) onCount(l mixer.LED) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ons[l]
}

// offCount is how often l was turned off.
func (f *fakeController) offCount(l mixer.LED) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.offs[l]
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
	desktop      *fakeDesktop // nil: no media players
	status       *fakeStatus  // nil: no Status line
}

// fakeStatus records the Status lines the service sends (SVC-13).
type fakeStatus struct {
	mu      sync.Mutex
	texts   []string
	flushed []string // the last text when Flush was called, and how long it may wait
}

func (f *fakeStatus) SetStatus(text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.texts = append(f.texts, text)
}

func (f *fakeStatus) Flush(wait time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	last := ""
	if len(f.texts) > 0 {
		last = f.texts[len(f.texts)-1]
	}
	f.flushed = append(f.flushed, fmt.Sprintf("%s (waiting at most %v)", last, wait))
}

func (f *fakeStatus) list() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.texts...)
}

func (f *fakeStatus) last() string {
	l := f.list()
	if len(l) == 0 {
		return ""
	}
	return l[len(l)-1]
}

// statusLine is the Status line with the running version in front.
func statusLine(state string) string {
	ver, _, _ := version.Info()
	return ver + " · " + state
}

// fakeDesktop reports the media players a test sends through in.
type fakeDesktop struct{ in chan mixer.Event }

func (f *fakeDesktop) Run(ctx context.Context, out chan<- mixer.Event) error {
	for {
		select {
		case ev := <-f.in:
			out <- ev
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (f *fakeDesktop) Apply(mixer.Action) error { return nil }

// fakeSession reports an unlocked screen, as on a desktop, then passes on
// the events a test sends (internal/session). ready is closed once the
// unlocked screen is queued, ahead of anything the test sends.
type fakeSession struct {
	in    chan mixer.Event
	ready chan struct{}
}

func (f *fakeSession) Run(ctx context.Context, out chan<- mixer.Event) error {
	out <- mixer.ScreenChanged{State: mixer.ScreenUnlocked}
	close(f.ready)
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

[inputs.mic]
name  = "Microphone"
match = ["goxlr"]

[layouts.default]
slider1 = "spotify"
slider2 = "browser"
slider8 = "mic"
`

// newEnv prepares a service; config is written unless it is empty.
func newEnv(t *testing.T, cfg string) *env {
	e := &env{t: t, dir: t.TempDir(), out: &syncBuf{}, logs: &fakeLogs{}, done: make(chan error, 1),
		session: fakeSession{in: make(chan mixer.Event, 4), ready: make(chan struct{})}}
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
		PauseDelay:    150 * time.Millisecond,
		// Only Discord is installed in the tests, whatever the machine has.
		InstalledApps: func() launcher.Apps { return launcher.Apps{"discord": {ID: "discord", Name: "Discord"}} },
		Launcher:      &e.launcher,
		Session:       &e.session,
	}
	if e.desktop != nil {
		o.Desktop = e.desktop
	}
	if e.status != nil {
		o.Status = e.status
	}
	go func() { e.done <- Run(ctx, o) }()
	e.t.Cleanup(e.stop)
	e.waitLog("Apptrol running")
	<-e.session.ready
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

// TestSTATE08_InvalidConfigurationKeepsTheSavedState: two restarts with an
// invalid configuration, as seen in the v0.2.0-rc1 checklist (#77), leave the
// state file as it was; the first valid configuration applies it.
func TestSTATE08_InvalidConfigurationKeepsTheSavedState(t *testing.T) {
	const invalid = "[apps.x]\nname = 1\n"
	e := newEnv(t, invalid)
	st := mixer.State{
		Positions: map[mixer.Control]int{{Kind: mixer.Slider, Column: 2}: 0},
		Muted:     map[mixer.Control]bool{{Kind: mixer.Slider, Column: 1}: true},
	}
	if err := state.Save(e.statePath(), st); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(e.statePath())
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		e.start()
		e.waitLog("saved state kept until a valid configuration is loaded")
		e.stop()
		after, err := os.ReadFile(e.statePath())
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, before) {
			t.Fatalf("state file changed by a start with an invalid configuration:\nbefore %s\nafter  %s", before, after)
		}
		// A fresh run: its own log, and the fakes ready to start again.
		e.out = &syncBuf{}
		e.done = make(chan error, 1)
		e.session.ready = make(chan struct{})
	}
	e.start()
	e.waitLog("saved state kept")
	e.write(testConfig)
	e.waitLog("saved state applied")
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

func TestLAUNCH13_RecordAtTheLockScreenStartsNothing(t *testing.T) {
	e := newEnv(t, testConfig+`
[layouts.default.buttons]
record = { app = "discord" }
`).start()
	rec := mixer.LED{Transport: mixer.Record}
	e.eventually("Record LED off at start", func() bool { return e.ctl.setCount(rec) > 0 && !e.ctl.led(rec) })
	before := e.ctl.onCount(rec)
	// Through the controller fake, so they arrive in order with the presses.
	e.send(mixer.ScreenChanged{State: mixer.ScreenLocked})
	e.send(mixer.TransportPressed{Button: mixer.Record})
	e.waitLog("launcher pressed while the screen is locked; nothing started")
	if n := e.launcher.count(); n != 0 {
		t.Errorf("%d apps started at the lock screen", n)
	}
	if e.ctl.onCount(rec) != before {
		t.Error("the Record LED flashed although nothing started")
	}
	e.send(mixer.ScreenChanged{State: mixer.ScreenUnlocked})
	e.send(mixer.TransportPressed{Button: mixer.Record})
	e.eventually("launch after unlocking", func() bool { return e.launcher.count() == 1 })
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

// The old form of an input is logged with its own message and attributes, at
// the start and again at every reload (CFG-19, ADR 0026 point 7). Other
// warnings keep their text in apptrol.warning.
func TestCFG19_OldFormWarningIsLoggedWithItsAttributes(t *testing.T) {
	old := strings.Replace(testConfig, "[inputs.mic]", "[apps.mic]\ntype  = \"input\"", 1) + `
[apps.spare]
match = ["spare"]
`
	e := newEnv(t, old).start()
	const line = "msg=\"config uses an old form; move the entry to its own table\" apptrol.component=config file.path="
	e.waitLog(line)
	e.waitLog("apptrol.app.id=mic apptrol.config.table=inputs")
	e.waitLog(`msg="configuration warning" apptrol.component=config file.path=` + e.cfg + ` apptrol.warning="apps.spare: not assigned to any control"`)
	if n := strings.Count(e.out.String(), line); n != 1 {
		t.Errorf("logged %d times at the start, want once", n)
	}

	e.write(old + "\n# saved again\n")
	e.waitLog("configuration reloaded")
	e.eventually("the warning again after the reload", func() bool { return strings.Count(e.out.String(), line) == 2 })
}

// The id defined in two tables is logged with its id, at the start and on a
// reload, under the fixed message of an invalid configuration (CFG-18,
// ADR 0026 point 7, ADR 0016).
func TestCFG18_SameIDInTwoTablesIsLoggedWithItsID(t *testing.T) {
	dup := testConfig + "\n[apps.mic]\ntype = \"input\"\nmatch = [\"goxlr\"]\n"
	const record = ` apptrol.app.id=mic error.type=config_invalid exception.message="inputs.mic: the same id is defined in two tables ([apps] and [inputs])"`

	e := newEnv(t, dup).start()
	e.waitLog("msg=\"invalid configuration; waiting for a valid one (run `apptrol check`)\" apptrol.component=config file.path=" + e.cfg + record)

	e.write(testConfig)
	e.waitLog("configuration reloaded")
	e.write(dup + "# saved again\n")
	e.waitLog(`msg="the changed configuration is invalid; keeping the current settings" apptrol.component=config file.path=` + e.cfg + record)
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

func TestSVC09_LEDsGoOffBeforeTheControllerIsReleased(t *testing.T) {
	e := newEnv(t, testConfig).start()
	e.send(mixer.ButtonPressed{Button: mixer.ButtonM, Column: 1})
	e.eventually("M1 lit", func() bool { return e.ctl.led(mixer.LED{Button: mixer.ButtonM, Column: 1}) })

	e.send(mixer.SeatChanged{Front: mixer.FrontOtherUser, SessionID: "c2"}) // Switch user
	e.eventually("released", func() bool { return e.ctl.callList() != "" })
	if got := e.ctl.callList(); got != "release" {
		t.Fatalf("controller calls = %q, want a release with every LED off", got)
	}
	e.waitLog(`msg="another user is in front; releasing the controller" apptrol.component=mixer apptrol.seat.front=other_user apptrol.session.id=c2`)

	e.send(mixer.SeatChanged{Front: mixer.FrontThisUser, SessionID: "c1"}) // back
	e.eventually("taken", func() bool { return e.ctl.callList() == "release, take" })
	e.waitLog(`msg="this user is in front again; taking the controller" apptrol.component=mixer apptrol.seat.front=this_user`)
}

// Monitor warnings use ADR 0026's fixed message and identifying attributes on
// startup and every reload, including old-form inputs (CFG-21, ADR 0016).
func TestCFG21_MonitorWarningsAreLoggedOnStartAndReload(t *testing.T) {
	src := strings.Replace(testConfig, "[inputs.mic]", "[apps.mic]\ntype = \"input\"", 1)
	src = strings.Replace(src, `match = ["goxlr"]`, `match = ["test-input.monitor"]`, 1)
	src += "\n[outputs.speakers]\nmatch = [\"test-output.MONITOR\"]\n"
	e := newEnv(t, src).start()
	const message = `msg="match names a monitor, which records an output and is never matched; use the output's name without .monitor" apptrol.component=config file.path=`
	for _, attrs := range []string{"apptrol.app.id=mic apptrol.app.match=test-input.monitor", "apptrol.app.id=speakers apptrol.app.match=test-output.MONITOR"} {
		e.waitLog(message + e.cfg + " " + attrs)
	}
	if n := strings.Count(e.out.String(), message); n != 2 {
		t.Fatalf("startup warnings=%d, want 2", n)
	}
	e.write(src + "\n# reload\n")
	e.waitLog("configuration reloaded")
	e.eventually("monitor warnings repeated on reload", func() bool { return strings.Count(e.out.String(), message) == 4 })
}

// An invalid output selector cannot replace a working config; the shared
// validator logs its reason with config_invalid (CFG-20, CFG-07, ADR 0016).
func TestCFG20_InvalidOutputReloadKeepsConfiguration(t *testing.T) {
	e := newEnv(t, testConfig).start()
	e.waitLog("stream matched")
	e.write(testConfig + "\n[outputs.main]\ndevice = \"default\"\nmatch = [\"test-output\"]\n")
	e.waitLog(`msg="the changed configuration is invalid; keeping the current settings" apptrol.component=config file.path=` + e.cfg + ` error.type=config_invalid exception.message="outputs.main: set exactly one of match and device"`)
	e.write(testConfig + "\n[outputs.main]\ndevice = \"default\"\n")
	e.waitLog("configuration reloaded")
	e.waitLog(`apptrol.warning="outputs.main: not assigned to any control"`)
}

// A slider on a fixed output sets the output device's volume through the
// audio adapter; a fixed output no longer gets the inactive warning (OUT-01,
// CFG-20, ADR 0027).
func TestOUT01_SliderSetsOutputVolumeThroughTheAudioAdapter(t *testing.T) {
	src := testConfig + "\n[outputs.speakers]\nname = \"Speakers\"\nmatch = [\"test-speaker\"]\n"
	src = strings.Replace(src, `slider8 = "mic"`, "slider8 = \"mic\"\nslider7 = \"speakers\"", 1)
	e := newEnv(t, src)
	out := mixer.Device{Name: "alsa_output.test-speaker.analog-stereo", Description: "Test Speaker"}
	e.audio.snapshot.Outputs = []mixer.Device{out}
	e.start()
	e.waitLog(`msg="output matched" apptrol.component=mixer apptrol.layout=default apptrol.control=slider7 apptrol.app.id=speakers apptrol.app.name=Speakers apptrol.app.type=output apptrol.device.name=` + out.Name)
	e.send(slider(7, 127))
	e.waitApplied(mixer.SetOutputVolume{Device: out.Name, Volume: 1})
	e.send(mixer.ButtonPressed{Button: mixer.ButtonM, Column: 7})
	e.waitApplied(mixer.SetOutputMute{Device: out.Name, Muted: true})
	e.send(mixer.ButtonPressed{Button: mixer.ButtonM, Column: 7})
	e.waitApplied(mixer.SetOutputMute{Device: out.Name, Muted: false})
	if strings.Contains(e.out.String(), "is not implemented yet") {
		t.Errorf("a fixed output is still called inactive:\n%s", e.out.String())
	}
}

// An output with device = "default" acts on the default output the audio
// adapter reports, and follows a change of it; it is no longer called
// inactive (OUT-03, CFG-20, ADR 0038).
func TestOUT03_DefaultOutputFollowedThroughTheService(t *testing.T) {
	src := testConfig + "\n[outputs.main]\nname = \"Main volume\"\ndevice = \"default\"\n"
	src = strings.Replace(src, `slider8 = "mic"`, "slider8 = \"mic\"\nslider7 = \"main\"", 1)
	e := newEnv(t, src)
	speakers := mixer.Device{Name: "alsa_output.test-speaker.analog-stereo", Description: "Test Speaker"}
	phones := mixer.Device{Name: "alsa_output.test-headset.analog-stereo", Description: "Test Headset"}
	e.audio.snapshot.Outputs = []mixer.Device{speakers, phones}
	e.audio.snapshot.DefaultOutput = speakers.Name
	e.start()
	e.waitLog(`msg="output matched" apptrol.component=mixer apptrol.layout=default apptrol.control=slider7 apptrol.app.id=main apptrol.app.name="Main volume" apptrol.app.type=output apptrol.device.name=` + speakers.Name)
	e.send(slider(7, 127))
	e.waitApplied(mixer.SetOutputVolume{Device: speakers.Name, Volume: 1})
	e.send(mixer.DefaultOutputChanged{Name: phones.Name})
	e.waitLog(`msg="default output changed" apptrol.component=mixer apptrol.device.name=` + phones.Name + " apptrol.device.previous_name=" + speakers.Name)
	e.send(slider(7, 0))
	e.waitApplied(mixer.SetOutputVolume{Device: phones.Name, Volume: 0})
	if strings.Contains(e.out.String(), "is not implemented yet") {
		t.Errorf("the default output is still called inactive:\n%s", e.out.String())
	}
}

// A browser pauses for a moment while you seek with the mouse; R must not
// blink, and must go dark only once the pause has lasted (MEDIA-09, ADR 0033).
// The mixer writes an LED only for the state it computes, and a resync repeats
// that state, so a write turning R2 off after it lit means the mixer showed the
// pause; no sleep has to prove that none came.
func TestMEDIA09_APauseShowsOnTheLEDsOnlyAfterTheDelay(t *testing.T) {
	e := newEnv(t, testConfig)
	e.desktop = &fakeDesktop{in: make(chan mixer.Event, 8)}
	e.start()
	r2 := mixer.LED{Button: mixer.ButtonR, Column: 2}
	tab := mixer.Player{BusName: "org.mpris.MediaPlayer2.firefox.instance_1_1", Identity: "Firefox", Status: mixer.StatusPlaying}
	e.desktop.in <- mixer.PlayerAdded{Player: tab}
	e.eventually("R2 lit", func() bool { return e.ctl.led(r2) })
	offs := e.ctl.offCount(r2)

	// A short pause: skipped, and its timer comes later and does nothing.
	e.desktop.in <- mixer.PlayerStatusChanged{BusName: tab.BusName, Status: mixer.StatusPaused}
	e.desktop.in <- mixer.PlayerStatusChanged{BusName: tab.BusName, Status: mixer.StatusPlaying}
	e.waitLog("short pause not shown on the LEDs")
	time.Sleep(2 * 150 * time.Millisecond) // the test's PauseDelay has passed
	// A marker after the wait, so the timer's event has most likely been handled;
	// the stale-timer rules themselves are proved in the mixer's tests.
	e.desktop.in <- mixer.PlayerAdded{Player: mixer.Player{BusName: "org.mpris.MediaPlayer2.other", Identity: "Other"}}
	e.waitLog("media player not matched")
	if !e.ctl.led(r2) || e.ctl.offCount(r2) != offs {
		t.Errorf("R2 blinked during a short pause (lit %v, turned off %d times)", e.ctl.led(r2), e.ctl.offCount(r2)-offs)
	}

	// A pause that lasts: R2 goes dark, and not before the delay is over.
	start := time.Now()
	e.desktop.in <- mixer.PlayerStatusChanged{BusName: tab.BusName, Status: mixer.StatusPaused}
	e.eventually("R2 dark after the delay", func() bool { return !e.ctl.led(r2) })
	if waited := time.Since(start); waited < 150*time.Millisecond {
		t.Errorf("R2 went dark after %v, before the 150 ms delay", waited)
	}
	e.waitLog("pause shown on the LEDs")
}

// waitStatus waits until the last Status line sent is want.
func (e *env) waitStatus(want string) {
	e.t.Helper()
	e.eventually("status "+want, func() bool { return e.status.last() == want })
}

func TestSVC13_StatusLineFollowsTheController(t *testing.T) {
	e := newEnv(t, testConfig)
	e.status = &fakeStatus{}
	e.start()

	// The controller connects at once in the fake; testConfig assigns 3 controls.
	e.waitStatus(statusLine("controller connected · 3 controls assigned"))

	e.send(mixer.ControllerDisconnected{})
	e.waitStatus(statusLine("controller not found"))

	e.send(mixer.ControllerConnected{})
	e.waitStatus(statusLine("controller connected · 3 controls assigned"))

	e.send(mixer.SeatChanged{Front: mixer.FrontOtherUser, SessionID: "c2"})
	e.waitStatus(statusLine("controller released (another user in front)"))

	// A late event of the controller does not take it back (SVC-09).
	e.send(mixer.ControllerConnected{Resync: true})
	e.send(mixer.SeatChanged{Front: mixer.FrontThisUser, SessionID: "c1"})
	e.waitStatus(statusLine("controller not found")) // until the controller connects again
	e.send(mixer.ControllerConnected{})
	e.waitStatus(statusLine("controller connected · 3 controls assigned"))
}

func TestSVC13_OnlyAChangeIsSent(t *testing.T) {
	e := newEnv(t, testConfig)
	e.status = &fakeStatus{}
	e.start()
	e.waitStatus(statusLine("controller connected · 3 controls assigned"))

	// The controller says it is there again (LED resync), and the service
	// resends the LEDs after the audio server's snapshot: nothing changes.
	e.send(mixer.ControllerConnected{Resync: true})
	e.send(slider(1, 64))
	e.waitApplied(mixer.SetStreamVolume{StreamID: spotify.ID, Volume: 64.0 / 127})

	want := []string{
		statusLine("controller not found"),
		statusLine("controller connected · 3 controls assigned"),
	}
	if got := e.status.list(); !reflect.DeepEqual(got, want) {
		t.Errorf("status lines = %q, want %q", got, want)
	}
}

func TestSVC13_WaitingForAValidConfiguration(t *testing.T) {
	e := newEnv(t, "[apps.broken]\n")
	e.status = &fakeStatus{}
	e.start()
	e.waitStatus(statusLine("waiting for a valid configuration"))

	e.write(testConfig)
	e.waitStatus(statusLine("controller connected · 3 controls assigned"))
}

func TestSVC13_TheNumberFollowsAReloadedConfiguration(t *testing.T) {
	e := newEnv(t, testConfig)
	e.status = &fakeStatus{}
	e.start()
	e.waitStatus(statusLine("controller connected · 3 controls assigned"))

	e.write(strings.Replace(testConfig, `slider8 = "mic"`, "", 1))
	e.waitStatus(statusLine("controller connected · 2 controls assigned"))
}

func TestSVC13_StatusSaysStoppingOnShutdown(t *testing.T) {
	e := newEnv(t, testConfig)
	e.status = &fakeStatus{}
	e.start()
	e.waitStatus(statusLine("controller connected · 3 controls assigned"))

	e.stop()
	if got, want := e.status.last(), statusLine("stopping"); got != want {
		t.Errorf("last status = %q, want %q", got, want)
	}
	// Shutdown waits for it to reach systemd, but only briefly (SVC-13).
	want := []string{statusLine("stopping") + " (waiting at most 200ms)"}
	e.status.mu.Lock()
	defer e.status.mu.Unlock()
	if !reflect.DeepEqual(e.status.flushed, want) {
		t.Errorf("Flush calls = %q, want %q", e.status.flushed, want)
	}
}

// The controller's LED resends come from their own goroutine, so one can arrive
// after the disconnect; it must not make a missing controller look connected,
// because no later event would correct the Status line (SVC-13).
func TestSVC13_ALateResyncDoesNotMakeTheControllerConnected(t *testing.T) {
	s := &service{}
	for _, step := range []struct {
		ev   mixer.Event
		want bool
	}{
		{mixer.ControllerConnected{}, true},
		{mixer.ControllerConnected{Resync: true}, true},
		{mixer.ControllerDisconnected{}, false},
		{mixer.ControllerConnected{Resync: true}, false}, // late
		{mixer.ControllerConnected{}, true},              // plugged in again
	} {
		s.trackController(step.ev)
		if s.connected != step.want {
			t.Fatalf("after %+v: connected = %v, want %v", step.ev, s.connected, step.want)
		}
	}

	// A connect while the controller is let go does not count either (SVC-09).
	s.connected, s.released = false, true
	s.trackController(mixer.ControllerConnected{})
	if s.connected {
		t.Error("a connect event while released made the controller connected")
	}
}
