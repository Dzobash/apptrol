package mixer

import (
	"log/slog"
	"reflect"
	"testing"
)

// R on app columns (MEDIA-04, MEDIA-07 to MEDIA-09). Spotify is on slider1,
// the browser (firefox, vivaldi) on slider2, Discord on slider3.

func withStatus(p Player, status string) Player { p.Status = status; return p }

func (w *world) wantCommands(want ...PlayerCommand) {
	w.t.Helper()
	if len(w.commands)+len(want) > 0 && !reflect.DeepEqual(w.commands, want) {
		w.t.Errorf("commands = %v, want %v", w.commands, want)
	}
	w.commands = nil
}

func TestMEDIA07_RPausesEveryPlayingPlayerOfTheApp(t *testing.T) {
	w := started(t)
	tab := player("firefox.instance_1_1", "Firefox", "firefox")
	other := player("vivaldi.instance42", "Vivaldi", "")
	w.do(PlayerSnapshot{Players: []Player{tab, other, spotifyPlayer}})
	w.press(ButtonR, 2)
	w.wantCommands(
		PlayerCommand{BusName: tab.BusName, Command: CommandPause},
		PlayerCommand{BusName: other.BusName, Command: CommandPause}) // not Spotify's
	w.wantNotice(slog.LevelInfo, "paused")
}

func TestMEDIA07_RResumesThePausedPlayerThatPlayedLast(t *testing.T) {
	w := started(t)
	a := player("firefox.instance_1_1", "Firefox", "")
	b := player("vivaldi.instance42", "Vivaldi", "")
	w.do(PlayerSnapshot{Players: []Player{withStatus(a, StatusPaused), withStatus(b, StatusPaused)}})
	w.do(PlayerStatusChanged{BusName: a.BusName, Status: StatusPlaying})
	w.do(PlayerStatusChanged{BusName: b.BusName, Status: StatusPlaying})
	w.do(PlayerStatusChanged{BusName: b.BusName, Status: StatusPaused})
	w.do(PlayerStatusChanged{BusName: a.BusName, Status: StatusPaused})
	// b started playing after a, so b is resumed: although a paused last, and
	// although a comes first by name.
	w.press(ButtonR, 2)
	w.wantCommands(PlayerCommand{BusName: b.BusName, Command: CommandPlay})
	w.wantNotice(slog.LevelInfo, "playing")
}

func TestMEDIA07_WithoutHistoryThePausedPlayerIsResumed(t *testing.T) {
	w := started(t)
	stopped := player("firefox.instance_1_1", "Firefox", "")
	paused := player("vivaldi.instance42", "Vivaldi", "")
	w.do(PlayerSnapshot{Players: []Player{withStatus(stopped, "Stopped"), withStatus(paused, StatusPaused)}})
	w.press(ButtonR, 2)
	w.wantCommands(PlayerCommand{BusName: paused.BusName, Command: CommandPlay})
}

func TestMEDIA07_AStoppedPlayerIsNeverStarted(t *testing.T) {
	w := started(t)
	w.do(PlayerSnapshot{Players: []Player{withStatus(spotifyPlayer, "Stopped")}})
	w.press(ButtonR, 1)
	w.wantCommands()
	w.wantNotice(slog.LevelDebug, "no paused media player")
}

func TestMEDIA04_CommandsArePlayOrPauseOnly(t *testing.T) {
	w := started(t)
	w.do(PlayerSnapshot{Players: []Player{spotifyPlayer}})
	w.press(ButtonR, 1)
	w.do(PlayerStatusChanged{BusName: spotifyPlayer.BusName, Status: StatusPaused})
	w.press(ButtonR, 1)
	w.wantCommands(
		PlayerCommand{BusName: spotifyPlayer.BusName, Command: CommandPause},
		PlayerCommand{BusName: spotifyPlayer.BusName, Command: CommandPlay})
}

func TestMEDIA08_AnAppWithoutPlayerDoesNothing(t *testing.T) {
	w := started(t)
	w.do(PlayerSnapshot{})
	w.press(ButtonR, 3) // Discord has no player
	w.wantCommands()
	w.wantNotice(slog.LevelDebug, "no media player for this app")
}

func TestMEDIA07_ROffAndLaunchersSendNothing(t *testing.T) {
	setup := testSetup()
	setup.Buttons = map[LED]Button{{Button: ButtonR, Column: 1}: {Mode: ModeOff}, {Button: ButtonR, Column: 2}: {Mode: ModeLauncher}}
	w := newWorld(t, setup, State{})
	w.do(AudioSnapshot{Streams: []Stream{spotify, firefox}})
	w.do(PlayerSnapshot{Players: []Player{spotifyPlayer, player("firefox", "Firefox", "")}})
	w.press(ButtonR, 1)
	w.press(ButtonR, 2)
	w.press(ButtonR, 5) // an empty column
	w.wantCommands()
	w.wantLEDs(1, false, false, false) // MEDIA-09: off in mode off…
	w.wantLEDs(2, false, false, false) // …and as a launcher
}

