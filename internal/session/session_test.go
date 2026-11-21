package session

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

	"github.com/Dzobash/apptrol/internal/logattr"
	"github.com/Dzobash/apptrol/internal/mixer"
)

// Integration tests run against a private bus that stands in for the system
// bus (QA-09): only with APPTROL_DBUS_TEST=1, inside the private session bus
// that `make test-desktop` and CI start. A fake logind on that bus sends the
// sleep signal and offers a user and a graphical session.

func requireBus(t *testing.T) string {
	t.Helper()
	if os.Getenv("APPTROL_DBUS_TEST") != "1" {
		t.Skip("set APPTROL_DBUS_TEST=1 (make test-desktop) to run against a private bus")
	}
	addr := os.Getenv("DBUS_SESSION_BUS_ADDRESS")
	if addr == "" {
		t.Skip("no private bus: DBUS_SESSION_BUS_ADDRESS is not set")
	}
	return addr
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

// harness runs a Watcher and collects its events and log.
type harness struct {
	t      *testing.T
	events chan mixer.Event
	log    *syncBuffer
}

func run(t *testing.T, address string, resend ...time.Duration) *harness {
	t.Helper()
	h := &harness{t: t, events: make(chan mixer.Event, 64), log: &syncBuffer{}}
	w := New(slog.New(slog.NewTextHandler(h.log, &slog.HandlerOptions{Level: slog.LevelDebug})), address)
	w.retryMin, w.retryMax, w.unreadableRetry, w.resend = 20*time.Millisecond, 50*time.Millisecond, 20*time.Millisecond, resend
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx, h.events); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return h
}

// waitLog waits until the log has msg n times.
func (h *harness) waitLog(msg string, n int) {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for strings.Count(h.log.String(), msg) < n {
		if time.Now().After(deadline) {
			h.t.Fatalf("log lacks %d× %q within 5 s:\n%s", n, msg, h.log.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// count counts the SystemResumed events that arrive within d; screen changes
// are skipped.
func (h *harness) count(d time.Duration) int {
	n := 0
	timeout := time.After(d)
	for {
		select {
		case ev := <-h.events:
			if _, ok := ev.(mixer.SystemResumed); ok {
				n++
			}
		case <-timeout:
			return n
		}
	}
}

// waitScreen waits for a screen change to state; wake-ups are skipped.
func (h *harness) waitScreen(state mixer.ScreenState) {
	h.t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev := <-h.events:
			if ev == (mixer.ScreenChanged{State: state}) {
				return
			}
			if sc, ok := ev.(mixer.ScreenChanged); ok {
				h.t.Logf("screen %q on the way to %q", sc.State, state)
			}
		case <-timeout:
			h.t.Fatalf("no screen change to %q within 5 s; log:\n%s", state, h.log.String())
		}
	}
}

// waitSeat waits for a seat change to front; other events are skipped.
func (h *harness) waitSeat(want mixer.SeatChanged) {
	h.t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev := <-h.events:
			if ev == want {
				return
			}
			if sc, ok := ev.(mixer.SeatChanged); ok {
				h.t.Logf("seat %+v on the way to %+v", sc, want)
			}
		case <-timeout:
			h.t.Fatalf("no seat change to %+v within 5 s; log:\n%s", want, h.log.String())
		}
	}
}

// Made-up ids; logind's real ones look alike.
const (
	fakeUser    = dbus.ObjectPath("/org/freedesktop/login1/user/_1000")
	fakeSession = dbus.ObjectPath("/org/freedesktop/login1/session/c1")
	fakeSeat    = dbus.ObjectPath("/org/freedesktop/login1/seat/seat0")
	bobSession  = dbus.ObjectPath("/org/freedesktop/login1/session/c2") // another user's
	greeter     = dbus.ObjectPath("/org/freedesktop/login1/session/c3") // the login screen
)

// fakeLogind owns logind's name on the private bus, sends its sleep signal
// and offers a user with one graphical session, a seat, and two sessions of
// others that can come to the front: another user's and the login screen.
type fakeLogind struct {
	t       *testing.T
	conn    *dbus.Conn
	user    *prop.Properties
	session *prop.Properties
	seat    *prop.Properties
}

