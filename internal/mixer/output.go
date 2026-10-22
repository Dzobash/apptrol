package mixer

import (
	"log/slog"
	"slices"
	"sort"
	"strings"

	"github.com/Dzobash/apptrol/internal/logattr"
)

// ---- output devices (OUT-*, ADR 0027) ---------------------------------------
//
// An output target names one output device by match. Its slider or knob sets
// that device's volume (OUT-01), and M toggles its saved mute (OUT-06), lit
// as on an app column (OUT-10). Talk-over and solo never affect outputs, and
// S does nothing on an output column (OUT-07, OUT-11, ADR 0036).
//
//	slider7 at 64 ──► "Speakers" (match "speaker") ──► SetOutputVolume 50 %
//	                  every stream on it plays at its own volume × 50 %

// chooseReason says why outputs are chosen again, for what is logged when a
// target is left without a device: a warning at a connect or reload, info when
// a device goes (OUT-13, ADR 0027 point 12).
type chooseReason int

const (
	chooseConnect chooseReason = iota // a (re)connect to the audio server
	chooseReload                      // a new configuration
	chooseChange                      // a device appeared, changed or went away
)

// OutputDevice returns the name of the output device chosen for an output
// target, or "" if none matches (OUT-12, ADR 0027, for `apptrol list`).
func (m *Mixer) OutputDevice(targetID string) string { return m.outputFor[targetID] }

// isOutputColumn reports whether the column's slider holds an output
// (OUT-06, ADR 0027).
func (m *Mixer) isOutputColumn(col int) bool {
	id, ok := m.setup.Assignments[Control{Slider, col}]
	return ok && m.setup.Targets[id].Kind == Output
}

// outputTargets lists the output targets on a control, in control order;
// targets on no control are never chosen a device (OUT-01, CTRL-06, ADR 0027).
func (m *Mixer) outputTargets() []string {
	var ids []string
	for _, c := range m.assignedControls() {
		if id := m.setup.Assignments[c]; m.setup.Targets[id].Kind == Output {
			ids = append(ids, id)
		}
	}
	return ids
}

// outputsSnapshot replaces every output device the mixer knew, on a
// (re)connect to the audio server, and chooses every output's device anew:
// a device that is there again gets its control's known position (OUT-05,
// SVC-04, ADR 0027).
func (m *Mixer) outputsSnapshot(a *actions, outputs []Device) {
	m.outputs = map[string]Device{}
	for _, d := range outputs {
		m.outputs[d.Name] = d
	}
	m.outputFor = map[string]string{}
	m.blockedOutputs = map[string]string{}
	m.outputMuteSent = map[string]bool{}
	m.chooseOutputs(a, chooseConnect)
	for _, n := range sortedDeviceNames(m.outputs) {
		m.answerOutput(a, m.outputs[n], 0)
		m.logIdleOutput(a, m.outputs[n])
	}
}

// outputAdded keeps an output device that appeared or was renamed; it may
// match an output target now, or no longer (OUT-02, OUT-05, ADR 0027). One a
// control holds is kept with that read's generation, even if it also gets a
// volume; a volume write alone cannot acknowledge ownership (CTRL-06).
func (m *Mixer) outputAdded(a *actions, d Device) {
	m.outputs[d.Name] = d
	from := len(*a)
	m.chooseOutputs(a, chooseChange)
	m.answerOutput(a, d, from)
	m.logIdleOutput(a, d)
	m.syncLEDs(a, false) // ownership may have moved, and M's LED with it (OUT-10, ADR 0035)
}

// answerOutput echoes each reported generation without interpreting it. Even
// an unassigned output gets an answer; a release already emitted while choosing
// is not duplicated (CTRL-06, OUT-05, ADR 0015, ADR 0027).
func (m *Mixer) answerOutput(a *actions, d Device, from int) {
	if contains(m.outputFor, d.Name) {
		a.add(KeepOutput{Device: d.Name, Generation: d.Generation})
		return
	}
	if !slices.ContainsFunc((*a)[from:], func(x Action) bool {
		r, ok := x.(ReleaseOutput)
		return ok && r.Device == d.Name
	}) {
		a.add(ReleaseOutput{Device: d.Name, Generation: d.Generation})
	}
}

