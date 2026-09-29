package mixer

import (
	"log/slog"
	"math"
	"sort"
	"strings"
)

// Mixer is Apptrol's state machine. It is not safe for concurrent use: the
// service's single event loop owns it (ADR 0015).
type Mixer struct {
	setup     Setup
	controlOf map[string]Control  // target id -> its control
	fragments map[string][]string // target id -> lower-case match fragments

	positions map[Control]int  // known physical positions (PRIO-04: absent = unknown)
	muted     map[Control]bool // user mutes of slider controls (MUTE-01)
	solo      int              // soloed column, 0 = none (SOLO-*)

	streams   map[uint32]*streamInfo
	devices   map[string]Device // by name
	deviceFor map[string]string // input target id -> chosen device name

	deviceMuteSent map[string]bool // last mute sent per device
	leds           map[LED]bool    // last LED state sent
}

type streamInfo struct {
	Stream
	target    string // assigned target id, "" if none
	muteSent  bool
	muteKnown bool
}

// New creates a mixer for setup, restoring saved state (STATE-03). Saved
// positions and mutes of controls that are no longer assigned are dropped
// (STATE-07).
func New(setup Setup, saved State) *Mixer {
	m := &Mixer{
		positions:      map[Control]int{},
		muted:          map[Control]bool{},
		streams:        map[uint32]*streamInfo{},
		devices:        map[string]Device{},
		deviceFor:      map[string]string{},
		deviceMuteSent: map[string]bool{},
		leds:           map[LED]bool{},
	}
	m.setSetup(setup)
	for c, v := range saved.Positions {
		if _, ok := m.setup.Assignments[c]; ok && c.Valid() {
			m.positions[c] = clamp(v)
		}
	}
	for c, on := range saved.Muted {
		if _, ok := m.setup.Assignments[c]; ok && on && c.Kind == Slider && c.Valid() {
			m.muted[c] = true
		}
	}
	return m
}

func (m *Mixer) setSetup(s Setup) {
	m.setup = Setup{Targets: map[string]Target{}, Assignments: map[Control]string{}}
	m.controlOf = map[string]Control{}
	m.fragments = map[string][]string{}
	for id, t := range s.Targets {
		m.setup.Targets[id] = t
		for _, f := range t.Match {
			if f = strings.ToLower(strings.TrimSpace(f)); f != "" {
				m.fragments[id] = append(m.fragments[id], f)
			}
		}
	}
	for c, id := range s.Assignments {
		if _, ok := s.Targets[id]; ok && c.Valid() {
			m.setup.Assignments[c] = id
			m.controlOf[id] = c
		}
	}
}

// Snapshot returns the state to persist (STATE-01): positions and user mutes of
// assigned controls only (STATE-07). Solo is not included (STATE-04).
func (m *Mixer) Snapshot() State {
	s := State{Positions: map[Control]int{}, Muted: map[Control]bool{}}
	for c, v := range m.positions {
		if _, ok := m.setup.Assignments[c]; ok {
			s.Positions[c] = v
		}
	}
	for c, on := range m.muted {
		if _, ok := m.setup.Assignments[c]; ok && on {
			s.Muted[c] = true
		}
	}
	return s
}

// StreamTarget returns the id of the target a known stream is matched to, or
// "" if the stream is unknown or matches nothing.
func (m *Mixer) StreamTarget(id uint32) string {
	if s, ok := m.streams[id]; ok {
		return s.target
	}
	return ""
}

// InputDevice returns the name of the capture device chosen for an input
// target, or "" if none matches.
func (m *Mixer) InputDevice(targetID string) string { return m.deviceFor[targetID] }

// ControlOf returns the control a target is assigned to.
func (m *Mixer) ControlOf(targetID string) (Control, bool) {
	c, ok := m.controlOf[targetID]
	return c, ok
}

// Shutdown ends solo and returns the actions that undo its silencing, so no app
// stays muted by a solo that is never saved (SVC-07, STATE-04). The audio
// server remembers mutes per app, so without this an app silenced by solo
// would stay muted even after Apptrol and the app restart.
func (m *Mixer) Shutdown() []Action {
	var a actions
	if m.solo != 0 {
		m.solo = 0
		m.applyMutes(&a)
		m.syncLEDs(&a, false)
	}
	return a
}

