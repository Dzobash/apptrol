package rawmidi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Dzobash/apptrol/internal/mixer"
)

// fakeSys builds a fake /proc/asound and /dev/snd.
func fakeSys(t *testing.T, cards map[int]string, devices ...string) (procDir, devDir string) {
	t.Helper()
	root := t.TempDir()
	procDir, devDir = filepath.Join(root, "proc"), filepath.Join(root, "dev")
	for n, id := range cards {
		dir := filepath.Join(procDir, "card"+itoa(n))
		must(t, os.MkdirAll(dir, 0o755))
		must(t, os.WriteFile(filepath.Join(dir, "id"), []byte(id+"\n"), 0o644))
	}
	must(t, os.MkdirAll(devDir, 0o755))
	for _, d := range devices {
		must(t, os.WriteFile(filepath.Join(devDir, d), nil, 0o644))
	}
	return procDir, devDir
}

func itoa(n int) string { return string(rune('0' + n)) }

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestHW05_FindByCardID(t *testing.T) {
	proc, dev := fakeSys(t, map[int]string{0: "PCH", 1: "C922", 2: "nanoKONTROL2"},
		"midiC2D1", "midiC2D0", "midiC0D0", "pcmC1D0c", "midiC2Dx")
	got, err := Find(proc, dev, "nanokontrol2") // case does not matter
	if err != nil || got != filepath.Join(dev, "midiC2D0") {
		t.Errorf("Find = %q, %v", got, err)
	}
	if ids := Cards(proc); !reflect.DeepEqual(ids, []string{"C922", "PCH", "nanoKONTROL2"}) {
		t.Errorf("Cards = %v", ids)
	}
}

func TestHW05_NotFound(t *testing.T) {
	proc, dev := fakeSys(t, map[int]string{0: "PCH", 1: "nanoKONTROL2"}, "midiC0D0")
	for _, port := range []string{"nanoKONTROL2", "nano", "Other"} {
		if _, err := Find(proc, dev, port); !errors.Is(err, ErrNotFound) {
			t.Errorf("Find(%q) = %v, want ErrNotFound", port, err)
		}
	}
	if _, err := Find(filepath.Join(t.TempDir(), "none"), dev, "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("no /proc/asound: %v", err)
	}
}

// fakePort is a connected controller. Input written to feed is read by the
// device; what the device writes ends up in written.
type fakePort struct {
	r    *io.PipeReader
	feed *io.PipeWriter

	mu      sync.Mutex
	written bytes.Buffer
}

func newFakePort() *fakePort {
	r, w := io.Pipe()
	return &fakePort{r: r, feed: w}
}

func (p *fakePort) Read(b []byte) (int, error) { return p.r.Read(b) }
func (p *fakePort) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.written.Write(b)
}
func (p *fakePort) Close() error { return p.r.Close() }
func (p *fakePort) out() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]byte(nil), p.written.Bytes()...)
}

// unplug makes the next Read fail like a removed USB device.
func (p *fakePort) unplug() { _ = p.feed.CloseWithError(syscall.ENODEV) }

// logBuf collects log output for assertions.
type logBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func (l *logBuf) count(s string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Count(l.b.String(), s)
}

// harness runs a Device whose find and open are controlled by the test.
type harness struct {
	t      *testing.T
	d      *Device
	log    *logBuf
	events chan mixer.Event
	cancel context.CancelFunc
	done   chan error

	mu      sync.Mutex
	present bool  // find succeeds
	openErr error // open fails with this
	ports   []*fakePort
}

func newHarness(t *testing.T) *harness { return newHarnessWith(t, make(chan mixer.Event, 64)) }

func newHarnessWith(t *testing.T, events chan mixer.Event) *harness {
	return newHarnessAt(t, events, false, nil, 0)
}

