package mixer

import (
	"log/slog"
	"testing"

	"github.com/Dzobash/apptrol/internal/logattr"
)

// Output devices used in the tests (placeholders, not real hardware).
var (
	speakers   = Device{Name: "alsa_output.platform-test_speakers.analog-stereo", Description: "Built-in Audio Analog Stereo"}
	headphones = Device{Name: "alsa_output.usb-Brand_Headset-00.analog-stereo", Description: "USB Headset"}
)

// idleOutputs returns the "output device not on a control" records among
// notices, in order, as device name → description; each must be at debug.
func idleOutputs(t *testing.T, notices []Notice) (names, descriptions []string) {
	t.Helper()
	for _, n := range notices {
		if n.Msg != "output device not on a control" {
			continue
		}
		if n.Level != slog.LevelDebug {
			t.Errorf("%q logged at %v, want debug", n.Msg, n.Level)
		}
		attrs := map[any]any{}
		for i := 0; i+1 < len(n.Attrs); i += 2 {
			attrs[n.Attrs[i]] = n.Attrs[i+1]
		}
		name, _ := attrs[logattr.KeyDeviceName].(string)
		desc, _ := attrs[logattr.KeyDeviceDescription].(string)
		names, descriptions = append(names, name), append(descriptions, desc)
	}
	return names, descriptions
}

// Every output device Apptrol sees is logged at debug with its unique name
// and description, so the log shows what a match list can use (OUT-13,
// LOG-15, ADR 0027).
func TestOUT13_OutputDevicesOnNoControlAreLoggedAtDebug(t *testing.T) {
	w := newWorld(t, testSetup(), State{})
	w.do(AudioSnapshot{Outputs: []Device{headphones, speakers}})
	names, descs := idleOutputs(t, w.notices)
	if len(names) != 2 || names[0] != speakers.Name || names[1] != headphones.Name {
		t.Errorf("logged %v, want both outputs sorted by name", names)
	}
	if len(descs) == 2 && (descs[0] != speakers.Description || descs[1] != headphones.Description) {
		t.Errorf("descriptions = %v", descs)
	}

	w.notices = nil
	dock := Device{Name: "alsa_output.usb-Brand_Dock-00.analog-stereo", Description: "USB Dock"}
	w.do(OutputAdded{Device: dock})
	if names, _ := idleOutputs(t, w.notices); len(names) != 1 || names[0] != dock.Name {
		t.Errorf("a new output logged as %v", names)
	}

	w.notices = nil
	w.do(OutputRemoved{Name: dock.Name})
	if names, _ := idleOutputs(t, w.notices); len(names) != 0 {
		t.Errorf("a removed output logged as %v", names)
	}
}

// The mixer keeps the output devices the audio server reports, and replaces
// them all on a snapshot after a reconnect (OUT-12, SVC-04).
func TestOUT12_MixerKeepsOutputDevices(t *testing.T) {
	w := newWorld(t, testSetup(), State{})
	w.do(AudioSnapshot{Outputs: []Device{speakers}})
	w.do(OutputAdded{Device: headphones})
	if len(w.m.outputs) != 2 {
		t.Fatalf("outputs = %v", w.m.outputs)
	}
	renamed := Device{Name: headphones.Name, Description: "Headset (renamed)"}
	w.do(OutputAdded{Device: renamed})
	if w.m.outputs[headphones.Name] != renamed {
		t.Errorf("renamed output = %v", w.m.outputs[headphones.Name])
	}
	w.do(OutputRemoved{Name: speakers.Name})
	if _, still := w.m.outputs[speakers.Name]; still || len(w.m.outputs) != 1 {
		t.Errorf("after removal: %v", w.m.outputs)
	}
	w.do(AudioSnapshot{Outputs: []Device{speakers}})
	if len(w.m.outputs) != 1 || w.m.outputs[speakers.Name] != speakers {
		t.Errorf("after a snapshot: %v", w.m.outputs)
	}
}

