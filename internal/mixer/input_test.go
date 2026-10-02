package mixer

import (
	"log/slog"
	"testing"

	"github.com/Dzobash/apptrol/internal/logattr"
)

// Microphone column buttons (INPUT-*, ADR 0020). The test layout has the
// microphone on slider 8.

var (
	m8 = LED{Button: ButtonM, Column: 8}
	s8 = LED{Button: ButtonS, Column: 8}
)

// startedWith is started with the given M and S settings.
func startedWith(t *testing.T, buttons map[LED]Button, saved State) *world {
	t.Helper()
	setup := testSetup()
	setup.Buttons = buttons
	w := newWorld(t, setup, saved)
	w.do(ControllerConnected{})
	w.do(AudioSnapshot{Streams: []Stream{spotify, firefox, discord, steam, viber}, Devices: []Device{goxlr, webcam}})
	return w
}

func (w *world) micMuted() bool { return w.devMute[goxlr.Name] }

func (w *world) wantMic(muted bool) {
	w.t.Helper()
	if w.micMuted() != muted {
		w.t.Errorf("mic muted = %v, want %v", w.micMuted(), muted)
	}
	if w.led(ButtonM, 8) == muted { // LED-04: M lit while live
		w.t.Errorf("M8 LED = %v with the mic muted = %v", w.led(ButtonM, 8), muted)
	}
}

func (w *world) noticeAttr(msg, key string) any {
	w.t.Helper()
	for _, n := range w.notices {
		if n.Msg != msg {
			continue
		}
		for i := 0; i+1 < len(n.Attrs); i += 2 {
			if n.Attrs[i] == key {
				return n.Attrs[i+1]
			}
		}
	}
	w.t.Errorf("no %q notice with %s in %v", msg, key, w.notices)
	return nil
}

func TestINPUT01_Defaults(t *testing.T) {
	w := started(t)
	if w.m.mode(m8) != ModeMute || w.m.mode(s8) != ModeCough {
		t.Errorf("modes M8, S8 = %v, %v; want mute, cough", w.m.mode(m8), w.m.mode(s8))
	}
	w.press(ButtonM, 8)
	w.wantMic(true) // M toggles, as before
	w.press(ButtonM, 8)
	w.wantMic(false)
	w.press(ButtonR, 8)
	if len(effects(w.last)) != 0 {
		t.Errorf("R on the input column produced %v", w.last)
	}
}

func TestINPUT01_OffDoesNothing(t *testing.T) {
	w := startedWith(t, map[LED]Button{s8: {Mode: ModeOff}}, State{})
	w.press(ButtonS, 8)
	w.release(ButtonS, 8)
	if len(effects(w.last)) != 0 {
		t.Errorf("S in mode off produced %v", w.last)
	}
	w.wantNotice(slog.LevelDebug, "its mode is off")
}

func TestINPUT01_CoughIsOffWithHoldToTalk(t *testing.T) {
	w := startedWith(t, map[LED]Button{m8: {Mode: ModeHoldToTalk}}, State{})
	if w.m.mode(s8) != ModeOff {
		t.Errorf("S8 = %v with hold-to-talk, want off", w.m.mode(s8))
	}
}

func TestINPUT02_CoughMutesWhileHeld(t *testing.T) {
	w := started(t)
	saves := w.saves
	w.press(ButtonS, 8)
	w.wantMic(true)
	w.wantNotice(slog.LevelInfo, "cough on")
	w.release(ButtonS, 8)
	w.wantMic(false)
	w.wantNotice(slog.LevelInfo, "cough off")
	if w.saves != saves {
		t.Error("a cough was saved")
	}
}

func TestINPUT03_HoldToTalkIsLiveOnlyWhileHeld(t *testing.T) {
	w := startedWith(t, map[LED]Button{m8: {Mode: ModeHoldToTalk}}, State{})
	w.wantMic(true) // from the start
	w.press(ButtonM, 8)
	w.wantMic(false)
	w.wantNotice(slog.LevelInfo, "hold-to-talk live")
	w.release(ButtonM, 8)
	w.wantMic(true)
	w.wantNotice(slog.LevelInfo, "hold-to-talk muted")
}

func TestINPUT03_HoldToTalkDropsTheSavedMute(t *testing.T) {
	saved := State{Muted: map[Control]bool{{Slider, 8}: true}}
	w := startedWith(t, map[LED]Button{m8: {Mode: ModeHoldToTalk}}, saved)
	w.press(ButtonM, 8)
	w.wantMic(false)
	if w.m.Snapshot().Muted[Control{Slider, 8}] {
		t.Error("the M mute is still saved")
	}
}

func TestINPUT03_HoldToTalkUndoesOutsideChanges(t *testing.T) {
	w := startedWith(t, map[LED]Button{m8: {Mode: ModeHoldToTalk}}, State{})
	w.do(DeviceMuteChanged{Name: goxlr.Name, Muted: false}) // the desktop's mic key
	w.wantMic(true)
	w.wantNotice(slog.LevelInfo, "hold-to-talk decides")
	w.press(ButtonM, 8)
	w.do(DeviceMuteChanged{Name: goxlr.Name, Muted: true})
	w.wantMic(false)
}