// newHarnessAt starts the adapter with the controller already present (or
// not), opening it failing with openErr, and the grace period after plugging
// in (0: 50 ms).
func newHarnessAt(t *testing.T, events chan mixer.Event, present bool, openErr error, grace time.Duration) *harness {
	h := &harness{t: t, log: &logBuf{}, events: events, done: make(chan error, 1), present: present, openErr: openErr}
	h.d = New(slog.New(slog.NewTextHandler(h.log, &slog.HandlerOptions{Level: slog.LevelDebug})), "nanoKONTROL2")
	h.d.poll = 5 * time.Millisecond
	h.d.procDir = t.TempDir()
	h.d.resync = nil // tested on its own
	h.d.ledGap = 0
	h.d.accessGrace = 50 * time.Millisecond
	if grace > 0 {
		h.d.accessGrace = grace
	}
	h.d.accessPoll = 5 * time.Millisecond
	h.d.find = func() (string, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		if !h.present {
			return "", ErrNotFound
		}
		return "/dev/snd/midiC1D0", nil
	}
	h.d.open = func(string) (io.ReadWriteCloser, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.openErr != nil {
			return nil, h.openErr
		}
		p := newFakePort()
		h.ports = append(h.ports, p)
		return p, nil
	}
	var ctx context.Context
	ctx, h.cancel = context.WithCancel(context.Background())
	go func() { h.done <- h.d.Run(ctx, h.events) }()
	t.Cleanup(h.stop)
	return h
}

func (h *harness) set(present bool, openErr error) {
	h.mu.Lock()
	h.present, h.openErr = present, openErr
	h.mu.Unlock()
}

func (h *harness) port() *fakePort {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.ports[len(h.ports)-1]
}

func (h *harness) next() mixer.Event {
	h.t.Helper()
	select {
	case ev := <-h.events:
		return ev
	case <-time.After(5 * time.Second):
		h.t.Fatal("no event")
		return nil
	}
}

