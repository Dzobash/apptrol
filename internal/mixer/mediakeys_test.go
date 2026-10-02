package mixer

import (
	"log/slog"
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
