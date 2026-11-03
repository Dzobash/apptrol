package mixer

import (
	"log/slog"
	"strings"

	"github.com/Dzobash/apptrol/internal/logattr"
)

// defaultInputChanged follows the server without carrying a live held press
// onto a different microphone (INPUT-09, INPUT-07, ADR 0038).
func (m *Mixer) defaultInputChanged(a *actions, name string) {
	if name == m.defaultInput {
		return
	}
	a.notice(slog.LevelInfo, "default input changed", logattr.KeyDeviceName, name,
		logattr.KeyDevicePreviousName, m.defaultInput)
	m.defaultInput = name
	m.chooseDevices(a, chooseChange)
}

// chooseDevices uses the first control for each microphone, as outputs do.
// Default inputs keep their volume until moved; mute follows the control.
// Held states end before writing the new microphone (INPUT-09, ADR 0038).
func (m *Mixer) chooseDevices(a *actions, why chooseReason) {
	before := m.talkOver()
	names := sortedDeviceNames(m.devices)
	chosen, owners, blocked := map[string]string{}, map[string]string{}, map[string]string{}
	for _, id := range m.inputTargets() {
		target := m.setup.Targets[id]
		cur := m.deviceFor[id]
		pick, matches := m.pickDevice(id, names, m.devices, cur)
		if target.FollowsDefault {
			pick, matches = "", nil
			if _, ok := m.devices[m.defaultInput]; ok {
				pick = m.defaultInput
			}
		}
		if pick == "" {
			if target.FollowsDefault && (cur != "" || m.blockedInputs[id] != "" || why != chooseChange) {
				a.notice(slog.LevelInfo, "no default input; the control does nothing until there is one", m.aboutTarget(id)...)
			}
			continue
		}
		if owner, exists := owners[pick]; exists {
			blocked[id] = pick
			if m.blockedInputs[id] != pick || m.deviceFor[owner] != pick || why == chooseReload {
				a.notice(slog.LevelInfo, "input is the same device as another input; the earlier control owns it",
					m.aboutTarget(id, logattr.KeyDeviceName, pick, logattr.KeyDeviceControls,
						m.controlOf[owner].String()+", "+m.controlOf[id].String())...)
			}
			continue
		}
		owners[pick], chosen[id] = id, pick
		if control, wasDefault := m.defaultInputHeld[pick]; target.FollowsDefault && contains(m.deviceFor, pick) && (!wasDefault || control != m.controlOf[id]) {
			a.add(ForgetDeviceVolume{Device: pick})
		}
		if pick != cur {
			selectedBy := "match"
			if target.FollowsDefault {
				selectedBy = "default"
			}
			a.notice(slog.LevelInfo, "input matched", m.aboutTarget(id, logattr.KeyDeviceName, pick,
				logattr.KeyDeviceDescription, m.devices[pick].Description, logattr.KeyDeviceSelectedBy, selectedBy)...)
			if len(matches) > 1 {
				a.notice(slog.LevelWarn, "several input devices match; using the first (make the match more specific: `apptrol list` shows each device's unique name)",
					m.aboutTarget(id, logattr.KeyDeviceName, pick, logattr.KeyDeviceMatches, strings.Join(matches, ", "))...)
			}
		}
	}
	m.endHeld(a, "input_changed", func(b LED) bool {
		id := m.setup.Assignments[Control{Slider, b.Column}]
		return m.deviceFor[id] != chosen[id]
	})
	// Apply the new owner's mute before releasing the old device so a held M
	// cannot briefly open the new microphone (INPUT-03, INPUT-09, ADR 0038).
	for _, id := range m.inputTargets() {
		dev, ok := chosen[id]
		if !ok || dev == m.deviceFor[id] {
			continue
		}
		if m.setup.Targets[id].FollowsDefault {
			a.notice(slog.LevelInfo, "default input keeps its volume until the control is moved",
				m.aboutTarget(id, logattr.KeyDeviceName, dev)...)
		} else {
			m.applyInputVolume(a, id, dev)
		}
		m.applyDeviceMute(a, dev, m.effectiveMute(id))
	}
	for _, dev := range names {
		if contains(m.deviceFor, dev) && !contains(chosen, dev) {
			m.applyDeviceMute(a, dev, false)
			delete(m.deviceMuteSent, dev)
			a.add(ReleaseDevice{Device: dev, Generation: m.devices[dev].Generation})
			a.notice(slog.LevelInfo, "input released; no control holds the device", logattr.KeyDeviceName, dev)
		}
	}
	m.deviceFor, m.blockedInputs = chosen, blocked
	m.defaultInputHeld = map[string]Control{}
	for id, dev := range chosen {
		if m.setup.Targets[id].FollowsDefault {
			m.defaultInputHeld[dev] = m.controlOf[id]
		}
	}
	// Reload compares talk-over before and after the whole configuration
	// change; only that caller should apply the transition (INPUT-04,
	// INPUT-09, CFG-06, ADR 0020, ADR 0038).
	if why != chooseReload {
		m.applyChange(a, before, Control{})
	}
}

// applyInputVolume leaves an unknown position alone (PRIO-04, INPUT-09, ADR 0038).
func (m *Mixer) applyInputVolume(a *actions, id, dev string) {
	if v, ok := m.positions[m.controlOf[id]]; ok {
		a.add(SetDeviceVolume{Device: dev, Volume: m.volume(id, v)})
	}
}

// answerDevice echoes input identities before guard defense resumes (INPUT-09, ADR 0038).
func (m *Mixer) answerDevice(a *actions, d Device) {
	if contains(m.deviceFor, d.Name) {
		a.add(KeepDevice{Device: d.Name, Generation: d.Generation})
	} else {
		a.add(ReleaseDevice{Device: d.Name, Generation: d.Generation})
	}
}
