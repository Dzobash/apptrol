package mixer

import "log/slog"

// Event is something that happened. The service passes events to Handle.
type Event interface{ isEvent() }

// ControlMoved reports that a slider or knob moved to Value (0–127).
type ControlMoved struct {
	Control Control
	Value   int
}

// ButtonPressed reports a column button press (releases are not events).
type ButtonPressed struct {
	Button ButtonKind
	Column int
}

// TransportPressed reports a transport button press. Ignored in Phase 1.
type TransportPressed struct{ Button TransportButton }

// ControllerConnected reports that the controller (re)appeared; all LEDs are sent.
type ControllerConnected struct{}

// AudioSnapshot carries the full list of streams and devices, sent on
// (re)connect to the audio server. It replaces everything the mixer knew (SVC-04).
type AudioSnapshot struct {
	Streams []Stream
	Devices []Device
}

// StreamAdded reports a playback stream that appeared or changed its properties.
type StreamAdded struct{ Stream Stream }

// StreamRemoved reports that a playback stream is gone.
type StreamRemoved struct{ ID uint32 }

// DeviceAdded reports a capture device that appeared.
type DeviceAdded struct{ Device Device }

// DeviceRemoved reports that a capture device is gone.
type DeviceRemoved struct{ Name string }

// ConfigChanged carries a new, valid configuration (CFG-06).
type ConfigChanged struct{ Setup Setup }

func (ControlMoved) isEvent()        {}
func (ButtonPressed) isEvent()       {}
func (TransportPressed) isEvent()    {}
func (ControllerConnected) isEvent() {}
func (AudioSnapshot) isEvent()       {}
func (StreamAdded) isEvent()         {}
func (StreamRemoved) isEvent()       {}
func (DeviceAdded) isEvent()         {}
func (DeviceRemoved) isEvent()       {}
func (ConfigChanged) isEvent()       {}

// Action is something the service must do.
type Action interface{ isAction() }

// SetStreamVolume sets a playback stream's volume; Volume is 0–1 (1 = 100 %).
type SetStreamVolume struct {
	StreamID uint32
	Volume   float64
}

// SetStreamMute mutes or unmutes a playback stream.
type SetStreamMute struct {
	StreamID uint32
	Muted    bool
}

// SetDeviceVolume sets a capture device's volume; Volume is 0–1.
type SetDeviceVolume struct {
	Device string
	Volume float64
}

// SetDeviceMute mutes or unmutes a capture device.
type SetDeviceMute struct {
	Device string
	Muted  bool
}

// SetLED turns an LED on or off.
type SetLED struct {
	LED LED
	On  bool
}

// StateChanged tells the service that the persistent state changed; it saves it (debounced).
type StateChanged struct{}

// Notice is a message for the log (LOG-09).
type Notice struct {
	Level slog.Level
	Msg   string
	Attrs []any
}

func (SetStreamVolume) isAction() {}
func (SetStreamMute) isAction()   {}
func (SetDeviceVolume) isAction() {}
func (SetDeviceMute) isAction()   {}
func (SetLED) isAction()          {}
func (StateChanged) isAction()    {}
func (Notice) isAction()          {}