// display is a (so) pair as logind uses it for Display and ActiveSession.
type display struct {
	ID   string
	Path dbus.ObjectPath
}

// userRef is a (uo) pair as logind uses it for a session's User.
type userRef struct {
	UID  uint32
	Path dbus.ObjectPath
}

// manager is logind's Manager: GetUser finds the user, unless noUser;
// GetSeat finds seat0, unless noSeat.
type manager struct{ noUser, noSeat bool }

func (m manager) GetUser(uint32) (dbus.ObjectPath, *dbus.Error) {
	if m.noUser {
		return "", dbus.NewError(noSuchUser, []any{"no such user"})
	}
	return fakeUser, nil
}

func (m manager) GetSeat(string) (dbus.ObjectPath, *dbus.Error) {
	if m.noSeat {
		return "", dbus.NewError(noSuchSeat, []any{"no such seat"})
	}
	return fakeSeat, nil
}

func newLogind(t *testing.T, address string, ownName bool) *fakeLogind {
	t.Helper()
	conn, err := dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	l := &fakeLogind{t: t, conn: conn}
	if !ownName {
		return l
	}
	if err := conn.Export(manager{}, login1Path, login1Iface); err != nil {
		t.Fatal(err)
	}
	if l.user, err = prop.Export(conn, fakeUser, prop.Map{userIface: {
		"Display": {Value: display{"c1", fakeSession}, Emit: prop.EmitTrue},
	}}); err != nil {
		t.Fatal(err)
	}
	me := uint32(os.Getuid())
	if l.session, err = prop.Export(conn, fakeSession, prop.Map{sessionIface: {
		"LockedHint": {Value: false, Emit: prop.EmitTrue},
		"Active":     {Value: true, Emit: prop.EmitTrue},
		"User":       {Value: userRef{me, fakeUser}, Emit: prop.EmitConst},
		"Class":      {Value: "user", Emit: prop.EmitConst},
	}}); err != nil {
		t.Fatal(err)
	}
	for path, s := range map[dbus.ObjectPath]struct {
		uid   uint32
		class string
	}{bobSession: {me + 1, "user"}, greeter: {me + 2, greeterClass}} {
		if _, err := prop.Export(conn, path, prop.Map{sessionIface: {
			"User":  {Value: userRef{s.uid, "/org/freedesktop/login1/user/_other"}, Emit: prop.EmitConst},
			"Class": {Value: s.class, Emit: prop.EmitConst},
		}}); err != nil {
			t.Fatal(err)
		}
	}
	if l.seat, err = prop.Export(conn, fakeSeat, prop.Map{seatIface: {
		"ActiveSession": {Value: display{"c1", fakeSession}, Emit: prop.EmitTrue},
	}}); err != nil {
		t.Fatal(err)
	}
	if reply, err := conn.RequestName(login1Name, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("RequestName(%s) = %v, %v", login1Name, reply, err)
	}
	return l
}

func (l *fakeLogind) prepareForSleep(sleeping bool) {
	l.t.Helper()
	if err := l.conn.Emit(login1Path, login1Iface+"."+sleepSignal, sleeping); err != nil {
		l.t.Fatal(err)
	}
}

func (l *fakeLogind) lock(locked bool)    { l.session.SetMust(sessionIface, "LockedHint", locked) }
func (l *fakeLogind) active(active bool)  { l.session.SetMust(sessionIface, "Active", active) }
func (l *fakeLogind) display(d display)   { l.user.SetMust(userIface, "Display", d) }
func (l *fakeLogind) noUser(t *testing.T) { exportManager(t, l.conn, manager{noUser: true}) }
func (l *fakeLogind) noSeat(t *testing.T) { exportManager(t, l.conn, manager{noSeat: true}) }

// front puts a session in front at the seat; {"", "/"} is none.
func (l *fakeLogind) front(d display) { l.seat.SetMust(seatIface, "ActiveSession", d) }

func exportManager(t *testing.T, conn *dbus.Conn, m manager) {
	t.Helper()
	if err := conn.Export(m, login1Path, login1Iface); err != nil {
		t.Fatal(err)
	}
}

