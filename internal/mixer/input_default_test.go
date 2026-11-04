package mixer

import (
	"log/slog"
	"testing"

	"github.com/Dzobash/apptrol/internal/logattr"
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

func TestINPUT09_ReloadEndsHeldTalkOverOnlyOnce(t *testing.T) {
	for _, mode := range []Mode{ModeTalkOver, ModeHoldToTalk} {
		t.Run(mode.String(), func(t *testing.T) {
			s := testSetup()
			button := ButtonS
			s.Buttons = map[LED]Button{s8: {Mode: ModeTalkOver}}
			if mode == ModeHoldToTalk {
				button = ButtonM
				s.Buttons = map[LED]Button{m8: {Mode: ModeHoldToTalk, TalkOver: true}}
			}
			w := newWorld(t, s, State{})
			w.do(AudioSnapshot{Devices: []Device{goxlr, webcam}, DefaultInput: webcam.Name, Streams: []Stream{spotify}})
			w.move(Slider, 1, 100)
			w.press(button, 8)
			if !w.m.talkingOver() {
				t.Fatal("held button did not start talk-over")
			}
			mic := s.Targets["mic"]
			mic.Match, mic.FollowsDefault = nil, true
			s.Targets["mic"] = mic
			w.do(ConfigChanged{Setup: s})
			transitions, volumes := 0, 0
			for _, a := range w.last {
				if n, ok := a.(Notice); ok && n.Msg == "talk-over off" {
					transitions++
				}
				if v, ok := a.(SetStreamVolume); ok && v.StreamID == spotify.ID {
					volumes++
				}
			}
			if transitions != 1 || volumes != 1 || w.m.talkingOver() || !near(w.streamVol[spotify.ID], Volume(100)) {
				t.Fatalf("reload: off records=%d, volume writes=%d, talk-over=%v, volume=%v", transitions, volumes, w.m.talkingOver(), w.streamVol[spotify.ID])
			}
		})
	}
}

func TestINPUT09_YieldingInputButtonsAndLEDsAreInactive(t *testing.T) {
	for _, tc := range []struct {
		name      string
		s         Mode
		mTalkOver bool
	}{
		{"cough", ModeCough, false},
		{"S talk-over", ModeTalkOver, false},
		{"M talk-over", ModeOff, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testSetup()
			mic := s.Targets["mic"]
			mic.Match, mic.FollowsDefault = nil, true
			s.Targets["mic"] = mic
			s.Targets["owner"] = Target{ID: "owner", Kind: Input, Match: []string{webcam.Name}}
			s.Assignments[Control{Slider, 7}] = "owner"
			s.Buttons = map[LED]Button{s8: {Mode: tc.s}, m8: {TalkOver: tc.mTalkOver}}
			w := newWorld(t, s, State{})
			w.do(ControllerConnected{}, AudioSnapshot{Devices: []Device{goxlr, webcam}, DefaultInput: webcam.Name, Streams: []Stream{spotify}})
			w.move(Slider, 1, 100)
			if w.m.InputDevice("mic") != "" || w.leds[m8] || w.m.talkingOver() {
				t.Fatal("yielding input showed live or started talk-over")
			}
			saves := w.saves
			w.move(Slider, 8, 51)
			if w.m.Snapshot().Positions[Control{Slider, 8}] != 51 || w.saves != saves+1 {
				t.Fatal("yielding move did not save the position")
			}
			for _, a := range w.last {
				switch a := a.(type) {
				case SetDeviceVolume:
					t.Fatal("yielding move changed device volume")
				case Notice:
					if a.Msg == "volume changed" {
						t.Fatal("yielding move claimed a volume change")
					}
				}
			}
			msg := "position saved without changing volume: input belongs to an earlier control"
			w.wantNotice(slog.LevelDebug, msg)
			if w.noticeAttr(msg, logattr.KeyDeviceName) != webcam.Name || w.noticeAttr(msg, logattr.KeyControl) != "slider8" {
				t.Fatal("yielding move log did not identify the device and control")
			}
			for _, button := range []ButtonKind{ButtonM, ButtonS} {
				w.press(button, 8)
				for _, a := range w.last {
					switch a.(type) {
					case SetDeviceMute, SetStreamVolume, SetStreamMute:
						t.Fatalf("yielding button changed audio: %v", a)
					}
				}
				if w.devMute[webcam.Name] || w.leds[m8] || w.m.talkingOver() || len(w.m.held) != 0 || w.m.Snapshot().Muted[Control{Slider, 8}] || !near(w.streamVol[spotify.ID], Volume(100)) {
					t.Fatal("yielding button changed the owner, LEDs, held state or app volume")
				}
				w.release(button, 8)
			}
		})
	}
}

func TestINPUT09_DeviceReleaseAnsweredOnce(t *testing.T) {
	for _, snapshot := range []bool{false, true} {
		t.Run(map[bool]string{false: "description changed", true: "snapshot default changed"}[snapshot], func(t *testing.T) {
			d := Device{Name: "test-input-a", Description: "Test microphone", Generation: 1}
			other := Device{Name: "test-input-b", Description: "Other input", Generation: 2}
			s := testSetup()
			mic := s.Targets["mic"]
			mic.Match = []string{"Test microphone"}
			if snapshot {
				mic.Match, mic.FollowsDefault = nil, true
			}
			s.Targets["mic"] = mic
			w := newWorld(t, s, State{})
			w.do(AudioSnapshot{Devices: []Device{d, other}, DefaultInput: d.Name})
			d.Generation = 3
			if snapshot {
				w.do(AudioSnapshot{Devices: []Device{d, other}, DefaultInput: other.Name})
			} else {
				d.Description = "No longer matches"
				w.do(DeviceAdded{Device: d})
			}
			releases := 0
			for _, a := range w.last {
				if r, ok := a.(ReleaseDevice); ok && r.Device == d.Name {
					releases++
					if r.Generation != d.Generation {
						t.Fatal("release did not answer the reported generation")
					}
				}
			}
			if releases != 1 {
				t.Fatalf("release answers = %d, want 1", releases)
			}
		})
	}
}

func TestINPUT09_RepeatedDefaultNoticeDoesNothing(t *testing.T) {
	w := defaultInputWorld(t, map[LED]Button{m8: {Mode: ModeHoldToTalk, TalkOver: true}}, State{})
	w.press(ButtonM, 8)
	w.do(DefaultInputChanged{Name: goxlr.Name})
	if len(w.last) != 0 || !w.m.held[m8] || w.devMute[goxlr.Name] {
		t.Fatal("repeated default notice changed the active microphone or held state")
	}
}
