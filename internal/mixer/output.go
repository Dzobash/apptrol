package mixer

import (
	"log/slog"
	"sort"

	"github.com/Dzobash/apptrol/internal/logattr"
)

// ---- output devices (OUT-*, ADR 0027) ---------------------------------------

// outputsSnapshot replaces every output device the mixer knew, on a
// (re)connect to the audio server (OUT-12, SVC-04, ADR 0027).
func (m *Mixer) outputsSnapshot(a *actions, outputs []Device) {
	m.outputs = map[string]Device{}
	for _, d := range outputs {
		m.outputs[d.Name] = d
	}
	names := make([]string, 0, len(m.outputs))
	for n := range m.outputs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		m.logIdleOutput(a, m.outputs[n])
	}
}

// outputAdded keeps an output device that appeared or was renamed (OUT-12,
// ADR 0027).
func (m *Mixer) outputAdded(a *actions, d Device) {
	m.outputs[d.Name] = d
	m.logIdleOutput(a, d)
}

// logIdleOutput logs an output device that no control acts on, at debug: the
// log then shows that Apptrol sees the device, and its exact name for a
// match list, even before any output is configured (OUT-13, LOG-15,
// ADR 0027).
func (m *Mixer) logIdleOutput(a *actions, d Device) {
	a.notice(slog.LevelDebug, "output device not on a control",
		logattr.KeyDeviceName, d.Name, logattr.KeyDeviceDescription, d.Description)
}