// ---- without a bus -----------------------------------------------------------

func TestLED09_SleepState(t *testing.T) {
	name := login1Iface + "." + sleepSignal
	for _, tc := range []struct {
		desc         string
		sig          dbus.Signal
		sleeping, ok bool
	}{
		{"before sleep", dbus.Signal{Name: name, Path: login1Path, Body: []any{true}}, true, true},
		{"after waking up", dbus.Signal{Name: name, Path: login1Path, Body: []any{false}}, false, true},
		{"other signal", dbus.Signal{Name: login1Iface + ".PrepareForShutdown", Path: login1Path, Body: []any{false}}, false, false},
		{"other path", dbus.Signal{Name: name, Path: "/elsewhere", Body: []any{false}}, false, false},
		{"no argument", dbus.Signal{Name: name, Path: login1Path}, false, false},
		{"wrong type", dbus.Signal{Name: name, Path: login1Path, Body: []any{"no"}}, false, false},
	} {
		sleeping, ok := sleepState(&tc.sig)
		if sleeping != tc.sleeping || ok != tc.ok {
			t.Errorf("%s: sleepState = %v, %v; want %v, %v", tc.desc, sleeping, ok, tc.sleeping, tc.ok)
		}
	}
}

func TestLED09_SystemBusAddress(t *testing.T) {
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", "")
	if got := SystemBusAddress(); got != systemSocket {
		t.Errorf("default = %q, want %q", got, systemSocket)
	}
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", "unix:path=/elsewhere")
	if got := SystemBusAddress(); got != "unix:path=/elsewhere" {
		t.Errorf("from the environment = %q", got)
	}
}

