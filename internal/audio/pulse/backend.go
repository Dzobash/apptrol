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

	"github.com/Dzobash/apptrol/internal/mixer"
)

// ErrNotConnected is returned by Apply while there is no connection to the
// audio server. The service re-applies everything after reconnecting (SVC-04).
var ErrNotConnected = errors.New("not connected to the audio server")

// errLost ends a session when the connection breaks.
var errLost = errors.New("connection lost")

// newGuard is how long after a stream or capture device appears Apptrol
// defends the volume and mute it set. PipeWire (WirePlumber) restores remembered
// settings shortly after something appears; when that lands after Apptrol's
// own change, it would undo it (PRIO-03). Within this window, Apptrol sets its
// values again.
const newGuard = 3 * time.Second

// guard records what Apptrol set on a new stream or device.
type guard struct {
	until  time.Time
	volume *float64
	muted  *bool
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

// Backend keeps a connection to the audio server, reports streams and capture
// devices as mixer events, and carries out volume and mute actions.
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
	guards   map[uint32]*guard // new streams
	devGuard map[string]*guard // new capture devices
}

// New returns a Backend. server is a PulseAudio server string; "" uses the
// default (PULSE_SERVER, or the user's pipewire-pulse socket).
func New(log *slog.Logger, server string) *Backend {
	return &Backend{log: log, server: server, retryMin: 500 * time.Millisecond, retryMax: 5 * time.Second}
}

// Run connects and keeps the connection until ctx is canceled. After each
// (re)connect it sends a full mixer.AudioSnapshot, then single changes:
// StreamAdded, StreamRemoved, DeviceAdded, DeviceRemoved. If the server goes
// away, Run logs it and reconnects (SVC-04). It returns ctx's error.
func (b *Backend) Run(ctx context.Context, out chan<- mixer.Event) error {
	wait := b.retryMin
	failed := false // the last attempt failed; don't repeat the error log
	for {
		k, err := dial(b.server)
		if err != nil {
			if !failed {
				b.log.Error("cannot connect to the audio server; retrying", "err", err)
				failed = true
			} else {
				b.log.Debug("audio server still unreachable", "err", err, "retry_in", wait)
			}
		} else {
			wait = b.retryMin
			err = b.session(ctx, k, out)
			b.setConn(nil)
			k.shutdown()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			b.log.Error("lost the connection to the audio server; reconnecting", "err", err)
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
	b.log.Info("connected to the audio server", "server", fmt.Sprintf("%s %s", info.PackageName, info.PackageVersion))

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
		t = tracker{streams: map[uint32]mixer.Stream{}, devices: map[uint32]mixer.Device{}}
		chans, devChans := map[uint32]int{}, map[string]int{}
		for _, s := range streams {
			t.streams[s.ID] = s.Stream
			chans[s.ID] = s.Channels
		}
		for _, d := range sources {
			t.devices[d.Index] = d.Device
			devChans[d.Name] = d.Channels
		}
		b.mu.Lock()
		b.cur, b.channels, b.devChans = k, chans, devChans
		b.guards, b.devGuard = map[uint32]*guard{}, map[string]*guard{}
		b.mu.Unlock()
		return send(Snapshot(streams, sources))
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
			ev, err := b.handle(k, &t, e)
			if err != nil {
				return err
			}
			if ev != nil {
				if err := send(ev); err != nil {
					return err
				}
			}
		}
	}
}

// handle turns one change notification into a mixer event, or nil.
func (b *Backend) handle(k *conn, t *tracker, e proto.SubscribeEvent) (mixer.Event, error) {
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
				if known && old == s.Stream {
					return nil, b.defend(k, s) // a volume change and the like
				}
				t.streams[s.ID] = s.Stream
				return mixer.StreamAdded{Stream: s.Stream}, nil
			case !IsGone(err):
				return nil, err
			}
			// Gone before it could be read: treat as removed.
		}
		if !known {
			return nil, nil
		}
		delete(t.streams, e.Index)
		b.mu.Lock()
		delete(b.channels, e.Index)
		delete(b.guards, e.Index)
		b.mu.Unlock()
		return mixer.StreamRemoved{ID: e.Index}, nil

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
				if known && old == d.Device {
					return nil, b.defendDevice(k, d)
				}
				t.devices[e.Index] = d.Device
				return mixer.DeviceAdded{Device: d.Device}, nil
			case !IsGone(err):
				return nil, err
			}
		}
		if !known {
			return nil, nil
		}
		delete(t.devices, e.Index)
		b.mu.Lock()
		delete(b.devChans, old.Name)
		delete(b.devGuard, old.Name)
		b.mu.Unlock()
		return mixer.DeviceRemoved{Name: old.Name}, nil
	}
	return nil, nil
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
			"stream", s.ID, "app", s.AppName, "volume", s.Volume, "want", *vol)
		if err := k.setStreamVolume(s.ID, s.Channels, *vol); err != nil && !IsGone(err) {
			return err
		}
	}
	if muted != nil {
		b.log.Debug("audio server changed the mute of a new stream; setting it again",
			"stream", s.ID, "app", s.AppName, "muted", s.Muted, "want", *muted)
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
			"device", d.Name, "volume", d.Volume, "want", *vol)
		if err := k.setSourceVolume(d.Name, d.Channels, *vol); err != nil && !IsGone(err) {
			return err
		}
	}
	if muted != nil {
		b.log.Debug("audio server changed the mute of a new input; setting it again",
			"device", d.Name, "muted", d.Muted, "want", *muted)
		if err := k.setSourceMute(d.Name, *muted); err != nil && !IsGone(err) {
			return err
		}
	}
	return nil
}

// Apply carries out a volume or mute action. Other actions are ignored.
// Errors for streams that just ended can be checked with IsGone.
func (b *Backend) Apply(a mixer.Action) error {
	b.mu.Lock()
	k := b.cur
	var chans int
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
	}
	b.mu.Unlock()
	if k == nil {
		switch a.(type) {
		case mixer.SetStreamVolume, mixer.SetStreamMute, mixer.SetDeviceVolume, mixer.SetDeviceMute:
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
		return k.setStreamVolume(a.StreamID, chans, a.Volume)
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
		return k.setSourceVolume(a.Device, chans, a.Volume)
	case mixer.SetDeviceMute:
		return k.setSourceMute(a.Device, a.Muted)
	}
	return nil
}

func (b *Backend) setConn(k *conn) {
	b.mu.Lock()
	b.cur = k
	b.mu.Unlock()
}