// outputRemoved forgets an output device that is gone; its target takes the
// next device that matches, or does nothing until one appears (OUT-02,
// ADR 0027).
func (m *Mixer) outputRemoved(a *actions, name string) {
	delete(m.outputs, name)
	delete(m.outputMuteSent, name)
	m.chooseOutputs(a, chooseChange)
	m.syncLEDs(a, false) // as in outputAdded (OUT-10, ADR 0035)
}

// logIdleOutput logs an output device that no control acts on, at debug: the
// log then shows that Apptrol sees the device, and its exact name for a
// match list, even before any output is configured (OUT-13, LOG-15,
// ADR 0027). A device an output target holds is logged as matched instead.
func (m *Mixer) logIdleOutput(a *actions, d Device) {
	if contains(m.outputFor, d.Name) {
		return
	}
	a.notice(slog.LevelDebug, "output device not on a control",
		logattr.KeyDeviceName, d.Name, logattr.KeyDeviceDescription, d.Description)
}

// chooseOutputs picks one device per output target: the current one while it
// still matches, otherwise the first match by name, case-insensitive on name
// or description (OUT-02, the same rule as for inputs, CFG-05). A newly
// chosen device gets its control's position when it is known, and is left
// alone until the control is moved when it is not (OUT-05, PRIO-04). A device
// that no target holds any more is released (ReleaseOutput, OUT-01, CTRL-06):
// its mute is released too. The first control owns a device when matches
// overlap; later controls cannot write it (OUT-04, ADR 0035).
func (m *Mixer) chooseOutputs(a *actions, why chooseReason) {
	names := sortedDeviceNames(m.outputs)
	chosen := map[string]string{}
	owners := map[string]string{}
	blocked := map[string]string{}
	for _, id := range m.outputTargets() {
		cur, had := m.outputFor[id]
		pick, matches := m.pickDevice(id, names, m.outputs, cur)
		switch {
		case pick == "" && (why == chooseConnect || why == chooseReload):
			a.notice(slog.LevelWarn, "no output device matches", m.aboutTarget(id,
				logattr.KeyMatch, strings.Join(m.setup.Targets[id].Match, ", "))...)
			continue
		case pick == "":
			if had {
				a.notice(slog.LevelInfo, "output device gone; the control does nothing until a device matches",
					m.aboutTarget(id, logattr.KeyDeviceName, cur)...)
			}
			continue
		}
		if owner, exists := owners[pick]; exists {
			blocked[id] = pick
			if m.blockedOutputs[id] != pick || m.outputFor[owner] != pick || why == chooseReload {
				a.notice(slog.LevelWarn, "output match ignored: an earlier control owns the device",
					m.aboutTarget(id, logattr.KeyDeviceName, pick,
						logattr.KeyDeviceControls, m.controlOf[owner].String()+", "+m.controlOf[id].String())...)
			}
			continue
		}
		owners[pick] = id
		chosen[id] = pick
		if pick == cur {
			continue
		}
		a.notice(slog.LevelInfo, "output matched", m.aboutTarget(id, logattr.KeyDeviceName, pick,
			logattr.KeyDeviceDescription, m.outputs[pick].Description, logattr.KeyDeviceSelectedBy, "match")...)
		if len(matches) > 1 {
			a.notice(slog.LevelWarn, "several output devices match; using the first (make the match more specific: apptrol list shows each device's unique name)",
				m.aboutTarget(id, logattr.KeyDeviceName, pick, logattr.KeyDeviceMatches, strings.Join(matches, ", "))...)
		}
		m.applyOutputVolume(a, id, pick)
		m.applyOutputMute(a, pick, m.effectiveMute(id))
	}
	m.releaseOutputs(a, chosen)
	m.outputFor = chosen
	m.blockedOutputs = blocked
}