// outputSetup is testSetup with two fixed outputs (placeholders):
//
//	slider7 = main   (output, match "speakers": the built-in speakers)
//	knob7   = phones (output, match "headset": the USB headset)
func outputSetup() Setup {
	s := testSetup()
	s.Targets["main"] = Target{ID: "main", Name: "Speakers", Kind: Output, Match: []string{"speakers"}}
	s.Targets["phones"] = Target{ID: "phones", Name: "Headphones", Kind: Output, Match: []string{"headset"}}
	s.Assignments[Control{Slider, 7}] = "main"
	s.Assignments[Control{Knob, 7}] = "phones"
	return s
}

// outputWorld: outputSetup with the usual streams and inputs, and both outputs present.
func outputWorld(t *testing.T, setup Setup) *world {
	t.Helper()
	w := newWorld(t, setup, State{})
	w.do(ControllerConnected{})
	w.do(AudioSnapshot{Streams: []Stream{spotify, firefox, discord, steam, viber}, Devices: []Device{goxlr, webcam},
		Outputs: []Device{speakers, headphones}})
	return w
}

// noticesNamed returns the notices with message msg.
func (w *world) noticesNamed(msg string) []Notice {
	var out []Notice
	for _, n := range w.notices {
		if n.Msg == msg {
			out = append(out, n)
		}
	}
	return out
}

// outputActions returns the SetOutputVolume actions of the most recent event.
func (w *world) outputActions() []SetOutputVolume {
	var out []SetOutputVolume
	for _, a := range w.last {
		if o, ok := a.(SetOutputVolume); ok {
			out = append(out, o)
		}
	}
	return out
}

// noStreamOrInputActions fails if the most recent event changed a stream or an input.
func (w *world) noStreamOrInputActions() {
	w.t.Helper()
	for _, a := range w.last {
		switch a.(type) {
		case SetStreamVolume, SetStreamMute, SetDeviceVolume, SetDeviceMute:
			w.t.Errorf("an output control caused %T: %+v", a, a)
		}
	}
}

func (w *world) wantOutputVolume(dev string, want float64) {
	w.t.Helper()
	got, ok := w.outVol[dev]
	if !ok || !near(got, want) {
		w.t.Errorf("output %s volume = %v (set: %v), want %v", dev, got, ok, want)
	}
}

// A slider on an output sets the output device's volume, up to max_volume,
// and no stream's volume (OUT-01, CTRL-02, CTRL-03).
func TestOUT01_SliderSetsOutputVolume(t *testing.T) {
	setup := outputSetup()
	main := setup.Targets["main"]
	main.MaxVolume = 0.8
	setup.Targets["main"] = main
	w := outputWorld(t, setup)
	w.move(Slider, 7, 64)
	w.wantOutputVolume(speakers.Name, 64.0/127*0.8)
	w.noStreamOrInputActions()
	if _, set := w.outVol[headphones.Name]; set {
		t.Error("the other output's volume was changed")
	}
	w.move(Slider, 7, 127)
	w.wantOutputVolume(speakers.Name, 0.8)
	n := w.noticesNamed("volume changed")
	if a := attrs(n[len(n)-1]); a[logattr.KeyAppType] != "output" || a[logattr.KeyControl] != "slider7" || a[logattr.KeyVolume] != 80 {
		t.Errorf("volume changed record = %v", a)
	}
}

func TestOUT01_KnobSetsOutputVolume(t *testing.T) {
	w := outputWorld(t, outputSetup())
	w.move(Knob, 7, 127)
	w.wantOutputVolume(headphones.Name, 1)
	w.move(Knob, 7, 0)
	w.wantOutputVolume(headphones.Name, 0)
	w.noStreamOrInputActions()
	if _, set := w.outVol[speakers.Name]; set {
		t.Error("the other output's volume was changed")
	}
}

