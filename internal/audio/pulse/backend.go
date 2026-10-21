package pulse

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/jfreymuth/pulse/proto"

	"github.com/Dzobash/apptrol/internal/logattr"
	"github.com/Dzobash/apptrol/internal/mixer"
)

// ErrNotConnected is returned by Apply while there is no connection to the
// audio server. The service re-applies everything after reconnecting (SVC-04).
var ErrNotConnected = errors.New("not connected to the audio server")

// errLost ends a session when the connection breaks.
var errLost = errors.New("connection lost")

// newGuard is how long after a stream, capture device or output device
// appears Apptrol defends the volume and mute it set. PipeWire (WirePlumber) restores remembered
// settings shortly after something appears; when that lands after Apptrol's
// own change, it would undo it (PRIO-03; for outputs OUT-05, ADR 0027).
// Within this window, Apptrol sets its values again.
const newGuard = 3 * time.Second

// guard records what Apptrol set on a new stream or device.
type guard struct {
	until  time.Time
	volume *float64
	muted  *bool

	// read is an output's volume last read, for KeepOutput: a read that saw a
	// new name or description waits for the mixer before it is defended
	// (OUT-05, ADR 0027). Streams and inputs leave it nil.
	read *float64
	// Only the newest reported identity's answer can resume output defense.
	// Volume-only notifications do not answer it (CTRL-06, ADR 0027).
	generation uint64
	answered   bool
}

// check returns what to set again, given the current values. It is called
// with Backend.mu held; expired reports that the guard can be dropped.
func (g *guard) check(volume float64, muted bool) (setVol *float64, setMute *bool, expired bool) {
	if time.Now().After(g.until) {
		return nil, nil, true
	}
	if g.volume != nil && math.Abs(volume-*g.volume) > 0.005 {
		setVol = g.volume
	}
	if g.muted != nil && muted != *g.muted {
		setMute = g.muted
	}
	return setVol, setMute, false
}

func newGuardNow() *guard { return &guard{until: time.Now().Add(newGuard)} }

// outputRecheck is how long after a volume write within the new-output guard's
// window the adapter reads the output again. The server answers a write once
// it has passed it on, not once the device has it: PipeWire 1.0 can lose a
// write in a new device's first moments without reporting any change, so the
// guard, which acts on reported changes, would never see it (OUT-05, ADR 0027,
// #166).
//
//	write 30 % ─► success ─► (lost) ─► 200 ms: read 100 % ─► 30 % again ─► 200 ms: read 30 %
const outputRecheck = 200 * time.Millisecond

// ownMute is how long Apptrol waits to see the change its own mute caused.
// It only bounds how long the expected value is kept, e.g. when a write
// failed; it never hides a change (MUTE-07, #118).
const ownMute = time.Second

// muteWant is the mute Apptrol set last on a stream or device. The first mute
// change seen afterwards is Apptrol's own if it has this value, and was made
// outside Apptrol if it has the other one, even within ownMute: an unmute right
// after a hold-to-talk release must be undone, not ignored (INPUT-03, #118).
type muteWant struct {
	muted bool
	until time.Time
}

// outsideMute reports whether a mute change to muted was made outside
// Apptrol, and uses up what Apptrol expected for key. It is called with
// Backend.mu held.
func outsideMute[K comparable](want map[K]muteWant, key K, muted bool) bool {
	w, ok := want[key]
	if !ok {
		return true
	}
	delete(want, key)
	return time.Now().After(w.until) || muted != w.muted
}