func TestINPUT03_HoldToTalkStaysMutedWhenStopping(t *testing.T) {
	w := startedWith(t, map[LED]Button{m8: {Mode: ModeHoldToTalk}}, State{})
	w.press(ButtonM, 8)
	w.shutdown()
	if !w.micMuted() {
		t.Error("the mic is live after Apptrol stopped")
	}
	w.wantNotice(slog.LevelInfo, "stays muted while Apptrol is stopped")
}

// talkOverWorld: Spotify at 100 %, the browser at 10/127 (below 25 %), the
// game knob at 100 %, Discord never moved; S8 in talk-over mode.
func talkOverWorld(t *testing.T, buttons map[LED]Button) *world {
	t.Helper()
	w := startedWith(t, buttons, State{})
	w.move(Slider, 1, 127).move(Slider, 2, 10).move(Knob, 1, 127)
	return w
}

func TestINPUT04_TalkOverTurnsAppsDownNeverUp(t *testing.T) {
	w := talkOverWorld(t, map[LED]Button{s8: {Mode: ModeTalkOver}})
	w.press(ButtonS, 8)
	w.wantVolume(spotify.ID, 0.25)
	w.wantVolume(steam.ID, 0.25)       // knobs too
	w.wantVolume(firefox.ID, 10.0/127) // below: stays
	w.wantMic(false)                   // inputs are not changed
	if got := w.noticeAttr("talk-over on", logattr.KeyTalkOver); got != DefaultTalkOverVolume {
		t.Errorf("talk-over percent = %v", got)
	}
	w.release(ButtonS, 8)
	w.wantVolume(spotify.ID, 1)
	w.wantVolume(steam.ID, 1)
	w.wantVolume(firefox.ID, 10.0/127)
	w.wantNotice(slog.LevelInfo, "talk-over off")
	if _, set := w.volume(viber.ID); set {
		t.Error("an app on no control was changed")
	}
}

func TestINPUT04_TalkOverMutesAppsWithUnknownPosition(t *testing.T) {
	w := talkOverWorld(t, map[LED]Button{s8: {Mode: ModeTalkOver}})
	w.press(ButtonS, 8)
	w.wantMuted(true, discord.ID)
	w.wantNotice(slog.LevelInfo, "its control has not been moved yet")
	w.release(ButtonS, 8)
	w.wantMuted(false, discord.ID)
	if _, set := w.volume(discord.ID); set {
		t.Error("Discord's volume was set although its position is unknown")
	}
}

func TestINPUT04_TalkOverKeepsAnMMute(t *testing.T) {
	w := talkOverWorld(t, map[LED]Button{s8: {Mode: ModeTalkOver}})
	w.press(ButtonM, 3) // Discord muted with M
	w.press(ButtonS, 8)
	w.release(ButtonS, 8)
	w.wantMuted(true, discord.ID)
}

func TestINPUT04_TalkOverFollowsMInMuteMode(t *testing.T) {
	w := talkOverWorld(t, map[LED]Button{m8: {TalkOver: true}})
	w.wantVolume(spotify.ID, 0.25) // the mic is live: talk-over is on
	w.press(ButtonM, 8)            // muted
	w.wantVolume(spotify.ID, 1)
	w.press(ButtonM, 8) // live again
	w.wantVolume(spotify.ID, 0.25)
}

func TestINPUT04_TalkOverIsLoggedAtTheStart(t *testing.T) {
	w := startedWith(t, map[LED]Button{m8: {TalkOver: true}}, State{})
	w.wantNotice(slog.LevelInfo, "talk-over on")
}

func TestINPUT04_TalkOverWithHoldToTalk(t *testing.T) {
	w := talkOverWorld(t, map[LED]Button{m8: {Mode: ModeHoldToTalk, TalkOver: true}})
	w.wantVolume(spotify.ID, 1)
	w.press(ButtonM, 8)
	w.wantMic(false)
	w.wantVolume(spotify.ID, 0.25)
	w.release(ButtonM, 8)
	w.wantMic(true)
	w.wantVolume(spotify.ID, 1)
}

func TestINPUT04_CoughDoesNotEndTalkOver(t *testing.T) {
	w := talkOverWorld(t, map[LED]Button{m8: {TalkOver: true}})
	w.press(ButtonS, 8) // cough
	w.wantMic(true)
	w.wantVolume(spotify.ID, 0.25)
}

func TestINPUT04_LowestTalkOverVolumeApplies(t *testing.T) {
	setup := testSetup()
	setup.Targets["cam"] = Target{ID: "cam", Name: "Webcam", Kind: Input, Match: []string{"c922"}, TalkOverVolume: 10}
	setup.Assignments[Control{Slider, 7}] = "cam"
	setup.Buttons = map[LED]Button{s8: {Mode: ModeTalkOver}, {Button: ButtonS, Column: 7}: {Mode: ModeTalkOver}}
	w := newWorld(t, setup, State{})
	w.do(AudioSnapshot{Streams: []Stream{spotify}, Devices: []Device{goxlr, webcam}})
	w.move(Slider, 1, 127)
	w.press(ButtonS, 8)
	w.wantVolume(spotify.ID, 0.25)
	w.press(ButtonS, 7)
	w.wantVolume(spotify.ID, 0.10)
	w.release(ButtonS, 7)
	w.wantVolume(spotify.ID, 0.25)
}