// An output device that no control holds is never touched, even when an
// output target on no control matches it (OUT-01, CTRL-06).
func TestOUT01_OutputsOnNoControlAreNotTouched(t *testing.T) {
	setup := outputSetup()
	dock := Device{Name: "alsa_output.usb-Brand_Dock-00.analog-stereo", Description: "USB Dock"}
	setup.Targets["dock"] = Target{ID: "dock", Name: "Dock", Kind: Output, Match: []string{"dock"}}
	w := outputWorld(t, setup)
	w.do(OutputAdded{Device: dock})
	for col := 1; col <= NumColumns; col++ {
		w.move(Slider, col, 100).move(Knob, col, 100)
	}
	w.do(ConfigChanged{Setup: setup})
	if _, set := w.outVol[dock.Name]; set {
		t.Errorf("an output on no control was set to %v", w.outVol[dock.Name])
	}
	if w.m.OutputDevice("dock") != "" {
		t.Errorf("a target on no control holds %q", w.m.OutputDevice("dock"))
	}
}

// A control that is moved to another target, or a target moved to another
// control, gets its volume set again on a reload, as inputs do (OUT-01, CFG-06).
func TestOUT01_OutputMovedToAnotherControlIsSetAgain(t *testing.T) {
	w := outputWorld(t, outputSetup())
	w.move(Slider, 6, 127).move(Slider, 7, 64)
	s := outputSetup()
	delete(s.Assignments, Control{Slider, 7})
	s.Assignments[Control{Slider, 6}] = "main"
	w.do(ConfigChanged{Setup: s})
	w.wantOutputVolume(speakers.Name, 1)

	main := s.Targets["main"]
	main.MaxVolume = 0.5
	s.Targets["main"] = main
	w.do(ConfigChanged{Setup: s})
	w.wantOutputVolume(speakers.Name, 0.5)

	w.do(ConfigChanged{Setup: s}) // nothing changed: nothing is set
	if got := w.outputActions(); len(got) != 0 {
		t.Errorf("an unchanged reload set %v", got)
	}
}

// match is a case-insensitive part of the device's name or description
// (OUT-02, CFG-05).
func TestOUT02_MatchIsCaseInsensitiveOnNameOrDescription(t *testing.T) {
	for _, tt := range []struct{ match, want string }{
		{"BUILT-IN audio", speakers.Name},  // description
		{"PLATFORM-test", speakers.Name},   // name
		{"brand_HEADSET", headphones.Name}, // name
		{"usb headset", headphones.Name},   // description
		{"no such device", ""},             // nothing
		{"analog-stereo.monitor", ""},      // a monitor is never an output
		{"  speakers ", speakers.Name},     // trimmed, as every fragment
		{"Built-in Audio Analog Stereo", speakers.Name},
	} {
		s := outputSetup()
		main := s.Targets["main"]
		main.Match = []string{tt.match}
		s.Targets["main"] = main
		w := outputWorld(t, s)
		if got := w.m.OutputDevice("main"); got != tt.want {
			t.Errorf("match %q chose %q, want %q", tt.match, got, tt.want)
		}
	}
}

// Several matching devices: the first by name is used, and a warning names
// all of them (OUT-02, OUT-13).
func TestOUT02_SeveralMatchesUseFirstByNameAndWarn(t *testing.T) {
	s := outputSetup()
	main := s.Targets["main"]
	main.Match = []string{"alsa_output"}
	s.Targets["main"] = main
	w := outputWorld(t, s)
	if got := w.m.OutputDevice("main"); got != speakers.Name {
		t.Fatalf("chose %q, want the first by name %q", got, speakers.Name)
	}
	warn := w.noticesNamed("several output devices match; using the first (make the match more specific: apptrol list shows each device's unique name)")
	if len(warn) != 1 || warn[0].Level != slog.LevelWarn {
		t.Fatalf("warnings = %v", warn)
	}
	a := attrs(warn[0])
	if a[logattr.KeyDeviceName] != speakers.Name || a[logattr.KeyDeviceMatches] != speakers.Name+", "+headphones.Name ||
		a[logattr.KeyControl] != "slider7" || a[logattr.KeyAppType] != "output" {
		t.Errorf("warning attributes = %v", a)
	}
	w.notices = nil
	w.do(OutputAdded{Device: Device{Name: headphones.Name, Description: "Renamed"}})
	if len(w.noticesNamed(warn[0].Msg)) != 0 {
		t.Error("warned again without a new pick")
	}
}