// Handle applies one event and returns the actions to carry out.
func (m *Mixer) Handle(ev Event) []Action {
	var a actions
	switch e := ev.(type) {
	case ControlMoved:
		m.controlMoved(&a, e)
	case ButtonPressed:
		m.buttonPressed(&a, e)
	case TransportPressed:
		// BTN-01: reserved for later phases.
	case ControllerConnected:
		m.syncLEDs(&a, true) // LED-07
	case AudioSnapshot:
		m.audioSnapshot(&a, e)
	case StreamAdded:
		m.streamAdded(&a, e.Stream)
	case StreamRemoved:
		delete(m.streams, e.ID)
	case DeviceAdded:
		m.devices[e.Device.Name] = e.Device
		m.chooseDevices(&a)
	case DeviceRemoved:
		delete(m.devices, e.Name)
		delete(m.deviceMuteSent, e.Name)
		m.chooseDevices(&a)
	case ConfigChanged:
		m.configChanged(&a, e.Setup)
	}
	return a
}

// actions collects the result of one Handle call.
type actions []Action

func (a *actions) add(x Action) { *a = append(*a, x) }

func (a *actions) notice(level slog.Level, msg string, attrs ...any) {
	a.add(Notice{Level: level, Msg: msg, Attrs: attrs})
}

// ---- controls ----------------------------------------------------------------

func (m *Mixer) controlMoved(a *actions, e ControlMoved) {
	if !e.Control.Valid() {
		return
	}
	v := clamp(e.Value)
	m.positions[e.Control] = v
	id, ok := m.setup.Assignments[e.Control]
	if !ok {
		return // CTRL-01: unassigned controls do nothing
	}
	vol := m.volume(id, v)
	t := m.setup.Targets[id]
	switch t.Kind {
	case App:
		for _, s := range m.sortedStreams() {
			if s.target == id {
				a.add(SetStreamVolume{StreamID: s.ID, Volume: vol}) // CTRL-02, CTRL-04
			}
		}
	case Input:
		if dev, ok := m.deviceFor[id]; ok {
			a.add(SetDeviceVolume{Device: dev, Volume: vol}) // CTRL-05
		}
	}
	a.notice(slog.LevelDebug, "volume", "control", e.Control.String(), "app", id, "percent", int(math.Round(vol*100)))
	a.add(StateChanged{})
}

// Volume maps a control value (0–127) linearly to 0–1 (CTRL-02).
func Volume(v int) float64 { return float64(clamp(v)) / MaxValue }

// volume is the volume for a control value on the control of target id: 0 at
// the bottom, the target's MaxVolume at the top (CTRL-02, CTRL-03).
func (m *Mixer) volume(id string, v int) float64 { return Volume(v) * m.maxVolume(id) }

// maxVolume returns a target's MaxVolume, 1 if unset, never above MaxBoost.
func (m *Mixer) maxVolume(id string) float64 {
	mv := m.setup.Targets[id].MaxVolume
	switch {
	case mv <= 0:
		return 1
	case mv > MaxBoost:
		return MaxBoost
	}
	return mv
}

// Percent is Volume as a rounded percentage, for logs and display.
func Percent(v int) int { return (clamp(v)*100 + MaxValue/2) / MaxValue }

func clamp(v int) int {
	switch {
	case v < 0:
		return 0
	case v > MaxValue:
		return MaxValue
	}
	return v
}

// ---- buttons -----------------------------------------------------------------

func (m *Mixer) buttonPressed(a *actions, e ButtonPressed) {
	if e.Column < 1 || e.Column > NumColumns {
		return
	}
	switch e.Button {
	case ButtonM:
		m.toggleMute(a, e.Column)
	case ButtonS:
		m.toggleSolo(a, e.Column)
	case ButtonR:
		// BTN-01: reserved.
	}
}

