package mixer

import (
	"log/slog"
	"slices"
	"testing"

	"github.com/Dzobash/apptrol/internal/logattr"
)

// defaultSetup is outputSetup with an output that follows the system's
// default on slider6, before the fixed outputs (placeholders):
//
//	slider6 = follow (output, device = "default")
//	slider7 = main   (output, match "speakers")
//	knob7   = phones (output, match "headset")
func defaultSetup() Setup {
	s := outputSetup()
	s.Targets["follow"] = Target{ID: "follow", Name: "Main volume", Kind: Output, FollowsDefault: true}
	s.Assignments[Control{Slider, 6}] = "follow"
	return s
}

// onlyDefault is defaultSetup without the fixed outputs, so nothing overlaps.
func onlyDefault() Setup {
	s := defaultSetup()
	delete(s.Assignments, Control{Slider, 7})
	delete(s.Assignments, Control{Knob, 7})
	return s
}

// defaultWorld is outputWorld for setup, with def as the server's default.
func defaultWorld(t *testing.T, setup Setup, saved State, def string) *world {
	t.Helper()
	w := newWorld(t, setup, saved)
	w.do(ControllerConnected{})
	w.do(AudioSnapshot{Outputs: []Device{speakers, headphones}, DefaultOutput: def})
	w.kept, w.released = nil, nil
	return w
}

// The control acts on the server's default output and follows each change of
// it at once: the new default keeps its own volume until the control moves,
// the control's mute applies to it at once, and the previous default keeps
// its volume and is unmuted, with nothing written to it afterwards (OUT-03,
// ADR 0027 point 3, ADR 0038).
func TestOUT03_DefaultOutputFollowsEveryChange(t *testing.T) {
	w := defaultWorld(t, onlyDefault(), State{}, speakers.Name)
	if got := w.m.OutputDevice("follow"); got != speakers.Name {
		t.Fatalf("follows %q, want the default %q", got, speakers.Name)
	}
	w.move(Slider, 6, 127)
	w.wantOutputVolume(speakers.Name, 1)
	w.press(ButtonM, 6)
	if !w.outMute[speakers.Name] || !w.led(ButtonM, 6) {
		t.Fatalf("M did not mute the default output: mute %v, LED %v", w.outMute[speakers.Name], w.led(ButtonM, 6))
	}

	w.notices = nil
	w.do(DefaultOutputChanged{Name: headphones.Name})
	if got := w.m.OutputDevice("follow"); got != headphones.Name {
		t.Fatalf("follows %q after the change, want %q", got, headphones.Name)
	}
	if _, set := w.outVol[headphones.Name]; set {
		t.Errorf("the new default got the control's volume %v before a move (hearing safety)", w.outVol[headphones.Name])
	}
	if !w.outMute[headphones.Name] || w.outMute[speakers.Name] || !w.led(ButtonM, 6) {
		t.Errorf("mute did not move with the control: headphones %v, speakers %v, LED %v",
			w.outMute[headphones.Name], w.outMute[speakers.Name], w.led(ButtonM, 6))
	}
	if len(w.released) != 1 || w.released[0] != speakers.Name {
		t.Errorf("released %v, want the previous default", w.released)
	}
	w.wantOutputVolume(speakers.Name, 1) // kept, not changed
	changed := w.noticesNamed("default output changed")
	if len(changed) != 1 || changed[0].Level != slog.LevelInfo {
		t.Fatalf("default output changed records = %v", changed)
	}
	if a := attrs(changed[0]); a[logattr.KeyDeviceName] != headphones.Name || a[logattr.KeyDevicePreviousName] != speakers.Name {
		t.Errorf("default output changed attributes = %v", a)
	}
	matched := w.noticesNamed("output matched")
	if len(matched) != 1 || attrs(matched[0])[logattr.KeyDeviceSelectedBy] != "default" || attrs(matched[0])[logattr.KeyControl] != "slider6" {
		t.Errorf("output matched records = %v", matched)
	}
	if keeps := w.noticesNamed("default output keeps its volume until the control is moved"); len(keeps) != 1 || keeps[0].Level != slog.LevelInfo {
		t.Errorf("keeps its volume records = %v", keeps)
	}

	w.move(Slider, 6, 64)
	w.wantOutputVolume(headphones.Name, 64.0/127)
	w.wantOutputVolume(speakers.Name, 1)
	for _, a := range w.last {
		switch a := a.(type) {
		case SetOutputVolume:
			if a.Device == speakers.Name {
				t.Errorf("a move after the change wrote the previous default: %+v", a)
			}
		case SetOutputMute:
			if a.Device == speakers.Name {
				t.Errorf("a move after the change wrote the previous default: %+v", a)
			}
		}
	}

	w.last = nil
	w.do(DefaultOutputChanged{Name: headphones.Name}) // the same default again: nothing to do
	if len(w.last) != 0 {
		t.Errorf("an unchanged default caused %v", w.last)
	}
}