func TestLED09_LAUNCH13_NoBusWarnsOnceAndBlocksLaunchers(t *testing.T) {
	h := run(t, "unix:path="+filepath.Join(t.TempDir(), "no-bus"))
	h.waitScreen(mixer.ScreenUnknown)
	h.waitLog("system bus still unreachable", 2)
	log := h.log.String()
	for msg, n := range map[string]int{
		"cannot connect to the system bus; launchers are blocked":   1, // retries at debug
		"launchers blocked: the screen is not known to be unlocked": 1, // only on a change
	} {
		if got := strings.Count(log, msg); got != n {
			t.Errorf("%q logged %d times, want %d:\n%s", msg, got, n, log)
		}
	}
	for _, want := range []string{"level=WARN", "error.type=system_bus_unreachable", "apptrol.component=session",
		"apptrol.screen.state=unknown apptrol.screen.reason=no_system_bus"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
}

func TestLAUNCH13_ErrorName(t *testing.T) {
	for _, err := range []error{dbus.Error{Name: noSuchUser}, &dbus.Error{Name: noSuchUser}} {
		if got := errorName(err); got != noSuchUser {
			t.Errorf("errorName(%T) = %q", err, got)
		}
	}
	if got := errorName(os.ErrNotExist); got != "" {
		t.Errorf("errorName(plain error) = %q", got)
	}
}

func TestLAUNCH13_UnreadableScreenWarnsOnceAndRecovers(t *testing.T) {
	const warnMsg = "cannot read the screen lock state; launchers are blocked"
	var buf syncBuffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})).
		With(logattr.Component(logattr.Session))
	events := make(chan mixer.Event, 16)
	scr := &screen{log: log, out: events}
	ctx := context.Background()

	expectEvent := func(want mixer.ScreenState) {
		t.Helper()
		select {
		case ev := <-events:
			if ev != (mixer.ScreenChanged{State: want}) {
				t.Fatalf("got event %#v, want ScreenChanged{%q}", ev, want)
			}
		default:
			t.Fatalf("expected ScreenChanged{%q}, got no event", want)
		}
	}
	expectNoEvent := func() {
		t.Helper()
		select {
		case ev := <-events:
			t.Fatalf("unexpected event %#v", ev)
		default:
		}
	}

	// Outage 1: multiple failed reads warn once and keep launchers blocked
	// (LAUNCH-13, ADR 0024).
	for _, msg := range []string{"read failed 1", "read failed 2", "read failed 3"} {
		if !scr.set(ctx, mixer.ScreenUnknown, reasonUnreadable, "", errors.New(msg)) {
			t.Fatal("scr.set returned false")
		}
	}
	expectEvent(mixer.ScreenUnknown)
	expectNoEvent()
	if !scr.unreadable {
		t.Fatal("scr.unreadable = false, want true")
	}
	out := buf.String()
	if got := strings.Count(out, warnMsg); got != 1 {
		t.Fatalf("warning logged %d times during outage 1, want 1:\n%s", got, out)
	}
	for _, want := range []string{
		"level=WARN",
		"error.type=screen_state_unreadable",
		"apptrol.component=session",
		`msg="launchers blocked: the screen is not known to be unlocked" apptrol.component=session apptrol.screen.state=unknown apptrol.screen.reason=unreadable`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q:\n%s", want, out)
		}
	}

	// Recovery to unlocked clears unreadable and logs recovery.
	if !scr.set(ctx, mixer.ScreenUnlocked, "", "c1", nil) {
		t.Fatal("scr.set returned false on recovery")
	}
	expectEvent(mixer.ScreenUnlocked)
	if scr.unreadable {
		t.Fatal("scr.unreadable = true after recovery, want false")
	}
	if !strings.Contains(buf.String(), `msg="launchers allowed: the screen is unlocked" apptrol.component=session apptrol.session.id=c1`) {
		t.Errorf("recovery to unlocked not logged:\n%s", buf.String())
	}

	// Outage 2 after recovery warns once again and deduplicates retries.
	for _, msg := range []string{"read failed 4", "read failed 5"} {
		if !scr.set(ctx, mixer.ScreenUnknown, reasonUnreadable, "", errors.New(msg)) {
			t.Fatal("scr.set returned false")
		}
	}
	expectEvent(mixer.ScreenUnknown)
	expectNoEvent()
	if got := strings.Count(buf.String(), warnMsg); got != 2 {
		t.Fatalf("warning logged %d times after outage 2, want 2:\n%s", got, buf.String())
	}

	// Recovery to locked also clears unreadable and logs the new state.
	if !scr.set(ctx, mixer.ScreenLocked, "", "c1", nil) {
		t.Fatal("scr.set returned false on recovery to locked")
	}
	expectEvent(mixer.ScreenLocked)
	if scr.unreadable {
		t.Fatal("scr.unreadable = true after recovery to locked, want false")
	}
	if !strings.Contains(buf.String(), `msg="launchers blocked: the screen is not known to be unlocked" apptrol.component=session apptrol.screen.state=locked`) {
		t.Errorf("recovery to locked not logged:\n%s", buf.String())
	}

	// Outage 3 warns again; bus loss clears unreadable without a screen warning.
	if !scr.set(ctx, mixer.ScreenUnknown, reasonUnreadable, "", errors.New("read failed 6")) {
		t.Fatal("scr.set returned false")
	}
	expectEvent(mixer.ScreenUnknown)
	if got := strings.Count(buf.String(), warnMsg); got != 3 {
		t.Fatalf("warning logged %d times after outage 3, want 3:\n%s", got, buf.String())
	}
	if !scr.set(ctx, mixer.ScreenUnknown, reasonBusLost, "", nil) {
		t.Fatal("scr.set returned false on bus loss")
	}
	expectEvent(mixer.ScreenUnknown)
	if scr.unreadable {
		t.Fatal("scr.unreadable = true after bus loss, want false")
	}
	if got := strings.Count(buf.String(), warnMsg); got != 3 {
		t.Fatalf("bus loss logged extra screen warning (%d total, want 3):\n%s", got, buf.String())
	}
	if !strings.Contains(buf.String(), "apptrol.screen.reason=system_bus_lost") {
		t.Errorf("bus loss reason not logged:\n%s", buf.String())
	}

	// Outage 4 after bus reconnect warns once again.
	if !scr.set(ctx, mixer.ScreenUnknown, reasonUnreadable, "", errors.New("read failed 7")) {
		t.Fatal("scr.set returned false")
	}
	expectEvent(mixer.ScreenUnknown)
	if got := strings.Count(buf.String(), warnMsg); got != 4 {
		t.Fatalf("warning logged %d times after outage 4, want 4:\n%s", got, buf.String())
	}
}