func TestINPUT05_MMuteAndCoughAreSeparate(t *testing.T) {
	w := started(t)
	w.press(ButtonM, 8)
	w.press(ButtonS, 8)
	w.release(ButtonS, 8)
	w.wantMic(true) // still muted with M
	w.press(ButtonS, 8).press(ButtonM, 8)
	w.wantMic(true) // M unmuted, but still coughing
	w.release(ButtonS, 8)
	w.wantMic(false)
}

func TestINPUT06_ControlMovedDuringTalkOver(t *testing.T) {
	w := talkOverWorld(t, map[LED]Button{s8: {Mode: ModeTalkOver}})
	w.press(ButtonS, 8)
	w.move(Slider, 1, 64)
	w.wantVolume(spotify.ID, 0.25)
	w.move(Slider, 1, 20)
	w.wantVolume(spotify.ID, 20.0/127)
	w.move(Slider, 1, 64)
	w.move(Slider, 3, 100) // Discord's first move ends its talk-over mute
	w.wantMuted(false, discord.ID)
	w.wantVolume(discord.ID, 0.25)
	w.release(ButtonS, 8)
	w.wantVolume(spotify.ID, 64.0/127)
	w.wantVolume(discord.ID, 100.0/127)
}

func TestINPUT06_NewStreamDuringTalkOver(t *testing.T) {
	w := talkOverWorld(t, map[LED]Button{s8: {Mode: ModeTalkOver}})
	w.press(ButtonS, 8)
	w.do(StreamAdded{ffTab2}) // browser at 10/127: below
	w.wantVolume(ffTab2.ID, 10.0/127)
	spotify2 := Stream{ID: 20, AppName: "Spotify"}
	w.do(StreamAdded{spotify2})
	w.wantVolume(spotify2.ID, 0.25)
}

func TestINPUT07_ControllerDisconnectEndsHeldStates(t *testing.T) {
	w := talkOverWorld(t, map[LED]Button{s8: {Mode: ModeTalkOver}})
	w.press(ButtonS, 8)
	w.do(ControllerDisconnected{})
	w.wantVolume(spotify.ID, 1)
	if got := w.noticeAttr("held state ended", logattr.KeyHeldReason); got != "controller_disconnected" {
		t.Errorf("reason = %v", got)
	}
	w.release(ButtonS, 8) // a late release does nothing
	if len(effects(w.last)) != 0 {
		t.Errorf("late release produced %v", w.last)
	}
}

func TestINPUT07_ConfigChangeEndsHeldState(t *testing.T) {
	w := started(t)
	w.press(ButtonS, 8) // cough
	setup := testSetup()
	setup.Buttons = map[LED]Button{s8: {Mode: ModeTalkOver}}
	w.do(ConfigChanged{setup})
	w.wantMic(false)
	if got := w.noticeAttr("held state ended", logattr.KeyHeldReason); got != "config_changed" {
		t.Errorf("reason = %v", got)
	}
}

func TestINPUT07_ConfigChangeKeepsAnUnchangedHeldState(t *testing.T) {
	w := started(t)
	w.press(ButtonS, 8)
	w.do(ConfigChanged{testSetup()})
	w.wantMic(true)
}

func TestINPUT07_StoppingEndsTalkOver(t *testing.T) {
	w := talkOverWorld(t, map[LED]Button{m8: {TalkOver: true}})
	w.shutdown()
	w.wantVolume(spotify.ID, 1)
	w.wantMuted(false, discord.ID)
}

func TestINPUT07_HeldStatesAreNotSaved(t *testing.T) {
	w := started(t)
	w.press(ButtonS, 8)
	if w.m.Snapshot().Muted[Control{Slider, 8}] {
		t.Error("a cough was saved as a mute")
	}
}

func TestINPUT08_OtherReleasesDoNothing(t *testing.T) {
	w := started(t)
	w.press(ButtonM, 1)
	for _, b := range []ButtonKind{ButtonS, ButtonM, ButtonR} {
		w.release(b, 1)
		if len(w.last) != 0 {
			t.Errorf("release of %v1 produced %v", b, w.last)
		}
	}
	w.wantMuted(true, spotify.ID)
}

func TestLED04_InputColumnLEDs(t *testing.T) {
	w := startedWith(t, map[LED]Button{s8: {Mode: ModeTalkOver}}, State{})
	w.wantLEDs(8, true, true, true)
	w.press(ButtonM, 8)
	w.wantLEDs(8, true, false, true)
	w = startedWith(t, map[LED]Button{m8: {Mode: ModeHoldToTalk}}, State{})
	w.wantLEDs(8, true, false, true) // S and R stay lit in any mode
}
