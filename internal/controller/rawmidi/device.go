package rawmidi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Dzobash/apptrol/internal/controller"
	"github.com/Dzobash/apptrol/internal/logattr"
	"github.com/Dzobash/apptrol/internal/mixer"
)

// ErrNotConnected is returned by SetLED while the controller is not connected.
// The mixer re-sends every LED when it reconnects (LED-07).
var ErrNotConnected = errors.New("controller not connected")

// ledHelp is logged on every connect (HW-03).
const ledHelp = "https://github.com/Dzobash/apptrol#before-you-install"

// Right after it is plugged in, the controller is still starting up and
// ignores LED messages. So after a connect, mixer.ControllerConnected (which
// makes the mixer send every LED) is sent again after each of these delays
// (LED-07).
var defaultResync = []time.Duration{500 * time.Millisecond, 2 * time.Second}

// defaultLEDGap is the least time between two LED messages. Sent all at once,
// the controller drops some of a burst of LED messages.
const defaultLEDGap = 2 * time.Millisecond

// defaultLEDWriteTimeout bounds one LED write to the controller (SVC-05,
// LED-08, HW-02). A 3-byte Control Change message crosses USB full-speed in
// well under a millisecond, so 100 ms is about a hundred times what a healthy
// write needs; yet it also bounds the worst case, every LED off at shutdown
// (LED-08: 8 columns times 3 buttons plus the transport LEDs, about 30
// writes), to roughly 3 seconds, instead of blocking the service's event loop
// or the shutdown forever when a wedged controller never accepts the write
// (SVC-05, #258). The LED feedback this protects is the one that needs the
// controller's LED mode "External" (HW-02).
const defaultLEDWriteTimeout = 100 * time.Millisecond

// writeDeadliner is what *os.File implements, and so does the raw MIDI device
// opened by openRaw and a pipe. Without a deadline, a stalled writer blocks
// forever: the device is opened O_NONBLOCK, but Go's poller then waits for
// writability on backpressure (SVC-05, #258).
type writeDeadliner interface {
	SetWriteDeadline(time.Time) error
}

// Right after the controller is plugged in, its device file exists before the
// system grants the user at the screen access to it (udev's uaccess). Within
// defaultAccessGrace of the file appearing, "permission denied" means "not
// ready yet": it is logged at debug and retried every defaultAccessPoll
// (HW-08, ADR 0015). The same applies right after Take: the system may not
// have given the rights to this user yet, and the other user's Apptrol may
// not have let go ("busy") (SVC-11, ADR 0029). And right after Apptrol
// starts: a second user's Apptrol starts with their login, while their
// session is still coming to the front (HW-08, SVC-11, #104).
const (
	defaultAccessGrace = 5 * time.Second
	defaultAccessPoll  = 100 * time.Millisecond
)

// Device is the controller, connected through raw MIDI. Run and SetLED may be
// called from different goroutines.
type Device struct {
	log  *slog.Logger
	port string
	m    *controller.Map
	poll time.Duration // how often to look for the controller while it is away

	procDir string // for listing sound cards in the "not found" message

	resync []time.Duration // see defaultResync
	ledGap time.Duration   // see defaultLEDGap

	ledWriteTimeout time.Duration // see defaultLEDWriteTimeout

	accessGrace time.Duration // see defaultAccessGrace
	accessPoll  time.Duration // see defaultAccessPoll

	find func() (string, error)
	open func(path string) (io.ReadWriteCloser, error)

	mu           sync.Mutex
	cur          io.ReadWriteCloser
	curPath      string        // cur's path, for log records
	lastLED      time.Time     // when the last LED message was written
	lastWriteErr string        // the write error already logged; "" while writes succeed (warn once per outage)
	channel      atomic.Uint32 // MIDI channel the controller sends on; LEDs use it too

	// released is set by Release while another user is in front, and cleared
	// by Take (SVC-09, ADR 0029); wake tells Run about either at once.
	released atomic.Bool
	wake     chan struct{}
}

// errReleased ends a session that Release overtook before it connected.
var errReleased = errors.New("released before connecting")

// New returns a Device that looks for the sound card with id port.
func New(log *slog.Logger, port string) *Device {
	return &Device{
		log:  log.With(logattr.Component(logattr.Controller)),
		port: port,
		m:    controller.NanoKONTROL2,
		poll: time.Second,

		procDir: DefaultProcDir,
		resync:  defaultResync,
		ledGap:  defaultLEDGap,

		ledWriteTimeout: defaultLEDWriteTimeout,

		accessGrace: defaultAccessGrace,
		accessPoll:  defaultAccessPoll,
		find:        func() (string, error) { return Find(DefaultProcDir, DefaultDevDir, port) },
		open:        openRaw,
		wake:        make(chan struct{}, 1),
	}
}