func TestLAUNCH13_ResetClearsCachedSession(t *testing.T) {
	// On logind restart (NameOwnerChanged), reset must clear cached session
	// path and ID alongside user and seat (LAUNCH-13, ADR 0024).
	g := &graphical{
		user:    fakeUser,
		seat:    fakeSeat,
		session: fakeSession,
		id:      "c1",
	}
	propSig := func(p dbus.ObjectPath) *dbus.Signal {
		return &dbus.Signal{Name: propsIface + ".PropertiesChanged", Path: p}
	}
	for _, p := range []dbus.ObjectPath{fakeUser, fakeSeat, fakeSession} {
		if !g.concerns(propSig(p)) {
			t.Fatalf("concerns(%q) = false before reset, want true", p)
		}
	}

	g.reset()

	if g.user != "" || g.seat != "" || g.session != "" || g.id != "" {
		t.Fatalf("reset left cached values: user=%q seat=%q session=%q id=%q",
			g.user, g.seat, g.session, g.id)
	}
	for _, p := range []dbus.ObjectPath{fakeUser, fakeSeat, fakeSession} {
		if g.concerns(propSig(p)) {
			t.Errorf("concerns(%q) = true after reset, want false", p)
		}
	}
}

// ---- wake-ups, against a private bus -------------------------------------------

func TestIntegration_LED09_WakeUpIsReportedAndRepeated(t *testing.T) {
	addr := requireBus(t)
	logind := newLogind(t, addr, true)
	h := run(t, addr, 20*time.Millisecond, 40*time.Millisecond, 60*time.Millisecond)
	h.waitScreen(mixer.ScreenUnlocked)

	logind.prepareForSleep(true)
	h.waitLog("system going to sleep", 1)
	if n := h.count(100 * time.Millisecond); n != 0 {
		t.Errorf("%d wake-ups before waking up, want none", n)
	}
	logind.prepareForSleep(false)
	if n := h.count(300 * time.Millisecond); n != 4 {
		t.Errorf("%d SystemResumed after waking up, want 4 (at once and 3 repeats)", n)
	}
	log := h.log.String()
	for _, want := range []string{"system resumed; sending LEDs again", "apptrol.component=session", "apptrol.session.bus_address=",
		`msg="LEDs sent again after waking up" apptrol.component=session apptrol.session.resend=1`,
		`msg="LEDs sent again after waking up" apptrol.component=session apptrol.session.resend=2`,
		`msg="LEDs sent again after waking up" apptrol.component=session apptrol.session.resend=3`} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
}

func TestIntegration_LED09_SleepingAgainStopsTheRepeats(t *testing.T) {
	addr := requireBus(t)
	logind := newLogind(t, addr, true)
	h := run(t, addr, 150*time.Millisecond, 300*time.Millisecond)
	h.waitScreen(mixer.ScreenUnlocked)

	logind.prepareForSleep(false)
	logind.prepareForSleep(true)
	if n := h.count(500 * time.Millisecond); n != 1 {
		t.Errorf("%d SystemResumed, want 1: the repeats end when the computer sleeps again", n)
	}
	if strings.Contains(h.log.String(), "LEDs sent again after waking up") {
		t.Errorf("a repeat was logged although none ran:\n%s", h.log.String())
	}
}

func TestIntegration_LED09_OnlyLogindIsHeard(t *testing.T) {
	addr := requireBus(t)
	other := newLogind(t, addr, false) // a program that does not own logind's name
	logind := newLogind(t, addr, true)
	h := run(t, addr)
	h.waitScreen(mixer.ScreenUnlocked)

	other.prepareForSleep(false)
	if n := h.count(100 * time.Millisecond); n != 0 {
		t.Errorf("%d wake-ups from a program other than logind, want none", n)
	}
	logind.prepareForSleep(false)
	if n := h.count(200 * time.Millisecond); n != 1 {
		t.Errorf("%d wake-ups from logind, want 1", n)
	}
}

