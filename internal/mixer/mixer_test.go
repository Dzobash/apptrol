package mixer

import (
	"log/slog"
	"testing"
)

// ---- controls and volume ----------------------------------------------------

func TestCTRL01_UnassignedControlsDoNothing(t *testing.T) {
	w := started(t)
	w.move(Slider, 5, 100).move(Knob, 8, 100)
	for _, a := range w.last {
		if _, ok := a.(Notice); !ok {
			t.Errorf("unassigned control produced %T", a)
		}
	}
}

func TestCTRL02_SliderSetsVolumeLinearly(t *testing.T) {
	w := started(t)
	for _, tc := range []struct {
		value int
		want  float64
	}{{0, 0}, {127, 1}, {64, 64.0 / 127}, {1, 1.0 / 127}} {
		w.move(Slider, 1, tc.value)
		w.wantVolume(spotify.ID, tc.want)
	}
}

func TestCTRL02_KnobSetsVolume(t *testing.T) {
	w := started(t)
	w.move(Knob, 1, 127)
	w.wantVolume(steam.ID, 1)
}

func TestCTRL03_VolumeStaysWithin0And100Percent(t *testing.T) {
	w := started(t)
	w.move(Slider, 1, 500)
	w.wantVolume(spotify.ID, 1)
	w.move(Slider, 1, -3)
	w.wantVolume(spotify.ID, 0)
}

func TestCTRL04_AllStreamsOfAnAppFollow(t *testing.T) {
	w := started(t)
	w.do(StreamAdded{ffTab2})
	w.move(Slider, 2, 32)
	w.wantVolume(firefox.ID, 32.0/127)
	w.wantVolume(ffTab2.ID, 32.0/127)
}

func TestCTRL04_MatchesProcessBinaryToo(t *testing.T) {
	w := started(t)
	w.move(Slider, 3, 90) // discord matches by binary "Discord"
	w.wantVolume(discord.ID, 90.0/127)
}

func TestCTRL05_InputVolume(t *testing.T) {
	w := started(t)
	w.move(Slider, 8, 127)
	if !near(w.devVol[goxlr.Name], 1) {
		t.Errorf("mic volume = %v", w.devVol[goxlr.Name])
	}
	if _, touched := w.devVol[webcam.Name]; touched {
		t.Error("the webcam was changed although it does not match")
	}
}

func TestCTRL06_UnassignedAppsAreNeverTouched(t *testing.T) {
	w := newWorld(t, testSetup(), State{Positions: map[Control]int{{Slider, 1}: 50}})
	w.do(AudioSnapshot{Streams: []Stream{viber}})
	w.move(Slider, 1, 10).press(ButtonS, 1).press(ButtonM, 1).press(ButtonS, 1).press(ButtonM, 1)
	if _, ok := w.streamVol[viber.ID]; ok {
		t.Error("viber volume changed")
	}
	if _, ok := w.streamMute[viber.ID]; ok {
		t.Error("viber mute changed")
	}
}

func TestCTRL04_FirstControlWinsWhenAStreamMatchesTwoApps(t *testing.T) {
	s := testSetup()
	s.Targets["web"] = Target{ID: "web", Name: "Web", Kind: App, Match: []string{"fire"}}
	s.Assignments[Control{Knob, 2}] = "web"
	w := newWorld(t, s, State{})
	w.do(AudioSnapshot{Streams: []Stream{firefox}})
	w.move(Knob, 2, 10)
	if _, ok := w.volume(firefox.ID); ok {
		t.Error("knob2 changed firefox, but slider2 (browser) comes first")
	}
}

// ---- controller priority ----------------------------------------------------

func TestPRIO01_ControlAlwaysWinsOnNextMove(t *testing.T) {
	w := started(t)
	w.move(Slider, 1, 100)
	w.streamVol[spotify.ID] = 0.2 // changed in the desktop mixer
	w.move(Slider, 1, 101)
	w.wantVolume(spotify.ID, 101.0/127)
}

func TestPRIO02_ExternalChangesAreNotReverted(t *testing.T) {
	w := started(t)
	w.move(Slider, 1, 100)
	w.streamVol[spotify.ID] = 0.2
	w.do(StreamAdded{firefox}) // any unrelated event
	if !near(w.streamVol[spotify.ID], 0.2) {
		t.Error("the mixer reverted an external volume change on its own")
	}
}

func TestPRIO03_NewStreamGetsControlPosition(t *testing.T) {
	w := started(t)
	w.move(Slider, 2, 40)
	w.do(StreamAdded{ffTab2})
	w.wantVolume(ffTab2.ID, 40.0/127)
}

