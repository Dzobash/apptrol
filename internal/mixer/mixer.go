package mixer

import (
	"log/slog"
	"math"
	"sort"
	"strings"

	"github.com/Dzobash/apptrol/internal/logattr"
)

// Mixer is Apptrol's state machine. It is not safe for concurrent use: the
// service's single event loop owns it (ADR 0015).
type Mixer struct {
	setup     Setup
	controlOf map[string]Control  // target id -> its control
	fragments map[string][]string // target id -> lower-case match fragments

	positions map[Control]int  // known physical positions (PRIO-04: absent = unknown)
	muted     map[Control]bool // user mutes: M (MUTE-01) or from outside Apptrol (MUTE-07)
	solo      int              // soloed column, 0 = none (SOLO-*)
	held      map[LED]bool     // M and S buttons held in a held mode (INPUT-*); never saved
	stopping  bool             // Shutdown ran: talk-over is over (INPUT-07)

	streams   map[uint32]*streamInfo
	devices   map[string]Device // by name
	deviceFor map[string]string // input target id -> chosen device name

	players        map[string]*playerInfo // media players by bus name (MEDIA-01, MEDIA-03)
	playSeq        int                    // counts players starting to play (lastPlaying)
	deviceMuteSent map[string]bool        // last mute sent per device
	leds           map[LED]bool           // last LED state sent
}

type streamInfo struct {
	Stream
	target    string // assigned target id, "" if none
	muteSent  bool
	muteKnown bool
}

// New creates a mixer for setup, restoring saved state (STATE-03, STATE-04).
// Saved positions and mutes of controls that are no longer assigned are
// dropped (STATE-07), and so is a solo whose column no longer holds an app.
func New(setup Setup, saved State) *Mixer {
	m := &Mixer{
		positions:      map[Control]int{},
		muted:          map[Control]bool{},
		held:           map[LED]bool{},
		streams:        map[uint32]*streamInfo{},
		devices:        map[string]Device{},
		deviceFor:      map[string]string{},
		players:        map[string]*playerInfo{},
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
		if _, ok := m.setup.Assignments[c]; ok && on && c.Valid() {
			m.muted[c] = true
		}
	}
	if id, ok := m.setup.Assignments[Control{Slider, saved.Solo}]; ok && m.setup.Targets[id].Kind == App {
		m.solo = saved.Solo
	}
	m.dropHoldToTalkMutes()
	return m
}

