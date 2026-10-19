package mixer

import (
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/Dzobash/apptrol/internal/logattr"
)

// The media keys ◀◀ ▶▶ ■ ▶ (MEDIA-05, MEDIA-06, LED-06).

var wavebox = player("chromium.instance7", "Wavebox", "") // on no control

func (w *world) key(b TransportButton) { w.do(TransportPressed{Button: b}) }

func (w *world) playLED() bool { return w.leds[LED{Transport: Play}] }

func TestMEDIA05_MediaKeysSendTheirCommands(t *testing.T) {
	w := started(t)
	w.do(PlayerSnapshot{Players: []Player{spotifyPlayer}})
	w.key(Play)
	w.do(PlayerStatusChanged{BusName: spotifyPlayer.BusName, Status: StatusPaused})
	w.key(Play)
	w.key(Stop)
	w.key(Rewind)
	w.key(FastForward)
	w.wantCommands(
		PlayerCommand{BusName: spotifyPlayer.BusName, Command: CommandPause},
		PlayerCommand{BusName: spotifyPlayer.BusName, Command: CommandPlay},
		PlayerCommand{BusName: spotifyPlayer.BusName, Command: CommandStop},
		PlayerCommand{BusName: spotifyPlayer.BusName, Command: CommandPrevious},
		PlayerCommand{BusName: spotifyPlayer.BusName, Command: CommandNext})
	if got := w.noticeAttr("media key", logattr.KeyPlayerSelection); got != "most_recent" {
		t.Errorf("selection = %v", got)
	}
}

func TestMEDIA05_PlayAfterStopIsSent(t *testing.T) {
	w := started(t)
	w.do(PlayerSnapshot{Players: []Player{withStatus(vlcPlayer, "Stopped")}})
	w.key(Play) // the player starts the track again, or does nothing without one
	w.wantCommands(PlayerCommand{BusName: vlcPlayer.BusName, Command: CommandPlay})
}

func TestMEDIA05_WithoutPlayersNothingHappens(t *testing.T) {
	w := started(t)
	w.do(PlayerSnapshot{Players: []Player{phonePlayer}}) // ignored: other device
	w.key(Play)
	w.wantCommands()
	w.wantNotice(slog.LevelDebug, "no media player")
}

func TestMEDIA06_TheMostRecentPlayerIsUsed(t *testing.T) {
	w := started(t)
	w.do(PlayerSnapshot{Players: []Player{spotifyPlayer, withStatus(wavebox, StatusPaused)}})
	w.do(PlayerStatusChanged{BusName: wavebox.BusName, Status: StatusPlaying}) // started after Spotify
	w.do(PlayerStatusChanged{BusName: wavebox.BusName, Status: StatusPaused})
	w.key(Play) // Wavebox is on no control, but still the media-key player
	w.wantCommands(PlayerCommand{BusName: wavebox.BusName, Command: CommandPlay})
}

func TestMEDIA06_WithoutHistoryPausedComesBeforeStopped(t *testing.T) {
	w := started(t)
	w.do(PlayerSnapshot{Players: []Player{withStatus(spotifyPlayer, "Stopped"), withStatus(vlcPlayer, StatusPaused)}})
	w.key(Play)
	w.wantCommands(PlayerCommand{BusName: vlcPlayer.BusName, Command: CommandPlay})
}

func TestMEDIA06_APinnedAppIsAlwaysUsed(t *testing.T) {
	setup := testSetup()
	setup.MediaPlayer = "spare" // an app on no control
	w := newWorld(t, setup, State{})
	w.do(ControllerConnected{})
	spare := player("spareplayer", "Spare", "")
	w.do(PlayerSnapshot{Players: []Player{withStatus(spare, StatusPaused), spotifyPlayer}}) // Spotify plays
	w.key(Play)
	w.wantCommands(PlayerCommand{BusName: spare.BusName, Command: CommandPlay})
	if got := w.noticeAttr("media key", logattr.KeyPlayerSelection); got != "pinned" {
		t.Errorf("selection = %v", got)
	}
	if w.playLED() {
		t.Error("▶ lit although the pinned app does not play")
	}
}