// releaseOutputs releases each device that a target held before and none
// holds in chosen, by name order; one another control still holds is kept, and
// one that is gone has nothing left to release (OUT-01, CTRL-06, ADR 0027).
// Each release is logged at info, once, so the log explains why a volume set
// elsewhere now stays (OUT-13, ADR 0027 note of 2026-10-20).
func (m *Mixer) releaseOutputs(a *actions, chosen map[string]string) {
	var released []string
	for _, dev := range m.outputFor {
		if _, there := m.outputs[dev]; there && !contains(chosen, dev) && !slices.Contains(released, dev) {
			released = append(released, dev)
		}
	}
	sort.Strings(released)
	for _, dev := range released {
		m.applyOutputMute(a, dev, false)
		delete(m.outputMuteSent, dev)
		a.add(ReleaseOutput{Device: dev, Generation: m.outputs[dev].Generation})
		a.notice(slog.LevelInfo, "output released; no control holds the device",
			logattr.KeyDeviceName, dev)
	}
}

// applyOutputVolume sets an output device to its control's position, up to the
// target's max_volume (OUT-01, CTRL-02, CTRL-03). An unknown position leaves
// the device's volume alone (PRIO-04, ADR 0027).
func (m *Mixer) applyOutputVolume(a *actions, id, dev string) {
	if v, ok := m.positions[m.controlOf[id]]; ok {
		a.add(SetOutputVolume{Device: dev, Volume: m.volume(id, v)})
	}
}

// applyOutputMute avoids redundant writes to the owned device (OUT-06,
// PRIO-02, ADR 0027). Output and input names have separate mute caches.
func (m *Mixer) applyOutputMute(a *actions, dev string, want bool) {
	if sent, ok := m.outputMuteSent[dev]; ok && sent == want {
		return
	}
	m.outputMuteSent[dev] = want
	a.add(SetOutputMute{Device: dev, Muted: want})
}

// outputMuteChanged takes over an outside mute for the owning control only,
// including knobs (OUT-05, MUTE-07, ADR 0035).
func (m *Mixer) outputMuteChanged(a *actions, e OutputMuteChanged) {
	for _, id := range m.outputTargets() {
		if m.outputFor[id] != e.Name {
			continue
		}
		if sent, ok := m.outputMuteSent[e.Name]; ok && sent == e.Muted {
			return
		}
		m.outputMuteSent[e.Name] = e.Muted
		m.followMute(a, id, e.Muted, logattr.KeyDeviceName, e.Name)
		return
	}
}

// reapplyOutputs sets the volume again of every output that kept its device
// across a reload but moved to another control or got another max_volume, as
// for inputs (OUT-01, CFG-06, ADR 0027). before holds the devices chosen before.
func (m *Mixer) reapplyOutputs(a *actions, before map[string]string, oldControlOf map[string]Control, maxChanged func(string) bool) {
	for _, id := range m.outputTargets() {
		dev, ok := m.outputFor[id]
		if ok && before[id] == dev && (oldControlOf[id] != m.controlOf[id] || maxChanged(id)) {
			m.applyOutputVolume(a, id, dev)
		}
	}
}

// ---- devices: shared by inputs and outputs -----------------------------------

// pickDevice returns the devices among names (sorted) that match target id,
// and the one to use: cur while it still matches, otherwise the first by name;
// "" if none matches (CFG-05 for inputs, OUT-02 and ADR 0027 for outputs).
func (m *Mixer) pickDevice(id string, names []string, devices map[string]Device, cur string) (pick string, matches []string) {
	for _, n := range names {
		if m.deviceMatches(id, devices[n]) {
			matches = append(matches, n)
		}
	}
	if len(matches) == 0 {
		return "", nil
	}
	pick = matches[0]
	for _, n := range matches {
		if n == cur {
			pick = cur
		}
	}
	return pick, matches
}

// sortedDeviceNames returns the devices' names in order, so the first match is
// the first by name (CFG-05, OUT-02, ADR 0027).
func sortedDeviceNames(devices map[string]Device) []string {
	names := make([]string, 0, len(devices))
	for n := range devices {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