func (m *Mixer) setSetup(s Setup) {
	m.setup = Setup{Layout: s.Layout, Targets: map[string]Target{}, Assignments: map[Control]string{}, Buttons: map[LED]Button{},
		MediaPlayer: s.MediaPlayer, Launchers: map[LED]Launch{}}
	for l, b := range s.Buttons {
		m.setup.Buttons[l] = b
	}
	for l, launch := range s.Launchers {
		m.setup.Launchers[l] = launch
	}
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
// assigned controls only (STATE-07), and the soloed column (STATE-04).
func (m *Mixer) Snapshot() State {
	s := State{Positions: map[Control]int{}, Muted: map[Control]bool{}, Solo: m.solo}
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

// Shutdown ends solo and held states and returns the actions that undo them,
// so no app stays muted or turned down while Apptrol is not running (SVC-07,
// INPUT-07). The audio server remembers mutes per app, so without this an app
// silenced by solo would stay muted even after Apptrol exits. Take the
// Snapshot before calling Shutdown: the saved state keeps the solo, and the
// next start restores it (STATE-04).
//
// A hold-to-talk input stays muted: a microphone that turns live by itself
// would send what the user says without them knowing (ADR 0020).
func (m *Mixer) Shutdown() []Action {
	var a actions
	before := m.talkOver()
	undo := m.solo != 0 || len(m.held) > 0 || before.on
	m.solo = 0
	m.endHeld(&a, "stopping", func(LED) bool { return true })
	m.stopping = true
	for _, b := range m.holdToTalkButtons() {
		a.notice(slog.LevelInfo, "hold-to-talk input stays muted while Apptrol is stopped",
			m.about(Control{Slider, b.Column}, logattr.KeyButton, b.String())...)
	}
	if undo {
		m.applyChange(&a, before, Control{})
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
	case ButtonReleased:
		m.buttonReleased(&a, e)
	case TransportPressed:
		m.transportPressed(&a, e.Button)
	case ControllerConnected:
		m.syncLEDs(&a, true) // LED-07
	case SystemResumed:
		m.syncLEDs(&a, true) // LED-09
	case ControllerDisconnected:
		// The releases of held buttons will never arrive (INPUT-07).
		before := m.talkOver()
		m.endHeld(&a, "controller_disconnected", func(LED) bool { return true })
		m.applyChange(&a, before, Control{})
	case AudioSnapshot:
		m.audioSnapshot(&a, e)
		// A restart of PipeWire can reset the controller's LEDs: the audio
		// server may open the controller's MIDI port too (LED-07).
		m.syncLEDs(&a, true)
	case StreamAdded:
		m.streamAdded(&a, e.Stream)
	case StreamRemoved:
		delete(m.streams, e.ID)
	case DeviceAdded:
		m.devices[e.Device.Name] = e.Device
		m.chooseDevices(&a)
	case StreamMuteChanged:
		m.streamMuteChanged(&a, e)
	case StreamCorkChanged:
		m.streamCorkChanged(&a, e)
	case DeviceMuteChanged:
		m.deviceMuteChanged(&a, e)
	case DeviceRemoved:
		delete(m.devices, e.Name)
		delete(m.deviceMuteSent, e.Name)
		m.chooseDevices(&a)
	case ConfigChanged:
		m.configChanged(&a, e.Setup)
	case PlayerSnapshot, PlayerAdded, PlayerRemoved, PlayerStatusChanged:
		m.playersChanged(&a, e)
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
				m.applyStreamVolume(a, s) // CTRL-02, CTRL-04; during talk-over INPUT-06
			}
		}
		if m.talkingOver() {
			m.applyMutes(a) // a first move ends talk-over's mute of an unknown position (INPUT-04)
		}
	case Input:
		if dev, ok := m.deviceFor[id]; ok {
			a.add(SetDeviceVolume{Device: dev, Volume: vol}) // CTRL-05
		}
	}
	attrs := []any{logattr.KeyVolume, int(math.Round(vol * 100))}
	if t.Kind == App && m.talkingOver() {
		attrs = append(attrs, logattr.KeyTalkOver, m.talkOverPercent())
	}
	a.notice(slog.LevelDebug, "volume changed", m.about(e.Control, attrs...)...)
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
	button := LED{Button: e.Button, Column: e.Column}.String()
	if m.isInputColumn(e.Column) {
		switch b := (LED{Button: e.Button, Column: e.Column}); {
		case e.Button == ButtonS, e.Button == ButtonM && m.holdToTalk(e.Column):
			m.holdPressed(a, b) // INPUT-01, SOLO-07
			return
		}
	}
	switch e.Button {
	case ButtonM:
		m.toggleMute(a, e.Column, button)
	case ButtonS:
		m.toggleSolo(a, e.Column, button)
	case ButtonR:
		m.rPressed(a, e.Column, button) // MEDIA-07
	}
}

// about returns the log attributes that say which control a record is about:
// the layout, the control and, if the control is assigned, the app on it
// (LOG-14). extra attributes follow.
func (m *Mixer) about(c Control, extra ...any) []any {
	layout := m.setup.Layout
	if layout == "" {
		layout = "default"
	}
	attrs := []any{logattr.KeyLayout, layout, logattr.KeyControl, c.String()}
	if id, ok := m.setup.Assignments[c]; ok {
		t := m.setup.Targets[id]
		attrs = append(attrs, logattr.KeyAppID, id, logattr.KeyApp, t.Name, logattr.KeyAppType, t.Kind.String())
	}
	return append(attrs, extra...)
}