// The default output gets the control's position only from a move: not at
// connect, not on a reload, not when the default changes, as the saved
// position may belong to another device (OUT-03, ADR 0027 point 3, ADR 0038).
func TestOUT03_DefaultOutputVolumeIsSetOnlyByAMove(t *testing.T) {
	saved := State{Positions: map[Control]int{{Slider, 6}: 127}}
	w := defaultWorld(t, onlyDefault(), saved, speakers.Name)
	w.do(ConfigChanged{Setup: onlyDefault()})
	w.do(DefaultOutputChanged{Name: headphones.Name})
	w.do(AudioSnapshot{Outputs: []Device{speakers, headphones}, DefaultOutput: headphones.Name})
	if len(w.outVol) != 0 {
		t.Errorf("default output volume set without a move: %v", w.outVol)
	}
	w.move(Slider, 6, 127)
	w.wantOutputVolume(headphones.Name, 1)
}

// Without a default output, or with one the server names but has not
// reported yet, the control does nothing and says so; once the device is
// there, the control acts on it (OUT-03, OUT-13).
func TestOUT03_NoDefaultOutputDoesNothing(t *testing.T) {
	dock := Device{Name: "alsa_output.usb-Brand_Dock-00.analog-stereo", Description: "USB Dock"}
	w := defaultWorld(t, onlyDefault(), State{}, "")
	none := w.noticesNamed("no default output; the control does nothing until there is one")
	if len(none) != 1 || none[0].Level != slog.LevelInfo || attrs(none[0])[logattr.KeyControl] != "slider6" {
		t.Fatalf("no default output records = %v", none)
	}
	w.move(Slider, 6, 100)
	if len(w.outVol) != 0 {
		t.Errorf("a control without a default output set %v", w.outVol)
	}

	w.notices = nil
	w.do(DefaultOutputChanged{Name: dock.Name}) // named before the device is reported
	if got := w.m.OutputDevice("follow"); got != "" {
		t.Errorf("follows %q before the device is there", got)
	}
	w.do(OutputAdded{Device: dock})
	if got := w.m.OutputDevice("follow"); got != dock.Name {
		t.Errorf("follows %q once the default is there, want %q", got, dock.Name)
	}
	w.do(OutputRemoved{Name: dock.Name})
	if got := w.m.OutputDevice("follow"); got != "" {
		t.Errorf("follows %q after the default went", got)
	}
	if len(w.noticesNamed("no default output; the control does nothing until there is one")) != 1 {
		t.Errorf("the default going away is not logged: %v", w.notices)
	}
}

// A mute or unmute made outside Apptrol on the default output, e.g. with the
// keyboard's mute key, is taken over by the control that follows it
// (OUT-05, MUTE-07, ADR 0035).
func TestOUT05_OutsideMuteOnTheDefaultOutputIsTakenOver(t *testing.T) {
	w := defaultWorld(t, onlyDefault(), State{}, speakers.Name)
	w.do(OutputMuteChanged{Name: speakers.Name, Muted: true})
	if !w.m.Snapshot().Muted[Control{Slider, 6}] || !w.led(ButtonM, 6) {
		t.Errorf("outside mute not taken over: saved %v, LED %v", w.m.Snapshot().Muted[Control{Slider, 6}], w.led(ButtonM, 6))
	}
}