func TestPRIO04_NewStreamKeepsVolumeWhenPositionUnknown(t *testing.T) {
	w := newWorld(t, testSetup(), State{})
	w.do(AudioSnapshot{Streams: []Stream{spotify}})
	if _, ok := w.volume(spotify.ID); ok {
		t.Error("volume set although the slider position is unknown")
	}
}

func TestPRIO05_NewStreamOfMutedAppIsMuted(t *testing.T) {
	w := started(t)
	w.press(ButtonM, 2)
	w.do(StreamAdded{ffTab2})
	w.wantMuted(true, ffTab2.ID)
}

func TestPRIO05_NewStreamSilencedBySoloIsMuted(t *testing.T) {
	w := started(t)
	w.press(ButtonS, 1)
	w.do(StreamAdded{ffTab2})
	w.wantMuted(true, ffTab2.ID)
}

func TestPRIO05_NewStreamIsExplicitlyUnmuted(t *testing.T) {
	// PipeWire may restore an old mute for the app; Apptrol states its view.
	w := started(t)
	w.streamMute[ffTab2.ID] = true
	w.do(StreamAdded{ffTab2})
	w.wantMuted(false, ffTab2.ID)
}

// ---- mute --------------------------------------------------------------------

func TestMUTE01_MToggles(t *testing.T) {
	w := started(t)
	w.press(ButtonM, 1)
	w.wantMuted(true, spotify.ID)
	w.wantLEDs(1, false, true, false)
	w.wantNotice(slog.LevelInfo, "muted")
	w.press(ButtonM, 1)
	w.wantMuted(false, spotify.ID)
	w.wantLEDs(1, false, false, false)
}

func TestMUTE02_KnobAppsHaveNoMute(t *testing.T) {
	// M in column 1 belongs to slider1 (spotify), never to knob1 (games).
	w := started(t)
	w.press(ButtonM, 1)
	w.wantMuted(false, steam.ID)
}

func TestMUTE03_MOnEmptyColumnDoesNothing(t *testing.T) {
	w := started(t)
	w.press(ButtonM, 5)
	if len(w.last) != 0 {
		t.Errorf("actions for M on an empty column: %v", w.last)
	}
}

func TestMUTE05_MuteDuringSoloTakesEffectAfterwards(t *testing.T) {
	w := started(t)
	w.press(ButtonS, 1)
	w.press(ButtonM, 3) // discord: already silent by solo
	w.wantMuted(true, discord.ID)
	w.wantLEDs(3, false, true, false)
	w.press(ButtonS, 1) // solo off
	w.wantMuted(true, discord.ID)
	w.wantMuted(false, firefox.ID, steam.ID, spotify.ID)
}

// ---- solo --------------------------------------------------------------------

func TestSOLO01_SOLO02_SoloSilencesOtherApps(t *testing.T) {
	w := started(t)
	w.press(ButtonS, 1)
	w.wantMuted(false, spotify.ID)
	w.wantMuted(true, firefox.ID, discord.ID, steam.ID) // steam is on a knob
	w.wantNotice(slog.LevelInfo, "solo on")
}

func TestSOLO03_SoloNeverTouchesInputs(t *testing.T) {
	w := started(t)
	w.press(ButtonS, 1)
	if w.devMute[goxlr.Name] {
		t.Error("solo muted the microphone")
	}
}

func TestSOLO04_SoloMovesToOtherColumn(t *testing.T) {
	w := started(t)
	w.press(ButtonS, 1).press(ButtonS, 2)
	w.wantMuted(false, firefox.ID)
	w.wantMuted(true, spotify.ID, discord.ID)
	w.wantLEDs(1, false, false, false)
	w.wantLEDs(2, true, false, false)
}

func TestSOLO05_PressingSoloAgainTurnsItOff(t *testing.T) {
	w := started(t)
	w.press(ButtonS, 1).press(ButtonS, 2).press(ButtonS, 2)
	w.wantMuted(false, spotify.ID, firefox.ID, discord.ID, steam.ID)
	for col := 1; col <= 3; col++ {
		if w.led(ButtonS, col) {
			t.Errorf("S%d still lit", col)
		}
	}
}

func TestSOLO05_UserMutesSurviveSolo(t *testing.T) {
	w := started(t)
	w.press(ButtonM, 3).press(ButtonS, 1).press(ButtonS, 1)
	w.wantMuted(true, discord.ID)
	w.wantMuted(false, firefox.ID)
}

func TestSOLO06_SoloedAppKeepsItsOwnMute(t *testing.T) {
	w := started(t)
	w.press(ButtonM, 1).press(ButtonS, 1)
	w.wantMuted(true, spotify.ID, firefox.ID)
}