func TestMEDIA06_APinnedAppWithoutPlayerDoesNothing(t *testing.T) {
	setup := testSetup()
	setup.MediaPlayer = "spare"
	w := newWorld(t, setup, State{})
	w.do(PlayerSnapshot{Players: []Player{spotifyPlayer}}) // Spotify plays, the pinned app is not running
	w.key(Play)
	w.key(FastForward)
	w.wantCommands()
	w.wantNotice(slog.LevelDebug, "the pinned app has no media player")
}

func TestLED06_PlayIsLitWhileTheMediaKeyPlayerPlays(t *testing.T) {
	w := started(t)
	if w.playLED() {
		t.Error("▶ lit without players")
	}
	w.do(PlayerAdded{Player: spotifyPlayer})
	if !w.playLED() {
		t.Error("▶ not lit while Spotify plays")
	}
	w.do(PlayerStatusChanged{BusName: spotifyPlayer.BusName, Status: StatusPaused})
	if w.playLED() {
		t.Error("▶ lit after Spotify paused")
	}
	for _, b := range []TransportButton{Stop, Rewind, FastForward, Cycle, Record} {
		if w.leds[LED{Transport: b}] {
			t.Errorf("%v lit", b)
		}
	}
}

func TestBTN01_OtherTransportButtonsHaveNoFunctionYet(t *testing.T) {
	w := started(t)
	w.do(PlayerSnapshot{Players: []Player{spotifyPlayer}})
	for _, b := range []TransportButton{Record, MarkerSet, Cycle, TrackPrev} {
		w.key(b)
	}
	w.wantCommands()
	w.wantNotice(slog.LevelDebug, "button has no function yet")
}

// ---- the media buttons while another session is in front (MEDIA-11, ADR 0030) ----

// toneA is a made-up player on no control: the media keys' player.
var toneA = player("tone-a", "Tone A", "")

const mediaBlockedMsg = "media button pressed while this session is not in front; nothing sent"

// noticesOf returns the notices among acts.
func noticesOf(acts []Action) []Notice {
	var out []Notice
	for _, a := range acts {
		if n, ok := a.(Notice); ok {
			out = append(out, n)
		}
	}
	return out
}

// pressMedia presses a media key, or R on a column when b is a column LED.
func (w *world) pressMedia(b LED) {
	if b.Transport != NoTransport {
		w.key(b.Transport)
		return
	}
	w.press(b.Button, b.Column)
}

func TestMEDIA11_MediaKeysSendNothingWhileInactive(t *testing.T) {
	buttons := []LED{{Transport: Play}, {Transport: Stop}, {Transport: Rewind}, {Transport: FastForward}, {Button: ButtonR, Column: 1}}
	for _, tc := range []struct {
		name    string
		pinned  string
		players []Player
	}{
		{"playing", "", []Player{toneA, spotifyPlayer}},
		{"paused", "", []Player{withStatus(toneA, StatusPaused), withStatus(spotifyPlayer, StatusPaused)}},
		{"no player", "", nil}, // still warns: nobody may play or resume (option A)
		{"pinned app without player", "spare", []Player{toneA}},
	} {
		for _, b := range buttons {
			setup := testSetup()
			setup.MediaPlayer = tc.pinned
			w := newWorld(t, setup, State{})
			w.do(ControllerConnected{})
			w.do(PlayerSnapshot{Players: tc.players})
			w.do(ScreenChanged{State: ScreenInactive})
			w.pressMedia(b)
			if len(w.commands) != 0 {
				t.Errorf("%s, %s: commands %v while inactive", tc.name, b, w.commands)
				w.commands = nil
			}
			got := noticesOf(w.last)
			if len(got) != 1 {
				t.Errorf("%s, %s: %d log lines for one press, want 1: %v", tc.name, b, len(got), got)
				continue
			}
			want := Notice{Level: slog.LevelWarn, Msg: mediaBlockedMsg,
				Attrs: []any{logattr.KeyButton, b.String(), logattr.KeyScreenState, "inactive"}}
			if !reflect.DeepEqual(got[0], want) {
				t.Errorf("%s, %s: logged %v, want %v", tc.name, b, got[0], want)
			}
		}
	}
}