// toggleMute flips the user mute of the slider column's target (MUTE-01).
func (m *Mixer) toggleMute(a *actions, col int) {
	c := Control{Slider, col}
	id, ok := m.setup.Assignments[c]
	if !ok {
		return // MUTE-03
	}
	m.muted[c] = !m.muted[c]
	if !m.muted[c] {
		delete(m.muted, c)
	}
	t := m.setup.Targets[id]
	if m.muted[c] {
		a.notice(slog.LevelInfo, "muted", "app", t.Name, "control", c.String())
	} else {
		a.notice(slog.LevelInfo, "unmuted", "app", t.Name, "control", c.String())
	}
	m.applyMutes(a) // MUTE-04, MUTE-05
	m.syncLEDs(a, false)
	a.add(StateChanged{})
}

// toggleSolo implements the exclusive solo toggle (SOLO-01 … SOLO-07).
func (m *Mixer) toggleSolo(a *actions, col int) {
	id, ok := m.setup.Assignments[Control{Slider, col}]
	if !ok || m.setup.Targets[id].Kind != App {
		return // SOLO-07: S on an input column (or an empty one) does nothing
	}
	if m.solo == col {
		m.solo = 0 // SOLO-05
		a.notice(slog.LevelInfo, "solo off", "app", m.setup.Targets[id].Name)
	} else {
		m.solo = col // SOLO-01, SOLO-04
		a.notice(slog.LevelInfo, "solo on", "app", m.setup.Targets[id].Name, "control", Control{Slider, col}.String())
	}
	m.applyMutes(a)
	m.syncLEDs(a, false)
}

// effectiveMute: a target is muted when user-muted, or when solo is on and it is
// another app (SOLO-02, SOLO-03, SOLO-06, MUTE-04).
func (m *Mixer) effectiveMute(id string) bool {
	c := m.controlOf[id]
	if c.Kind == Slider && m.muted[c] {
		return true
	}
	if m.solo != 0 && m.setup.Targets[id].Kind == App && c != (Control{Slider, m.solo}) {
		return true
	}
	return false
}

// applyMutes sends every mute that differs from what was last sent.
func (m *Mixer) applyMutes(a *actions) {
	for _, s := range m.sortedStreams() {
		m.applyStreamMute(a, s)
	}
	for _, id := range m.inputTargets() {
		if dev, ok := m.deviceFor[id]; ok {
			m.applyDeviceMute(a, dev, m.effectiveMute(id))
		}
	}
}

func (m *Mixer) applyStreamMute(a *actions, s *streamInfo) {
	want := false
	if s.target != "" {
		want = m.effectiveMute(s.target)
	}
	if s.target == "" && (!s.muteKnown || !s.muteSent) {
		return // never touch unassigned streams (CTRL-06) unless releasing our own mute
	}
	if s.muteKnown && s.muteSent == want {
		return
	}
	s.muteSent, s.muteKnown = want, true
	a.add(SetStreamMute{StreamID: s.ID, Muted: want})
}

func (m *Mixer) applyDeviceMute(a *actions, dev string, want bool) {
	if sent, ok := m.deviceMuteSent[dev]; ok && sent == want {
		return
	}
	m.deviceMuteSent[dev] = want
	a.add(SetDeviceMute{Device: dev, Muted: want})
}

// ---- LEDs --------------------------------------------------------------------

// ledStates computes every LED (LED-01 … LED-06).
func (m *Mixer) ledStates() []SetLED {
	var out []SetLED
	for col := 1; col <= NumColumns; col++ {
		s, mu, r := false, false, false
		if id, ok := m.setup.Assignments[Control{Slider, col}]; ok {
			switch m.setup.Targets[id].Kind {
			case App:
				s = m.solo == col                  // LED-01
				mu = m.muted[Control{Slider, col}] // LED-02
			case Input:
				s, r = true, true                   // LED-04: input columns are lit…
				mu = !m.muted[Control{Slider, col}] // …and M goes dark when muted
			}
		}
		out = append(out,
			SetLED{LED{Button: ButtonS, Column: col}, s},
			SetLED{LED{Button: ButtonM, Column: col}, mu},
			SetLED{LED{Button: ButtonR, Column: col}, r}) // LED-03
	}
	for _, t := range AllTransport {
		out = append(out, SetLED{LED{Transport: t}, false}) // LED-06
	}
	return out
}

