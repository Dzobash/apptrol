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
	return nil
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
	e := &env{t: t, dir: t.TempDir(), out: &syncBuf{}, logs: &fakeLogs{}, done: make(chan error, 1)}
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
		Log:           slog.New(slog.NewTextHandler(e.out, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Logs:          e.logs,
		Audio:         e.audio,
		NewController: func(port string) Controller { e.ctl.port = port; return e.ctl },
		WatchInterval: 10 * time.Millisecond,
		SaveDelay:     10 * time.Millisecond,
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

func TestCFG07_InvalidReloadKeepsSettings(t *testing.T) {
	e := newEnv(t, testConfig).start()
	e.waitLog("stream matched")
	e.write(testConfig + "\n[layouts.default.extra]\n") // invalid
	e.waitLog("the changed configuration is invalid")
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
