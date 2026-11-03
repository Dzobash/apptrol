package mixer

import (
	"log/slog"
	"testing"
)

// defaultInputWorld exercises input controls without I/O (INPUT-09, ADR 0038).
func defaultInputWorld(t *testing.T, buttons map[LED]Button, saved State) *world {
	t.Helper()
	s := testSetup()
	mic := s.Targets["mic"]
	mic.Match, mic.FollowsDefault = nil, true
	s.Targets["mic"], s.Buttons = mic, buttons
	w := newWorld(t, s, saved)
	w.do(ControllerConnected{}, AudioSnapshot{Devices: []Device{goxlr, webcam}, DefaultInput: goxlr.Name,
		Streams: []Stream{spotify}})
	return w
}

func TestINPUT09_DefaultFollowsSwitchAndSnapshot(t *testing.T) {
	for _, snapshot := range []bool{false, true} {
		w := defaultInputWorld(t, nil, State{Positions: map[Control]int{{Slider, 8}: 100}, Muted: map[Control]bool{{Slider, 8}: true}})
		if _, set := w.devVol[goxlr.Name]; set {
			t.Fatal("saved position set default volume at connect")
		}
		if snapshot {
			w.do(AudioSnapshot{Devices: []Device{goxlr, webcam}, DefaultInput: webcam.Name})
		} else {
			w.do(DefaultInputChanged{Name: webcam.Name})
		}
		if w.m.InputDevice("mic") != webcam.Name || !w.devMute[webcam.Name] || w.devMute[goxlr.Name] {
			t.Fatalf("switch did not move saved mute: binding=%s mutes=%v", w.m.InputDevice("mic"), w.devMute)
		}
		if len(w.devVol) != 0 {
			t.Fatalf("switch wrote a volume: %v", w.devVol)
		}
		w.move(Slider, 8, 64)
		if !near(w.devVol[webcam.Name], Volume(64)) {
			t.Fatal("move did not set new default's volume")
		}
		w.do(DeviceMuteChanged{Name: webcam.Name, Muted: false})
		if w.m.Snapshot().Muted[Control{Slider, 8}] {
			t.Fatal("outside unmute not taken over")
		}
	}
}

func TestINPUT09_NoDefaultAndDeviceArrivesLater(t *testing.T) {
	w := defaultInputWorld(t, nil, State{})
	w.do(DefaultInputChanged{})
	if w.m.InputDevice("mic") != "" {
		t.Fatal("missing default retained device")
	}
	w.do(DefaultInputChanged{Name: "test-later"})
	w.do(DeviceAdded{Device: Device{Name: "test-later", Generation: 42}})
	if w.m.InputDevice("mic") != "test-later" {
		t.Fatal("default named before device was not selected")
	}
	w.wantNotice(slog.LevelInfo, "input matched")
	w.do(DeviceRemoved{Name: "test-later"})
	if w.m.InputDevice("mic") != "" {
		t.Fatal("removed default still bound")
	}
}

func TestINPUT09_SwitchEndsHeldStatesAndAllowsFreshPress(t *testing.T) {
	for _, mode := range []Mode{ModeHoldToTalk, ModeCough, ModeTalkOver} {
		buttons := map[LED]Button{s8: {Mode: mode}}
		button := ButtonS
		if mode == ModeHoldToTalk {
			buttons = map[LED]Button{m8: {Mode: mode, TalkOver: true}}
			button = ButtonM
		}
		w := defaultInputWorld(t, buttons, State{})
		w.move(Slider, 1, 100)
		w.press(button, 8)
		w.do(DefaultInputChanged{Name: webcam.Name})
		for _, a := range w.last {
			if mute, ok := a.(SetDeviceMute); ok && mode == ModeHoldToTalk && mute.Device == webcam.Name && !mute.Muted {
				t.Fatal("held M briefly opened the new default")
			}
		}
		if len(w.m.held) != 0 || w.m.talkingOver() {
			t.Fatalf("mode %v: held states survived switch", mode)
		}
		if w.devMute[webcam.Name] != (mode == ModeHoldToTalk) || w.devMute[goxlr.Name] {
			t.Fatalf("mode %v: wrong switch mutes %v", mode, w.devMute)
		}
		w.release(button, 8)
		w.press(button, 8)
		if mode == ModeHoldToTalk && w.devMute[webcam.Name] || mode == ModeCough && !w.devMute[webcam.Name] || mode == ModeTalkOver && !w.m.talkingOver() {
			t.Fatalf("mode %v: fresh press failed", mode)
		}
	}
}

func TestINPUT09_ReloadFromFixedToDefaultForgetsVolume(t *testing.T) {
	s := testSetup()
	w := newWorld(t, s, State{Positions: map[Control]int{{Slider, 8}: 100}})
	w.do(AudioSnapshot{Devices: []Device{goxlr}, DefaultInput: goxlr.Name})
	target := s.Targets["mic"]
	target.Match, target.FollowsDefault = nil, true
	s.Targets["mic"] = target
	w.do(ConfigChanged{Setup: s})
	forgot := false
	for _, a := range w.last {
		switch a := a.(type) {
		case ForgetDeviceVolume:
			forgot = a.Device == goxlr.Name
		case SetDeviceVolume:
			t.Fatal("reload applied saved volume to a default input")
		}
	}
	if !forgot {
		t.Fatal("reload kept defending fixed input volume")
	}
}

func TestINPUT09_FirstControlOwnsOverlap(t *testing.T) {
	for _, firstDefault := range []bool{true, false} {
		w := defaultInputWorld(t, nil, State{})
		s := w.m.setup
		s.Targets["fixed-mic"] = Target{ID: "fixed-mic", Kind: Input, Match: []string{webcam.Name}}
		if firstDefault {
			s.Assignments[Control{Slider, 1}], s.Assignments[Control{Slider, 8}] = "mic", "fixed-mic"
		} else {
			s.Assignments[Control{Slider, 1}] = "fixed-mic"
		}
		w.do(ConfigChanged{Setup: s}, DefaultInputChanged{Name: webcam.Name})
		owner, loser := "fixed-mic", "mic"
		if firstDefault {
			owner, loser = loser, owner
		}
		if w.m.InputDevice(owner) != webcam.Name || w.m.InputDevice(loser) != "" {
			t.Fatalf("wrong overlap owners: %v", w.m.deviceFor)
		}
		w.press(ButtonM, 8)
		if w.m.Snapshot().Muted[Control{Slider, 8}] {
			t.Fatal("blocked control changed saved mute")
		}
		w.do(DefaultInputChanged{Name: goxlr.Name})
		if w.m.InputDevice("fixed-mic") != webcam.Name || w.m.InputDevice("mic") != goxlr.Name {
			t.Fatal("overlap did not hand both devices back")
		}
	}
}