func TestMEDIA09_RLightsWhileAPlayerOfTheAppPlays(t *testing.T) {
	w := started(t)
	w.wantLEDs(1, false, false, false) // no player yet
	w.do(PlayerAdded{Player: spotifyPlayer})
	w.wantLEDs(1, false, false, true)
	w.do(PlayerStatusChanged{BusName: spotifyPlayer.BusName, Status: StatusPaused})
	w.pauseDelayOver(spotifyPlayer.BusName)
	w.wantLEDs(1, false, false, false) // after the delay, not when the app corks its stream
	w.do(StreamCorkChanged{ID: spotify.ID, Corked: true})
	w.wantLEDs(1, false, false, false)
	w.do(PlayerStatusChanged{BusName: spotifyPlayer.BusName, Status: StatusPlaying})
	w.wantLEDs(1, false, false, true)
	w.do(PlayerStatusChanged{BusName: spotifyPlayer.BusName, Status: "Stopped"})
	w.wantLEDs(1, false, false, false)
	w.do(PlayerStatusChanged{BusName: spotifyPlayer.BusName, Status: StatusPlaying})
	w.do(PlayerRemoved{BusName: spotifyPlayer.BusName})
	w.wantLEDs(1, false, false, false)
	w.wantLEDs(8, true, true, true) // the microphone's R stays lit (LED-04)
}

func TestMEDIA09_RLightsWhileAnyOfSeveralPlayersPlays(t *testing.T) {
	w := started(t)
	a := player("firefox.instance_1_1", "Firefox", "")
	b := player("vivaldi.instance42", "Vivaldi", "")
	w.do(PlayerSnapshot{Players: []Player{a, withStatus(b, StatusPaused)}})
	w.wantLEDs(2, false, false, true)
	w.do(PlayerStatusChanged{BusName: a.BusName, Status: StatusPaused})
	w.pauseDelayOver(a.BusName)
	w.wantLEDs(2, false, false, false)
	w.do(PlayerStatusChanged{BusName: b.BusName, Status: StatusPlaying})
	w.wantLEDs(2, false, false, true)
}

func TestMEDIA09_PhonePlayersDoNotLightR(t *testing.T) {
	w := started(t)
	w.do(PlayerAdded{Player: phonePlayer}) // "Spotify - <phone name>", ignored
	w.wantLEDs(1, false, false, false)
	w.press(ButtonR, 1)
	w.wantCommands()
}

// ---- a short pause does not blink R and ▶ (MEDIA-09, ADR 0033) ----------------

// pauseDelayOver models the service's timer: the delay of the player's latest
// HoldPauseLED is over.
func (w *world) pauseDelayOver(busName string) {
	w.t.Helper()
	for i := len(w.holds) - 1; i >= 0; i-- {
		if w.holds[i].BusName == busName {
			w.do(PlayerPauseSettled{BusName: busName, Seq: w.holds[i].Seq})
			return
		}
	}
	w.t.Fatalf("no pause delay was asked for %s", busName)
}

func (w *world) wantPlayLED(want bool) {
	w.t.Helper()
	if got := w.playLED(); got != want {
		w.t.Errorf("▶ LED = %v, want %v", got, want)
	}
}

func TestMEDIA09_AShortPauseDoesNotDarkenR(t *testing.T) {
	// A browser pauses for ~80 ms while you seek with the mouse.
	w := started(t)
	tab := player("firefox.instance_1_1", "Firefox", "")
	w.do(PlayerSnapshot{Players: []Player{tab}})
	w.wantLEDs(2, false, false, true)
	w.do(PlayerStatusChanged{BusName: tab.BusName, Status: StatusPaused})
	w.wantLEDs(2, false, false, true) // still lit: the delay is running
	if len(w.holds) != 1 {
		t.Fatalf("pause delays asked for = %v, want 1", w.holds)
	}
	w.do(PlayerStatusChanged{BusName: tab.BusName, Status: StatusPlaying})
	w.wantLEDs(2, false, false, true)
	w.wantNotice(slog.LevelDebug, "short pause not shown on the LEDs")
	w.pauseDelayOver(tab.BusName) // the timer comes late: nothing to do
	w.wantLEDs(2, false, false, true)
}