func TestSOLO07_SOnInputColumnDoesNothing(t *testing.T) {
	w := started(t)
	w.press(ButtonS, 8)
	if len(w.last) != 0 {
		t.Errorf("S on the input column produced %v", w.last)
	}
	w.wantMuted(false, spotify.ID)
}

// ---- LEDs --------------------------------------------------------------------

func TestLED_InitialStateOnConnect(t *testing.T) {
	w := started(t)
	w.wantLEDs(1, false, false, false)
	w.wantLEDs(5, false, false, false) // LED-05: empty column
	w.wantLEDs(8, true, true, true)    // LED-04: input column lit
	for _, tr := range AllTransport {  // LED-06
		if on, ok := w.leds[LED{Transport: tr}]; !ok || on {
			t.Errorf("transport LED %v = %v (sent: %v), want off", tr, on, ok)
		}
	}
}

func TestLED02_MLEDIgnoresSoloSilencing(t *testing.T) {
	w := started(t)
	w.press(ButtonS, 1)
	w.wantLEDs(2, false, false, false) // browser is silent, but not user-muted
}

func TestLED04_InputColumnMLEDGoesDarkWhenMuted(t *testing.T) {
	w := started(t)
	w.press(ButtonM, 8)
	if !w.devMute[goxlr.Name] {
		t.Error("microphone not muted")
	}
	w.wantLEDs(8, true, false, true)
	w.press(ButtonM, 8)
	w.wantLEDs(8, true, true, true)
}

func TestLED07_AllLEDsResentOnReconnect(t *testing.T) {
	w := started(t)
	w.press(ButtonM, 1)
	w.do(ControllerConnected{})
	leds := 0
	for _, a := range w.last {
		if _, ok := a.(SetLED); ok {
			leds++
		}
	}
	if want := NumColumns*3 + len(AllTransport); leds != want {
		t.Errorf("reconnect sent %d LEDs, want all %d", leds, want)
	}
	w.wantLEDs(1, false, true, false)
}

func TestLED_OnlyChangedLEDsAreSent(t *testing.T) {
	w := started(t)
	w.press(ButtonM, 1)
	var sent []LED
	for _, a := range w.last {
		if l, ok := a.(SetLED); ok {
			sent = append(sent, l.LED)
		}
	}
	if len(sent) != 1 || sent[0] != (LED{Button: ButtonM, Column: 1}) {
		t.Errorf("LED updates = %v, want only M1", sent)
	}
}

// ---- other buttons -----------------------------------------------------------

func TestBTN01_ReservedButtonsDoNothing(t *testing.T) {
	w := started(t)
	w.press(ButtonR, 1)
	for _, tr := range AllTransport {
		w.do(TransportPressed{tr})
		if len(w.last) != 0 {
			t.Errorf("%v produced %v", tr, w.last)
		}
	}
	w.do(ButtonPressed{ButtonM, 0}, ButtonPressed{ButtonM, 9})
	if len(w.last) != 0 {
		t.Errorf("out-of-range columns produced %v", w.last)
	}
}

// ---- state -------------------------------------------------------------------

func TestSTATE01_SnapshotHasPositionsAndMutes(t *testing.T) {
	w := started(t)
	w.move(Slider, 1, 99).move(Knob, 1, 12).press(ButtonM, 3).press(ButtonS, 2)
	s := w.m.Snapshot()
	if s.Positions[Control{Slider, 1}] != 99 || s.Positions[Control{Knob, 1}] != 12 {
		t.Errorf("positions = %v", s.Positions)
	}
	if !s.Muted[Control{Slider, 3}] || len(s.Muted) != 1 {
		t.Errorf("mutes = %v, want only slider3", s.Muted)
	}
	if w.saves == 0 {
		t.Error("no StateChanged action")
	}
}

func TestSTATE03_RestoredStateIsApplied(t *testing.T) {
	saved := State{
		Positions: map[Control]int{{Slider, 1}: 64, {Slider, 8}: 127},
		Muted:     map[Control]bool{{Slider, 2}: true},
	}
	w := newWorld(t, testSetup(), saved)
	w.do(ControllerConnected{}, AudioSnapshot{Streams: []Stream{spotify, firefox}, Devices: []Device{goxlr}})
	w.wantVolume(spotify.ID, 64.0/127)
	w.wantMuted(true, firefox.ID)
	w.wantLEDs(2, false, true, false)
	if !near(w.devVol[goxlr.Name], 1) {
		t.Errorf("mic volume = %v", w.devVol[goxlr.Name])
	}
}

