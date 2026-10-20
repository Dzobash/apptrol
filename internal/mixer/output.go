package mixer

import (
	"log/slog"
	"sort"
	"strings"

	"github.com/Dzobash/apptrol/internal/logattr"
)

// ---- output devices (OUT-*, ADR 0027) ---------------------------------------
//
// An output target names one output device by match. Its slider or knob sets
// that device's volume (OUT-01); nothing else touches outputs yet: no mute,
// no solo, and talk-over and app solo never do (OUT-07, OUT-11). Mute and
// output solo follow (#85).
//
//	slider7 at 64 ──► "Speakers" (match "speaker") ──► SetOutputVolume 50 %
//	                  every stream on it plays at its own volume × 50 %

// chooseReason says why outputs are chosen again, for what is logged when a
// target is left without a device.
type chooseReason int

const (
	chooseConnect chooseReason = iota // a (re)connect to the audio server
	chooseReload                      // a new configuration
	chooseChange                      // a device appeared, changed or went away
)

// OutputDevice returns the name of the output device chosen for an output
// target, or "" if none matches (OUT-12, for `apptrol list`).
func (m *Mixer) OutputDevice(targetID string) string { return m.outputFor[targetID] }

// isOutputColumn reports whether the column's slider holds an output.
func (m *Mixer) isOutputColumn(col int) bool {
	id, ok := m.setup.Assignments[Control{Slider, col}]
	return ok && m.setup.Targets[id].Kind == Output
}

// outputTargets lists the output targets on a control, in control order.
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
	m.sharedOutputs = map[string]string{}
	m.chooseOutputs(a, chooseConnect)
	for _, n := range sortedDeviceNames(m.outputs) {
		m.logIdleOutput(a, m.outputs[n])
	}
}

// outputAdded keeps an output device that appeared or was renamed; it may
// match an output target now, or no longer (OUT-02, OUT-05, ADR 0027).
func (m *Mixer) outputAdded(a *actions, d Device) {
	m.outputs[d.Name] = d
	m.chooseOutputs(a, chooseChange)
	m.logIdleOutput(a, d)
}

// outputRemoved forgets an output device that is gone; its target takes the
// next device that matches, or does nothing until one appears (OUT-02).
func (m *Mixer) outputRemoved(a *actions, name string) {
	delete(m.outputs, name)
	m.chooseOutputs(a, chooseChange)
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
// alone until the control is moved when it is not (OUT-05, PRIO-04). Outputs
// have no mute yet, so nothing is released when a device stops being chosen.
func (m *Mixer) chooseOutputs(a *actions, why chooseReason) {
	names := sortedDeviceNames(m.outputs)
	chosen := map[string]string{}
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
	}
	m.outputFor = chosen
	m.logSharedOutputs(a)
}

// applyOutputVolume sets an output device to its control's position, up to the
// target's max_volume (OUT-01, CTRL-02, CTRL-03). An unknown position leaves
// the device's volume alone (PRIO-04).
func (m *Mixer) applyOutputVolume(a *actions, id, dev string) {
	if v, ok := m.positions[m.controlOf[id]]; ok {
		a.add(SetOutputVolume{Device: dev, Volume: m.volume(id, v)})
	}
}

// logSharedOutputs logs, once when it begins, each output device that two or
// more controls set: allowed, and each control sets the volume when it is
// moved (OUT-04, ADR 0027 point 4). It is logged again only when the set of
// controls changes.
func (m *Mixer) logSharedOutputs(a *actions) {
	controls := map[string][]string{}
	var order []string
	for _, id := range m.outputTargets() {
		dev, ok := m.outputFor[id]
		if !ok {
			continue
		}
		if len(controls[dev]) == 0 {
			order = append(order, dev)
		}
		controls[dev] = append(controls[dev], m.controlOf[id].String())
	}
	shared := map[string]string{}
	for _, dev := range order {
		if len(controls[dev]) < 2 {
			continue
		}
		list := strings.Join(controls[dev], ", ")
		shared[dev] = list
		if m.sharedOutputs[dev] == list {
			continue
		}
		first, _ := ParseControl(controls[dev][0])
		a.notice(slog.LevelInfo, "two controls set the same output",
			m.about(first, logattr.KeyDeviceName, dev, logattr.KeyDeviceControls, list)...)
	}
	m.sharedOutputs = shared
}

// reapplyOutputs sets the volume again of every output that kept its device
// across a reload but moved to another control or got another max_volume, as
// for inputs (OUT-01, CFG-06). before holds the devices chosen before.
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
// "" if none matches (CFG-05 for inputs, OUT-02 for outputs).
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
// the first by name (CFG-05, OUT-02).
func sortedDeviceNames(devices map[string]Device) []string {
	names := make([]string, 0, len(devices))
	for n := range devices {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