// Release lets go of the controller while another user's session is in front:
// it is closed, and not looked for until Take (SVC-09, ADR 0029). The service
// turns its LEDs off before.
func (d *Device) Release() {
	d.released.Store(true)
	d.mu.Lock()
	if d.cur != nil {
		_ = d.cur.Close() // ends the session's pending Read
	}
	d.mu.Unlock()
	d.wakeUp()
}

// Take ends Release: the controller is looked for at once, and connects as
// after being plugged in (LED-07).
func (d *Device) Take() {
	d.released.Store(false)
	d.wakeUp()
}

func (d *Device) wakeUp() {
	select {
	case d.wake <- struct{}{}:
	default: // already pending
	}
}

// openRaw opens a raw MIDI device for reading and writing. It is opened
// non-blocking so that Go's poller handles it: a pending Read then returns
// when the file is closed or the device is unplugged.
func openRaw(path string) (io.ReadWriteCloser, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(fd), path), nil
}

// Run looks for the controller, reads it while it is connected, and waits for
// it while it is not (SVC-03), until ctx is canceled. After every connect it
// sends mixer.ControllerConnected, again after the resync delays, and the
// decoded events. It returns ctx's error.
func (d *Device) Run(ctx context.Context, out chan<- mixer.Event) error {
	var lastProblem string // logged once until something changes
	report := func(level slog.Level, msg string, args ...any) {
		key := msg + fmtArgs(args)
		if key != lastProblem {
			d.log.Log(ctx, level, msg, args...)
			lastProblem = key
		}
	}
	// appeared is when the device file was found after it had been missing,
	// i.e. the controller was just plugged in; zero if it was there at start.
	var appeared time.Time
	missing := false
	// taken is when Take ended a Release; zero if there was none.
	var taken time.Time
	wasReleased := false
	started := time.Now()
	// grace says why a failed open may mean "not ready yet": the moment it
	// follows, or "" once all of them are longer ago than accessGrace.
	grace := func() string {
		switch {
		case justAfter(taken, d.accessGrace):
			return "taken_back"
		case justAfter(appeared, d.accessGrace):
			return "plugged_in"
		case justAfter(started, d.accessGrace):
			return "started"
		}
		return ""
	}
	for {
		if d.released.Load() {
			// Another user is in front: not looked for until Take (SVC-09).
			wasReleased = true
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-d.wake:
			}
			continue
		}
		if wasReleased {
			wasReleased, taken, lastProblem = false, time.Now(), ""
		}
		wait := d.poll
		path, err := d.find()
		if err == nil && missing {
			appeared, missing = time.Now(), false
		}
		if err != nil {
			missing = true
			report(slog.LevelWarn, "controller not found; waiting for it to be plugged in",
				logattr.KeyPort, d.port, logattr.KeySoundCards, cardList(d.procDir))
		} else if f, err := d.open(path); err != nil {
			denied := errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM)
			why := grace()
			switch {
			case denied && why != "":
				// Just started, plugged in, or taken back from another user:
				// the system grants access to the user in front a moment later
				// (HW-08, SVC-11; ADR 0015, ADR 0029).
				report(slog.LevelDebug, "controller not accessible yet; retrying",
					logattr.KeyMIDIDevice, path, logattr.KeyGrace, why,
					logattr.Error(logattr.ErrControllerDenied, err))
				wait = d.accessPoll
			case errors.Is(err, syscall.EBUSY) && (why == "taken_back" || why == "started"):
				// Just taken back, or started with a login: the other user's
				// Apptrol may not have let go yet (SVC-11). Not after plugging
				// in: then no other session can have it.
				report(slog.LevelDebug, "controller still held by the other session; retrying",
					logattr.KeyMIDIDevice, path, logattr.KeyGrace, why,
					logattr.Error(logattr.ErrControllerBusy, err))
				wait = d.accessPoll
			case errors.Is(err, syscall.EBUSY):
				report(slog.LevelError, "the controller is in use by another program; retrying (`fuser <device>` names it)",
					logattr.KeyMIDIDevice, path, logattr.Error(logattr.ErrControllerBusy, err)) // HW-06
			case denied:
				report(slog.LevelError, "no permission to open the controller; see the README section on permissions",
					logattr.KeyMIDIDevice, path, logattr.Error(logattr.ErrControllerDenied, err))
			default:
				report(slog.LevelError, "cannot open the controller; retrying",
					logattr.KeyMIDIDevice, path, logattr.Error(logattr.ErrControllerOpen, err))
			}
		} else {
			lastProblem = ""
			err := d.session(ctx, path, f, out)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if d.released.Load() {
				// Let go on purpose; the mixer already ended held states (SVC-09).
				if !errors.Is(err, errReleased) {
					d.log.Info("controller released", logattr.KeyMIDIDevice, path)
				}
				continue
			}
			d.log.Info("controller disconnected", logattr.KeyMIDIDevice, path, logattr.KeyDisconnected, reason(err))
			// Held buttons will never send their release (INPUT-07).
			select {
			case out <- mixer.ControllerDisconnected{}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		case <-d.wake: // Release or Take: act on it at once
		}
	}
}