func TestMEDIA09_APauseThatLastsDarkensR(t *testing.T) {
	// Buffering after a seek beyond the loaded part: nothing plays.
	w := started(t)
	tab := player("firefox.instance_1_1", "Firefox", "")
	w.do(PlayerSnapshot{Players: []Player{tab}})
	w.do(PlayerStatusChanged{BusName: tab.BusName, Status: StatusPaused})
	w.pauseDelayOver(tab.BusName)
	w.wantLEDs(2, false, false, false)
	w.wantNotice(slog.LevelDebug, "pause shown on the LEDs")
	w.do(PlayerStatusChanged{BusName: tab.BusName, Status: StatusPlaying})
	w.wantLEDs(2, false, false, true) // lighting up stays immediate
}

func TestMEDIA09_AnOldDelayDoesNotEndANewerPause(t *testing.T) {
	w := started(t)
	tab := player("firefox.instance_1_1", "Firefox", "")
	w.do(PlayerSnapshot{Players: []Player{tab}})
	w.do(PlayerStatusChanged{BusName: tab.BusName, Status: StatusPaused})
	first := w.holds[0]
	w.do(PlayerStatusChanged{BusName: tab.BusName, Status: StatusPlaying})
	w.do(PlayerStatusChanged{BusName: tab.BusName, Status: StatusPaused}) // a second seek
	w.do(PlayerPauseSettled{BusName: tab.BusName, Seq: first.Seq})        // the first timer fires
	w.wantLEDs(2, false, false, true)                                     // the second pause has not lasted yet
	w.pauseDelayOver(tab.BusName)
	w.wantLEDs(2, false, false, false)
}

func TestMEDIA09_StoppedAndRemovedPlayersDarkenAtOnce(t *testing.T) {
	w := started(t)
	tab := player("firefox.instance_1_1", "Firefox", "")
	w.do(PlayerSnapshot{Players: []Player{tab}})
	w.do(PlayerStatusChanged{BusName: tab.BusName, Status: "Stopped"})
	w.wantLEDs(2, false, false, false)
	w.do(PlayerStatusChanged{BusName: tab.BusName, Status: StatusPlaying})
	w.do(PlayerStatusChanged{BusName: tab.BusName, Status: StatusPaused})
	w.do(PlayerRemoved{BusName: tab.BusName})
	w.wantLEDs(2, false, false, false)
	w.do(PlayerPauseSettled{BusName: tab.BusName, Seq: w.holds[0].Seq}) // a timer for a gone player
	w.wantLEDs(2, false, false, false)
}

func TestMEDIA09_ThePauseOfAPressShowsAtOnce(t *testing.T) {
	// R and ▶ pause on purpose: the LED must not lag behind the press.
	w := started(t)
	tab := player("firefox.instance_1_1", "Firefox", "")
	w.do(PlayerSnapshot{Players: []Player{tab}})
	w.press(ButtonR, 2)
	w.do(PlayerStatusChanged{BusName: tab.BusName, Status: StatusPaused})
	w.wantLEDs(2, false, false, false)
	if len(w.holds) != 0 {
		t.Errorf("pause delays asked for = %v, want none", w.holds)
	}
	w.do(PlayerStatusChanged{BusName: tab.BusName, Status: StatusPlaying})
	w.key(Play) // ▶ pauses the media-key player
	w.do(PlayerStatusChanged{BusName: tab.BusName, Status: StatusPaused})
	w.wantLEDs(2, false, false, false)
	w.wantPlayLED(false)
}

func TestMEDIA09_PressesActOnTheRealStatusDuringTheDelay(t *testing.T) {
	w := started(t)
	tab := player("firefox.instance_1_1", "Firefox", "")
	w.do(PlayerSnapshot{Players: []Player{tab}})
	w.do(PlayerStatusChanged{BusName: tab.BusName, Status: StatusPaused})
	w.wantLEDs(2, false, false, true) // R still looks lit…
	w.press(ButtonR, 2)
	w.wantCommands(PlayerCommand{BusName: tab.BusName, Command: CommandPlay}) // …but the player is paused: R resumes
}

func TestMEDIA09_APauseDoesNotBlinkTheMediaKeyLED(t *testing.T) {
	w := started(t)
	tab := player("firefox.instance_1_1", "Firefox", "")
	w.do(PlayerSnapshot{Players: []Player{tab}})
	w.wantPlayLED(true)
	w.do(PlayerStatusChanged{BusName: tab.BusName, Status: StatusPaused})
	w.wantPlayLED(true)
	w.pauseDelayOver(tab.BusName)
	w.wantPlayLED(false)
}

func TestMEDIA09_AConfigReloadDoesNotEndTheDelayEarly(t *testing.T) {
	w := started(t)
	tab := player("firefox.instance_1_1", "Firefox", "")
	w.do(PlayerSnapshot{Players: []Player{tab}})
	w.do(PlayerStatusChanged{BusName: tab.BusName, Status: StatusPaused})
	w.do(ConfigChanged{Setup: testSetup()})
	w.wantLEDs(2, false, false, true)
	w.pauseDelayOver(tab.BusName)
	w.wantLEDs(2, false, false, false)
}
