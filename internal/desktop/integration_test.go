package desktop

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
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
	conn := connect(t)
	p := exportPlayer(t, conn, mprisPrefix+suffix, identity, desktopEntry, status)
	requestName(t, conn, p.name, 0)
	return p
}

// connect opens a connection of the test's own to the session bus (MEDIA-01,
// ADR 0018, #254).
func connect(t *testing.T) *dbus.Conn {
	t.Helper()
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// requestName makes conn own name; flags may allow or ask for a handover
// (MEDIA-01, ADR 0018, #254).
func requestName(t *testing.T, conn *dbus.Conn, name string, flags dbus.RequestNameFlags) {
	t.Helper()
	if reply, err := conn.RequestName(name, flags|dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("RequestName(%s) = %v, %v", name, reply, err)
	}
}

// exportPlayer exports the MPRIS object of a player that owns, or will own,
// name on conn. It sends no PropertiesChanged, as a player starting up does
// not (MEDIA-01, ADR 0018, #254).
func exportPlayer(t *testing.T, conn *dbus.Conn, name, identity, desktopEntry, status string) *fakePlayer {
	t.Helper()
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
	p := &fakePlayer{conn: conn, props: props, name: name}
	if err := conn.Export(playerMethods{p}, mprisPath, ifacePlayer); err != nil {
		t.Fatal(err)
	}
	return p
}

func (p *fakePlayer) setStatus(s string) { p.props.SetMust(ifacePlayer, "PlaybackStatus", s) }

// playerMethods are the MPRIS commands of a fake player, exported on D-Bus.
type playerMethods struct{ p *fakePlayer }

func (m playerMethods) Play() *dbus.Error  { m.p.setStatus("Playing"); return nil }
func (m playerMethods) Pause() *dbus.Error { m.p.setStatus("Paused"); return nil }
func (m playerMethods) Stop() *dbus.Error  { m.p.setStatus("Stopped"); return nil }

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

// One connection owns two player names, as a daemon serving several players
// does. Both share one object, so a change reaches both, and when one name
// goes the other is still followed (MEDIA-01, ADR 0018, #254).
func TestIntegration_MEDIA01_OneConnectionWithTwoPlayerNames(t *testing.T) {
	requireBus(t)
	first := newPlayer(t, "apptroltest_multi_a", "Multi", "", "Paused")
	h := runBus(t, "")
	if _, ok := find(h.snapshot().Players, first.name); !ok {
		t.Fatalf("snapshot lacks %s; log:\n%s", first.name, h.log.String())
	}

	// A second name on the same connection.
	second := mprisPrefix + "apptroltest_multi_b"
	if reply, err := first.conn.RequestName(second, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("RequestName(%s) = %v, %v", second, reply, err)
	}
	want := mixer.Player{BusName: second, Identity: "Multi", Status: "Paused"}
	if ev := h.next(); ev != (mixer.PlayerAdded{Player: want}) {
		t.Fatalf("second name: %v, want PlayerAdded %+v", ev, want)
	}

	// One change on the shared object reaches both names.
	first.setStatus("Playing")
	got := []mixer.Event{h.next(), h.next()}
	for _, name := range []string{first.name, second} {
		if ev := (mixer.PlayerStatusChanged{BusName: name, Status: "Playing"}); !slices.Contains(got, mixer.Event(ev)) {
			t.Errorf("after play: %v, want %+v among them", got, ev)
		}
	}

	// The second name goes; the first is still followed.
	if _, err := first.conn.ReleaseName(second); err != nil {
		t.Fatal(err)
	}
	if ev := h.next(); ev != (mixer.PlayerRemoved{BusName: second}) {
		t.Fatalf("released name: %v", ev)
	}
	// A stray change for the released name would sort after the first name's,
	// so a second change must follow the first one directly.
	for _, status := range []string{"Paused", "Playing"} {
		first.setStatus(status)
		if ev := h.next(); ev != (mixer.PlayerStatusChanged{BusName: first.name, Status: status}) {
			t.Fatalf("after %s: %v, want only %s's change; log:\n%s", status, ev, first.name, h.log.String())
		}
	}
}

// silentProps answers Properties.Get never: it blocks until the test ends,
// like a hanging player.
type silentProps struct{ release <-chan struct{} }

func (s silentProps) Get(_, _ string) (dbus.Variant, *dbus.Error) {
	<-s.release
	return dbus.Variant{}, dbus.MakeFailedError(errors.New("released"))
}

// newSilentPlayer owns an MPRIS name but never answers Properties.Get.
func newSilentPlayer(t *testing.T, suffix string) string {
	t.Helper()
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	t.Cleanup(func() { close(release); _ = conn.Close() })
	if err := conn.Export(silentProps{release}, mprisPath, ifaceProps); err != nil {
		t.Fatal(err)
	}
	name := mprisPrefix + suffix
	if reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("RequestName(%s) = %v, %v", name, reply, err)
	}
	return name
}