// aboutTarget is about for the control target id is on.
func (m *Mixer) aboutTarget(id string, extra ...any) []any {
	return m.about(m.controlOf[id], extra...)
}

// toggleMute flips the user mute of the slider column's target (MUTE-01).
func (m *Mixer) toggleMute(a *actions, col int, button string) {
	c := Control{Slider, col}
	_, ok := m.setup.Assignments[c]
	if !ok {
		a.notice(slog.LevelDebug, "button has no function: no app on this column", m.about(c, logattr.KeyButton, button)...)
		return // MUTE-03
	}
	before := m.talkOver()
	m.muted[c] = !m.muted[c]
	if !m.muted[c] {
		delete(m.muted, c)
	}
	msg := "unmuted"
	if m.muted[c] {
		msg = "muted"
	}
	a.notice(slog.LevelInfo, msg, m.about(c, logattr.KeyButton, button)...)
	m.applyChange(a, before, c) // MUTE-04, MUTE-05; talk-over may follow M (INPUT-04)
	a.add(StateChanged{})
}

// toggleSolo implements the exclusive solo toggle (SOLO-01 … SOLO-07).
func (m *Mixer) toggleSolo(a *actions, col int, button string) {
	c := Control{Slider, col}
	id, ok := m.setup.Assignments[c]
	if !ok || m.setup.Targets[id].Kind != App {
		// S on an empty column does nothing; on an input column it is a held mode (SOLO-07)
		a.notice(slog.LevelDebug, "button has no function: solo needs an app on this column", m.about(c, logattr.KeyButton, button)...)
		return
	}
	switch {
	case m.solo == col:
		m.solo = 0 // SOLO-05
		a.notice(slog.LevelInfo, "solo off", m.about(c, logattr.KeyButton, button)...)
	case m.solo != 0:
		prev := Control{Slider, m.solo}
		m.solo = col // SOLO-04
		a.notice(slog.LevelInfo, "solo moved", m.about(c, logattr.KeyButton, button,
			logattr.KeySoloFrom, prev.String(), logattr.KeySoloFromApp, m.setup.Targets[m.setup.Assignments[prev]].Name)...)
	default:
		m.solo = col // SOLO-01
		a.notice(slog.LevelInfo, "solo on", m.about(c, logattr.KeyButton, button)...)
	}
	m.applyMutes(a)
	m.syncLEDs(a, false)
	a.add(StateChanged{})
}

// streamMuteChanged follows a mute change made outside Apptrol (MUTE-07): on
// a slider column it becomes the column's user mute, for every stream of the
// app. Solo still silences other apps: an app unmuted outside Apptrol while
// another is soloed is muted again.
func (m *Mixer) streamMuteChanged(a *actions, e StreamMuteChanged) {
	s, ok := m.streams[e.ID]
	if !ok || s.target == "" {
		return // not ours (CTRL-06)
	}
	if s.muteKnown && s.muteSent == e.Muted {
		return // already so
	}
	s.muteSent, s.muteKnown = e.Muted, true
	m.followMute(a, s.target, e.Muted, streamAttrs(s.Stream)...)
}

// streamCorkChanged keeps track of whether a stream's app has paused it
// (MEDIA-10), and logs it.
func (m *Mixer) streamCorkChanged(a *actions, e StreamCorkChanged) {
	s, known := m.streams[e.ID]
	if !known {
		return
	}
	s.Corked = e.Corked
	msg := "stream uncorked"
	if e.Corked {
		msg = "stream corked"
	}
	// Every stream is logged, also those on no control: the log then shows
	// that Apptrol sees a stream even when it matches no app (LOG-15).
	if s.target == "" {
		a.notice(slog.LevelDebug, msg, streamAttrs(s.Stream)...)
		return
	}
	a.notice(slog.LevelDebug, msg, m.about(m.controlOf[s.target], streamAttrs(s.Stream)...)...)
}