// Backend keeps a connection to the audio server, reports streams, capture
// devices and output devices (OUT-12, ADR 0027) as mixer events, and carries
// out volume and mute actions.
//
// Run and Apply may be called from different goroutines.
type Backend struct {
	log    *slog.Logger
	server string // "" = default

	// Wait before reconnecting: starts at retryMin, doubles up to retryMax.
	retryMin, retryMax time.Duration

	mu       sync.Mutex
	cur      *conn
	channels map[uint32]int    // channel count per stream, for setting volume
	devChans map[string]int    // channel count per capture device
	outChans map[string]int    // channel count per output device (OUT-01)
	guards   map[uint32]*guard // new streams
	devGuard map[string]*guard // new capture devices
	outGuard map[string]*guard // new output devices: only the volume, outputs have no mute yet (OUT-05)
	// Never reset on removal or reconnect: an old buffered answer must not
	// acknowledge a new incarnation of the same name (CTRL-06, ADR 0027).
	outputGeneration uint64
	// recheck carries the outputs to read again outputRecheck after a write
	// within the guard's window; session reads them in turn with the change
	// notifications (OUT-05, ADR 0027, #166).
	recheck chan string

	muteWant    map[uint32]muteWant // the mute Apptrol last set on a stream
	devMuteWant map[string]muteWant // the mute Apptrol last set on a device

	// wire makes mute writes and event handling take turns, so a mute read by
	// handle is never older than the expected value it is compared with
	// (#118). Stream, input and output volume writes take it too, so the
	// new-stream, new-input and new-output guards never write a volume the
	// slider has moved away from in the meantime (CTRL-04, CTRL-05, PRIO-03,
	// #158; OUT-01, OUT-05, ADR 0027). Take it before mu.
	wire sync.Mutex

	// streamWrite, sourceWrite and outputWrite replace the write of a
	// stream's, an input's or an output device's volume in tests; nil writes
	// to the server.
	streamWrite func(id uint32, channels int, v float64) error
	sourceWrite func(name string, channels int, v float64) error
	outputWrite func(name string, channels int, v float64) error

	// beforeWire, if set, runs in Apply just before it waits for wire, so a
	// test knows when Apply waits instead of sleeping (QA-11); nil in use.
	beforeWire func()
}

// New returns a Backend. server is a PulseAudio server string; "" uses the
// default (PULSE_SERVER, or the user's pipewire-pulse socket).
func New(log *slog.Logger, server string) *Backend {
	return &Backend{log: log.With(logattr.Component(logattr.Audio)), server: server, retryMin: 500 * time.Millisecond, retryMax: 5 * time.Second,
		recheck: make(chan string, 64)}
}