func TestSTATE04_SoloIsNotSaved(t *testing.T) {
	w := started(t)
	w.press(ButtonS, 1)
	w2 := newWorld(t, testSetup(), w.m.Snapshot())
	w2.do(AudioSnapshot{Streams: []Stream{firefox}})
	w2.wantMuted(false, firefox.ID)
}

func TestSTATE07_UnassignedControlsAreDropped(t *testing.T) {
	saved := State{
		Positions: map[Control]int{{Slider, 5}: 10, {Slider, 1}: 20, {Knob, 99}: 1},
		Muted:     map[Control]bool{{Slider, 6}: true, {Knob, 1}: true},
	}
	s := New(testSetup(), saved).Snapshot()
	if len(s.Positions) != 1 || s.Positions[Control{Slider, 1}] != 20 {
		t.Errorf("positions = %v", s.Positions)
	}
	if len(s.Muted) != 0 {
		t.Errorf("mutes = %v: slider6 is unassigned and knobs have no mute", s.Muted)
	}
}

// ---- streams and devices come and go ----------------------------------------

func TestStreamRemovedIsForgotten(t *testing.T) {
	w := started(t)
	w.do(StreamRemoved{spotify.ID})
	w.move(Slider, 1, 5)
	w.noActionFor(spotify.ID)
}

func TestStreamPropertyChangeCanMoveItToAnotherApp(t *testing.T) {
	w := started(t)
	w.press(ButtonM, 3)
	w.do(StreamAdded{Stream{ID: viber.ID, AppName: "Vesktop"}}) // same id, now matches discord
	w.wantMuted(true, viber.ID)
	w.do(StreamAdded{Stream{ID: viber.ID, AppName: "Vesktop"}}) // no change: no actions
	if len(w.last) != 0 {
		t.Errorf("repeated identical update produced %v", w.last)
	}
}

func TestAudioSnapshotReplacesEverything(t *testing.T) {
	w := started(t)
	w.move(Slider, 1, 70).press(ButtonM, 2)
	// PipeWire restarted: new ids, mutes must be re-applied (SVC-04).
	w.streamMute = map[uint32]bool{}
	w.do(AudioSnapshot{Streams: []Stream{{ID: 100, AppName: "Spotify"}, {ID: 101, AppName: "Firefox"}}, Devices: []Device{goxlr}})
	w.wantVolume(100, 70.0/127)
	w.wantMuted(true, 101)
	w.move(Slider, 1, 71)
	w.noActionFor(spotify.ID)
}

func TestCFG05_OneDevicePerInputAndWarnings(t *testing.T) {
	s := testSetup()
	s.Targets["mic"] = Target{ID: "mic", Name: "Microphone", Kind: Input, Match: []string{"usb"}}
	w := newWorld(t, s, State{Positions: map[Control]int{{Slider, 8}: 127}})
	w.do(AudioSnapshot{Devices: []Device{webcam, goxlr}})
	// Both names contain "usb"; the first by name is the webcam ("alsa_input.usb-046d…").
	if !near(w.devVol[webcam.Name], 1) || w.devVol[goxlr.Name] != 0 {
		t.Errorf("device volumes = %v", w.devVol)
	}
	w.wantNotice(slog.LevelWarn, "several input devices match")

	w2 := newWorld(t, testSetup(), State{})
	w2.do(AudioSnapshot{Devices: []Device{webcam}})
	w2.wantNotice(slog.LevelWarn, "no input device matches")
}

func TestDeviceHotplug(t *testing.T) {
	w := newWorld(t, testSetup(), State{Positions: map[Control]int{{Slider, 8}: 64}})
	w.do(AudioSnapshot{})
	w.press(ButtonM, 8)
	w.do(DeviceAdded{goxlr}) // plugged in later: gets position and mute
	if !near(w.devVol[goxlr.Name], 64.0/127) || !w.devMute[goxlr.Name] {
		t.Errorf("new device: vol %v mute %v", w.devVol[goxlr.Name], w.devMute[goxlr.Name])
	}
	w.do(DeviceRemoved{goxlr.Name})
	w.move(Slider, 8, 10)
	if near(w.devVol[goxlr.Name], 10.0/127) {
		t.Error("volume sent to a removed device")
	}
}

// ---- configuration changes ---------------------------------------------------

func TestCFG06_ReassignedControlDrivesTheNewApp(t *testing.T) {
	w := started(t)
	w.move(Slider, 1, 50)
	s := testSetup()
	s.Assignments[Control{Slider, 1}] = "discord"
	delete(s.Assignments, Control{Slider, 3})
	w.do(ConfigChanged{s})
	w.wantVolume(discord.ID, 50.0/127) // position is physical: applies to the new app
	w.move(Slider, 1, 20)
	w.wantVolume(discord.ID, 20.0/127)
	w.noActionFor(spotify.ID)
}