func (h *harness) stop() {
	if h.cancel == nil {
		return
	}
	h.cancel()
	h.cancel = nil
	select {
	case err := <-h.done:
		if !errors.Is(err, context.Canceled) {
			h.t.Errorf("Run = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		h.t.Error("Run did not stop")
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); time.Sleep(2 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func TestSVC03_ConnectReadUnplugReplug(t *testing.T) {
	h := newHarness(t)
	eventually(t, "not-found warning", func() bool { return h.log.count("controller not found") == 1 })
	time.Sleep(30 * time.Millisecond) // several more polls
	if n := h.log.count("controller not found"); n != 1 {
		t.Errorf("not-found warning logged %d times, want once", n)
	}
	if err := h.d.SetLED(mixer.LED{Button: mixer.ButtonS, Column: 1}, true); !errors.Is(err, ErrNotConnected) {
		t.Errorf("SetLED while away = %v", err)
	}

	// Plugged in.
	h.set(true, nil)
	if ev := h.next(); ev != (mixer.ControllerConnected{}) {
		t.Fatalf("first event %v, want ControllerConnected", ev)
	}
	if h.log.count("LED mode is set to External") != 1 {
		t.Error("HW-03 info message missing")
	}
	p := h.port()
	// Slider 3 to 100 on MIDI channel 2, then S1 pressed and released.
	go func() { _, _ = p.feed.Write([]byte{0xB1, 2, 100, 32, 127, 32, 0}) }()
	want := []mixer.Event{
		mixer.ControlMoved{Control: mixer.Control{Kind: mixer.Slider, Column: 3}, Value: 100},
		mixer.ButtonPressed{Button: mixer.ButtonS, Column: 1},
		mixer.ButtonReleased{Button: mixer.ButtonS, Column: 1}, // INPUT-08
	}
	for _, w := range want {
		if ev := h.next(); ev != w {
			t.Errorf("event %v, want %v", ev, w)
		}
	}
	// LEDs go out on the channel the controller uses.
	eventually(t, "channel learned", func() bool { return h.d.channel.Load() == 1 })
	if err := h.d.SetLED(mixer.LED{Button: mixer.ButtonM, Column: 2}, true); err != nil {
		t.Fatal(err)
	}
	if got := p.out(); !bytes.Equal(got, []byte{0xB1, 49, 127}) {
		t.Errorf("LED bytes % x", got)
	}
	// Buttons without an LED get nothing.
	if err := h.d.SetLED(mixer.LED{Transport: mixer.TrackPrev}, true); err != nil || h.d.HasLED(mixer.LED{Transport: mixer.TrackPrev}) {
		t.Errorf("SetLED(track◀) = %v", err)
	}
	if got := p.out(); len(got) != 3 {
		t.Errorf("bytes sent for an LED that does not exist: % x", got)
	}

	// Unplugged.
	h.set(false, nil)
	p.unplug()
	eventually(t, "disconnect", func() bool { return h.log.count("controller disconnected") == 1 })
	if ev := h.next(); ev != (mixer.ControllerDisconnected{}) { // held states end (INPUT-07)
		t.Fatalf("after unplug: %v, want ControllerDisconnected", ev)
	}
	eventually(t, "SetLED fails", func() bool {
		return errors.Is(h.d.SetLED(mixer.LED{Button: mixer.ButtonS, Column: 1}, true), ErrNotConnected)
	})

	// Plugged in again: connected once more, all LEDs will be re-sent (LED-07).
	h.set(true, nil)
	if ev := h.next(); ev != (mixer.ControllerConnected{}) {
		t.Fatalf("after replug: %v, want ControllerConnected", ev)
	}
}

func TestHW06_BusyDevice(t *testing.T) {
	h := newHarness(t)
	h.set(true, &os.PathError{Op: "open", Path: "/dev/snd/midiC1D0", Err: syscall.EBUSY})
	eventually(t, "busy error", func() bool { return h.log.count("in use by another program") == 1 })
	time.Sleep(30 * time.Millisecond)
	if n := h.log.count("in use by another program"); n != 1 {
		t.Errorf("busy error logged %d times, want once", n)
	}
	if h.log.count("/dev/snd/midiC1D0") == 0 {
		t.Error("busy error does not name the device")
	}
	h.set(true, nil) // the other program let go
	if ev := h.next(); ev != (mixer.ControllerConnected{}) {
		t.Fatalf("got %v, want ControllerConnected", ev)
	}
}

func TestOpenErrors(t *testing.T) {
	for name, err := range map[string]error{
		"permission": &os.PathError{Op: "open", Path: "x", Err: syscall.EACCES},
		"other":      &os.PathError{Op: "open", Path: "x", Err: syscall.EIO},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.set(true, err)
			want := map[string]string{"permission": "no permission", "other": "cannot open the controller"}[name]
			eventually(t, want, func() bool { return h.log.count(want) == 1 })
		})
	}
}

var denied = &os.PathError{Op: "open", Path: "/dev/snd/midiC1D0", Err: syscall.EACCES}

func TestHW08_PermissionRightAfterPluggingInIsRetriedQuietly(t *testing.T) {
	h := newHarnessAt(t, make(chan mixer.Event, 64), false, nil, time.Minute) // grace longer than the test
	eventually(t, "not-found warning", func() bool { return h.log.count("controller not found") == 1 })
	h.set(true, denied) // plugged in; access not granted yet
	eventually(t, "debug retry", func() bool { return h.log.count("controller not accessible yet") == 1 })
	h.set(true, nil) // access granted
	if ev := h.next(); ev != (mixer.ControllerConnected{}) {
		t.Fatalf("got %v, want ControllerConnected", ev)
	}
	if n := h.log.count("no permission"); n != 0 {
		t.Errorf("permission error logged %d times right after plugging in, want none:\n%s", n, h.log.String())
	}
	if h.log.count("level=DEBUG msg=\"controller not accessible yet") != 1 {
		t.Errorf("the retry is not logged once at debug:\n%s", h.log.String())
	}
}

func TestHW08_PermissionStillDeniedAfterTheGraceIsAnError(t *testing.T) {
	h := newHarness(t)
	eventually(t, "not-found warning", func() bool { return h.log.count("controller not found") == 1 })
	h.set(true, denied)
	eventually(t, "permission error", func() bool { return h.log.count("no permission") == 1 })
	time.Sleep(30 * time.Millisecond) // several more polls
	if n := h.log.count("no permission"); n != 1 {
		t.Errorf("permission error logged %d times, want once", n)
	}
}

func TestHW08_PermissionDeniedAtStartIsAnErrorAtOnce(t *testing.T) {
	// The grace must not apply: the device was there at start.
	h := newHarnessAt(t, make(chan mixer.Event, 64), true, denied, time.Minute)
	eventually(t, "permission error", func() bool { return h.log.count("no permission") == 1 })
	if n := h.log.count("not accessible yet"); n != 0 {
		t.Errorf("treated as just plugged in although present at start:\n%s", h.log.String())
	}
}

func TestStopWhileConnected(t *testing.T) {
	h := newHarness(t)
	h.set(true, nil)
	h.next()
	h.stop() // must end the pending Read
}

func TestStopWhileSending(t *testing.T) {
	h := newHarnessWith(t, make(chan mixer.Event)) // unbuffered: Run blocks on every send
	h.set(true, nil)
	h.next() // ControllerConnected
	p := h.port()
	go func() { _, _ = p.feed.Write([]byte{0xB0, 0, 1}) }()
	time.Sleep(20 * time.Millisecond) // Run is now blocked sending the slider event
	h.stop()
}

func TestStopBeforeConnectedIsSent(t *testing.T) {
	h := newHarnessWith(t, make(chan mixer.Event))
	h.set(true, nil)
	eventually(t, "connected", func() bool { return h.log.count("controller connected") == 1 })
	h.stop() // Run is blocked sending ControllerConnected
}

func TestLED07_StateSentAgainAfterConnect(t *testing.T) {
	// The controller ignores LED messages while it starts up after being
	// plugged in, so the mixer is asked for all LEDs again a little later.
	h := newHarness(t)
	h.d.resync = []time.Duration{20 * time.Millisecond, 60 * time.Millisecond}
	start := time.Now()
	h.set(true, nil)
	for i, want := range []mixer.Event{
		mixer.ControllerConnected{},
		mixer.ControllerConnected{Resync: true},
		mixer.ControllerConnected{Resync: true},
	} {
		if ev := h.next(); ev != want {
			t.Fatalf("event %d: %v, want %v", i, ev, want)
		}
	}
	if d := time.Since(start); d < 60*time.Millisecond {
		t.Errorf("three ControllerConnected within %v, want the last after 60ms", d)
	}
	select {
	case ev := <-h.events:
		t.Errorf("unexpected fourth event %v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestLED07_ResyncStopsWithTheSession(t *testing.T) {
	h := newHarness(t)
	h.d.resync = []time.Duration{50 * time.Millisecond}
	h.set(true, nil)
	h.next() // ControllerConnected
	h.set(false, nil)
	h.port().unplug()
	eventually(t, "disconnect", func() bool { return h.log.count("controller disconnected") == 1 })
	if ev := h.next(); ev != (mixer.ControllerDisconnected{}) {
		t.Fatalf("after unplug: %v, want ControllerDisconnected", ev)
	}
	select {
	case ev := <-h.events:
		t.Errorf("event %v after the controller was unplugged", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestLED07_LEDMessagesArePaced(t *testing.T) {
	h := newHarness(t)
	h.d.ledGap = 15 * time.Millisecond
	h.set(true, nil)
	h.next()
	start := time.Now()
	for col := 1; col <= 3; col++ {
		if err := h.d.SetLED(mixer.LED{Button: mixer.ButtonS, Column: col}, true); err != nil {
			t.Fatal(err)
		}
	}
	if d := time.Since(start); d < 30*time.Millisecond {
		t.Errorf("3 LED messages took %v, want at least 2 gaps of 15ms", d)
	}
	if got := len(h.port().out()); got != 9 {
		t.Errorf("%d bytes written, want 9", got)
	}
}

func TestOpenRawFIFO(t *testing.T) {
	// A FIFO stands in for the device: openRaw must give a file whose Read
	// returns when it is closed, as Run relies on.
	path := filepath.Join(t.TempDir(), "midi")
	must(t, syscall.Mkfifo(path, 0o600))
	f, err := openRaw(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte{0xB0, 1, 2}); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 8)
	if n, err := f.Read(buf); err != nil || !bytes.Equal(buf[:n], []byte{0xB0, 1, 2}) {
		t.Fatalf("Read = % x, %v", buf[:n], err)
	}
	done := make(chan error, 1)
	go func() { _, err := f.Read(buf); done <- err }()
	time.Sleep(20 * time.Millisecond)
	_ = f.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Error("Read after Close returned no error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not end a pending Read")
	}

	if _, err := openRaw(filepath.Join(t.TempDir(), "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("openRaw(missing) = %v", err)
	}
}

// ---- another user in front (ADR 0029) -------------------------------------------

// connected starts a harness with the controller present and waits until it
// is connected.
func connected(t *testing.T, grace time.Duration) *harness {
	t.Helper()
	h := newHarnessAt(t, make(chan mixer.Event, 64), true, nil, grace)
	if ev := h.next(); ev != (mixer.ControllerConnected{}) {
		t.Fatalf("got %v, want ControllerConnected", ev)
	}
	return h
}

func (h *harness) opened() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.ports)
}

func TestSVC09_ReleaseClosesTheControllerAndStopsLooking(t *testing.T) {
	h := connected(t, 0)
	h.d.Release()
	eventually(t, "released", func() bool { return h.log.count("controller released") == 1 })
	time.Sleep(30 * time.Millisecond) // several polls
	if n := h.opened(); n != 1 {
		t.Errorf("opened %d times while released, want only the first", n)
	}
	if n := h.log.count("controller disconnected"); n != 0 {
		t.Errorf("a release was logged as a disconnect:\n%s", h.log.String())
	}
	select {
	case ev := <-h.events:
		t.Errorf("event %v after a release; held states are ended by the mixer", ev)
	default:
	}
	if err := h.d.SetLED(mixer.LED{Button: mixer.ButtonS, Column: 1}, true); !errors.Is(err, ErrNotConnected) {
		t.Errorf("SetLED while released = %v, want ErrNotConnected", err)
	}
	if !strings.Contains(h.log.String(), `msg="controller released" apptrol.component=controller apptrol.controller.device=/dev/snd/midiC1D0`) {
		t.Errorf("release record:\n%s", h.log.String())
	}
}

func TestSVC09_TakeConnectsAtOnce(t *testing.T) {
	h := connected(t, 0)
	h.d.poll = time.Minute // only Take may make it look again
	h.d.Release()
	eventually(t, "released", func() bool { return h.log.count("controller released") == 1 })
	start := time.Now()
	h.d.Take()
	if ev := h.next(); ev != (mixer.ControllerConnected{}) { // the LEDs follow (LED-07)
		t.Fatalf("got %v, want ControllerConnected", ev)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("connected %v after Take, want at once", d)
	}
}

func TestSVC09_ReleaseBeforeTheSessionStartsIsNotLost(t *testing.T) {
	h := newHarnessAt(t, make(chan mixer.Event, 64), false, nil, 0)
	h.d.Release() // before the controller is even found
	h.set(true, nil)
	time.Sleep(30 * time.Millisecond)
	if n := h.opened(); n != 0 {
		t.Errorf("opened %d times while released, want none", n)
	}
	h.d.Take()
	if ev := h.next(); ev != (mixer.ControllerConnected{}) {
		t.Fatalf("got %v, want ControllerConnected", ev)
	}
}

var busy = &os.PathError{Op: "open", Path: "/dev/snd/midiC1D0", Err: syscall.EBUSY}

func TestSVC11_BusyRightAfterTakeIsRetriedQuietly(t *testing.T) {
	h := connected(t, time.Minute) // grace longer than the test
	h.d.Release()
	eventually(t, "released", func() bool { return h.log.count("controller released") == 1 })
	h.set(true, busy) // the other user's Apptrol still has it
	h.d.Take()
	eventually(t, "debug retry", func() bool { return h.log.count("controller still held by the other session") == 1 })
	h.set(true, nil) // it let go
	if ev := h.next(); ev != (mixer.ControllerConnected{}) {
		t.Fatalf("got %v, want ControllerConnected", ev)
	}
	if n := h.log.count("in use by another program"); n != 0 {
		t.Errorf("busy logged as an error right after Take:\n%s", h.log.String())
	}
	if !strings.Contains(h.log.String(), `level=DEBUG msg="controller still held by the other session; retrying" apptrol.component=controller apptrol.controller.device=/dev/snd/midiC1D0 error.type=controller_busy`) {
		t.Errorf("retry record:\n%s", h.log.String())
	}
}

func TestSVC11_BusyAfterTheGraceIsAnError(t *testing.T) {
	h := connected(t, 0) // grace 50 ms
	h.d.Release()
	eventually(t, "released", func() bool { return h.log.count("controller released") == 1 })
	h.set(true, busy)
	h.d.Take()
	eventually(t, "busy error", func() bool { return h.log.count("in use by another program") == 1 }) // HW-06
}
