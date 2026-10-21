package mixer

import "testing"

// Moving and restarting must preserve M's saved mute (OUT-06, STATE-01, ADR 0027).
func TestOUT06_MuteSavedPerControlAndRestored(t *testing.T) {
	w := outputWorld(t, outputSetup())
	w.move(Slider, 7, 64)
	w.press(ButtonM, 7)
	if !w.outMute[speakers.Name] || !w.m.Snapshot().Muted[Control{Slider, 7}] {
		t.Fatal("M did not mute and save the control")
	}
	w.noStreamOrInputActions()
	w.move(Slider, 7, 127)
	if !w.outMute[speakers.Name] {
		t.Fatal("moving a muted output unmuted it")
	}
	w.wantOutputVolume(speakers.Name, 1)
	restarted := newWorld(t, outputSetup(), w.m.Snapshot())
	restarted.do(AudioSnapshot{Outputs: []Device{speakers}})
	if !restarted.outMute[speakers.Name] {
		t.Fatal("restart lost mute")
	}
	restarted.press(ButtonM, 7)
	if restarted.outMute[speakers.Name] || restarted.m.Snapshot().Muted[Control{Slider, 7}] {
		t.Fatal("second M did not unmute")
	}
}

// Outside mute belongs to the owner, even on a knob (OUT-05, ADR 0035).
func TestOUT05_OutsideMuteTakenOverOnSliderAndKnob(t *testing.T) {
	for _, tc := range []struct {
		control Control
		device  Device
	}{
		{Control{Slider, 7}, speakers}, {Control{Knob, 7}, headphones},
	} {
		t.Run(tc.control.String(), func(t *testing.T) {
			w := outputWorld(t, outputSetup())
			for _, muted := range []bool{true, false} {
				w.do(OutputMuteChanged{Name: tc.device.Name, Muted: muted})
				if w.m.Snapshot().Muted[tc.control] != muted {
					t.Fatal("outside mute was not saved")
				}
				for _, a := range w.last {
					if _, ok := a.(SetOutputMute); ok {
						t.Fatalf("outside change caused a redundant write: %v", a)
					}
				}
				w.move(tc.control.Kind, tc.control.Column, 90)
				if w.outMute[tc.device.Name] != muted {
					t.Fatal("movement changed outside mute")
				}
			}
		})
	}
}

// A new incarnation receives the control's known state (OUT-05, ADR 0027).
func TestOUT05_ReplugRestoresPositionAndMute(t *testing.T) {
	w := outputWorld(t, outputSetup())
	w.move(Slider, 7, 64).press(ButtonM, 7)
	w.do(OutputRemoved{Name: speakers.Name}, OutputAdded{Device: speakers})
	w.wantOutputVolume(speakers.Name, 64.0/127)
	if !w.outMute[speakers.Name] {
		t.Fatal("replug lost mute")
	}
}

// Ownership transfers state from the new control, not the old one (OUT-04, ADR 0035).
func TestOUT04_ChangingOwnerAppliesNewOwnersState(t *testing.T) {
	s := outputSetup()
	s.Targets["earlier"] = Target{ID: "earlier", Name: "Different label", Kind: Output, Match: []string{"built-in"}}
	s.Assignments[Control{Slider, 1}] = "earlier"
	w := outputWorld(t, s)
	w.move(Slider, 1, 127).press(ButtonM, 1)
	w.move(Slider, 7, 32)
	w.do(OutputMuteChanged{Name: speakers.Name, Muted: false})
	if w.m.Snapshot().Muted[Control{Slider, 7}] {
		t.Fatal("outside mute affected the ignored control")
	}
	w.press(ButtonM, 1)
	delete(s.Assignments, Control{Slider, 1})
	w.do(ConfigChanged{Setup: s})
	w.wantOutputVolume(speakers.Name, 32.0/127)
	if w.outMute[speakers.Name] || w.m.OutputDevice("main") != speakers.Name {
		t.Fatal("new owner did not take over with its own mute")
	}
	w.press(ButtonM, 7)
	delete(s.Assignments, Control{Slider, 7})
	w.do(ConfigChanged{Setup: s})
	if w.outMute[speakers.Name] {
		t.Fatal("released output stayed muted")
	}
}

// Device matching, not display labels, decides conflicts (OUT-04, ADR 0035).
func TestOUT04_SameLabelsOnDifferentDevicesDoNotConflict(t *testing.T) {
	s := outputSetup()
	p := s.Targets["phones"]
	p.Name = s.Targets["main"].Name
	s.Targets["phones"] = p
	w := outputWorld(t, s)
	if w.m.OutputDevice("main") != speakers.Name || w.m.OutputDevice("phones") != headphones.Name {
		t.Fatal("display names affected matching")
	}
}

// All sliders take precedence over all knobs (OUT-04, ADR 0035).
func TestOUT04_SlidersPrecedeKnobs(t *testing.T) {
	s := outputSetup()
	s.Assignments[Control{Knob, 1}] = "phones"
	delete(s.Assignments, Control{Knob, 7})
	p := s.Targets["phones"]
	p.Match = []string{"built-in"}
	s.Targets["phones"] = p
	w := outputWorld(t, s)
	if w.m.OutputDevice("main") != speakers.Name || w.m.OutputDevice("phones") != "" {
		t.Fatal("knob1 won over slider7")
	}
	w.move(Slider, 7, 127).move(Knob, 1, 0)
	w.wantOutputVolume(speakers.Name, 1)
}

// Mute is saved per control rather than per target (OUT-06, ADR 0027).
func TestOUT06_ReloadUsesMuteOfNewControl(t *testing.T) {
	s := outputSetup()
	w := outputWorld(t, s)
	w.press(ButtonM, 7)
	delete(s.Assignments, Control{Slider, 7})
	s.Assignments[Control{Slider, 6}] = "main"
	w.do(ConfigChanged{Setup: s})
	if w.outMute[speakers.Name] {
		t.Fatal("mute followed the target instead of the control")
	}
}

// Unowned devices cannot change a control's saved mute (OUT-05, CTRL-06, ADR 0035).
func TestOUT05_UnownedOutputMuteDoesNotAffectControls(t *testing.T) {
	w := outputWorld(t, outputSetup())
	w.do(OutputMuteChanged{Name: "unassigned-output", Muted: true})
	if len(w.last) != 0 || len(w.m.Snapshot().Muted) != 0 {
		t.Fatalf("unowned mute affected mixer: %v", w.last)
	}
}