// The chosen device is kept while it still matches, even when a device that
// comes first by name appears; when it goes, the next match takes over and
// gets the control's known position (OUT-02, OUT-05).
func TestOUT02_ChosenDeviceIsKeptWhileItMatches(t *testing.T) {
	s := outputSetup()
	main := s.Targets["main"]
	main.Match = []string{"alsa_output"}
	s.Targets["main"] = main
	delete(s.Assignments, Control{Knob, 7})
	w := newWorld(t, s, State{})
	w.do(AudioSnapshot{Outputs: []Device{headphones}})
	w.move(Slider, 7, 127)
	w.wantOutputVolume(headphones.Name, 1)
	w.do(OutputAdded{Device: speakers})
	if got := w.m.OutputDevice("main"); got != headphones.Name {
		t.Errorf("switched to %q while the chosen device still matches", got)
	}
	if _, set := w.outVol[speakers.Name]; set {
		t.Error("the new device was set although the chosen one is kept")
	}
	w.do(OutputRemoved{Name: headphones.Name})
	if got := w.m.OutputDevice("main"); got != speakers.Name {
		t.Errorf("after removal chose %q", got)
	}
	w.wantOutputVolume(speakers.Name, 1)
}

// Until the control is moved, an output's volume is left alone: at connect,
// when its device appears, and on a reload (OUT-05, PRIO-04).
func TestOUT05_UnknownPositionLeavesVolumeAlone(t *testing.T) {
	w := outputWorld(t, outputSetup())
	if len(w.outVol) != 0 {
		t.Fatalf("outputs set before any control moved: %v", w.outVol)
	}
	w.do(OutputRemoved{Name: speakers.Name}, OutputAdded{Device: speakers})
	w.do(ConfigChanged{Setup: outputSetup()})
	w.do(AudioSnapshot{Outputs: []Device{speakers, headphones}})
	if len(w.outVol) != 0 {
		t.Fatalf("outputs set before any control moved: %v", w.outVol)
	}
	// A saved position is known: it is set when the device is there (OUT-05).
	w = newWorld(t, outputSetup(), State{Positions: map[Control]int{{Slider, 7}: 127}})
	w.do(AudioSnapshot{Outputs: []Device{speakers, headphones}})
	w.wantOutputVolume(speakers.Name, 1)
	if _, set := w.outVol[headphones.Name]; set {
		t.Error("the knob's output was set without a known position")
	}
}

// A volume changed outside Apptrol (volume keys, the desktop's applet) is not
// reverted; the next move of the control sets its position (OUT-05, PRIO-01,
// PRIO-02).
func TestOUT05_OutsideVolumeIsNotReverted(t *testing.T) {
	w := outputWorld(t, outputSetup())
	w.move(Slider, 7, 127)
	w.outVol[speakers.Name] = 0.2 // the volume keys
	w.do(StreamAdded{Stream: Stream{ID: 30, AppName: "Spotify"}}, StreamRemoved{ID: 30})
	w.do(OutputAdded{Device: Device{Name: speakers.Name, Description: "Speakers (renamed)"}})
	w.do(OutputAdded{Device: Device{Name: "alsa_output.other_speakers", Description: "Other"}})
	w.do(ConfigChanged{Setup: outputSetup()})
	w.press(ButtonM, 1).press(ButtonS, 1).press(ButtonS, 1)
	w.do(ControllerConnected{}, SystemResumed{})
	w.wantOutputVolume(speakers.Name, 0.2)
	w.move(Slider, 7, 127)
	w.wantOutputVolume(speakers.Name, 1)
}

// M and S on an output column do nothing until output mute and output solo
// are built (#85; OUT-06, OUT-07 later), and say so at debug.
func TestOUT06_MAndSOnAnOutputColumnDoNothingYet(t *testing.T) {
	w := outputWorld(t, outputSetup())
	w.move(Slider, 7, 64)
	for _, b := range []ButtonKind{ButtonM, ButtonS} {
		w.notices = nil
		w.press(b, 7)
		n := notice(t, w.last)
		if n.Level != slog.LevelDebug || n.Msg != "button has no function: not built yet on an output column" {
			t.Errorf("%v7: %v", b, n)
		}
		if a := attrs(n); a[logattr.KeyButton] != b.String()+"7" || a[logattr.KeyAppType] != "output" {
			t.Errorf("%v7 attributes = %v", b, a)
		}
		for _, a := range w.last {
			if _, ok := a.(Notice); !ok {
				t.Errorf("%v7 caused %T %+v", b, a, a)
			}
		}
	}
	if w.m.solo != 0 || len(w.m.muted) != 0 {
		t.Errorf("solo = %d, muted = %v", w.m.solo, w.m.muted)
	}
	w.wantLEDs(7, false, false, false)
}

