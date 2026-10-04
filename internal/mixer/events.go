package mixer

import "log/slog"

// Event is something that happened. The service passes events to Handle.
type Event interface{ isEvent() }

// ControlMoved reports that a slider or knob moved to Value (0–127).
type ControlMoved struct {
	Control Control
	Value   int
}

// ButtonPressed reports a column button press.
type ButtonPressed struct {
	Button ButtonKind
	Column int
}

// ButtonReleased reports that a column button was let go. Only held states
// use it (INPUT-08); the controller's buttons must be Momentary (HW-01).
type ButtonReleased struct {
	Button ButtonKind
	Column int
}

// TransportPressed reports a transport button press. Ignored in Phase 1.
type TransportPressed struct{ Button TransportButton }

// ControllerConnected reports that the controller (re)appeared; all LEDs are
// sent. Resync marks the repeats sent shortly after a connect, because the
// controller ignores LED messages while it starts up (LED-07).
type ControllerConnected struct{ Resync bool }

// ControllerDisconnected reports that the controller is gone. Held states
// end: their release would never arrive (INPUT-07).
type ControllerDisconnected struct{}

// SystemResumed reports that the computer woke up from sleep or hibernation;
// all LEDs are sent. The controller lost power while the computer slept and
// starts with its LEDs off, but stays connected, so ControllerConnected does
// not come (LED-09).
type SystemResumed struct{}

// ScreenChanged reports whether the user's screen is unlocked, locked, behind
// another user's session, or unknown. Launchers start apps only while it is
// unlocked (LAUNCH-13, ADR 0024).
type ScreenChanged struct{ State ScreenState }

// ScreenState is the state of the user's graphical session, as logind
// reports it.
type ScreenState string

// The screen states (ADR 0024).
const (
	ScreenUnlocked ScreenState = "unlocked" // unlocked and in front: launchers start
	ScreenLocked   ScreenState = "locked"   // LockedHint is true
	ScreenInactive ScreenState = "inactive" // another session is in front (Active is false)
	ScreenUnknown  ScreenState = "unknown"  // it cannot be told
)

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

// StreamMuteChanged reports that a playback stream was muted or unmuted by
// someone other than Apptrol, e.g. in the desktop's volume applet (MUTE-07).
type StreamMuteChanged struct {
	ID    uint32
	Muted bool
}

// StreamCorkChanged reports that a playback stream was paused (corked) or
// resumed by its app, e.g. a paused browser tab (MEDIA-10). Only the app can
// cork its stream; Apptrol only observes it.
type StreamCorkChanged struct {
	ID     uint32
	Corked bool
}

// DeviceMuteChanged reports that a capture device was muted or unmuted by
// someone other than Apptrol, e.g. with the desktop's microphone mute key (MUTE-07).
type DeviceMuteChanged struct {
	Name  string
	Muted bool
}

// PlayerSnapshot carries every media player on the session bus, sent on
// (re)connect to the bus. It replaces every player the mixer knew (DESK-02).
type PlayerSnapshot struct{ Players []Player }

// PlayerAdded reports a media player that appeared (MEDIA-01).
type PlayerAdded struct{ Player Player }

// PlayerRemoved reports that a media player is gone.
type PlayerRemoved struct{ BusName string }

// PlayerStatusChanged reports a player's new PlaybackStatus.
type PlayerStatusChanged struct {
	BusName string
	Status  string
}

// ConfigChanged carries a new, valid configuration (CFG-06).
type ConfigChanged struct{ Setup Setup }

func (ControlMoved) isEvent()           {}
func (ButtonPressed) isEvent()          {}
func (ButtonReleased) isEvent()         {}
func (ControllerDisconnected) isEvent() {}
func (TransportPressed) isEvent()       {}
func (ControllerConnected) isEvent()    {}
func (SystemResumed) isEvent()          {}
func (ScreenChanged) isEvent()          {}
func (AudioSnapshot) isEvent()          {}
func (StreamAdded) isEvent()            {}
func (StreamRemoved) isEvent()          {}
func (DeviceAdded) isEvent()            {}
func (DeviceRemoved) isEvent()          {}
func (StreamMuteChanged) isEvent()      {}
func (StreamCorkChanged) isEvent()      {}
func (DeviceMuteChanged) isEvent()      {}
func (ConfigChanged) isEvent()          {}
func (PlayerSnapshot) isEvent()         {}
func (PlayerAdded) isEvent()            {}
func (PlayerRemoved) isEvent()          {}
func (PlayerStatusChanged) isEvent()    {}

// Action is something the service must do.
type Action interface{ isAction() }

// SetStreamVolume sets a playback stream's volume; Volume is 0–1 (1 = 100 %).
type SetStreamVolume struct {
	StreamID uint32
	Volume   float64
}

// PlayerCommand sends a command to a media player. Play and pause are always
// one of the two, never the toggle PlayPause (MEDIA-04).
type PlayerCommand struct {
	BusName string
	Command string // one of the Command constants
}

// LaunchApp starts an app for a launcher button (LAUNCH-01). The service
// passes it to the launcher, which does not wait for the app.
type LaunchApp struct {
	Button LED
	Launch Launch
}

// Player commands (MPRIS method names).
const (
	CommandPlay     = "Play"
	CommandPause    = "Pause"
	CommandStop     = "Stop"
	CommandNext     = "Next"
	CommandPrevious = "Previous"
)

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
func (PlayerCommand) isAction()   {}
func (LaunchApp) isAction()       {}