// startDaemon runs a bus of the test's own at address, so the test can stop it.
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

func TestIntegration_LED09_LAUNCH13_ReconnectsAfterTheBusRestarts(t *testing.T) {
	requireBus(t)
	addr := "unix:path=" + filepath.Join(t.TempDir(), "bus")
	daemon := startDaemon(t, addr)
	newLogind(t, addr, true)
	h := run(t, addr)
	h.waitScreen(mixer.ScreenUnlocked)

	_ = daemon.Process.Kill()
	_ = daemon.Wait()
	h.waitScreen(mixer.ScreenUnknown) // launchers blocked while the bus is gone
	startDaemon(t, addr)
	logind := newLogind(t, addr, true)
	h.waitScreen(mixer.ScreenUnlocked)
	for _, want := range []string{"lost the connection to the system bus", "error.type=system_bus_lost",
		"apptrol.screen.reason=system_bus_lost"} {
		if !strings.Contains(h.log.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, h.log.String())
		}
	}
	// Still listening after the reconnect.
	logind.prepareForSleep(false)
	if n := h.count(200 * time.Millisecond); n != 1 {
		t.Errorf("%d wake-ups after the reconnect, want 1", n)
	}
}

// ---- the screen state, against a private bus -----------------------------------

func TestIntegration_LAUNCH13_LockAndUnlockAreFollowed(t *testing.T) {
	addr := requireBus(t)
	logind := newLogind(t, addr, true)
	h := run(t, addr)
	h.waitScreen(mixer.ScreenUnlocked)
	logind.lock(true)
	h.waitScreen(mixer.ScreenLocked)
	logind.lock(false)
	h.waitScreen(mixer.ScreenUnlocked)

	log := h.log.String()
	for msg, n := range map[string]int{
		`msg="launchers allowed: the screen is unlocked" apptrol.component=session apptrol.session.id=c1`:                       2,
		`msg="launchers blocked: the screen is not known to be unlocked" apptrol.component=session apptrol.screen.state=locked`: 1,
		`msg="graphical session found" apptrol.component=session apptrol.session.id=c1`:                                         1,
	} {
		if got := strings.Count(log, msg); got != n {
			t.Errorf("%q logged %d times, want %d:\n%s", msg, got, n, log)
		}
	}
}

func TestIntegration_LAUNCH13_AnotherSessionInFrontBlocksLaunchers(t *testing.T) {
	addr := requireBus(t)
	logind := newLogind(t, addr, true)
	h := run(t, addr)
	h.waitScreen(mixer.ScreenUnlocked)
	logind.active(false) // e.g. Switch user
	h.waitScreen(mixer.ScreenInactive)
	logind.active(true)
	h.waitScreen(mixer.ScreenUnlocked)
}

func TestIntegration_LAUNCH13_NoGraphicalSessionBlocksLaunchers(t *testing.T) {
	addr := requireBus(t)
	logind := newLogind(t, addr, true)
	logind.display(display{"", "/"}) // e.g. only logged in over SSH
	h := run(t, addr)
	h.waitScreen(mixer.ScreenUnknown)
	if !strings.Contains(h.log.String(), "apptrol.screen.reason=no_graphical_session") {
		t.Errorf("log lacks the reason:\n%s", h.log.String())
	}
	logind.display(display{"c1", fakeSession}) // a graphical login follows
	h.waitScreen(mixer.ScreenUnlocked)
}

func TestIntegration_LAUNCH13_UnknownUserBlocksLaunchers(t *testing.T) {
	addr := requireBus(t)
	logind := newLogind(t, addr, true)
	logind.noUser(t)
	h := run(t, addr)
	h.waitScreen(mixer.ScreenUnknown)
	if !strings.Contains(h.log.String(), "apptrol.screen.reason=no_graphical_session") {
		t.Errorf("log lacks the reason:\n%s", h.log.String())
	}
}

