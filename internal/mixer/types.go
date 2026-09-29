// Package mixer holds Apptrol's behavior: which control drives which app,
// volume, mute, solo, LED states and what new streams get.
//
// The mixer does no I/O. The service feeds it events (Handle) and carries out
// the actions it returns, so every rule here is tested without hardware or
// PipeWire (QA-06, ADR 0015).
package mixer

import "fmt"

// NumColumns is the number of controller columns (slider, knob, S/M/R).
const NumColumns = 8

// MaxValue is the highest MIDI control value; it maps to 100 % volume.
const MaxValue = 127

// ControlKind is a kind of continuous control.
type ControlKind int

// Control kinds.
const (
	Slider ControlKind = iota
	Knob
)

func (k ControlKind) String() string {
	if k == Knob {
		return "knob"
	}
	return "slider"
}

// Control is a slider or knob in column 1–8.
type Control struct {
	Kind   ControlKind
	Column int
}

func (c Control) String() string { return fmt.Sprintf("%s%d", c.Kind, c.Column) }

// Valid reports whether the column is in range.
func (c Control) Valid() bool { return c.Column >= 1 && c.Column <= NumColumns }

// ButtonKind is a column button.
type ButtonKind int

// Column buttons.
const (
	ButtonS ButtonKind = iota // solo
	ButtonM                   // mute
	ButtonR                   // reserved (BTN-01)
)

func (b ButtonKind) String() string { return [...]string{"S", "M", "R"}[b] }

// LED identifies one LED: a column button (S/M/R in column 1–8) or a
// transport button (Transport set, Column 0).
type LED struct {
	Button    ButtonKind
	Column    int
	Transport TransportButton // non-zero for transport LEDs
}

func (l LED) String() string {
	if l.Transport != 0 {
		return l.Transport.String()
	}
	return fmt.Sprintf("%s%d", l.Button, l.Column)
}

// TransportButton identifies a non-column button. They do nothing in Phase 1
// (BTN-01); their LEDs are kept off (LED-06).
type TransportButton int

// Transport buttons of the nanoKONTROL2.
const (
	NoTransport TransportButton = iota
	TrackPrev
	TrackNext
	Cycle
	MarkerSet
	MarkerPrev
	MarkerNext
	Rewind
	FastForward
	Stop
	Play
	Record
)

var transportNames = [...]string{"none", "track◀", "track▶", "cycle", "marker set", "marker◀", "marker▶", "◀◀", "▶▶", "■", "▶", "●"}

func (t TransportButton) String() string {
	if int(t) < len(transportNames) {
		return transportNames[t]
	}
	return fmt.Sprintf("transport%d", int(t))
}

// AllTransport lists every transport button.
var AllTransport = []TransportButton{TrackPrev, TrackNext, Cycle, MarkerSet, MarkerPrev, MarkerNext, Rewind, FastForward, Stop, Play, Record}

// TargetKind says what a target controls.
type TargetKind int

// Target kinds.
const (
	App   TargetKind = iota // playback streams of an application
	Input                   // one capture device
)

// Target is something a control acts on (config: [apps.<id>]).
type Target struct {
	ID    string
	Name  string
	Kind  TargetKind
	Match []string // case-insensitive fragments
}

// Setup is the part of the configuration the mixer needs.
type Setup struct {
	Targets     map[string]Target  // by id
	Assignments map[Control]string // control -> target id
}

// Stream is a playback stream (a PulseAudio "sink input").
type Stream struct {
	ID      uint32
	AppName string // application.name
	Binary  string // application.process.binary
}

// Device is a capture device (a PulseAudio "source").
type Device struct {
	Name        string // unique name, e.g. alsa_input.usb-…
	Description string // human-readable, e.g. "GoXLR Mini Mic"
}

// State is what the mixer persists (STATE-01): known control positions and the
// user mutes of slider columns. Solo is never saved (STATE-04).
type State struct {
	Positions map[Control]int
	Muted     map[Control]bool
}