// When the default output is a device a fixed output also holds, the first
// control owns it, sliders 1–8 then knobs 1–8, as for fixed outputs; the
// overlap is allowed, so it is logged at info, not as a conflict. The later
// control does nothing while they share the device, and gets it back with its
// own position and saved mute when the default moves away (OUT-04, ADR 0035,
// ADR 0038).
func TestOUT04_DefaultOverlapFirstControlOwns(t *testing.T) {
	const overlap = "default output is the same device as another output; the earlier control owns it"
	saved := State{Positions: map[Control]int{{Slider, 7}: 64}, Muted: map[Control]bool{{Slider, 7}: true}}

	// slider6 follows the default; slider7 holds the speakers.
	w := defaultWorld(t, defaultSetup(), saved, headphones.Name)
	w.wantOutputVolume(speakers.Name, 64.0/127)
	if !w.outMute[speakers.Name] {
		t.Fatal("slider7's saved mute is not on the speakers")
	}
	w.notices = nil
	w.do(DefaultOutputChanged{Name: speakers.Name})
	if w.m.OutputDevice("follow") != speakers.Name || w.m.OutputDevice("main") != "" {
		t.Fatalf("owners: follow %q, main %q; want slider6 to own the speakers", w.m.OutputDevice("follow"), w.m.OutputDevice("main"))
	}
	if w.outMute[speakers.Name] || w.led(ButtonM, 7) {
		t.Errorf("slider6's mute (off) does not apply: speakers muted %v, M7 %v", w.outMute[speakers.Name], w.led(ButtonM, 7))
	}
	n := w.noticesNamed(overlap)
	if len(n) != 1 || n[0].Level != slog.LevelInfo {
		t.Fatalf("overlap records = %v", n)
	}
	if a := attrs(n[0]); a[logattr.KeyControl] != "slider7" || a[logattr.KeyDeviceName] != speakers.Name || a[logattr.KeyDeviceControls] != "slider6, slider7" {
		t.Errorf("overlap attributes = %v", a)
	}
	if len(w.noticesNamed("output match ignored: an earlier control owns the device")) != 0 {
		t.Error("the allowed overlap was logged as a conflict")
	}
	w.move(Slider, 7, 127)
	w.press(ButtonM, 7)
	w.wantOutputVolume(speakers.Name, 64.0/127)
	if w.outMute[speakers.Name] || !w.m.Snapshot().Muted[Control{Slider, 7}] {
		t.Errorf("the later control changed the shared device: muted %v, saved %v", w.outMute[speakers.Name], w.m.Snapshot().Muted[Control{Slider, 7}])
	}

	// The default moves to the headphones: slider7 takes the speakers back
	// with its position and saved mute; knob7 now yields to slider6.
	w.notices = nil
	w.do(DefaultOutputChanged{Name: headphones.Name})
	if w.m.OutputDevice("main") != speakers.Name || w.m.OutputDevice("follow") != headphones.Name || w.m.OutputDevice("phones") != "" {
		t.Fatalf("owners: main %q, follow %q, phones %q", w.m.OutputDevice("main"), w.m.OutputDevice("follow"), w.m.OutputDevice("phones"))
	}
	w.wantOutputVolume(speakers.Name, 1)
	if !w.outMute[speakers.Name] || !w.led(ButtonM, 7) {
		t.Errorf("slider7's saved mute not applied again: %v, LED %v", w.outMute[speakers.Name], w.led(ButtonM, 7))
	}
	if n := w.noticesNamed(overlap); len(n) != 1 || attrs(n[0])[logattr.KeyControl] != "knob7" {
		t.Errorf("overlap records = %v", n)
	}

	// The other way round: the default on knob8, after every fixed output.
	s := defaultSetup()
	delete(s.Assignments, Control{Slider, 6})
	s.Assignments[Control{Knob, 8}] = "follow"
	w = defaultWorld(t, s, State{}, speakers.Name)
	if w.m.OutputDevice("main") != speakers.Name || w.m.OutputDevice("follow") != "" {
		t.Fatalf("owners: main %q, follow %q; want slider7 to keep the speakers", w.m.OutputDevice("main"), w.m.OutputDevice("follow"))
	}
	if n := w.noticesNamed(overlap); len(n) != 1 || attrs(n[0])[logattr.KeyControl] != "knob8" || attrs(n[0])[logattr.KeyDeviceControls] != "slider7, knob8" {
		t.Errorf("overlap records = %v", n)
	}
	w.move(Knob, 8, 127)
	if _, set := w.outVol[speakers.Name]; set {
		t.Error("knob8 set the speakers slider7 owns")
	}
	w.do(OutputMuteChanged{Name: speakers.Name, Muted: true}) // the mute key
	if !w.m.Snapshot().Muted[Control{Slider, 7}] || w.m.Snapshot().Muted[Control{Knob, 8}] {
		t.Errorf("outside mute went to %v, want slider7, the owner", w.m.Snapshot().Muted)
	}
}