// deviceMuteChanged is streamMuteChanged for input devices.
func (m *Mixer) deviceMuteChanged(a *actions, e DeviceMuteChanged) {
	for _, id := range m.inputTargets() {
		if m.deviceFor[id] != e.Name {
			continue
		}
		if sent, ok := m.deviceMuteSent[e.Name]; ok && sent == e.Muted {
			return
		}
		m.deviceMuteSent[e.Name] = e.Muted
		if c := m.controlOf[id]; c.Kind == Slider && m.holdToTalk(c.Column) {
			// Apptrol alone decides in hold-to-talk: undo the change (INPUT-03).
			a.notice(slog.LevelInfo, "changed outside Apptrol, but hold-to-talk decides",
				m.aboutTarget(id, logattr.KeyDeviceName, e.Name, logattr.KeyMuted, e.Muted)...)
			m.applyDeviceMute(a, e.Name, m.effectiveMute(id))
			return
		}
		m.followMute(a, id, e.Muted, logattr.KeyDeviceName, e.Name)
		return
	}
}

// followMute takes over a mute state set outside Apptrol for target id.
func (m *Mixer) followMute(a *actions, id string, muted bool, attrs ...any) {
	c := m.controlOf[id]
	before := m.talkOver()
	changed := false
	if m.muted[c] != muted { // on a knob too: it has no M button, but can be muted outside (MUTE-07)
		if muted {
			m.muted[c] = true
		} else {
			delete(m.muted, c)
		}
		changed = true
	}
	msg := "unmuted outside Apptrol"
	if muted {
		msg = "muted outside Apptrol"
	}
	if !muted && m.effectiveMute(id) {
		msg = "unmuted outside Apptrol, but solo keeps it silent"
	}
	a.notice(slog.LevelInfo, msg, m.aboutTarget(id, attrs...)...)
	m.applyChange(a, before, c) // the app's other streams follow; solo silences again
	if changed {
		a.add(StateChanged{})
	}
}

