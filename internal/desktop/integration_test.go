package desktop

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"github.com/Dzobash/apptrol/internal/mixer"
)

// Integration tests against a real session bus (QA-09). They run only with
// APPTROL_DBUS_TEST=1, inside a private bus: `make test-desktop` starts one
// with dbus-run-session, and CI runs its own. Fake media players are exported
// on that bus by the test itself.

func requireBus(t *testing.T) {
	t.Helper()
	if os.Getenv("APPTROL_DBUS_TEST") != "1" {
		t.Skip("set APPTROL_DBUS_TEST=1 (make test-desktop) to run against a private session bus")
	}
}

// fakePlayer is a minimal MPRIS player on its own connection.
type fakePlayer struct {
	conn  *dbus.Conn
	props *prop.Properties
	name  string
}

func newPlayer(t *testing.T, suffix, identity, desktopEntry, status string) *fakePlayer {
	t.Helper()
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	root := map[string]*prop.Prop{"Identity": {Value: identity, Emit: prop.EmitConst}}
	if desktopEntry != "" {
		root["DesktopEntry"] = &prop.Prop{Value: desktopEntry, Emit: prop.EmitConst}
	}
	props, err := prop.Export(conn, mprisPath, prop.Map{
		ifaceRoot:   root,
		ifacePlayer: {"PlaybackStatus": {Value: status, Emit: prop.EmitTrue}},
	})
	if err != nil {
		t.Fatal(err)
	}
	p := &fakePlayer{conn: conn, props: props, name: mprisPrefix + suffix}
	if err := conn.Export(playerMethods{p}, mprisPath, ifacePlayer); err != nil {
		t.Fatal(err)
	}
	if reply, err := conn.RequestName(p.name, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("RequestName(%s) = %v, %v", p.name, reply, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return p
}

func (p *fakePlayer) setStatus(s string) { p.props.SetMust(ifacePlayer, "PlaybackStatus", s) }

// playerMethods are the MPRIS commands of a fake player, exported on D-Bus.
type playerMethods struct{ p *fakePlayer }

func (m playerMethods) Play() *dbus.Error  { m.p.setStatus("Playing"); return nil }
func (m playerMethods) Pause() *dbus.Error { m.p.setStatus("Paused"); return nil }

// harness runs a Bus and collects its events and log.
type harness struct {
	t      *testing.T
	bus    *Bus
	events chan mixer.Event
	log    *syncBuffer
	cancel context.CancelFunc
	done   chan struct{}
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func runBus(t *testing.T, address string) *harness {
	t.Helper()
	h := &harness{t: t, events: make(chan mixer.Event, 64), log: &syncBuffer{}, done: make(chan struct{})}
	b := New(slog.New(slog.NewTextHandler(h.log, &slog.HandlerOptions{Level: slog.LevelDebug})), address)
	b.retryMin, b.retryMax = 20*time.Millisecond, 50*time.Millisecond
	h.bus = b
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go func() { _ = b.Run(ctx, h.events); close(h.done) }()
	t.Cleanup(func() { cancel(); <-h.done })
	return h
}

func (h *harness) next() mixer.Event {
	h.t.Helper()
	select {
	case ev := <-h.events:
		return ev
	case <-time.After(5 * time.Second):
		h.t.Fatalf("no event within 5 s; log:\n%s", h.log.String())
		return nil
	}
}

// snapshotWith waits for a snapshot and returns the player with the name.
func (h *harness) snapshot() mixer.PlayerSnapshot {
	h.t.Helper()
	for {
		if s, ok := h.next().(mixer.PlayerSnapshot); ok {
			return s
		}
	}
}

func find(players []mixer.Player, name string) (mixer.Player, bool) {
	for _, p := range players {
		if p.BusName == name {
			return p, true
		}
	}
	return mixer.Player{}, false
}

func TestIntegration_MEDIA01_PlayersAreFoundAndFollowed(t *testing.T) {
	requireBus(t)
	spotify := newPlayer(t, "apptroltest_spotify", "Spotify", "spotify", "Playing")
	h := runBus(t, "")

	// Found at connect, with their properties.
	snap := h.snapshot()
	got, ok := find(snap.Players, spotify.name)
	want := mixer.Player{BusName: spotify.name, Identity: "Spotify", DesktopEntry: "spotify", Status: "Playing"}
	if !ok || got != want {
		t.Fatalf("snapshot %v, want it to contain %+v", snap.Players, want)
	}

	// Status changes are followed.
	spotify.setStatus("Paused")
	if ev := h.next(); ev != (mixer.PlayerStatusChanged{BusName: spotify.name, Status: "Paused"}) {
		t.Fatalf("after pause: %v", ev)
	}

	// A player that appears later, without DesktopEntry (like Chrome).
	chrome := newPlayer(t, "apptroltest_chromium.instance42", "Chrome", "", "Stopped")
	want = mixer.Player{BusName: chrome.name, Identity: "Chrome", Status: "Stopped"}
	if ev := h.next(); ev != (mixer.PlayerAdded{Player: want}) {
		t.Fatalf("new player: %v, want PlayerAdded %+v", ev, want)
	}

	// A player that goes away.
	_ = chrome.conn.Close()
	if ev := h.next(); ev != (mixer.PlayerRemoved{BusName: chrome.name}) {
		t.Fatalf("closed player: %v", ev)
	}
	for _, msg := range []string{"connected to the session bus", "media player found", "playback status changed", "media player gone", "apptrol.component=desktop"} {
		if !strings.Contains(h.log.String(), msg) {
			t.Errorf("log lacks %q:\n%s", msg, h.log.String())
		}
	}
}

func TestIntegration_DESK03_ActivatableNamesAreNotStarted(t *testing.T) {
	requireBus(t)
	h := runBus(t, "")
	snap := h.snapshot()
	var activatable []string
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.BusObject().Call(busName+".ListActivatableNames", 0).Store(&activatable); err != nil {
		t.Fatal(err)
	}
	for _, name := range activatable {
		if _, listed := find(snap.Players, name); listed {
			t.Errorf("activatable name %s was listed as a player", name)
		}
	}
}

func TestIntegration_DESK02_NoBusThenRetry(t *testing.T) {
	requireBus(t)
	h := runBus(t, "unix:path=/nonexistent/apptrol-test-bus")
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(h.log.String(), "session bus still unreachable") {
		if time.Now().After(deadline) {
			t.Fatalf("no retry within 5 s; log:\n%s", h.log.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n := strings.Count(h.log.String(), "cannot connect to the session bus"); n != 1 {
		t.Errorf("error logged %d times, want once (retries at debug)", n)
	}
}

// startDaemon runs a session bus of the test's own at address, so the test
// can stop it.
func startDaemon(t *testing.T, address string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("dbus-daemon", "--session", "--nofork", "--nopidfile", "--address="+address)
	if err := cmd.Start(); err != nil {
		t.Skipf("dbus-daemon not available: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	deadline := time.Now().Add(5 * time.Second)
	for {
		if c, err := dbus.Connect(address); err == nil {
			_ = c.Close()
			return cmd
		}
		if time.Now().After(deadline) {
			t.Fatal("dbus-daemon did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestIntegration_DESK02_ReconnectsAfterTheBusRestarts(t *testing.T) {
	requireBus(t)
	address := "unix:path=" + filepath.Join(t.TempDir(), "bus")
	daemon := startDaemon(t, address)
	h := runBus(t, address)
	h.snapshot()

	_ = daemon.Process.Kill()
	_ = daemon.Wait()
	startDaemon(t, address)
	h.snapshot() // reconnected: the players are listed again
	for _, msg := range []string{"lost the connection to the session bus", "desktop_bus_lost"} {
		if !strings.Contains(h.log.String(), msg) {
			t.Errorf("log lacks %q:\n%s", msg, h.log.String())
		}
	}
	if n := strings.Count(h.log.String(), "connected to the session bus"); n != 2 {
		t.Errorf("connected %d times, want 2", n)
	}
}

func TestIntegration_MEDIA04_PlayAndPauseReachThePlayer(t *testing.T) {
	requireBus(t)
	spotify := newPlayer(t, "apptroltest_r_spotify", "Spotify", "spotify", "Paused")
	h := runBus(t, "")
	h.snapshot()

	if err := h.bus.Apply(mixer.PlayerCommand{BusName: spotify.name, Command: mixer.CommandPlay}); err != nil {
		t.Fatal(err)
	}
	if ev := h.next(); ev != (mixer.PlayerStatusChanged{BusName: spotify.name, Status: "Playing"}) {
		t.Fatalf("after Play: %v", ev)
	}
	if err := h.bus.Apply(mixer.PlayerCommand{BusName: spotify.name, Command: mixer.CommandPause}); err != nil {
		t.Fatal(err)
	}
	if ev := h.next(); ev != (mixer.PlayerStatusChanged{BusName: spotify.name, Status: "Paused"}) {
		t.Fatalf("after Pause: %v", ev)
	}
}

func TestIntegration_MEDIA04_ARefusedCommandIsLogged(t *testing.T) {
	requireBus(t)
	h := runBus(t, "")
	h.snapshot()
	gone := mprisPrefix + "apptroltest_gone"
	if err := h.bus.Apply(mixer.PlayerCommand{BusName: gone, Command: mixer.CommandPlay}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(h.log.String(), "media_command_failed") {
		if time.Now().After(deadline) {
			t.Fatalf("no error logged; log:\n%s", h.log.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestDESK02_ApplyWithoutBus(t *testing.T) {
	b := New(slog.New(slog.DiscardHandler), "")
	if err := b.Apply(mixer.PlayerCommand{BusName: "x", Command: mixer.CommandPlay}); !errors.Is(err, ErrNotConnected) {
		t.Errorf("Apply without a connection = %v, want ErrNotConnected", err)
	}
}