func TestMEDIA11_LockedAndUnknownStillSendCommands(t *testing.T) {
	for _, state := range []ScreenState{ScreenLocked, ScreenUnknown, ScreenUnlocked} {
		w := started(t)
		w.do(PlayerSnapshot{Players: []Player{withStatus(toneA, StatusPaused), spotifyPlayer}})
		w.do(ScreenChanged{State: state})
		w.key(Stop) // ■ goes to Spotify: it plays, so it is the most recent
		w.press(ButtonR, 1)
		w.wantCommands(
			PlayerCommand{BusName: spotifyPlayer.BusName, Command: CommandStop},
			PlayerCommand{BusName: spotifyPlayer.BusName, Command: CommandPause})
		for _, nt := range w.notices {
			if nt.Msg == mediaBlockedMsg || strings.HasPrefix(nt.Msg, "media buttons ") {
				t.Errorf("screen %q: %v", state, nt)
			}
		}
	}
}

func TestMEDIA11_OtherButtonsWorkWhileInactive(t *testing.T) {
	w := started(t)
	w.do(PlayerSnapshot{Players: []Player{spotifyPlayer}})
	w.do(ScreenChanged{State: ScreenInactive})
	w.move(Slider, 1, 64)
	w.wantVolume(spotify.ID, Volume(64))
	w.press(ButtonM, 1)
	w.wantMuted(true, spotify.ID)
	w.press(ButtonM, 8)
	w.wantMic(true)
	w.press(ButtonR, 4) // mode off: not a media button, the general warning stays
	const locked = "button pressed while the screen is locked"
	for button, n := range map[string]int{"M1": 1, "M8": 1, "R4": 1} {
		if got := w.noticesWith(slog.LevelWarn, locked, logattr.KeyButton, button); got != n {
			t.Errorf("%s: %d warnings, want %d", button, got, n)
		}
	}
	if n := w.noticesWith(slog.LevelWarn, mediaBlockedMsg, logattr.KeyButton, "R4"); n != 0 {
		t.Errorf("R4 in mode off warned as a media button")
	}
	if !w.playLED() || !w.led(ButtonR, 1) {
		t.Error("▶ or R1 dark while Spotify plays: the LEDs follow the players")
	}
}

func TestMEDIA11_BlockedAndAllowedAreLoggedOncePerChange(t *testing.T) {
	w := newWorld(t, testSetup(), State{})
	for _, s := range []ScreenState{ScreenUnlocked, ScreenInactive, ScreenInactive, ScreenLocked,
		ScreenInactive, "", ScreenUnlocked, ScreenUnlocked} {
		w.do(ScreenChanged{State: s})
	}
	type line struct{ msg, state string }
	var got []line
	for _, nt := range w.notices {
		if nt.Level != slog.LevelInfo || !strings.HasPrefix(nt.Msg, "media buttons ") {
			continue
		}
		if len(nt.Attrs) != 2 || nt.Attrs[0] != logattr.KeyScreenState {
			t.Errorf("attributes %v", nt.Attrs)
			continue
		}
		got = append(got, line{nt.Msg, nt.Attrs[1].(string)})
	}
	const blocked, allowed = "media buttons blocked: this session is not in front", "media buttons allowed: this session is in front"
	want := []line{{blocked, "inactive"}, {allowed, "locked"}, {blocked, "inactive"}, {allowed, "unknown"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("logged %v, want %v", got, want)
	}
}
