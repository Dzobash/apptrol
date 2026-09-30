package mixer

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// world is a fake audio server and controller: it applies the mixer's actions,
// so tests can assert on what a user would hear and see.
type world struct {
	t *testing.T
	m *Mixer

	streamVol  map[uint32]float64
	streamMute map[uint32]bool
	devVol     map[string]float64
	devMute    map[string]bool
	leds       map[LED]bool
	saves      int
	notices    []Notice
	last       []Action // actions of the most recent event
}

// Standard test setup: the example layout.
//
//	slider1 = spotify   slider2 = browser (firefox, vivaldi)   slider3 = discord
//	slider8 = mic (input "goxlr")   knob1 = games (steam)
func testSetup() Setup {
	return Setup{
		Targets: map[string]Target{
			"spotify": {ID: "spotify", Name: "Spotify", Kind: App, Match: []string{"spotify"}},
			"browser": {ID: "browser", Name: "Browser", Kind: App, Match: []string{"firefox", "Vivaldi"}},
			"discord": {ID: "discord", Name: "Discord", Kind: App, Match: []string{"discord", "vesktop"}},
			"games":   {ID: "games", Name: "Games", Kind: App, Match: []string{"steam"}},
			"mic":     {ID: "mic", Name: "Microphone", Kind: Input, Match: []string{"goxlr"}},
			"spare":   {ID: "spare", Name: "Spare", Kind: App, Match: []string{"spare"}},
		},
		Assignments: map[Control]string{
			{Slider, 1}: "spotify",
			{Slider, 2}: "browser",
			{Slider, 3}: "discord",
			{Slider, 8}: "mic",
			{Knob, 1}:   "games",
		},
	}
}

// Streams and devices used throughout the tests.
var (
	spotify = Stream{ID: 10, AppName: "Spotify"}
	firefox = Stream{ID: 11, AppName: "Firefox", Binary: "firefox"}
	ffTab2  = Stream{ID: 12, AppName: "Firefox", Binary: "firefox"}
	discord = Stream{ID: 13, AppName: "WEBRTC VoiceEngine", Binary: "Discord"}
	steam   = Stream{ID: 14, AppName: "SDL Application", Binary: "steam_game"}
	viber   = Stream{ID: 15, AppName: "ViberPC", Binary: "Viber"}
	goxlr   = Device{Name: "alsa_input.usb-TC-Helicon_GoXLRMini-00.HiFi__Mic__source", Description: "GoXLR Mini Mic"}
	webcam  = Device{Name: "alsa_input.usb-046d_C922_Pro_Stream_Webcam-02.analog-stereo", Description: "C922 Pro Stream Webcam"}
)

func newWorld(t *testing.T, setup Setup, saved State) *world {
	t.Helper()
	w := &world{
		t: t, m: New(setup, saved),
		streamVol: map[uint32]float64{}, streamMute: map[uint32]bool{},
		devVol: map[string]float64{}, devMute: map[string]bool{},
		leds: map[LED]bool{},
	}
	return w
}

// started: a world with the controller connected and the usual streams playing.
func started(t *testing.T) *world {
	t.Helper()
	w := newWorld(t, testSetup(), State{})
	w.do(ControllerConnected{})
	w.do(AudioSnapshot{Streams: []Stream{spotify, firefox, discord, steam, viber}, Devices: []Device{goxlr, webcam}})
	return w
}

func (w *world) do(evs ...Event) *world {
	w.t.Helper()
	w.last = nil
	for _, ev := range evs {
		w.forgetGone(ev)
		acts := w.m.Handle(ev)
		w.last = append(w.last, acts...)
		for _, a := range acts {
			switch a := a.(type) {
			case SetStreamVolume:
				if a.Volume < 0 || a.Volume > MaxBoost {
					w.t.Fatalf("volume out of range: %v", a)
				}
				w.streamVol[a.StreamID] = a.Volume
			case SetStreamMute:
				w.streamMute[a.StreamID] = a.Muted
			case SetDeviceVolume:
				w.devVol[a.Device] = a.Volume
			case SetDeviceMute:
				w.devMute[a.Device] = a.Muted
			case SetLED:
				w.leds[a.LED] = a.On
			case StateChanged:
				w.saves++
			case Notice:
				w.notices = append(w.notices, a)
			default:
				w.t.Fatalf("unknown action %T", a)
			}
		}
	}
	return w
}