// effectiveMute: a target is muted when user-muted, or when solo is on and it is
// another app (SOLO-02, SOLO-03, SOLO-06, MUTE-04).
// An input is also muted while coughing, and in push-to-talk while its
// button is not held (INPUT-05). An app whose control has not been moved yet
// is muted during talk-over, as its volume is unknown (INPUT-04).
func (m *Mixer) effectiveMute(id string) bool {
	c := m.controlOf[id]
	if m.muted[c] { // M on a slider column, or a mute from outside Apptrol (MUTE-07)
		return true
	}
	switch m.setup.Targets[id].Kind {
	case App:
		if m.solo != 0 && c != (Control{Slider, m.solo}) {
			return true
		}
		if _, known := m.positions[c]; !known && m.talkingOver() {
			return true
		}
	case Input:
		if c.Kind == Slider && m.heldMute(c.Column) {
			return true
		}
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

// ---- microphone buttons and held states (INPUT-*, ADR 0020) -----------------

// isInputColumn reports whether the column's slider holds an input.
func (m *Mixer) isInputColumn(col int) bool {
	id, ok := m.setup.Assignments[Control{Slider, col}]
	return ok && m.setup.Targets[id].Kind == Input
}

// mode returns what M or S does on an input column: the configured mode or
// the default (M: mute; S: cough, or off when M is in hold-to-talk). Other
// buttons and columns have no mode (ModeOff).
func (m *Mixer) mode(b LED) Mode {
	if b.Transport != 0 || !m.isInputColumn(b.Column) {
		return ModeOff
	}
	mode := m.setup.Buttons[b].Mode
	switch {
	case mode != ModeDefault:
		return mode
	case b.Button == ButtonM:
		return ModeMute
	case b.Button == ButtonS && m.mode(LED{Button: ButtonM, Column: b.Column}) == ModeMute:
		return ModeCough
	}
	return ModeOff
}

// holdToTalk reports whether M on the column is in hold-to-talk mode.
func (m *Mixer) holdToTalk(col int) bool {
	return m.mode(LED{Button: ButtonM, Column: col}) == ModeHoldToTalk
}

// heldMute reports whether the input column is muted by a held state: S held
// in cough mode, or hold-to-talk with M not held (INPUT-02, INPUT-03).
func (m *Mixer) heldMute(col int) bool {
	s := LED{Button: ButtonS, Column: col}
	if m.held[s] && m.mode(s) == ModeCough {
		return true
	}
	return m.holdToTalk(col) && !m.held[LED{Button: ButtonM, Column: col}]
}

// talkOverState is whether talk-over is active, its volume in percent, and
// the input column it comes from (for the log).
type talkOverState struct {
	on      bool
	percent int
	col     int
}

// same reports whether talk-over is unchanged for the apps.
func (t talkOverState) same(u talkOverState) bool { return t.on == u.on && t.percent == u.percent }

// talkOver returns the talk-over state (INPUT-04): active while S in
// talk-over mode is held, or, with talk_over on M, while the input is live
// through M. A cough does not end it. With talk-over active from several
// inputs, the lowest volume applies. It is never active once Apptrol stops.
func (m *Mixer) talkOver() talkOverState {
	var st talkOverState
	if m.stopping {
		return st
	}
	for col := 1; col <= NumColumns; col++ {
		if !m.isInputColumn(col) {
			continue
		}
		c := Control{Slider, col}
		mb, sb := LED{Button: ButtonM, Column: col}, LED{Button: ButtonS, Column: col}
		active := m.held[sb] && m.mode(sb) == ModeTalkOver
		if m.setup.Buttons[mb].TalkOver {
			if m.holdToTalk(col) {
				active = active || m.held[mb]
			} else {
				active = active || !m.muted[c]
			}
		}
		if !active {
			continue
		}
		p := m.setup.Targets[m.setup.Assignments[c]].TalkOverVolume
		if !st.on || p < st.percent {
			st = talkOverState{on: true, percent: p, col: col}
		}
	}
	return st
}

func (m *Mixer) talkingOver() bool    { return m.talkOver().on }
func (m *Mixer) talkOverPercent() int { return m.talkOver().percent }

// holdToTalkButtons lists every M button in hold-to-talk mode, in column order.
func (m *Mixer) holdToTalkButtons() []LED {
	var out []LED
	for col := 1; col <= NumColumns; col++ {
		if m.holdToTalk(col) {
			out = append(out, LED{Button: ButtonM, Column: col})
		}
	}
	return out
}

// dropHoldToTalkMutes drops the M mute of columns in hold-to-talk mode: the
// button no longer toggles there, so the mute could never be undone (INPUT-03).
func (m *Mixer) dropHoldToTalkMutes() {
	for _, b := range m.holdToTalkButtons() {
		delete(m.muted, Control{Slider, b.Column})
	}
}

// holdPressed starts a held state: M in hold-to-talk, S in cough or
// talk-over (INPUT-01).
func (m *Mixer) holdPressed(a *actions, b LED) {
	c := Control{Slider, b.Column}
	mode := m.mode(b)
	if mode == ModeOff {
		a.notice(slog.LevelDebug, "button has no function: its mode is off", m.about(c, logattr.KeyButton, b.String())...)
		return
	}
	if m.held[b] {
		return // a repeated press without a release in between
	}
	before := m.talkOver()
	m.held[b] = true
	switch mode {
	case ModeHoldToTalk:
		a.notice(slog.LevelInfo, "hold-to-talk live", m.about(c, logattr.KeyButton, b.String())...)
	case ModeCough:
		a.notice(slog.LevelInfo, "cough on", m.about(c, logattr.KeyButton, b.String())...)
	}
	m.applyChange(a, before, c)
}

// buttonReleased ends a held state (INPUT-07, INPUT-08). Releases of other
// buttons mean nothing.
func (m *Mixer) buttonReleased(a *actions, e ButtonReleased) {
	b := LED{Button: e.Button, Column: e.Column}
	if !m.held[b] {
		return
	}
	before := m.talkOver()
	delete(m.held, b)
	c := Control{Slider, b.Column}
	switch m.mode(b) {
	case ModeHoldToTalk:
		a.notice(slog.LevelInfo, "hold-to-talk muted", m.about(c, logattr.KeyButton, b.String())...)
	case ModeCough:
		a.notice(slog.LevelInfo, "cough off", m.about(c, logattr.KeyButton, b.String())...)
	}
	m.applyChange(a, before, c)
}

// endHeld ends the held states for which end is true, without their release
// (INPUT-07). The caller applies the result.
func (m *Mixer) endHeld(a *actions, reason string, end func(LED) bool) {
	for _, b := range sortedLEDs(m.held) {
		if end(b) {
			delete(m.held, b)
			a.notice(slog.LevelInfo, "held state ended",
				m.about(Control{Slider, b.Column}, logattr.KeyButton, b.String(), logattr.KeyHeldReason, reason)...)
		}
	}
}

// applyChange carries out a change of mutes or held states: mutes, LEDs and,
// when talk-over started, ended or changed its volume, every app's volume.
// c is the control the change came from, for the log; without one (a
// disconnect, a reload, stopping) the log names the input column of talk-over.
func (m *Mixer) applyChange(a *actions, before talkOverState, c Control) {
	after := m.talkOver()
	logAt := func(st talkOverState) Control {
		if c.Valid() {
			return c
		}
		return Control{Slider, st.col}
	}
	switch {
	case after.same(before):
	case !after.on:
		a.notice(slog.LevelInfo, "talk-over off", m.about(logAt(before), logattr.KeyTalkOver, before.percent)...)
	default:
		a.notice(slog.LevelInfo, "talk-over on", m.about(logAt(after), logattr.KeyTalkOver, after.percent)...)
		if !before.on {
			m.logUnknownPositions(a)
		}
	}
	m.applyMutes(a)
	if !after.same(before) {
		m.applyVolumes(a)
	}
	m.syncLEDs(a, false)
}

// logUnknownPositions names each app that talk-over mutes because its control
// has not been moved yet (INPUT-04).
func (m *Mixer) logUnknownPositions(a *actions) {
	for _, c := range m.assignedControls() {
		id := m.setup.Assignments[c]
		if _, known := m.positions[c]; !known && m.setup.Targets[id].Kind == App {
			a.notice(slog.LevelInfo, "talk-over mutes app: its control has not been moved yet", m.about(c)...)
		}
	}
}

func sortedLEDs(set map[LED]bool) []LED {
	out := make([]LED, 0, len(set))
	for l := range set {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Column != out[j].Column {
			return out[i].Column < out[j].Column
		}
		return out[i].Button < out[j].Button
	})
	return out
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
				r = m.rLit(col, id)                // LED-03, MEDIA-09
			case Input:
				s, r = true, true         // LED-04: input columns are lit…
				mu = !m.effectiveMute(id) // …and M is lit only while the input is live
			}
		}
		out = append(out,
			SetLED{LED{Button: ButtonS, Column: col}, s},
			SetLED{LED{Button: ButtonM, Column: col}, mu},
			SetLED{LED{Button: ButtonR, Column: col}, r}) // LED-03
	}
	for _, t := range AllTransport {
		on := t == Play && m.mediaKeyPlaying()           // ▶ lit while the media-key player plays
		out = append(out, SetLED{LED{Transport: t}, on}) // LED-06
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
		a.notice(slog.LevelInfo, "stream matched", m.about(c, append(streamAttrs(s), logattr.KeyStreamCorked, s.Corked)...)...)
	}
	m.applyStream(a, info)
}