func TestCFG06_MutesAreReleasedWhenAnAppIsUnassigned(t *testing.T) {
	w := started(t)
	w.press(ButtonM, 2).press(ButtonS, 1) // browser muted, everything else silenced
	s := testSetup()
	delete(s.Assignments, Control{Slider, 2})
	delete(s.Assignments, Control{Knob, 1})
	w.do(ConfigChanged{s})
	w.wantMuted(false, firefox.ID, steam.ID) // no longer controlled: not left muted
	w.wantMuted(true, discord.ID)            // still assigned, solo still on slider1
	w.wantLEDs(2, false, false, false)
}

func TestCFG06_MuteAndSoloEndWhenTheControlChangesApp(t *testing.T) {
	w := started(t)
	w.press(ButtonM, 2).press(ButtonS, 3)
	s := testSetup()
	s.Assignments[Control{Slider, 2}] = "spare"
	s.Assignments[Control{Slider, 3}] = "mic"
	delete(s.Assignments, Control{Slider, 8})
	w.do(ConfigChanged{s})
	if w.m.muted[Control{Slider, 2}] || w.m.solo != 0 {
		t.Errorf("muted=%v solo=%d, want both cleared", w.m.muted, w.m.solo)
	}
	w.wantMuted(false, spotify.ID, firefox.ID)
}

func TestCFG06_InputChangesDevice(t *testing.T) {
	w := started(t)
	w.press(ButtonM, 8) // mute the GoXLR
	s := testSetup()
	s.Targets["mic"] = Target{ID: "mic", Name: "Microphone", Kind: Input, Match: []string{"c922"}}
	w.do(ConfigChanged{s})
	if w.devMute[goxlr.Name] {
		t.Error("the old device was left muted")
	}
	if !w.devMute[webcam.Name] {
		t.Error("the new device did not get the mute")
	}
}

func TestPercent(t *testing.T) {
	for v, want := range map[int]int{0: 0, 127: 100, 64: 50, 1: 1, 200: 100} {
		if got := Percent(v); got != want {
			t.Errorf("Percent(%d) = %d, want %d", v, got, want)
		}
	}
}

func TestStrings(t *testing.T) {
	if (Control{Knob, 2}).String() != "knob2" || (LED{Button: ButtonM, Column: 3}).String() != "M3" ||
		(LED{Transport: Record}).String() != "●" || TransportButton(99).String() != "transport99" {
		t.Error("String methods")
	}
}

func TestCFG06_InputMovedToAnotherControl(t *testing.T) {
	w := started(t)
	w.move(Slider, 8, 127).move(Slider, 7, 30)
	s := testSetup()
	delete(s.Assignments, Control{Slider, 8})
	s.Assignments[Control{Slider, 7}] = "mic"
	w.do(ConfigChanged{s})
	if !near(w.devVol[goxlr.Name], 30.0/127) {
		t.Errorf("mic volume = %v, want slider7's position", w.devVol[goxlr.Name])
	}
	w.wantLEDs(7, true, true, true)
	w.wantLEDs(8, false, false, false)
}

func TestSVC07_ShutdownEndsSolo(t *testing.T) {
	w := started(t)
	w.press(ButtonM, 3).press(ButtonS, 1)
	for _, a := range w.m.Shutdown() {
		w.last = nil
		switch a := a.(type) {
		case SetStreamMute:
			w.streamMute[a.StreamID] = a.Muted
		case SetLED:
			w.leds[a.LED] = a.On
		}
	}
	w.wantMuted(false, firefox.ID, steam.ID) // were silenced by solo only
	w.wantMuted(true, discord.ID)            // user mute stays (it is saved)
	w.wantLEDs(1, false, false, false)
	if len(New(testSetup(), State{}).Shutdown()) != 0 {
		t.Error("Shutdown without solo should do nothing")
	}
}

func TestParseControl(t *testing.T) {
	for s, want := range map[string]Control{"slider1": {Slider, 1}, "knob8": {Knob, 8}} {
		if c, ok := ParseControl(s); !ok || c != want || c.String() != s {
			t.Errorf("ParseControl(%q) = %v, %v", s, c, ok)
		}
	}
	for _, s := range []string{"", "slider", "slider0", "slider9", "slider10", "knob-1", "fader1", "Slider1"} {
		if _, ok := ParseControl(s); ok {
			t.Errorf("ParseControl(%q) accepted", s)
		}
	}
}