// App solo silences other apps, never an output; ending it does not touch
// outputs either (OUT-07, SOLO-02).
func TestOUT07_AppSoloDoesNotMuteOutputs(t *testing.T) {
	w := outputWorld(t, outputSetup())
	w.move(Slider, 7, 100).move(Knob, 7, 100)
	before := map[string]float64{speakers.Name: w.outVol[speakers.Name], headphones.Name: w.outVol[headphones.Name]}
	for _, step := range []func(){
		func() { w.press(ButtonS, 1) }, // solo on
		func() { w.press(ButtonS, 2) }, // moved
		func() { w.press(ButtonS, 2) }, // off
		func() { w.press(ButtonS, 1); w.shutdown() },
	} {
		step()
		if got := w.outputActions(); len(got) != 0 {
			t.Errorf("solo changed outputs: %v", got)
		}
		for _, a := range w.last {
			if d, ok := a.(SetDeviceMute); ok && (d.Device == speakers.Name || d.Device == headphones.Name) {
				t.Errorf("solo muted an output: %v", d)
			}
		}
	}
	for dev, v := range before {
		w.wantOutputVolume(dev, v)
	}
	w.wantLEDs(7, false, false, false)
}

// Talk-over turns apps down, never outputs, and an output whose control has
// not been moved is not muted for it (OUT-11, INPUT-04).
func TestOUT11_TalkOverDoesNotChangeOutputs(t *testing.T) {
	s := outputSetup()
	s.Buttons = map[LED]Button{s8: {Mode: ModeTalkOver}}
	w := outputWorld(t, s)
	w.move(Slider, 1, 127).move(Slider, 7, 127) // knob7 never moved
	w.press(ButtonS, 8)
	w.wantVolume(spotify.ID, 0.25) // apps go down…
	if got := w.outputActions(); len(got) != 0 {
		t.Errorf("talk-over changed outputs: %v", got)
	}
	w.release(ButtonS, 8)
	if got := w.outputActions(); len(got) != 0 {
		t.Errorf("the end of talk-over changed outputs: %v", got)
	}
	w.wantOutputVolume(speakers.Name, 1) // …outputs stay
	if _, set := w.outVol[headphones.Name]; set {
		t.Error("talk-over set an output whose control was never moved")
	}
	for _, n := range w.noticesNamed("talk-over mutes app: its control has not been moved yet") {
		if attrs(n)[logattr.KeyAppType] == "output" {
			t.Errorf("talk-over named an output: %v", n)
		}
	}
}

// Two fixed outputs may match the same device: each control sets its volume
// when moved, and this is logged once at info (OUT-04, ADR 0027 point 4).
func TestOUT04_TwoFixedOutputsShareADevice(t *testing.T) {
	s := outputSetup()
	s.Targets["also"] = Target{ID: "also", Name: "Speakers too", Kind: Output, Match: []string{"built-in"}}
	s.Assignments[Control{Slider, 6}] = "also"
	w := outputWorld(t, s)
	shared := w.noticesNamed("two controls set the same output")
	if len(shared) != 1 || shared[0].Level != slog.LevelInfo {
		t.Fatalf("records = %v", shared)
	}
	if a := attrs(shared[0]); a[logattr.KeyDeviceName] != speakers.Name || a[logattr.KeyDeviceControls] != "slider6, slider7" ||
		a[logattr.KeyControl] != "slider6" {
		t.Errorf("attributes = %v", a)
	}
	w.move(Slider, 6, 127)
	w.wantOutputVolume(speakers.Name, 1)
	w.move(Slider, 7, 0)
	w.wantOutputVolume(speakers.Name, 0)

	w.do(OutputAdded{Device: Device{Name: "alsa_output.other", Description: "Other"}}, ConfigChanged{Setup: s})
	if got := w.noticesNamed("two controls set the same output"); len(got) != 1 {
		t.Errorf("logged again while it went on: %v", got)
	}
	// It ends, and begins again: logged again.
	w.do(OutputRemoved{Name: speakers.Name}, OutputAdded{Device: speakers})
	if got := w.noticesNamed("two controls set the same output"); len(got) != 2 {
		t.Errorf("a new beginning was not logged: %d records", len(got))
	}
}