// Run connects and keeps the connection until ctx is canceled. After each
// (re)connect it sends a full mixer.AudioSnapshot, then single changes:
// StreamAdded, StreamRemoved, DeviceAdded, DeviceRemoved, OutputAdded and
// OutputRemoved (OUT-12, ADR 0027). If the server goes away, Run logs it and
// reconnects (SVC-04). It returns ctx's error.
func (b *Backend) Run(ctx context.Context, out chan<- mixer.Event) error {
	wait := b.retryMin
	failed := false // the last attempt failed; don't repeat the error log
	for {
		k, err := dial(b.server)
		if err != nil {
			if !failed {
				b.log.Error("cannot connect to the audio server; retrying", logattr.Error(logattr.ErrAudioUnreachable, err))
				failed = true
			} else {
				b.log.Debug("audio server still unreachable", logattr.Error(logattr.ErrAudioUnreachable, err),
					logattr.KeyRetryDelay, wait.Seconds())
			}
		} else {
			wait = b.retryMin
			err = b.session(ctx, k, out)
			b.setConn(nil)
			k.shutdown()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			b.log.Error("lost the connection to the audio server; reconnecting", logattr.Error(logattr.ErrAudioLost, err))
			failed = true // already reported; retries are logged at debug level
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		wait = min(wait*2, b.retryMax)
	}
}

// tracker remembers what was reported, so only real changes become events.
type tracker struct {
	streams map[uint32]mixer.Stream
	devices map[uint32]mixer.Device // by source index
	muted   map[uint32]bool         // last seen mute, by stream
	devMute map[uint32]bool         // last seen mute, by source index
	outputs map[uint32]mixer.Device // by sink index (OUT-12, ADR 0027)
}

// session runs one connection until it breaks or ctx ends.
func (b *Backend) session(ctx context.Context, k *conn, out chan<- mixer.Event) error {
	// Subscribe first, then read everything: nothing that happens in between is
	// missed, and events about things already read are recognized as known.
	if err := k.subscribe(); err != nil {
		return err
	}
	info, err := k.serverInfo()
	if err != nil {
		return err
	}
	b.log.Info("connected to the audio server", logattr.KeyAudioServer, fmt.Sprintf("%s %s", info.PackageName, info.PackageVersion))

	var t tracker
	send := func(ev mixer.Event) error {
		select {
		case out <- ev:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	resync := func() error {
		k.overflow.Store(false)
		streams, err := k.streams()
		if err != nil {
			return err
		}
		sources, err := k.sources()
		if err != nil {
			return err
		}
		sinks, err := k.sinks()
		if err != nil {
			return err
		}
		t = tracker{streams: map[uint32]mixer.Stream{}, devices: map[uint32]mixer.Device{},
			muted: map[uint32]bool{}, devMute: map[uint32]bool{}, outputs: map[uint32]mixer.Device{}}
		chans, devChans, outChans := map[uint32]int{}, map[string]int{}, map[string]int{}
		for _, s := range streams {
			t.streams[s.ID] = s.Stream
			t.muted[s.ID] = s.Muted
			chans[s.ID] = s.Channels
		}
		for _, d := range sources {
			t.devices[d.Index] = d.Device
			t.devMute[d.Index] = d.Muted
			devChans[d.Name] = d.Channels
		}
		b.mu.Lock()
		for i := range sinks {
			b.outputGeneration++
			sinks[i].Generation = b.outputGeneration
			d := sinks[i]
			t.outputs[d.Index] = d.Device
			outChans[d.Name] = d.Channels
		}
		b.cur, b.channels, b.devChans, b.outChans = k, chans, devChans, outChans
		b.guards, b.devGuard, b.outGuard = map[uint32]*guard{}, map[string]*guard{}, map[string]*guard{}
		b.muteWant, b.devMuteWant = map[uint32]muteWant{}, map[string]muteWant{}
		b.mu.Unlock()
		return send(Snapshot(streams, sources, sinks))
	}
	if err := resync(); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-k.closed:
			return errLost
		case e := <-k.events:
			if k.overflow.Load() {
				b.log.Warn("too many audio changes at once; reading everything again")
				if err := resync(); err != nil {
					return err
				}
				continue
			}
			b.wire.Lock()
			evs, err := b.handle(k, &t, e)
			b.wire.Unlock()
			if err != nil {
				return err
			}
			for _, ev := range evs {
				if err := send(ev); err != nil {
					return err
				}
			}
		case name := <-b.recheck:
			b.wire.Lock()
			err := b.recheckOutput(k, &t, name)
			b.wire.Unlock()
			if err != nil {
				return err
			}
		}
	}
}

// recheckOutput reads an output again after a write within the new-output
// guard's window and defends it as a reported volume change would be, so a
// write the server lost is set again (OUT-05, ADR 0027, #166). It is called
// with wire held. An output without a guard is left alone; one that is gone or
// has a new name or description is left to its change notification, so the
// mixer still decides first (CTRL-06).
func (b *Backend) recheckOutput(k *conn, t *tracker, name string) error {
	b.mu.Lock()
	_, guarded := b.outGuard[name]
	b.mu.Unlock()
	if !guarded {
		return nil
	}
	d, err := k.sinkByName(name)
	if err != nil {
		if IsGone(err) {
			return nil
		}
		return err
	}
	if old, known := t.outputs[d.Index]; !known || !sameOutputIdentity(old, d.Device) {
		return nil
	}
	b.log.Debug("output read again after a volume write", logattr.KeyDeviceName, name)
	return b.sinkRead(k, &d, true)
}

// handle turns one change notification into mixer events, possibly none. One
// notification can carry several changes, e.g. a stream muted and corked.
func (b *Backend) handle(k *conn, t *tracker, e proto.SubscribeEvent) ([]mixer.Event, error) {
	kind := e.Event.GetType()
	switch e.Event.GetFacility() {
	case proto.EventSinkSinkInput:
		old, known := t.streams[e.Index]
		if kind != proto.EventRemove {
			s, err := k.stream(e.Index)
			switch {
			case err == nil:
				b.mu.Lock()
				b.channels[s.ID] = s.Channels
				if !known {
					b.guards[s.ID] = newGuardNow()
				}
				b.mu.Unlock()
				wasMuted := t.muted[s.ID]
				t.muted[s.ID] = s.Muted
				t.streams[s.ID] = s.Stream
				if known && mixer.SameStream(old, s.Stream) {
					if err := b.defend(k, s); err != nil { // a volume change and the like
						return nil, err
					}
					return streamChanges(old, wasMuted, s, b.outsideStreamMute), nil
				}
				return []mixer.Event{mixer.StreamAdded{Stream: s.Stream}}, nil
			case !IsGone(err):
				return nil, err
			}
			// Gone before it could be read: treat as removed.
		}
		if !known {
			return nil, nil
		}
		delete(t.streams, e.Index)
		delete(t.muted, e.Index)
		b.mu.Lock()
		delete(b.channels, e.Index)
		delete(b.guards, e.Index)
		delete(b.muteWant, e.Index)
		b.mu.Unlock()
		return []mixer.Event{mixer.StreamRemoved{ID: e.Index}}, nil

	case proto.EventSource:
		old, known := t.devices[e.Index]
		if kind != proto.EventRemove {
			d, ok, err := k.source(e.Index)
			switch {
			case err == nil && !ok:
				return nil, nil // a monitor
			case err == nil:
				b.mu.Lock()
				b.devChans[d.Name] = d.Channels
				if !known {
					b.devGuard[d.Name] = newGuardNow()
				}
				b.mu.Unlock()
				wasMuted := t.devMute[e.Index]
				t.devMute[e.Index] = d.Muted
				if known && old == d.Device {
					if err := b.defendDevice(k, d); err != nil {
						return nil, err
					}
					if d.Muted != wasMuted && b.outsideDeviceMute(d.Name, d.Muted) {
						return []mixer.Event{mixer.DeviceMuteChanged{Name: d.Name, Muted: d.Muted}}, nil
					}
					return nil, nil
				}
				t.devices[e.Index] = d.Device
				return []mixer.Event{mixer.DeviceAdded{Device: d.Device}}, nil
			case !IsGone(err):
				return nil, err
			}
		}
		if !known {
			return nil, nil
		}
		delete(t.devices, e.Index)
		delete(t.devMute, e.Index)
		b.mu.Lock()
		delete(b.devChans, old.Name)
		delete(b.devGuard, old.Name)
		delete(b.devMuteWant, old.Name)
		b.mu.Unlock()
		return []mixer.Event{mixer.DeviceRemoved{Name: old.Name}}, nil

	case proto.EventSink:
		// Read through sinkRead, which keeps the channel count and the
		// new-output guard, and defends Apptrol's volume on a known output.
		old, known := t.outputs[e.Index]
		var defendErr error
		evs, err := outputChange(t, e.Index, kind == proto.EventRemove, func(index uint32) (SinkInfo, error) {
			d, err := k.sink(index)
			if err == nil {
				defendErr = b.sinkRead(k, &d, known && sameOutputIdentity(old, d.Device))
			}
			return d, err
		})
		if err == nil {
			err = defendErr
		}
		for _, ev := range evs {
			if r, ok := ev.(mixer.OutputRemoved); ok {
				b.mu.Lock()
				delete(b.outChans, r.Name)
				delete(b.outGuard, r.Name)
				b.mu.Unlock()
			}
		}
		return evs, err
	}
	return nil, nil
}

// sinkRead keeps what Apply needs about an output device just read: its
// channel count, and a guard when it is new, so the volume WirePlumber
// restores on it does not undo the one Apptrol sets (OUT-05, ADR 0027). same says that
// the device was known with the same name and description: then only its
// volume or mute changed. Defense stays paused until the newest reported
// generation is answered, even across those reads (CTRL-06, ADR 0027).
//
// A read that sees a new device, or a new name or description, is not
// defended yet: the match may be on the description, so only the mixer knows
// whether a control still holds the device. It answers with KeepOutput, which
// defends the volume read here at once, or ReleaseOutput, which lets it go
// (OUT-01, OUT-05, CTRL-06, ADR 0027).
//
//	read: description changed, volume 100 % ──► OutputAdded ──► mixer
//	  still matches ──► KeepOutput    ──► guard: 100 % ≠ 30 % ──► set 30 % again
//	  no longer     ──► ReleaseOutput ──► guard lets it go     ──► 100 % stays
func (b *Backend) sinkRead(k *conn, d *SinkInfo, same bool) error {
	b.mu.Lock()
	if !same {
		b.outputGeneration++
		d.Generation = b.outputGeneration
	}
	_, tracked := b.outChans[d.Name]
	b.outChans[d.Name] = d.Channels
	if !tracked {
		b.outGuard[d.Name] = newGuardNow()
	}
	if g := b.outGuard[d.Name]; g != nil {
		if !same {
			g.generation, g.answered = d.Generation, false
		}
		v := d.Volume
		g.read = &v
	}
	b.mu.Unlock()
	if !same {
		b.logOutputRead(*d, guardIdentityChanged)
		return nil
	}
	return b.defendOutput(k, *d)
}

// sameOutputIdentity ignores the adapter's ownership token when deciding
// whether matching needs another reported read (OUT-02, CTRL-06, ADR 0027).
func sameOutputIdentity(a, b mixer.Device) bool {
	return a.Name == b.Name && a.Description == b.Description
}

// outputChange returns the event for a change notification about the output
// device (sink) with index: OutputAdded when it appeared or its name or
// description changed, OutputRemoved when it is gone, nothing for a change of
// its volume or mute (OUT-12, ADR 0027). read reads the sink; an output gone
// before it could be read counts as removed.
func outputChange(t *tracker, index uint32, removed bool, read func(uint32) (SinkInfo, error)) ([]mixer.Event, error) {
	old, known := t.outputs[index]
	if !removed {
		d, err := read(index)
		switch {
		case err == nil:
			if known && sameOutputIdentity(old, d.Device) {
				return nil, nil
			}
			t.outputs[index] = d.Device
			return []mixer.Event{mixer.OutputAdded{Device: d.Device}}, nil
		case !IsGone(err):
			return nil, err
		}
	}
	if !known {
		return nil, nil
	}
	delete(t.outputs, index)
	return []mixer.Event{mixer.OutputRemoved{Name: old.Name}}, nil
}

// streamChanges returns the events for a known stream whose properties
// changed but whose identity did not: a mute made outside Apptrol (MUTE-07)
// and a pause or resume by its app (MEDIA-10). outsideMute tells whether a
// mute change came from outside Apptrol; it is only asked when the mute changed.
func streamChanges(old mixer.Stream, wasMuted bool, s StreamInfo, outsideMute func(id uint32, muted bool) bool) []mixer.Event {
	var evs []mixer.Event
	if s.Muted != wasMuted && outsideMute(s.ID, s.Muted) {
		evs = append(evs, mixer.StreamMuteChanged{ID: s.ID, Muted: s.Muted})
	}
	if s.Corked != old.Corked {
		evs = append(evs, mixer.StreamCorkChanged{ID: s.ID, Corked: s.Corked})
	}
	return evs
}

// defend sets the volume and mute Apptrol chose for a new stream again if the
// server changed them within newGuard.
func (b *Backend) defend(k *conn, s StreamInfo) error {
	var vol *float64
	var muted *bool
	b.mu.Lock()
	if g := b.guards[s.ID]; g != nil {
		var expired bool
		if vol, muted, expired = g.check(s.Volume, s.Muted); expired {
			delete(b.guards, s.ID)
		}
	}
	b.mu.Unlock()

	if vol != nil {
		b.log.Debug("audio server changed the volume of a new stream; setting it again",
			logattr.KeyStreamID, s.ID, logattr.KeyStreamName, s.AppName,
			logattr.KeyVolume, percent(s.Volume), logattr.KeyWanted, percent(*vol))
		if err := b.setStreamVolume(k, s.ID, s.Channels, *vol); err != nil && !IsGone(err) {
			return err
		}
	}
	if muted != nil {
		b.log.Debug("audio server changed the mute of a new stream; setting it again",
			logattr.KeyStreamID, s.ID, logattr.KeyStreamName, s.AppName,
			logattr.KeyMuted, s.Muted, logattr.KeyWantMut, *muted)
		if err := k.setStreamMute(s.ID, *muted); err != nil && !IsGone(err) {
			return err
		}
	}
	return nil
}

// defendDevice is defend for capture devices.
func (b *Backend) defendDevice(k *conn, d SourceInfo) error {
	var vol *float64
	var muted *bool
	b.mu.Lock()
	if g := b.devGuard[d.Name]; g != nil {
		var expired bool
		if vol, muted, expired = g.check(d.Volume, d.Muted); expired {
			delete(b.devGuard, d.Name)
		}
	}
	b.mu.Unlock()

	if vol != nil {
		b.log.Debug("audio server changed the volume of a new input; setting it again",
			logattr.KeyDeviceName, d.Name, logattr.KeyVolume, percent(d.Volume), logattr.KeyWanted, percent(*vol))
		if err := b.setSourceVolume(k, d.Name, d.Channels, *vol); err != nil && !IsGone(err) {
			return err
		}
	}
	if muted != nil {
		b.log.Debug("audio server changed the mute of a new input; setting it again",
			logattr.KeyDeviceName, d.Name, logattr.KeyMuted, d.Muted, logattr.KeyWantMut, *muted)
		if err := k.setSourceMute(d.Name, *muted); err != nil && !IsGone(err) {
			return err
		}
	}
	return nil
}

// defendOutput is defendDevice for output devices: only their volume, which
// Apptrol sets (OUT-01, OUT-05, ADR 0027).
func (b *Backend) defendOutput(k *conn, d SinkInfo) error {
	b.mu.Lock()
	vol, state := b.checkOutputGuard(d.Name, d.Volume)
	b.mu.Unlock()
	b.logOutputRead(d, state)
	return b.restoreOutput(k, d.Name, d.Channels, d.Volume, vol)
}

// What the new-output guard decided about a read, for apptrol.audio.guard.state:
// the log then says why a volume was or was not set again (OUT-05, OUT-13,
// ADR 0027).
const (
	guardNone             = "no_guard"          // the output is not new, or nothing was read yet
	guardExpired          = "expired"           // the guard's 3 seconds are over
	guardNoVolume         = "no_volume_set"     // no control set it, or it was released
	guardAtWanted         = "at_wanted_volume"  // it reads what Apptrol set
	guardRestore          = "restore_volume"    // it reads something else: set again
	guardIdentityChanged  = "identity_changed"  // new, or a new name or description: the mixer decides first
	guardOwnershipPending = "ownership_pending" // no answer for the newest reported identity yet
)

// checkOutputGuard returns the volume to set again on an output that reads
// volume, and why; it is called with mu held (OUT-05, ADR 0027).
func (b *Backend) checkOutputGuard(name string, volume float64) (*float64, string) {
	g := b.outGuard[name]
	if g == nil {
		return nil, guardNone
	}
	vol, _, expired := g.check(volume, false)
	switch {
	case expired:
		delete(b.outGuard, name)
		return nil, guardExpired
	case g.generation != 0 && !g.answered:
		return nil, guardOwnershipPending
	case g.volume == nil:
		return nil, guardNoVolume
	case vol == nil:
		return nil, guardAtWanted
	}
	return vol, guardRestore
}

// keepOutput defends an output that a control still holds after a read saw
// it new, or with a new name or description: that read waited for the mixer
// (sinkRead), and its volume is checked now, without waiting for another
// change from the server (KeepOutput, OUT-05, ADR 0027). It is called with
// wire held, so no newer read can come in between.
func (b *Backend) keepOutput(name string, generation uint64) error {
	b.mu.Lock()
	if g := b.outGuard[name]; g != nil {
		if g.generation != generation {
			current := g.generation
			b.mu.Unlock()
			b.logStaleAnswer("KeepOutput", name, generation, current)
			return nil
		}
		g.answered = true
	}
	k, chans := b.cur, b.outChans[name]
	var vol *float64
	var read float64
	state := guardNone
	attrs := []any{logattr.KeyDeviceName, name}
	if g := b.outGuard[name]; g != nil && g.read != nil {
		read = *g.read
		vol, state = b.checkOutputGuard(name, read)
		// Without a read (no guard, e.g. a snapshot) there is no volume to log.
		attrs = append(attrs, logattr.KeyVolume, percent(read))
	}
	b.mu.Unlock()
	b.log.Debug("output kept by a control; checking the volume last read",
		append(attrs, logattr.KeyGuardState, state)...)
	if k == nil {
		return nil // the guards start again on a reconnect
	}
	return b.restoreOutput(k, name, chans, read, vol)
}

// logStaleAnswer logs, at debug, a KeepOutput or ReleaseOutput the guard
// ignored because the adapter reported a newer identity of the output since:
// only the newest answer may resume or end defense, and the log shows which
// answer lost to which identity (CTRL-06, OUT-13, ADR 0027).
func (b *Backend) logStaleAnswer(action, name string, answer, current uint64) {
	b.log.Debug("output ownership answer ignored; a newer identity was reported",
		logattr.KeyDeviceName, name, logattr.KeyAction, action,
		logattr.KeyOwnerAnswerGeneration, answer, logattr.KeyOwnerGeneration, current)
}

// restoreOutput sets vol again on an output whose read volume the guard
// rejected; nil sets nothing (OUT-05, ADR 0027).
func (b *Backend) restoreOutput(k *conn, name string, channels int, read float64, vol *float64) error {
	if vol == nil {
		return nil
	}
	b.log.Debug("audio server changed the volume of a new output; setting it again",
		logattr.KeyDeviceName, name, logattr.KeyVolume, percent(read), logattr.KeyWanted, percent(*vol))
	if err := b.setOutputVolume(k, name, channels, *vol); err != nil && !IsGone(err) {
		return err
	}
	return nil
}

// setOutputVolume writes an output device's volume, through outputWrite in
// tests, and logs the server's answer at debug: a write the server accepted
// shows as success, so a volume that still reads otherwise was changed
// afterwards, not lost on the way (OUT-01, OUT-13, ADR 0027).
//
// A write the server accepted is also the guard's last read: it runs with
// wire held, so no read can come in between, and a later restore is read
// again. Without it, a KeepOutput right after the mixer's first volume
// compared the read from before the write and set the volume twice (OUT-05,
// CTRL-06, ADR 0027). A failed write keeps the read it had.
//
// Accepted is not applied, though: within the guard's window the output is
// read again after outputRecheck, which sets a lost write again (#166).
func (b *Backend) setOutputVolume(k *conn, name string, channels int, v float64) error {
	var err error
	if b.outputWrite != nil {
		err = b.outputWrite(name, channels, v)
	} else {
		err = k.setSinkVolume(name, channels, v)
	}
	attrs := []any{logattr.KeyDeviceName, name, logattr.KeyAudioChannels, channels,
		logattr.KeyWanted, percent(v), logattr.KeyAudioResult, "success"}
	guarded := false
	if err != nil {
		attrs[len(attrs)-1] = "failed"
		attrs = append(attrs, logattr.Error(logattr.ErrAudioApply, err))
	} else {
		b.mu.Lock()
		if g := b.outGuard[name]; g != nil {
			g.read = &v
			guarded = true
		}
		b.mu.Unlock()
	}
	b.log.Debug("output volume request completed", attrs...)
	if guarded {
		// A full queue drops the read again; reported changes are still defended.
		time.AfterFunc(outputRecheck, func() {
			select {
			case b.recheck <- name:
			default:
			}
		})
	}
	return err
}

// logOutputRead logs, at debug, each read of an output device: its identity,
// volume and what the new-output guard made of it, including reads it skipped
// and why (OUT-05, OUT-13, ADR 0027).
func (b *Backend) logOutputRead(d SinkInfo, state string) {
	attrs := []any{logattr.KeyDeviceIndex, d.Index, logattr.KeyDeviceName, d.Name,
		logattr.KeyDeviceDescription, d.Description, logattr.KeyAudioChannels, d.Channels,
		logattr.KeyVolume, percent(d.Volume), logattr.KeyGuardState, state}
	b.mu.Lock()
	if g := b.outGuard[d.Name]; g != nil {
		attrs = append(attrs, logattr.KeyGuardRemaining, max(0, time.Until(g.until).Milliseconds()))
		if g.volume != nil {
			attrs = append(attrs, logattr.KeyWanted, percent(*g.volume))
		}
	}
	b.mu.Unlock()
	b.log.Debug("output device read", attrs...)
}

// outsideStreamMute reports whether a stream's mute changing to muted was made
// by someone else: not the value Apptrol just set (muteWant), and not while
// the new-stream guard is putting back what the server restored (PRIO-03).
func (b *Backend) outsideStreamMute(id uint32, muted bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if g := b.guards[id]; g != nil && time.Now().Before(g.until) {
		return false
	}
	return outsideMute(b.muteWant, id, muted)
}

// outsideDeviceMute is outsideStreamMute for capture devices.
func (b *Backend) outsideDeviceMute(name string, muted bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if g := b.devGuard[name]; g != nil && time.Now().Before(g.until) {
		return false
	}
	return outsideMute(b.devMuteWant, name, muted)
}

// Apply carries out a volume or mute action: of a stream, an input, or an
// output device (SetOutputVolume, OUT-01), and ReleaseOutput and KeepOutput,
// which only change what the new-output guard defends and need no connection
// (CTRL-06, OUT-05). Other actions are ignored.
// Errors for streams that just ended can be checked with IsGone.
func (b *Backend) Apply(a mixer.Action) error {
	switch a.(type) {
	case mixer.SetStreamMute, mixer.SetDeviceMute, mixer.SetStreamVolume, mixer.SetDeviceVolume,
		mixer.SetOutputVolume, mixer.ReleaseOutput, mixer.KeepOutput:
		// A volume waits for a guard write in progress, and the guard then
		// sees the new value: the latest slider position wins (CTRL-04,
		// CTRL-05, PRIO-03, #158; OUT-01, OUT-05, ADR 0027).
		if b.beforeWire != nil {
			b.beforeWire()
		}
		b.wire.Lock()
		defer b.wire.Unlock()
	}
	if a, ok := a.(mixer.KeepOutput); ok {
		return b.keepOutput(a.Device, a.Generation)
	}
	b.mu.Lock()
	k := b.cur
	var chans int
	var staleRelease *uint64 // the newest generation, if a release came for an older one
	switch a := a.(type) {
	case mixer.SetStreamVolume:
		chans = b.channels[a.StreamID]
		if g := b.guards[a.StreamID]; g != nil {
			v := a.Volume
			g.volume = &v
		}
	case mixer.SetStreamMute:
		if g := b.guards[a.StreamID]; g != nil {
			m := a.Muted
			g.muted = &m
		}
		if b.muteWant != nil {
			b.muteWant[a.StreamID] = muteWant{muted: a.Muted, until: time.Now().Add(ownMute)}
		}
	case mixer.SetDeviceVolume:
		chans = b.devChans[a.Device]
		if g := b.devGuard[a.Device]; g != nil {
			v := a.Volume
			g.volume = &v
		}
	case mixer.SetDeviceMute:
		if g := b.devGuard[a.Device]; g != nil {
			m := a.Muted
			g.muted = &m
		}
		if b.devMuteWant != nil {
			b.devMuteWant[a.Device] = muteWant{muted: a.Muted, until: time.Now().Add(ownMute)}
		}
	case mixer.SetOutputVolume:
		chans = b.outChans[a.Device]
		if g := b.outGuard[a.Device]; g != nil {
			v := a.Volume
			g.volume = &v
		}
	case mixer.ReleaseOutput:
		// No control holds the output any more: its guard stops defending
		// Apptrol's volume, and only sets one again if a control takes the
		// output back within the window (OUT-01, CTRL-06, ADR 0027).
		if g := b.outGuard[a.Device]; g != nil {
			if g.generation == a.Generation {
				g.answered = true
				g.volume = nil
			} else {
				current := g.generation
				staleRelease = &current
			}
		}
	}
	b.mu.Unlock()
	if a, ok := a.(mixer.ReleaseOutput); ok && staleRelease != nil {
		b.logStaleAnswer("ReleaseOutput", a.Device, a.Generation, *staleRelease)
	}
	if k == nil {
		switch a.(type) {
		case mixer.SetStreamVolume, mixer.SetStreamMute, mixer.SetDeviceVolume, mixer.SetDeviceMute, mixer.SetOutputVolume:
			return ErrNotConnected
		}
		return nil
	}

	switch a := a.(type) {
	case mixer.SetStreamVolume:
		if chans == 0 {
			s, err := k.stream(a.StreamID)
			if err != nil {
				return err
			}
			chans = s.Channels
		}
		return b.setStreamVolume(k, a.StreamID, chans, a.Volume)
	case mixer.SetStreamMute:
		return k.setStreamMute(a.StreamID, a.Muted)
	case mixer.SetDeviceVolume:
		if chans == 0 {
			d, _, err := k.sourceByName(a.Device)
			if err != nil {
				return err
			}
			chans = d.Channels
		}
		return b.setSourceVolume(k, a.Device, chans, a.Volume)
	case mixer.SetDeviceMute:
		return k.setSourceMute(a.Device, a.Muted)
	case mixer.SetOutputVolume:
		if chans == 0 {
			d, err := k.sinkByName(a.Device)
			if err != nil {
				return err
			}
			chans = d.Channels
		}
		return b.setOutputVolume(k, a.Device, chans, a.Volume)
	}
	return nil
}

// setStreamVolume writes a stream's volume, through streamWrite in tests
// (CTRL-04, #158).
func (b *Backend) setStreamVolume(k *conn, id uint32, channels int, v float64) error {
	if b.streamWrite != nil {
		return b.streamWrite(id, channels, v)
	}
	return k.setStreamVolume(id, channels, v)
}

// setSourceVolume writes an input's volume, through sourceWrite in tests
// (CTRL-05, #158).
func (b *Backend) setSourceVolume(k *conn, name string, channels int, v float64) error {
	if b.sourceWrite != nil {
		return b.sourceWrite(name, channels, v)
	}
	return k.setSourceVolume(name, channels, v)
}

func (b *Backend) setConn(k *conn) {
	b.mu.Lock()
	b.cur = k
	b.mu.Unlock()
}

// percent is a volume for the log: percent of normal, rounded.
func percent(v float64) int { return int(math.Round(v * 100)) }