func TestIntegration_LAUNCH13_UnreadableStateBlocksLaunchers(t *testing.T) {
	addr := requireBus(t)
	logind := newLogind(t, addr, true)
	logind.display(display{"c2", "/org/freedesktop/login1/session/c2"}) // a session that does not answer
	h := run(t, addr)
	h.waitScreen(mixer.ScreenUnknown)
	for _, want := range []string{"cannot read the screen lock state; launchers are blocked",
		"error.type=screen_state_unreadable", "apptrol.screen.reason=unreadable"} {
		if !strings.Contains(h.log.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, h.log.String())
		}
	}
}

func TestIntegration_LAUNCH13_UnreadableScreenWarnsOnceAcrossRetriesAndRecovers(t *testing.T) {
	addr := requireBus(t)
	logind := newLogind(t, addr, true)
	logind.display(display{"c2", bobSession}) // lacks LockedHint/Active: unreadable
	h := run(t, addr)
	h.waitScreen(mixer.ScreenUnknown)

	// Let several unreadableRetry ticks and a seat change run while the state
	// stays unreadable; WARN must appear only once (LAUNCH-13, ADR 0024).
	time.Sleep(100 * time.Millisecond)
	logind.front(display{"c3", greeter}) // PropertiesChanged on seat also triggers reread()
	h.waitSeat(mixer.SeatChanged{Front: mixer.FrontLoginScreen, SessionID: "c3"})
	time.Sleep(60 * time.Millisecond)

	const warnMsg = "cannot read the screen lock state; launchers are blocked"
	if got := strings.Count(h.log.String(), warnMsg); got != 1 {
		t.Fatalf("warning logged %d times during first outage, want 1:\n%s", got, h.log.String())
	}

	// Recovery logs the new state and unlocks launchers.
	logind.display(display{"c1", fakeSession})
	h.waitScreen(mixer.ScreenUnlocked)
	if !strings.Contains(h.log.String(), `msg="launchers allowed: the screen is unlocked" apptrol.component=session apptrol.session.id=c1`) {
		t.Errorf("recovery not logged:\n%s", h.log.String())
	}

	// A second outage after recovery warns once again and deduplicates retries.
	logind.display(display{"c2", bobSession})
	h.waitScreen(mixer.ScreenUnknown)
	time.Sleep(100 * time.Millisecond)
	if got := strings.Count(h.log.String(), warnMsg); got != 2 {
		t.Errorf("warning logged %d times across two outages, want 2:\n%s", got, h.log.String())
	}
}

func TestIntegration_LAUNCH13_LogindRestartClearsCachedSession(t *testing.T) {
	requireBus(t)
	addr := "unix:path=" + filepath.Join(t.TempDir(), "bus")
	startDaemon(t, addr)
	logind := newLogind(t, addr, true)
	h := run(t, addr)
	h.waitScreen(mixer.ScreenUnlocked)

	// Stopping logind emits NameOwnerChanged; cached session path and ID are
	// cleared so a failed reread cannot reuse them, and retries warn once
	// (LAUNCH-13, ADR 0024).
	_ = logind.conn.Close()
	h.waitScreen(mixer.ScreenUnknown)
	time.Sleep(100 * time.Millisecond)

	const warnMsg = "cannot read the screen lock state; launchers are blocked"
	if got := strings.Count(h.log.String(), warnMsg); got != 1 {
		t.Fatalf("warning logged %d times while logind is down, want 1:\n%s", got, h.log.String())
	}

	// Restarting logind rediscovers the graphical session (logging "graphical
	// session found" again because g.id was cleared) and logs recovery.
	newLogind(t, addr, true)
	h.waitScreen(mixer.ScreenUnlocked)
	log := h.log.String()
	if got := strings.Count(log, `msg="graphical session found" apptrol.component=session apptrol.session.id=c1`); got < 2 {
		t.Errorf("graphical session found logged %d times across logind restart, want at least 2:\n%s", got, log)
	}
	if got := strings.Count(log, `msg="launchers allowed: the screen is unlocked" apptrol.component=session apptrol.session.id=c1`); got != 2 {
		t.Errorf("recovery logged %d times, want 2:\n%s", got, log)
	}
}

