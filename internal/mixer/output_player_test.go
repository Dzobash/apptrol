package mixer

import (
	"log/slog"
	"testing"

	"github.com/Dzobash/apptrol/internal/logattr"
)

// R on an output with a player list (MEDIA-12, OUT-09, OUT-10, ADR 0045). A
// client that plays into its own virtual output shows as a sink, while its
// player has the name of the app it replaces; "spotifast" stands in for it.
//
//	slider7 = main   (output, match "speakers", player "spotifast")
//	knob7   = phones (output, match "headset")

var spotifastPlayer = player("spotifast", "Spotifast", "")

// playerOutputSetup is outputSetup with a player list on the speakers.
func playerOutputSetup() Setup {
	s := outputSetup()
	main := s.Targets["main"]
	main.Player = []string{"SpotiFast"} // case-insensitive, as for apps
	s.Targets["main"] = main
	return s
}

func TestMEDIA12_ROnAnOutputWithAPlayerListPlaysAndPausesIt(t *testing.T) {
	w := outputWorld(t, playerOutputSetup())
	w.do(PlayerSnapshot{Players: []Player{spotifastPlayer, spotifyPlayer}})
	w.wantPlayer(spotifastPlayer, "main", "bus_name")
	w.wantLEDs(7, false, false, true) // R lit while it plays (MEDIA-09)

	w.press(ButtonR, 7) // play_pause is the default with a player list
	w.wantCommands(PlayerCommand{BusName: spotifastPlayer.BusName, Command: CommandPause}) // not Spotify's
	w.do(PlayerStatusChanged{BusName: spotifastPlayer.BusName, Status: StatusPaused})
	w.wantLEDs(7, false, false, false) // a pause from R shows at once (ADR 0033)

	w.press(ButtonR, 7)
	w.wantCommands(PlayerCommand{BusName: spotifastPlayer.BusName, Command: CommandPlay})
	w.wantNotice(slog.LevelInfo, "playing")
	if len(w.outputActions()) != 0 {
		t.Errorf("R changed the output: %v", w.outputActions()) // OUT-09
	}
}

func TestMEDIA12_TheMatchedPlayerIsLoggedWithTheOutput(t *testing.T) {
	w := outputWorld(t, playerOutputSetup())
	w.notices = nil
	w.do(PlayerAdded{Player: player("other", "Spotifast", "")})
	a := w.noticeAttrs("media player matched")
	if a[logattr.KeyAppID] != "main" || a[logattr.KeyAppType] != "output" || a[logattr.KeyControl] != "slider7" ||
		a[logattr.KeyPlayerMatchedBy] != "identity" {
		t.Errorf("attributes = %v", a)
	}
	if level := w.noticesNamed("media player matched")[0].Level; level != slog.LevelInfo {
		t.Errorf("level = %v, want info", level)
	}
}

func TestMEDIA12_AnOutputWithoutAPlayerListKeepsROff(t *testing.T) {
	w := outputWorld(t, outputSetup())
	// The output's match fragment is not a player list: a player named like
	// the device stays unmatched.
	named := player("speakers", "Speakers", "")
	w.do(PlayerSnapshot{Players: []Player{named, spotifastPlayer}})
	w.wantPlayer(named, "", "")
	w.wantPlayer(spotifastPlayer, "", "")
	w.press(ButtonR, 7)
	w.wantCommands()
	w.wantNotice(slog.LevelDebug, "button has no function: its mode is off") // OUT-09
	w.wantLEDs(7, false, false, false)                                     // OUT-10
}

func TestMEDIA12_ROffAndLaunchersOnAPlayerOutputSendNothing(t *testing.T) {
	for _, mode := range []Mode{ModeOff, ModeLauncher} {
		s := playerOutputSetup()
		s.Buttons = map[LED]Button{{Button: ButtonR, Column: 7}: {Mode: mode}}
		w := outputWorld(t, s)
		w.do(PlayerSnapshot{Players: []Player{spotifastPlayer}})
		w.press(ButtonR, 7)
		w.wantCommands()
		w.wantLEDs(7, false, false, false) // MEDIA-09: off in mode off and as a launcher
	}
}