// syncLEDs sends LEDs that changed, or all of them when force is set (LED-07).
func (m *Mixer) syncLEDs(a *actions, force bool) {
	for _, l := range m.ledStates() {
		if prev, ok := m.leds[l.LED]; ok && prev == l.On && !force {
			continue
		}
		m.leds[l.LED] = l.On
		a.add(l)
	}
}

// ---- streams -----------------------------------------------------------------

// matchStream returns the assigned app target a stream belongs to. Several
// matches are resolved by control order: sliders 1–8, then knobs 1–8.
func (m *Mixer) matchStream(s Stream) string {
	name, bin := strings.ToLower(s.AppName), strings.ToLower(s.Binary)
	for _, c := range m.assignedControls() {
		id := m.setup.Assignments[c]
		if m.setup.Targets[id].Kind != App {
			continue
		}
		for _, f := range m.fragments[id] {
			if strings.Contains(name, f) || strings.Contains(bin, f) { // CFG-04
				return id
			}
		}
	}
	return ""
}

func (m *Mixer) streamAdded(a *actions, s Stream) {
	info, known := m.streams[s.ID]
	if !known {
		info = &streamInfo{}
		m.streams[s.ID] = info
	}
	oldTarget := info.target
	info.Stream = s
	info.target = m.matchStream(s)
	if known && oldTarget == info.target {
		return // property change without effect
	}
	if info.target != "" {
		c := m.controlOf[info.target]
		a.notice(slog.LevelInfo, "stream matched", "app", m.setup.Targets[info.target].Name,
			"control", c.String(), "stream", s.ID, "name", s.AppName)
	}
	m.applyStream(a, info)
}

// applyStream gives a stream its control's position (PRIO-03, or leaves the
// volume alone when unknown, PRIO-04) and its mute (PRIO-05).
func (m *Mixer) applyStream(a *actions, s *streamInfo) {
	if s.target != "" {
		if v, ok := m.positions[m.controlOf[s.target]]; ok {
			a.add(SetStreamVolume{StreamID: s.ID, Volume: m.volume(s.target, v)})
		}
	}
	m.applyStreamMute(a, s)
}

func (m *Mixer) audioSnapshot(a *actions, e AudioSnapshot) {
	m.streams = map[uint32]*streamInfo{}
	m.devices = map[string]Device{}
	m.deviceFor = map[string]string{}
	m.deviceMuteSent = map[string]bool{}
	for _, d := range e.Devices {
		m.devices[d.Name] = d
	}
	m.chooseDevices(a)
	for _, id := range m.inputTargets() {
		if _, ok := m.deviceFor[id]; !ok {
			a.notice(slog.LevelWarn, "no input device matches", "app", m.setup.Targets[id].Name,
				"match", strings.Join(m.setup.Targets[id].Match, ", "))
		}
	}
	for _, s := range e.Streams {
		m.streamAdded(a, s)
	}
}

// ---- devices -----------------------------------------------------------------

func (m *Mixer) inputTargets() []string {
	var ids []string
	for _, c := range m.assignedControls() {
		id := m.setup.Assignments[c]
		if m.setup.Targets[id].Kind == Input {
			ids = append(ids, id)
		}
	}
	return ids
}

func (m *Mixer) deviceMatches(id string, d Device) bool {
	name, desc := strings.ToLower(d.Name), strings.ToLower(d.Description)
	for _, f := range m.fragments[id] {
		if strings.Contains(name, f) || strings.Contains(desc, f) { // CFG-05
			return true
		}
	}
	return false
}