// A player that does not answer is left out, but the players listed after it
// are still found: each call has its own timeout (MEDIA-01, #119).
func TestIntegration_MEDIA01_ASilentPlayerDoesNotHideOthers(t *testing.T) {
	requireBus(t)
	silent := newSilentPlayer(t, "apptroltest_a_silent") // sorts first
	healthy := newPlayer(t, "apptroltest_b_healthy", "Healthy", "", "Paused")
	h := runBus(t, "")

	snap := h.snapshot()
	if _, ok := find(snap.Players, silent); ok {
		t.Errorf("snapshot %v contains the silent player", snap.Players)
	}
	want := mixer.Player{BusName: healthy.name, Identity: "Healthy", Status: "Paused"}
	if got, ok := find(snap.Players, healthy.name); !ok || got != want {
		t.Fatalf("snapshot %v, want it to contain %+v; log:\n%s", snap.Players, want, h.log.String())
	}
	if !strings.Contains(h.log.String(), "player_unreadable") {
		t.Errorf("the silent player is not logged:\n%s", h.log.String())
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
	// The media keys' commands go the same way (MEDIA-05).
	if err := h.bus.Apply(mixer.PlayerCommand{BusName: spotify.name, Command: mixer.CommandStop}); err != nil {
		t.Fatal(err)
	}
	if ev := h.next(); ev != (mixer.PlayerStatusChanged{BusName: spotify.name, Status: "Stopped"}) {
		t.Fatalf("after Stop: %v", ev)
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

// More status changes than the session's signal channel holds still arrive
// in order, so every change is followed and the last one is what the player
// reports in the end (MEDIA-01, ADR 0018, #254).
func TestIntegration_MEDIA01_ABacklogOfChangesKeepsItsOrder(t *testing.T) {
	requireBus(t)
	p := newPlayer(t, "apptroltest_backlog", "Backlog", "", "Paused")
	h := runBus(t, "")
	h.snapshot()
	// The events are not read meanwhile, so the session blocks and the
	// signals pile up behind it.
	for i := range 300 {
		p.setStatus([]string{"Playing", "Paused"}[i%2])
	}
	p.setStatus("Stopped")
	// Let the bus deliver every signal first, so they queue up past the
	// session's channel before anything is read.
	time.Sleep(300 * time.Millisecond)
	for i := range 301 {
		status := "Stopped"
		if i < 300 {
			status = []string{"Playing", "Paused"}[i%2]
		}
		if ev := h.next(); ev != (mixer.PlayerStatusChanged{BusName: p.name, Status: status}) {
			t.Fatalf("change %d: %v, want %s", i, ev, status)
		}
	}
}

// waitForLog waits until one log line contains every part (MEDIA-01,
// ADR 0018, #254).
func (h *harness) waitForLog(parts ...string) {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for h.count(parts...) == 0 {
		if time.Now().After(deadline) {
			h.t.Fatalf("no log line with %q within 5 s; log:\n%s", parts, h.log.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// count returns how many log lines contain every part (MEDIA-01, ADR 0018,
// #254).
func (h *harness) count(parts ...string) int {
	n := 0
	for line := range strings.Lines(h.log.String()) {
		if !slices.ContainsFunc(parts, func(p string) bool { return !strings.Contains(line, p) }) {
			n++
		}
	}
	return n
}

// longRetries gives a player that is not ready about 5 s of reads, every
// 100 ms, as long as the tests wait for an event: a slow test machine cannot
// end the wait before the test exports or closes the player (MEDIA-01,
// ADR 0018, #254). Call it before runBus, so the cleanup ends the Bus first.
func longRetries(t *testing.T) {
	t.Helper()
	setRetries(t, slices.Repeat([]time.Duration{100 * time.Millisecond}, 50)...)
}

// setRetries replaces notReadyRetry for one test; call it before runBus
// (MEDIA-01, ADR 0018, #254).
func setRetries(t *testing.T, waits ...time.Duration) {
	t.Helper()
	old := notReadyRetry
	notReadyRetry = waits
	t.Cleanup(func() { notReadyRetry = old })
}

// A player that owns its name a moment before it exports its object is read
// again until it answers, at connect and while running (MEDIA-01, ADR 0018,
// #254). The object comes without a PropertiesChanged, as at a player's start.
func TestIntegration_MEDIA01_APlayerExportingItsObjectLateIsFound(t *testing.T) {
	requireBus(t)
	for _, tc := range []struct{ when, suffix string }{
		{"at connect", "apptroltest_late_connect"},
		{"while running", "apptroltest_late_running"},
	} {
		t.Run(tc.when, func(t *testing.T) {
			longRetries(t)
			name := mprisPrefix + tc.suffix
			conn := connect(t)
			if tc.when == "at connect" {
				requestName(t, conn, name, 0)
			}
			h := runBus(t, "")
			if _, ok := find(h.snapshot().Players, name); ok {
				t.Fatalf("snapshot contains %s before it is ready", name)
			}
			if tc.when == "while running" {
				requestName(t, conn, name, 0)
			}
			h.waitForLog("media player not ready; retrying", name)

			p := exportPlayer(t, conn, name, "Late", "", "Paused")
			want := mixer.Player{BusName: name, Identity: "Late", Status: "Paused"}
			if ev := h.next(); ev != (mixer.PlayerAdded{Player: want}) {
				t.Fatalf("late player: %v, want PlayerAdded %+v; log:\n%s", ev, want, h.log.String())
			}
			p.setStatus("Playing")
			if ev := h.next(); ev != (mixer.PlayerStatusChanged{BusName: name, Status: "Playing"}) {
				t.Fatalf("after play: %v", ev)
			}
			if h.count("does not answer", name) > 0 {
				t.Errorf("a late player is logged as not answering:\n%s", h.log.String())
			}
		})
	}
}

// A status the player sends while it waits is not lost: the player ends with
// its last status, whether the read or the change comes first (MEDIA-01,
// ADR 0018, #254).
func TestIntegration_MEDIA01_AChangeWhileWaitingIsNotLost(t *testing.T) {
	requireBus(t)
	longRetries(t)
	h := runBus(t, "")
	h.snapshot()
	name := mprisPrefix + "apptroltest_late_change"
	conn := connect(t)
	requestName(t, conn, name, 0)
	h.waitForLog("media player not ready; retrying", name)
	p := exportPlayer(t, conn, name, "Late", "", "Paused")
	p.setStatus("Playing")

	added, ok := h.next().(mixer.PlayerAdded)
	if !ok || added.Player.BusName != name {
		t.Fatalf("want PlayerAdded for %s first; log:\n%s", name, h.log.String())
	}
	if added.Player.Status == "Paused" {
		if ev := h.next(); ev != (mixer.PlayerStatusChanged{BusName: name, Status: "Playing"}) {
			t.Fatalf("after play: %v", ev)
		}
	} else if added.Player.Status != "Playing" {
		t.Fatalf("added with status %q", added.Player.Status)
	}
}

// notReadyYet stands in for a player whose object is not there: every
// Properties.Get answers UnknownObject, as GDBus and Qt do. Only calls from
// Apptrol's connection (from) are counted: other programs on a shared bus,
// as in CI, may read players too. onRetry, if set, runs during Apptrol's
// second read, the first retry, so a test can hold that read open (MEDIA-01,
// ADR 0018, #254).
type notReadyYet struct {
	from    string
	calls   *atomic.Int32
	onRetry func()
}

func (n notReadyYet) Get(sender dbus.Sender, _, _ string) (dbus.Variant, *dbus.Error) {
	if string(sender) == n.from && n.calls.Add(1) == 2 && n.onRetry != nil {
		n.onRetry()
	}
	return dbus.Variant{}, &dbus.Error{Name: "org.freedesktop.DBus.Error.UnknownObject", Body: []any{"not exported yet"}}
}

// unique returns the unique name of the Bus's connection, once it is
// connected (MEDIA-01, ADR 0018, #254).
func (h *harness) unique() string {
	h.t.Helper()
	h.bus.mu.Lock()
	defer h.bus.mu.Unlock()
	if h.bus.conn == nil {
		h.t.Fatal("the Bus is not connected")
	}
	return h.bus.conn.Names()[0]
}

// A player that never exports its object is read len(notReadyRetry) times
// more, then given up with one warning (MEDIA-01, ADR 0018, #254).
func TestIntegration_MEDIA01_APlayerThatNeverExportsIsGivenUp(t *testing.T) {
	requireBus(t)
	h := runBus(t, "")
	h.snapshot()
	name := mprisPrefix + "apptroltest_never"
	conn := connect(t)
	calls := &atomic.Int32{}
	if err := conn.Export(notReadyYet{from: h.unique(), calls: calls}, mprisPath, ifaceProps); err != nil {
		t.Fatal(err)
	}
	requestName(t, conn, name, 0)

	h.waitForLog("media player does not answer; ignoring it", name, "media_player_unreadable")
	if n := h.count("media player not ready; retrying", name); n != len(notReadyRetry) {
		t.Errorf("%d retries logged, want %d:\n%s", n, len(notReadyRetry), h.log.String())
	}
	// The first read and one per retry were made, not only announced.
	if n, want := int(calls.Load()), 1+len(notReadyRetry); n != want {
		t.Errorf("the player was read %d times, want %d", n, want)
	}
	if n := h.count("does not answer", name); n != 1 {
		t.Errorf("warned %d times, want once:\n%s", n, h.log.String())
	}
	select {
	case ev := <-h.events:
		t.Errorf("event for a player that never answered: %v", ev)
	default:
	}
}

// A player that goes before it is ready ends its wait quietly: no warning,
// no event (MEDIA-01, ADR 0018, #254).
func TestIntegration_MEDIA01_APlayerGoneBeforeItIsReadyIsForgotten(t *testing.T) {
	requireBus(t)
	longRetries(t)
	h := runBus(t, "")
	h.snapshot()
	name := mprisPrefix + "apptroltest_gone_early"
	conn := connect(t)
	requestName(t, conn, name, 0)
	h.waitForLog("media player not ready; retrying", name)
	// Released, not disconnected: a read in flight then still gets an answer
	// (not ready, or no owner), where a closed connection would give NoReply,
	// which is warned about (a known limit, ADR 0018).
	if _, err := conn.ReleaseName(name); err != nil {
		t.Fatal(err)
	}
	h.waitForLog("media player gone before it was ready", name)

	select {
	case ev := <-h.events:
		t.Errorf("event for a player that was never ready: %v", ev)
	case <-time.After(500 * time.Millisecond): // five retries' time
	}
	if h.count("does not answer", name) > 0 {
		t.Errorf("a player gone before it was ready is warned about:\n%s", h.log.String())
	}
}

// A name handed to a ready program while the first one is still not ready
// ends the first wait and adds the new owner's player (MEDIA-01, ADR 0018,
// #254). The handover happens while the first owner holds its first retry
// open, so the retry cannot reach the new owner before the handover is
// handled (that order is #277).
func TestIntegration_MEDIA01_AHandoverWhileWaitingAddsTheNewOwner(t *testing.T) {
	requireBus(t)
	// After the held first retry, the next is 1 s away, so the queued
	// handover is handled before it even when the loop is slow to resume,
	// and the test watches long enough to see that it never comes.
	setRetries(t, 50*time.Millisecond, time.Second)
	h := runBus(t, "")
	h.snapshot()
	name := mprisPrefix + "apptroltest_handover"
	// held has room for the handler's send, so it never blocks there, also
	// when the test has stopped waiting.
	held, release := make(chan struct{}, 1), make(chan struct{})
	var released sync.Once
	letGo := func() { released.Do(func() { close(release) }) }
	t.Cleanup(letGo) // a failed test does not leave the handler blocked
	first := connect(t)
	// The held retry times out after callTimeout (2 s); the handover below
	// takes milliseconds.
	hold := func() { held <- struct{}{}; <-release }
	if err := first.Export(notReadyYet{from: h.unique(), calls: &atomic.Int32{}, onRetry: hold}, mprisPath, ifaceProps); err != nil {
		t.Fatal(err)
	}
	requestName(t, first, name, dbus.NameFlagAllowReplacement)
	select {
	case <-held:
	case <-time.After(5 * time.Second):
		t.Fatalf("no retry within 5 s; log:\n%s", h.log.String())
	}

	second := connect(t)
	exportPlayer(t, second, name, "Ready", "", "Playing")
	requestName(t, second, name, dbus.NameFlagReplaceExisting)
	letGo()
	want := mixer.Player{BusName: name, Identity: "Ready", Status: "Playing"}
	if ev := h.next(); ev != (mixer.PlayerAdded{Player: want}) {
		t.Fatalf("after the handover: %v, want PlayerAdded %+v; log:\n%s", ev, want, h.log.String())
	}
	h.waitForLog("media player gone before it was ready", name)
	select {
	case ev := <-h.events:
		t.Errorf("a second event after the handover: %v", ev)
	case <-time.After(1300 * time.Millisecond): // past the next retry's time
	}
	if h.count("does not answer", name) > 0 {
		t.Errorf("the handed-over name is warned about:\n%s", h.log.String())
	}
}