// justAfter reports whether t is set and less than d ago.
func justAfter(t time.Time, d time.Duration) bool { return !t.IsZero() && time.Since(t) < d }

// cardList names the sound cards there are, to help spot a wrong port.
func cardList(procDir string) string {
	if ids := Cards(procDir); len(ids) > 0 {
		return strings.Join(ids, ", ")
	}
	return "none"
}

func fmtArgs(args []any) string {
	var b strings.Builder
	for _, a := range args {
		switch v := a.(type) {
		case string:
			b.WriteString(v)
		case slog.Attr:
			b.WriteString(v.String())
		}
		b.WriteByte(0)
	}
	return b.String()
}

// reason says why a session ended, in a word or two: the device was unplugged,
// or the read error's text.
func reason(err error) string {
	if errors.Is(err, syscall.ENODEV) || errors.Is(err, io.EOF) {
		return "unplugged"
	}
	if err == nil {
		return "unknown"
	}
	return err.Error()
}

// session reads one connected controller until it goes away or ctx ends.
func (d *Device) session(ctx context.Context, path string, f io.ReadWriteCloser, out chan<- mixer.Event) error {
	d.mu.Lock()
	if d.released.Load() {
		// Release came between opening and now; checked under the lock it
		// takes, so one of the two always sees the other.
		d.mu.Unlock()
		_ = f.Close()
		return errReleased
	}
	d.cur = f
	d.curPath = path
	d.lastWriteErr = "" // a new session ends the last outage's logging
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.cur = nil
		d.curPath = ""
		d.mu.Unlock()
		_ = f.Close()
	}()
	// Closing the file ends a pending Read when ctx is canceled.
	stop := context.AfterFunc(ctx, func() { _ = f.Close() })
	defer stop()

	d.log.Info("controller connected", logattr.KeyMIDIDevice, path, logattr.KeyPort, d.port)
	d.log.Info("LEDs show mute and solo only when the controller's LED mode is set to External; "+
		"buttons must be Momentary", logattr.KeyURL, ledHelp) // HW-03

	send := func(ev mixer.Event) bool {
		select {
		case out <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}
	if !send(mixer.ControllerConnected{}) {
		return ctx.Err()
	}
	// Send the LED state again once the controller has surely started up.
	done := make(chan struct{})
	defer close(done)
	go func() {
		var waited time.Duration
		for _, at := range d.resync {
			select {
			case <-time.After(at - waited):
			case <-done:
				return
			}
			waited = at
			select {
			case out <- mixer.ControllerConnected{Resync: true}:
			case <-done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	var p controller.Parser
	buf := make([]byte, 256)
	for {
		n, err := f.Read(buf)
		ok := true
		p.Feed(buf[:n], func(cc controller.CC) {
			d.channel.Store(uint32(cc.Channel))
			if ev, known := d.m.Decode(cc); known && ok {
				d.log.Debug("midi message", logattr.KeyMIDICC, cc.Controller, logattr.KeyMIDIValue, cc.Value,
					logattr.KeyMIDIChannel, cc.Channel+1)
				ok = send(ev)
			}
		})
		if !ok {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
	}
}

// HasLED reports whether the controller has this LED.
func (d *Device) HasLED(l mixer.LED) bool { return d.m.HasLED(l) }

// SetLED turns one LED on or off. LEDs the controller does not have are ignored.
// The write is bounded by ledWriteTimeout (SVC-05, LED-08, HW-02): a controller
// that stops accepting writes fails fast instead of blocking the event loop or
// the shutdown, and the failure is logged once per outage, not once per LED.
func (d *Device) SetLED(l mixer.LED, on bool) error {
	cc, ok := d.m.LED(l, on, byte(d.channel.Load()))
	if !ok {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cur == nil {
		return ErrNotConnected
	}
	if wait := d.ledGap - time.Since(d.lastLED); wait > 0 {
		time.Sleep(wait)
	}
	wd, canDeadline := d.cur.(writeDeadliner)
	if canDeadline {
		_ = wd.SetWriteDeadline(time.Now().Add(d.ledWriteTimeout))
	}
	_, err := d.cur.Write(cc.Encode())
	if canDeadline {
		_ = wd.SetWriteDeadline(time.Time{}) // leave no deadline set on the device
	}
	d.lastLED = time.Now()
	if err != nil {
		// Warn once per outage, not once per LED: while writes keep failing
		// with the same error, only the first failure is logged
		// (CONTRIBUTING.md › Log messages).
		if key := logattr.OneLine(err.Error()); key != d.lastWriteErr {
			d.log.Warn("LED write failed; the controller is not accepting writes",
				logattr.KeyMIDIDevice, d.curPath, logattr.Error(logattr.ErrLEDFailed, err))
			d.lastWriteErr = key
		}
	} else {
		d.lastWriteErr = ""
	}
	return err
}
