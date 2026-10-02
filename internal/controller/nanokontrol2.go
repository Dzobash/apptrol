package controller

import "github.com/Dzobash/apptrol/internal/mixer"

// Map is a controller's layout in CC mode: which Control Change number each
// slider, knob and button sends, and which number lights its LED (the same
// one). All CC numbers for a controller are defined in one Map (HW-04).
type Map struct {
	Name      string
	Slider    [mixer.NumColumns]byte
	Knob      [mixer.NumColumns]byte
	S, M, R   [mixer.NumColumns]byte
	Transport map[mixer.TransportButton]byte
	// NoLED lists transport buttons without an LED.
	NoLED map[mixer.TransportButton]bool

	lookup map[byte]mixer.Event // built by index
}

// NanoKONTROL2 is the Korg nanoKONTROL2 with its factory CC-mode settings (HW-01).
var NanoKONTROL2 = (&Map{
	Name:   "nanoKONTROL2",
	Slider: [8]byte{0, 1, 2, 3, 4, 5, 6, 7},
	Knob:   [8]byte{16, 17, 18, 19, 20, 21, 22, 23},
	S:      [8]byte{32, 33, 34, 35, 36, 37, 38, 39},
	M:      [8]byte{48, 49, 50, 51, 52, 53, 54, 55},
	R:      [8]byte{64, 65, 66, 67, 68, 69, 70, 71},
	Transport: map[mixer.TransportButton]byte{
		mixer.TrackPrev:   58,
		mixer.TrackNext:   59,
		mixer.Cycle:       46,
		mixer.MarkerSet:   60,
		mixer.MarkerPrev:  61,
		mixer.MarkerNext:  62,
		mixer.Rewind:      43,
		mixer.FastForward: 44,
		mixer.Stop:        42,
		mixer.Play:        41,
		mixer.Record:      45,
	},
	// Only Cycle, ◀◀, ▶▶, ■, ▶ and ● have LEDs among the transport buttons.
	NoLED: map[mixer.TransportButton]bool{
		mixer.TrackPrev: true, mixer.TrackNext: true,
		mixer.MarkerSet: true, mixer.MarkerPrev: true, mixer.MarkerNext: true,
	},
}).index()

// index builds the reverse lookup from CC number to control. For buttons,
// the event is stored without a value; Decode fills it in.
func (m *Map) index() *Map {
	m.lookup = map[byte]mixer.Event{}
	for i := 0; i < mixer.NumColumns; i++ {
		col := i + 1
		m.lookup[m.Slider[i]] = mixer.ControlMoved{Control: mixer.Control{Kind: mixer.Slider, Column: col}}
		m.lookup[m.Knob[i]] = mixer.ControlMoved{Control: mixer.Control{Kind: mixer.Knob, Column: col}}
		m.lookup[m.S[i]] = mixer.ButtonPressed{Button: mixer.ButtonS, Column: col}
		m.lookup[m.M[i]] = mixer.ButtonPressed{Button: mixer.ButtonM, Column: col}
		m.lookup[m.R[i]] = mixer.ButtonPressed{Button: mixer.ButtonR, Column: col}
	}
	for b, cc := range m.Transport {
		m.lookup[cc] = mixer.TransportPressed{Button: b}
	}
	return m
}

// Decode turns a Control Change into a mixer event. Buttons send a value
// above 0 when pressed and 0 when released (Momentary, HW-01); a column
// button's release becomes ButtonReleased (INPUT-08), a transport button's
// gives no event. ok is false for messages that mean nothing to Apptrol.
func (m *Map) Decode(cc CC) (ev mixer.Event, ok bool) {
	ev, ok = m.lookup[cc.Controller]
	if !ok {
		return nil, false
	}
	switch e := ev.(type) {
	case mixer.ControlMoved:
		e.Value = int(cc.Value)
		return e, true
	case mixer.ButtonPressed:
		if cc.Value == 0 {
			return mixer.ButtonReleased(e), true
		}
		return e, true
	default:
		if cc.Value == 0 {
			return nil, false // transport button released
		}
		return ev, true
	}
}

// HasLED reports whether the controller has this LED.
func (m *Map) HasLED(l mixer.LED) bool {
	_, ok := m.LED(l, false, 0)
	return ok
}

// LED returns the Control Change that turns an LED on or off. With LED mode
// External, the controller lights a button's LED when it receives the
// button's own CC number with a value above 0 (HW-02). ok is false for an
// LED the controller does not have.
func (m *Map) LED(l mixer.LED, on bool, channel byte) (cc CC, ok bool) {
	cc = CC{Channel: channel}
	if on {
		cc.Value = 127
	}
	if l.Transport != mixer.NoTransport {
		if m.NoLED[l.Transport] {
			return CC{}, false
		}
		cc.Controller, ok = m.Transport[l.Transport]
		return cc, ok
	}
	if l.Column < 1 || l.Column > mixer.NumColumns {
		return CC{}, false
	}
	switch l.Button {
	case mixer.ButtonS:
		cc.Controller = m.S[l.Column-1]
	case mixer.ButtonM:
		cc.Controller = m.M[l.Column-1]
	case mixer.ButtonR:
		cc.Controller = m.R[l.Column-1]
	default:
		return CC{}, false
	}
	return cc, true
}