// applyStream gives a stream its control's position (PRIO-03, or leaves the
// volume alone when unknown, PRIO-04) and its mute (PRIO-05).
// During talk-over, a new stream gets the lower of the talk-over level and its
// control's position (INPUT-06).
func (m *Mixer) applyStream(a *actions, s *streamInfo) {
	m.applyStreamVolume(a, s)
	m.applyStreamMute(a, s)
}

// applyStreamVolume sets a stream's volume from its control's position, held
// down to the talk-over level while talk-over is on (INPUT-04). A stream whose
// control has not been moved is left alone (PRIO-04).
func (m *Mixer) applyStreamVolume(a *actions, s *streamInfo) {
	if s.target == "" {
		return
	}
	v, ok := m.positions[m.controlOf[s.target]]
	if !ok {
		return
	}
	vol := m.volume(s.target, v)
	if m.talkingOver() {
		vol = math.Min(vol, float64(m.talkOverPercent())/100) // never up
	}
	a.add(SetStreamVolume{StreamID: s.ID, Volume: vol})
}

// applyVolumes sets the volume of every app stream; used when talk-over
// starts or ends.
func (m *Mixer) applyVolumes(a *actions) {
	for _, s := range m.sortedStreams() {
		m.applyStreamVolume(a, s)
	}
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
			a.notice(slog.LevelWarn, "no input device matches", m.aboutTarget(id,
				logattr.KeyMatch, strings.Join(m.setup.Targets[id].Match, ", "))...)
		}
	}
	if st := m.talkOver(); st.on {
		// talk_over on M in mute mode: live since the start (INPUT-04)
		a.notice(slog.LevelInfo, "talk-over on", m.about(Control{Slider, st.col}, logattr.KeyTalkOver, st.percent)...)
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
			a.notice(slog.LevelInfo, "input matched", m.aboutTarget(id,
				logattr.KeyDeviceDescription, m.devices[pick].Description, logattr.KeyDeviceName, pick)...)
			if len(matches) > 1 {
				a.notice(slog.LevelWarn, "several input devices match; using the first (make the match more specific: `apptrol list` shows each device's unique name)",
					m.aboutTarget(id, logattr.KeyDeviceName, pick, logattr.KeyDeviceMatches, strings.Join(matches, ", "))...)
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
	talkBefore := m.talkOver()
	oldModes := map[LED]Mode{} // modes of held buttons
	for b := range m.held {
		oldModes[b] = m.mode(b)
	}
	oldMax := map[string]float64{}
	for id := range m.setup.Targets {
		oldMax[id] = m.maxVolume(id)
	}
	m.setSetup(s)
	// A held state ends when its button's mode or its column's input changes (INPUT-07).
	m.endHeld(a, "config_changed", func(b LED) bool {
		c := Control{Slider, b.Column}
		return old[c] != m.setup.Assignments[c] || oldModes[b] != m.mode(b)
	})
	m.dropHoldToTalkMutes()
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
				a.notice(slog.LevelInfo, "stream matched", m.aboutTarget(info.target, streamAttrs(info.Stream)...)...)
			}
			m.applyStream(a, info)
		}
	}
	m.rematchPlayers(a) // a player may belong to another control now (MEDIA-03)
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
	m.applyChange(a, talkBefore, Control{}) // talk-over may have ended, or changed its volume
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

// streamAttrs are the log attributes of a stream; the binary only when the
// app reports one.
func streamAttrs(s Stream) []any {
	attrs := []any{logattr.KeyStreamID, s.ID, logattr.KeyStreamName, s.AppName}
	if s.Binary != "" {
		attrs = append(attrs, logattr.KeyExecutableName, s.Binary)
	}
	return attrs
}
