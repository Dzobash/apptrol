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

func TestMEDIA09_RLightsWhileTheAppPlaysAndHasAPlayer(t *testing.T) {
	w := started(t)                    // Spotify's stream is not corked
	w.wantLEDs(1, false, false, false) // no player yet
	w.do(PlayerAdded{Player: spotifyPlayer})
	w.wantLEDs(1, false, false, true)
	w.do(StreamCorkChanged{ID: spotify.ID, Corked: true}) // paused: the stream is corked
	w.wantLEDs(1, false, false, false)
	w.do(StreamCorkChanged{ID: spotify.ID, Corked: false})
	w.wantLEDs(1, false, false, true)
	w.do(StreamRemoved{ID: spotify.ID})
	w.wantLEDs(1, false, false, false)
	w.do(StreamAdded{Stream: spotify})
	w.wantLEDs(1, false, false, true)
	w.do(PlayerRemoved{BusName: spotifyPlayer.BusName})
	w.wantLEDs(1, false, false, false)
	w.wantLEDs(8, true, true, true) // the microphone's R stays lit (LED-04)
}

func TestMEDIA09_PhonePlayersDoNotLightR(t *testing.T) {
	w := started(t)
	w.do(PlayerAdded{Player: phonePlayer}) // "Spotify - <phone name>", ignored
	w.wantLEDs(1, false, false, false)
	w.press(ButtonR, 1)
	w.wantCommands()
}