// Each output decision is logged with its reason (OUT-13, LOG-15).
func TestOUT13_OutputMatchedIsLogged(t *testing.T) {
	w := outputWorld(t, outputSetup())
	matched := w.noticesNamed("output matched")
	if len(matched) != 2 {
		t.Fatalf("records = %v", matched)
	}
	a := attrs(matched[0])
	want := map[string]any{logattr.KeyLayout: "default", logattr.KeyControl: "slider7", logattr.KeyAppID: "main",
		logattr.KeyApp: "Speakers", logattr.KeyAppType: "output", logattr.KeyDeviceName: speakers.Name,
		logattr.KeyDeviceDescription: speakers.Description, logattr.KeyDeviceSelectedBy: "match"}
	for k, v := range want {
		if a[k] != v {
			t.Errorf("%s = %v, want %v", k, a[k], v)
		}
	}
	if matched[0].Level != slog.LevelInfo || attrs(matched[1])[logattr.KeyControl] != "knob7" {
		t.Errorf("records = %v", matched)
	}
	// Devices that a target holds are not logged as on no control.
	if names, _ := idleOutputs(t, w.notices); len(names) != 0 {
		t.Errorf("held outputs logged as idle: %v", names)
	}
}

func TestOUT13_NoOutputDeviceMatchesAtConnectAndReload(t *testing.T) {
	w := newWorld(t, outputSetup(), State{})
	w.do(AudioSnapshot{Outputs: []Device{speakers}})
	warn := w.noticesNamed("no output device matches")
	if len(warn) != 1 || warn[0].Level != slog.LevelWarn {
		t.Fatalf("at connect: %v", warn)
	}
	if a := attrs(warn[0]); a[logattr.KeyMatch] != "headset" || a[logattr.KeyControl] != "knob7" {
		t.Errorf("attributes = %v", a)
	}
	w.notices = nil
	w.do(OutputAdded{Device: Device{Name: "alsa_output.other", Description: "Other"}})
	if got := w.noticesNamed("no output device matches"); len(got) != 0 {
		t.Errorf("warned on an unrelated device: %v", got)
	}
	w.do(ConfigChanged{Setup: outputSetup()})
	if got := w.noticesNamed("no output device matches"); len(got) != 1 {
		t.Errorf("after a reload: %v", got)
	}
}

func TestOUT13_OutputDeviceGoneIsLogged(t *testing.T) {
	w := outputWorld(t, outputSetup())
	w.notices = nil
	w.do(OutputRemoved{Name: headphones.Name})
	gone := w.noticesNamed("output device gone; the control does nothing until a device matches")
	if len(gone) != 1 || gone[0].Level != slog.LevelInfo {
		t.Fatalf("records = %v", gone)
	}
	if a := attrs(gone[0]); a[logattr.KeyDeviceName] != headphones.Name || a[logattr.KeyControl] != "knob7" {
		t.Errorf("attributes = %v", a)
	}
	if w.m.OutputDevice("phones") != "" {
		t.Error("the target still holds a removed device")
	}
	w.move(Knob, 7, 100)
	if got := w.outputActions(); len(got) != 0 {
		t.Errorf("a control without a device set %v", got)
	}
	w.notices = nil
	w.do(OutputAdded{Device: headphones})
	if len(w.noticesNamed("output matched")) != 1 {
		t.Error("the device that came back was not matched")
	}
	w.wantOutputVolume(headphones.Name, Volume(100))
}