// When the default moves onto a device another control held, that control
// may have set a volume the adapter's new-output guard still defends; the
// default control sets none, so the mixer tells the adapter to forget it. A
// default change onto a device no control held, or a fixed control taking a
// device back, sends nothing of the kind (OUT-03, OUT-04, ADR 0038 point 6).
//
//	knob7 holds the headphones ─ default: headphones ─► slider6 owns them, ForgetOutputVolume
func TestOUT03_DefaultTakeoverForgetsTheEarlierOwnersVolume(t *testing.T) {
	w := defaultWorld(t, defaultSetup(), State{}, speakers.Name)
	w.do(DefaultOutputChanged{Name: headphones.Name}) // knob7 held them; slider7 takes the speakers back
	if !slices.Equal(w.forgot, []string{headphones.Name}) {
		t.Fatalf("forgotten volumes = %v, want the headphones only", w.forgot)
	}
	w.do(DefaultOutputChanged{Name: speakers.Name}) // slider7 held them
	if !slices.Equal(w.forgot, []string{headphones.Name, speakers.Name}) {
		t.Fatalf("forgotten volumes = %v, want the speakers too", w.forgot)
	}

	w = defaultWorld(t, onlyDefault(), State{}, speakers.Name)
	w.do(DefaultOutputChanged{Name: headphones.Name})
	if len(w.forgot) != 0 {
		t.Fatalf("forgotten volumes = %v, want none: no control held the headphones", w.forgot)
	}
}

// A reload turns the target that holds the default device from a fixed match
// into device = "default": the device and the target stay, but the volume
// the fixed control set is no longer the owner's to defend (OUT-03,
// ADR 0038 point 6, #191 review). A reload that keeps a default control on
// its device sends nothing.
func TestOUT03_ReloadToDefaultForgetsTheFixedVolume(t *testing.T) {
	fixed := onlyDefault()
	fixed.Targets["follow"] = Target{ID: "follow", Name: "Main volume", Kind: Output, Match: []string{"headset"}}
	w := defaultWorld(t, fixed, State{Positions: map[Control]int{{Slider, 6}: 102}}, headphones.Name)
	if w.m.OutputDevice("follow") != headphones.Name {
		t.Fatalf("fixed target holds %q, want the headphones", w.m.OutputDevice("follow"))
	}
	w.do(ConfigChanged{Setup: onlyDefault()})
	if !slices.Equal(w.forgot, []string{headphones.Name}) {
		t.Fatalf("forgotten volumes = %v, want the headphones", w.forgot)
	}
	w.do(ConfigChanged{Setup: onlyDefault()})
	if len(w.forgot) != 1 {
		t.Fatalf("forgotten volumes = %v, want nothing more from a reload that changes nothing", w.forgot)
	}
}

// A snapshot after a reconnect or a resync can bring a new default without a
// DefaultOutputChanged. The device the control held before is released as on
// a change: unmuted, not left with the control's mute (OUT-03, ADR 0038).
func TestOUT03_SnapshotWithAnotherDefaultReleasesThePreviousOne(t *testing.T) {
	for _, def := range []string{headphones.Name, ""} {
		w := defaultWorld(t, onlyDefault(), State{}, speakers.Name)
		w.press(ButtonM, 6)
		if !w.outMute[speakers.Name] {
			t.Fatal("M did not mute the default output")
		}
		w.do(AudioSnapshot{Outputs: []Device{speakers, headphones}, DefaultOutput: def})
		if w.outMute[speakers.Name] {
			t.Errorf("default %q: the previous default stays muted after the snapshot", def)
		}
		if len(w.noticesNamed("output released; no control holds the device")) == 0 {
			t.Errorf("default %q: the release is not logged", def)
		}
		if def != "" && !w.outMute[headphones.Name] {
			t.Errorf("the new default does not get the control's mute")
		}
	}
}
