package pulse

import (
	"time"

	"github.com/Dzobash/apptrol/internal/logattr"
	"github.com/Dzobash/apptrol/internal/mixer"
)

// sourceEvent holds new-device restores away from saved mutes and waits for
// ownership after identity changes, as outputs do (INPUT-09, ADR 0038).
// The caller holds wire; read is replaceable for deterministic tests (QA-11).
func (b *Backend) sourceEvent(k *conn, t *tracker, index uint32, removed bool, read func(uint32) (SourceInfo, bool, error)) ([]mixer.Event, error) {
	old, known := t.devices[index]
	if !removed {
		d, ok, err := read(index)
		if err != nil && !IsGone(err) {
			return nil, err
		}
		if err == nil && !ok {
			return nil, nil // monitors never become inputs (CFG-21, ADR 0026)
		}
		if err == nil {
			wasMuted := t.devMute[index]
			b.mu.Lock()
			b.devChans[d.Name] = d.Channels
			g := b.devGuard[d.Name]
			if !known {
				g = newGuardNow()
				b.devGuard[d.Name] = g
			}
			same := known && old.Name == d.Name && old.Description == d.Description
			if !same {
				b.inputGeneration++
				d.Generation = b.inputGeneration
				if g != nil {
					g.generation, g.answered = d.Generation, false
				}
			} else {
				d.Generation = old.Generation
			}
			hold := false
			if g != nil {
				volume, muted := d.Volume, d.Muted
				g.read, g.readMuted = &volume, &muted
				hold = time.Now().Before(g.until) && (g.muted != nil || g.released || !g.answered)
				// A read after the window is outside and cancels old mute debt;
				// defense must not overwrite it before reporting it (INPUT-09,
				// MUTE-07, ADR 0038).
				if hold {
					g.owedMute = g.muted != nil && *g.muted != d.Muted
				} else if wasMuted != d.Muted {
					g.owedMute = false
				}
			}
			b.mu.Unlock()
			t.devices[index], t.devMute[index] = d.Device, d.Muted
			if !same {
				var evs []mixer.Event
				if known && old.Name == d.Name && wasMuted != d.Muted && !hold && b.outsideDeviceMute(d.Name, d.Muted) {
					evs = append(evs, mixer.DeviceMuteChanged{Name: d.Name, Muted: d.Muted})
				}
				return append(evs, mixer.DeviceAdded{Device: d.Device}), nil
			}
			if err := b.defendDevice(k, d); err != nil {
				return nil, err
			}
			if hold {
				b.log.Debug("input mute change not taken over: the new-input guard holds the mute",
					logattr.KeyDeviceName, d.Name, logattr.KeyMuted, d.Muted)
				return nil, nil
			}
			if d.Muted != wasMuted && b.outsideDeviceMute(d.Name, d.Muted) {
				return []mixer.Event{mixer.DeviceMuteChanged{Name: d.Name, Muted: d.Muted}}, nil
			}
			return nil, nil
		}
	}
	if !known {
		return nil, nil
	}
	delete(t.devices, index)
	delete(t.devMute, index)
	b.mu.Lock()
	delete(b.devChans, old.Name)
	delete(b.devGuard, old.Name)
	delete(b.devMuteWant, old.Name)
	b.mu.Unlock()
	return []mixer.Event{mixer.DeviceRemoved{Name: old.Name}}, nil
}

// keepDevice answers only the newest identity, then settles its held restore
// even when the guard's window ended while waiting (INPUT-09, ADR 0038).
func (b *Backend) keepDevice(name string, generation uint64) error {
	b.mu.Lock()
	g := b.devGuard[name]
	if g == nil {
		b.mu.Unlock()
		return nil
	}
	if g.generation != generation {
		current := g.generation
		b.mu.Unlock()
		b.logInputAnswer("KeepDevice", name, generation, current)
		return nil
	}
	g.answered = true
	d := SourceInfo{Device: mixer.Device{Name: name}, Channels: b.devChans[name]}
	if g.read != nil {
		d.Volume = *g.read
	}
	if g.readMuted != nil {
		d.Muted = *g.readMuted
	}
	k := b.cur
	b.mu.Unlock()
	if k == nil {
		return nil
	}
	return b.defendDevice(k, d)
}

// logInputAnswer makes stale input decisions visible (INPUT-09, ADR 0038).
func (b *Backend) logInputAnswer(action, name string, answer, current uint64) {
	b.log.Debug("input ownership answer ignored; a newer identity was reported",
		logattr.KeyDeviceName, name, logattr.KeyAction, action,
		logattr.KeyOwnerAnswerGeneration, answer, logattr.KeyOwnerGeneration, current)
}

// setSourceMute attributes guard writes as Apptrol's echoes, too (INPUT-09,
// MUTE-07, ADR 0038). The caller holds wire.
func (b *Backend) setSourceMute(k *conn, name string, muted bool) error {
	b.mu.Lock()
	if b.devMuteWant != nil {
		b.devMuteWant[name] = muteWant{muted: muted, until: time.Now().Add(ownMute)}
	}
	b.mu.Unlock()
	if b.sourceMuteWrite != nil {
		return b.sourceMuteWrite(name, muted)
	}
	return k.setSourceMute(name, muted)
}