// Owner decision 2 on #231: an app and an output that both match a player
// follow CFG-12, sliders 1–8, then knobs 1–8.
func TestMEDIA12_APlayerOfAnAppAndAnOutputGoesToTheFirstControl(t *testing.T) {
	s := playerOutputSetup()
	s.Targets["clone"] = Target{ID: "clone", Name: "Clone", Kind: App, Match: []string{"spotifast"}}
	s.Assignments[Control{Slider, 4}] = "clone"
	w := outputWorld(t, s)
	w.do(PlayerAdded{Player: spotifastPlayer})
	w.wantPlayer(spotifastPlayer, "clone", "bus_name") // slider4 before slider7

	delete(s.Assignments, Control{Slider, 4})
	s.Assignments[Control{Knob, 4}] = "clone"
	w.notices = nil
	w.do(ConfigChanged{Setup: s})
	w.wantPlayer(spotifastPlayer, "main", "bus_name") // slider7 before knob4
	if a := w.noticeAttrs("media player matched"); a[logattr.KeyAppID] != "main" || a[logattr.KeyAppType] != "output" {
		t.Errorf("the reload did not log the output as the new target: %v", a)
	}
	w.wantLEDs(7, false, false, true)
}

func TestMEDIA12_TwoOutputsWithTheSamePlayerFollowTheFirstControl(t *testing.T) {
	s := playerOutputSetup()
	phones := s.Targets["phones"]
	phones.Player = []string{"spotifast"}
	s.Targets["phones"] = phones
	w := outputWorld(t, s)
	w.do(PlayerAdded{Player: spotifastPlayer})
	w.wantPlayer(spotifastPlayer, "main", "bus_name") // slider7 before knob7
}

// Decided with the owner during #231: an output whose device an earlier
// control owns still plays and pauses its players, as R never touches the
// device (OUT-04, OUT-09, ADR 0045 point 4).
func TestMEDIA12_RWorksOnAnOutputWhoseDeviceAnEarlierControlOwns(t *testing.T) {
	s := playerOutputSetup()
	s.Targets["also"] = Target{ID: "also", Name: "Speakers too", Kind: Output, Match: []string{"built-in"}}
	s.Assignments[Control{Slider, 6}] = "also"
	w := outputWorld(t, s)
	w.do(PlayerAdded{Player: spotifastPlayer})
	w.wantLEDs(7, false, false, true)
	w.press(ButtonR, 7)
	w.wantCommands(PlayerCommand{BusName: spotifastPlayer.BusName, Command: CommandPause})
	w.press(ButtonM, 7) // M is still ignored (OUT-04)
	w.wantNotice(slog.LevelDebug, "button ignored: output belongs to an earlier control")
}

func TestMEDIA12_AReloadWithoutThePlayerListTurnsROff(t *testing.T) {
	w := outputWorld(t, playerOutputSetup())
	w.do(PlayerAdded{Player: spotifastPlayer})
	w.wantLEDs(7, false, false, true)
	w.do(ConfigChanged{Setup: outputSetup()})
	w.wantPlayer(spotifastPlayer, "", "")
	w.wantLEDs(7, false, false, false)
	w.press(ButtonR, 7)
	w.wantCommands()
}

// The third item on #231: [media] player may name an output with a player
// list; the media keys then follow that output's players, on a control or not
// (MEDIA-06, CFG-15, ADR 0045 point 5).
func TestMEDIA06_MediaKeysPinnedToAnOutputFollowItsPlayers(t *testing.T) {
	for _, onControl := range []bool{true, false} {
		s := playerOutputSetup()
		if !onControl {
			delete(s.Assignments, Control{Slider, 7})
		}
		s.MediaPlayer = "main"
		w := outputWorld(t, s)
		w.do(PlayerSnapshot{Players: []Player{withStatus(spotifastPlayer, StatusPaused), spotifyPlayer}}) // Spotify plays
		w.key(Play)
		w.wantCommands(PlayerCommand{BusName: spotifastPlayer.BusName, Command: CommandPlay})
		if got := w.noticeAttr("media key", logattr.KeyPlayerSelection); got != "pinned" {
			t.Errorf("onControl=%v: selection = %v", onControl, got)
		}
		if w.playLED() {
			t.Errorf("onControl=%v: ▶ lit although the pinned output's player does not play", onControl)
		}
	}
}

func TestMEDIA06_AnOutputPinnedWithoutAPlayerListDoesNothing(t *testing.T) {
	s := outputSetup() // the configuration rejects this; the mixer stays safe (CFG-15)
	s.MediaPlayer = "main"
	w := outputWorld(t, s)
	w.do(PlayerSnapshot{Players: []Player{player("speakers", "Speakers", ""), spotifyPlayer}})
	w.key(Play)
	w.wantCommands()
	w.wantNotice(slog.LevelDebug, "the pinned app has no media player")
}
