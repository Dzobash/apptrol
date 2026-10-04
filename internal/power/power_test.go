package power

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/Dzobash/apptrol/internal/mixer"
)

// Integration tests run against a private bus that stands in for the system
// bus (QA-09): only with APPTROL_DBUS_TEST=1, inside the private session bus
// that `make test-desktop` and CI start. A fake logind on that bus sends the
// sleep signal.

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
	w.retryMin, w.retryMax, w.resend = 20*time.Millisecond, 50*time.Millisecond, resend
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

// count counts the events that arrive within d.
func (h *harness) count(d time.Duration) int {
	n := 0
	timeout := time.After(d)
	for {
		select {
		case ev := <-h.events:
			if _, ok := ev.(mixer.SystemResumed); !ok {
				h.t.Errorf("event %T, want only SystemResumed", ev)
			}
			n++
		case <-timeout:
			return n
		}
	}
}

// fakeLogind owns logind's name on the private bus and sends its signal.
type fakeLogind struct {
	t    *testing.T
	conn *dbus.Conn
}

func newLogind(t *testing.T, address string, ownName bool) *fakeLogind {
	t.Helper()
	conn, err := dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if ownName {
		if reply, err := conn.RequestName(login1Name, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
			t.Fatalf("RequestName(%s) = %v, %v", login1Name, reply, err)
		}
	}
	return &fakeLogind{t: t, conn: conn}
}

func (l *fakeLogind) prepareForSleep(sleeping bool) {
	l.t.Helper()
	if err := l.conn.Emit(login1Path, login1Iface+"."+sleepSignal, sleeping); err != nil {
		l.t.Fatal(err)
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

func TestLED09_NoBusWarnsOnceThenRetries(t *testing.T) {
	h := run(t, "unix:path="+filepath.Join(t.TempDir(), "no-bus"))
	h.waitLog("system bus still unreachable", 2)
	log := h.log.String()
	if n := strings.Count(log, "cannot connect to the system bus"); n != 1 {
		t.Errorf("warning logged %d times, want once (retries at debug):\n%s", n, log)
	}
	for _, want := range []string{"level=WARN", "error.type=power_bus_unreachable", "apptrol.component=power"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
}

// ---- against a private bus ---------------------------------------------------

func TestIntegration_LED09_WakeUpIsReportedAndRepeated(t *testing.T) {
	addr := requireBus(t)
	logind := newLogind(t, addr, true)
	h := run(t, addr, 20*time.Millisecond, 40*time.Millisecond, 60*time.Millisecond)
	h.waitLog("connected to the system bus", 1)

	logind.prepareForSleep(true)
	h.waitLog("system going to sleep", 1)
	if n := h.count(100 * time.Millisecond); n != 0 {
		t.Errorf("%d events before waking up, want none", n)
	}
	logind.prepareForSleep(false)
	if n := h.count(300 * time.Millisecond); n != 4 {
		t.Errorf("%d SystemResumed after waking up, want 4 (at once and 3 repeats)", n)
	}
	log := h.log.String()
	for _, want := range []string{"system resumed; sending LEDs again", "apptrol.component=power", "apptrol.power.bus_address="} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
}

func TestIntegration_LED09_SleepingAgainStopsTheRepeats(t *testing.T) {
	addr := requireBus(t)
	logind := newLogind(t, addr, true)
	h := run(t, addr, 150*time.Millisecond, 300*time.Millisecond)
	h.waitLog("connected to the system bus", 1)

	logind.prepareForSleep(false)
	logind.prepareForSleep(true)
	if n := h.count(500 * time.Millisecond); n != 1 {
		t.Errorf("%d SystemResumed, want 1: the repeats end when the computer sleeps again", n)
	}
}

func TestIntegration_LED09_OnlyLogindIsHeard(t *testing.T) {
	addr := requireBus(t)
	other := newLogind(t, addr, false) // a program that does not own logind's name
	logind := newLogind(t, addr, true)
	h := run(t, addr)
	h.waitLog("connected to the system bus", 1)

	other.prepareForSleep(false)
	if n := h.count(100 * time.Millisecond); n != 0 {
		t.Errorf("%d events from a program other than logind, want none", n)
	}
	logind.prepareForSleep(false)
	if n := h.count(200 * time.Millisecond); n != 1 {
		t.Errorf("%d events from logind, want 1", n)
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

func TestIntegration_LED09_ReconnectsAfterTheBusRestarts(t *testing.T) {
	requireBus(t)
	addr := "unix:path=" + filepath.Join(t.TempDir(), "bus")
	daemon := startDaemon(t, addr)
	h := run(t, addr)
	h.waitLog("connected to the system bus", 1)

	_ = daemon.Process.Kill()
	_ = daemon.Wait()
	startDaemon(t, addr)
	h.waitLog("connected to the system bus", 2)
	for _, want := range []string{"lost the connection to the system bus", "error.type=power_bus_lost"} {
		if !strings.Contains(h.log.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, h.log.String())
		}
	}
	// Still listening after the reconnect.
	newLogind(t, addr, true).prepareForSleep(false)
	if n := h.count(200 * time.Millisecond); n != 1 {
		t.Errorf("%d events after the reconnect, want 1", n)
	}
}