func TestIntegration_LAUNCH13_LogindStartingLaterIsNoticed(t *testing.T) {
	requireBus(t)
	addr := "unix:path=" + filepath.Join(t.TempDir(), "bus")
	startDaemon(t, addr) // a bus of its own: logind is not there yet
	h := run(t, addr)
	h.waitScreen(mixer.ScreenUnknown)
	if !strings.Contains(h.log.String(), "apptrol.screen.reason=unreadable") {
		t.Errorf("log lacks the reason:\n%s", h.log.String())
	}
	newLogind(t, addr, true) // logind (re)starts
	h.waitScreen(mixer.ScreenUnlocked)
}

// ---- the session in front at the seat (ADR 0029) --------------------------------

func TestSVC12_NoBusReportsTheSeatUnknown(t *testing.T) {
	h := run(t, "unix:path="+filepath.Join(t.TempDir(), "no-bus"))
	h.waitScreen(mixer.ScreenUnknown)
	h.waitSeat(mixer.SeatChanged{Front: mixer.FrontUnknown})
	// The bus warning already says it; no second warning for the seat.
	if strings.Contains(h.log.String(), "error.type=seat_unreadable") {
		t.Errorf("seat_unreadable logged without a bus:\n%s", h.log.String())
	}
}

func TestIntegration_SVC08_SVC10_SessionInFrontIsFollowed(t *testing.T) {
	addr := requireBus(t)
	logind := newLogind(t, addr, true)
	h := run(t, addr)
	h.waitScreen(mixer.ScreenUnlocked)
	h.waitSeat(mixer.SeatChanged{Front: mixer.FrontThisUser, SessionID: "c1"})

	for _, step := range []struct {
		front display
		want  mixer.SeatChanged
	}{
		{display{"c2", bobSession}, mixer.SeatChanged{Front: mixer.FrontOtherUser, SessionID: "c2"}}, // Switch user to Bob
		{display{"c3", greeter}, mixer.SeatChanged{Front: mixer.FrontLoginScreen, SessionID: "c3"}},  // the login screen
		{display{"", "/"}, mixer.SeatChanged{Front: mixer.FrontNobody}},                              // nobody
		{display{"c1", fakeSession}, mixer.SeatChanged{Front: mixer.FrontThisUser, SessionID: "c1"}}, // back
	} {
		logind.front(step.front)
		h.waitSeat(step.want)
	}
	log := h.log.String()
	for _, want := range []string{
		`msg="session in front changed" apptrol.component=session apptrol.seat.front=this_user apptrol.session.id=c1`,
		`msg="session in front changed" apptrol.component=session apptrol.seat.front=other_user apptrol.session.id=c2`,
		`msg="session in front changed" apptrol.component=session apptrol.seat.front=login_screen apptrol.session.id=c3`,
		`msg="session in front changed" apptrol.component=session apptrol.seat.front=nobody`,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
}

func TestIntegration_SVC08_NoSeatMeansNobodyInFront(t *testing.T) {
	addr := requireBus(t)
	logind := newLogind(t, addr, true)
	logind.noSeat(t) // e.g. a server without a seat
	h := run(t, addr)
	h.waitScreen(mixer.ScreenUnlocked)
	h.waitSeat(mixer.SeatChanged{Front: mixer.FrontNobody})
}

func TestIntegration_SVC12_UnreadableSeatWarnsOnceAndRecovers(t *testing.T) {
	addr := requireBus(t)
	logind := newLogind(t, addr, true)
	logind.front(display{"c9", "/org/freedesktop/login1/session/c9"}) // a session that does not answer
	h := run(t, addr)
	h.waitScreen(mixer.ScreenUnlocked)
	h.waitSeat(mixer.SeatChanged{Front: mixer.FrontUnknown, SessionID: "c9"})
	logind.front(display{"c1", fakeSession})
	h.waitSeat(mixer.SeatChanged{Front: mixer.FrontThisUser, SessionID: "c1"})

	log := h.log.String()
	if n := strings.Count(log, "cannot read who is in front; holding the controller as before"); n != 1 {
		t.Errorf("warning logged %d times, want 1:\n%s", n, log)
	}
	for _, want := range []string{"level=WARN", "error.type=seat_unreadable", "apptrol.component=session"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
}