// chooseDevices picks one device per input target: the current one while it
// still matches, otherwise the first match by name (CFG-05). Newly chosen
// devices get the control's position and mute.
func (m *Mixer) chooseDevices(a *actions) {
	names := make([]string, 0, len(m.devices))
	for n := range m.devices {
		names = append(names, n)
	}
	sort.Strings(names)

	chosen := map[string]string{}
	for _, id := range m.inputTargets() {
		var matches []string
		for _, n := range names {
			if m.deviceMatches(id, m.devices[n]) {
				matches = append(matches, n)
			}
		}
		if len(matches) == 0 {
			continue
		}
		pick := matches[0]
		cur := m.deviceFor[id]
		for _, n := range matches {
			if n == cur {
				pick = cur
			}
		}
		chosen[id] = pick
		if pick != cur {
			t := m.setup.Targets[id]
			a.notice(slog.LevelInfo, "input matched", "app", t.Name, "device", m.devices[pick].Description,
				"control", m.controlOf[id].String())
			if len(matches) > 1 {
				a.notice(slog.LevelWarn, "several input devices match; using the first", "app", t.Name,
					"using", pick, "matches", strings.Join(matches, ", "))
			}
			if v, ok := m.positions[m.controlOf[id]]; ok {
				a.add(SetDeviceVolume{Device: pick, Volume: m.volume(id, v)})
			}
			m.applyDeviceMute(a, pick, m.effectiveMute(id))
		}
	}
	// Release devices that are no longer controlled but were muted by us.
	for id, dev := range m.deviceFor {
		if chosen[id] == dev {
			continue
		}
		if _, still := m.devices[dev]; still && m.deviceMuteSent[dev] && !contains(chosen, dev) {
			m.applyDeviceMute(a, dev, false)
		}
		if !contains(chosen, dev) {
			delete(m.deviceMuteSent, dev)
		}
	}
	m.deviceFor = chosen
}

func contains(m map[string]string, v string) bool {
	for _, x := range m {
		if x == v {
			return true
		}
	}
	return false
}

// ---- configuration -----------------------------------------------------------

// configChanged applies a new setup (CFG-06). Positions stay (they are
// physical). A user mute stays only if the control keeps the same target; solo
// ends if its column changes.
func (m *Mixer) configChanged(a *actions, s Setup) {
	old := m.setup.Assignments
	oldControlOf := m.controlOf
	oldMax := map[string]float64{}
	for id := range m.setup.Targets {
		oldMax[id] = m.maxVolume(id)
	}
	m.setSetup(s)
	// maxChanged: the target's max_volume changed, so its volume must be set again.
	maxChanged := func(id string) bool { return id != "" && oldMax[id] != m.maxVolume(id) }
	for c := range m.muted {
		if old[c] != m.setup.Assignments[c] {
			delete(m.muted, c)
		}
	}
	if m.solo != 0 {
		c := Control{Slider, m.solo}
		id, ok := m.setup.Assignments[c]
		if !ok || old[c] != id || m.setup.Targets[id].Kind != App {
			m.solo = 0
		}
	}
	for _, info := range m.sortedStreams() {
		before, beforeControl := info.target, oldControlOf[info.target]
		info.target = m.matchStream(info.Stream)
		// Re-apply when the stream's app, the app's control or its max_volume changed.
		if info.target != before || (info.target != "" && m.controlOf[info.target] != beforeControl) || maxChanged(info.target) {
			if info.target != "" {
				a.notice(slog.LevelInfo, "stream matched", "app", m.setup.Targets[info.target].Name,
					"control", m.controlOf[info.target].String(), "stream", info.ID, "name", info.AppName)
			}
			m.applyStream(a, info)
		}
	}
	devBefore := map[string]string{}
	for id, dev := range m.deviceFor {
		devBefore[id] = dev
	}
	m.chooseDevices(a)
	// An input that kept its device but moved to another control, or got
	// another max_volume, gets its volume set again.
	for id, dev := range m.deviceFor {
		c := m.controlOf[id]
		if devBefore[id] == dev && (oldControlOf[id] != c || maxChanged(id)) {
			if v, ok := m.positions[c]; ok {
				a.add(SetDeviceVolume{Device: dev, Volume: m.volume(id, v)})
			}
		}
	}
	m.applyMutes(a)
	m.syncLEDs(a, false)
	a.add(StateChanged{})
}

// ---- helpers -----------------------------------------------------------------

// assignedControls lists assigned controls in a fixed order: sliders 1–8, then knobs 1–8.
func (m *Mixer) assignedControls() []Control {
	var out []Control
	for _, k := range []ControlKind{Slider, Knob} {
		for col := 1; col <= NumColumns; col++ {
			c := Control{k, col}
			if _, ok := m.setup.Assignments[c]; ok {
				out = append(out, c)
			}
		}
	}
	return out
}

func (m *Mixer) sortedStreams() []*streamInfo {
	out := make([]*streamInfo, 0, len(m.streams))
	for _, s := range m.streams {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
