// Package mixer holds Apptrol's behavior: which control drives which app,
// volume, mute, solo, LED states and what new streams get.
//
// The mixer does no I/O. The service feeds it events (Handle) and carries out
// the actions it returns, so every rule here is tested without hardware or
// PipeWire (QA-06, ADR 0015).
package mixer

import (
	"fmt"
	"strings"
)

// NumColumns is the number of controller columns (slider, knob, S/M/R).
const NumColumns = 8

// MaxValue is the highest MIDI control value; it maps to the target's
// MaxVolume (100 % unless configured otherwise).
const MaxValue = 127

// MaxBoost is the highest volume a target may reach: 1.5 = 150 % (CTRL-03).
const MaxBoost = 1.5

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

// ParseControl parses a control name such as "slider3" or "knob8".
func ParseControl(s string) (Control, bool) {
	var c Control
	var rest string
	switch {
	case strings.HasPrefix(s, "slider"):
		c.Kind, rest = Slider, s[len("slider"):]
	case strings.HasPrefix(s, "knob"):
		c.Kind, rest = Knob, s[len("knob"):]
	default:
		return Control{}, false
	}
	if len(rest) != 1 || rest[0] < '1' || rest[0] > '0'+NumColumns {
		return Control{}, false
	}
	c.Column = int(rest[0] - '0')
	return c, true
}

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

// String is the name the configuration uses: "app" or "input".
func (k TargetKind) String() string {
	if k == Input {
		return "input"
	}
	return "app"
}

// Target is something a control acts on (config: [apps.<id>]).
type Target struct {
	ID    string
	Name  string
	Kind  TargetKind
	Match []string // case-insensitive fragments
	// MaxVolume is the volume at the top of the control: 1 = 100 %, up to
	// MaxBoost. 0 means 1.
	MaxVolume float64
}

// Setup is the part of the configuration the mixer needs.
type Setup struct {
	Layout      string             // name of the active layout, for the log; "" = "default"
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

// State is what the mixer persists (STATE-01): known control positions, the
// user mutes of slider columns and the soloed column (STATE-04).
type State struct {
	Positions map[Control]int
	Muted     map[Control]bool
	Solo      int // soloed slider column, 0 = none
}
