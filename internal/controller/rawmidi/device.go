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

	find func() (string, error)
	open func(path string) (io.ReadWriteCloser, error)

	mu      sync.Mutex
	cur     io.ReadWriteCloser
	lastLED time.Time     // when the last LED message was written
	channel atomic.Uint32 // MIDI channel the controller sends on; LEDs use it too
}

// New returns a Device that looks for the sound card with id port.
func New(log *slog.Logger, port string) *Device {
	return &Device{
		log:  log,
		port: port,
		m:    controller.NanoKONTROL2,
		poll: time.Second,

		procDir: DefaultProcDir,
		resync:  defaultResync,
		ledGap:  defaultLEDGap,
		find:    func() (string, error) { return Find(DefaultProcDir, DefaultDevDir, port) },
		open:    openRaw,
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
	for {
		path, err := d.find()
		if err != nil {
			report(slog.LevelWarn, "controller not found; waiting for it to be plugged in",
				"port", d.port, "sound_cards", cardList(d.procDir))
		} else if f, err := d.open(path); err != nil {
			switch {
			case errors.Is(err, syscall.EBUSY):
				report(slog.LevelError, "the controller is in use by another program; retrying (close the other program, or see `fuser "+path+"`)",
					"device", path) // HW-06
			case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
				report(slog.LevelError, "no permission to open the controller; see the README section on permissions",
					"device", path, "err", err)
			default:
				report(slog.LevelError, "cannot open the controller; retrying", "device", path, "err", err)
			}
		} else {
			lastProblem = ""
			err := d.session(ctx, path, f, out)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			d.log.Info("controller disconnected", "device", path, "reason", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d.poll):
		}
	}
}

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
		if s, ok := a.(string); ok {
			b.WriteString(s)
		}
		b.WriteByte(0)
	}
	return b.String()
}

// session reads one connected controller until it goes away or ctx ends.
func (d *Device) session(ctx context.Context, path string, f io.ReadWriteCloser, out chan<- mixer.Event) error {
	d.mu.Lock()
	d.cur = f
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.cur = nil
		d.mu.Unlock()
		_ = f.Close()
	}()
	// Closing the file ends a pending Read when ctx is canceled.
	stop := context.AfterFunc(ctx, func() { _ = f.Close() })
	defer stop()

	d.log.Info("controller connected", "device", path, "port", d.port)
	d.log.Info("LEDs show mute and solo only when the controller's LED mode is set to External; "+
		"buttons must be Momentary", "help", ledHelp) // HW-03

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
				d.log.Debug("midi", "cc", cc.Controller, "value", cc.Value, "channel", cc.Channel+1)
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
	_, err := d.cur.Write(cc.Encode())
	d.lastLED = time.Now()
	return err
}