// forgetGone models the audio server: a stream or device that disappears takes
// its volume and mute with it; one that reappears starts fresh.
func (w *world) forgetGone(ev Event) {
	switch e := ev.(type) {
	case StreamRemoved:
		delete(w.streamVol, e.ID)
		delete(w.streamMute, e.ID)
	case DeviceRemoved:
		delete(w.devVol, e.Name)
		delete(w.devMute, e.Name)
	case AudioSnapshot:
		present := map[uint32]bool{}
		for _, s := range e.Streams {
			present[s.ID] = true
		}
		for id := range w.streamMute {
			if !present[id] {
				delete(w.streamMute, id)
				delete(w.streamVol, id)
			}
		}
		devs := map[string]bool{}
		for _, d := range e.Devices {
			devs[d.Name] = true
		}
		for n := range w.devMute {
			if !devs[n] {
				delete(w.devMute, n)
				delete(w.devVol, n)
			}
		}
	case StreamMuteChanged: // someone else changed it on the server (MUTE-07)
		w.streamMute[e.ID] = e.Muted
	case DeviceMuteChanged:
		w.devMute[e.Name] = e.Muted
	case StreamAdded:
		if _, known := w.m.streams[e.Stream.ID]; !known {
			delete(w.streamVol, e.Stream.ID)
			delete(w.streamMute, e.Stream.ID)
		}
	}
}

func (w *world) move(k ControlKind, col, value int) *world {
	return w.do(ControlMoved{Control{k, col}, value})
}

func (w *world) press(b ButtonKind, col int) *world { return w.do(ButtonPressed{b, col}) }

func (w *world) volume(id uint32) (float64, bool) {
	v, ok := w.streamVol[id]
	return v, ok
}

// muted reports what the user hears: true only if a mute was actually sent.
func (w *world) muted(id uint32) bool { return w.streamMute[id] }

func (w *world) led(b ButtonKind, col int) bool { return w.leds[LED{Button: b, Column: col}] }

func (w *world) wantVolume(id uint32, want float64) {
	w.t.Helper()
	got, ok := w.volume(id)
	if !ok || !near(got, want) {
		w.t.Errorf("stream %d volume = %v (set: %v), want %v", id, got, ok, want)
	}
}

func (w *world) wantMuted(want bool, ids ...uint32) {
	w.t.Helper()
	for _, id := range ids {
		if w.muted(id) != want {
			w.t.Errorf("stream %d muted = %v, want %v", id, w.muted(id), want)
		}
	}
}

func (w *world) wantLEDs(col int, s, m, r bool) {
	w.t.Helper()
	got := [3]bool{w.led(ButtonS, col), w.led(ButtonM, col), w.led(ButtonR, col)}
	if got != [3]bool{s, m, r} {
		w.t.Errorf("column %d LEDs S/M/R = %v, want %v", col, got, [3]bool{s, m, r})
	}
}

func (w *world) wantNotice(level slog.Level, msg string) {
	w.t.Helper()
	for _, n := range w.notices {
		if n.Level == level && strings.Contains(n.Msg, msg) {
			return
		}
	}
	w.t.Errorf("no %v notice containing %q in %v", level, msg, w.notices)
}

func (w *world) noActionFor(id uint32) {
	w.t.Helper()
	for _, a := range w.last {
		switch a := a.(type) {
		case SetStreamVolume:
			if a.StreamID == id {
				w.t.Errorf("unexpected %v", a)
			}
		case SetStreamMute:
			if a.StreamID == id {
				w.t.Errorf("unexpected %v", a)
			}
		}
	}
}

func near(a, b float64) bool { d := a - b; return d < 1e-9 && d > -1e-9 }

func (a SetStreamVolume) String() string {
	return fmt.Sprintf("SetStreamVolume(%d, %.3f)", a.StreamID, a.Volume)
}
func (a SetStreamMute) String() string {
	return fmt.Sprintf("SetStreamMute(%d, %v)", a.StreamID, a.Muted)
}
