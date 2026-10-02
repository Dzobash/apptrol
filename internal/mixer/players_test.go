package mixer

import (
	"reflect"
	"testing"
)

// Media players (MEDIA-01, DESK-02): the mixer keeps what the desktop adapter
// reports. Matching them to controls is MEDIA-03.

var (
	spotifyPlayer = Player{BusName: "org.mpris.MediaPlayer2.spotify", Identity: "Spotify", DesktopEntry: "spotify", Status: "Playing"}
	vlcPlayer     = Player{BusName: "org.mpris.MediaPlayer2.vlc", Identity: "VLC media player", DesktopEntry: "vlc", Status: "Stopped"}
)

func TestMEDIA01_PlayersAreFollowed(t *testing.T) {
	w := started(t)
	w.do(PlayerSnapshot{Players: []Player{spotifyPlayer}})
	w.do(PlayerAdded{Player: vlcPlayer})
	w.do(PlayerStatusChanged{BusName: spotifyPlayer.BusName, Status: "Paused"})
	w.do(PlayerStatusChanged{BusName: "org.mpris.MediaPlayer2.unknown", Status: "Playing"}) // never seen: ignored
	paused := spotifyPlayer
	paused.Status = "Paused"
	want := map[string]Player{spotifyPlayer.BusName: paused, vlcPlayer.BusName: vlcPlayer}
	if !reflect.DeepEqual(w.m.players, want) {
		t.Errorf("players = %v, want %v", w.m.players, want)
	}
	w.do(PlayerRemoved{BusName: vlcPlayer.BusName})
	if _, still := w.m.players[vlcPlayer.BusName]; still {
		t.Error("removed player is still known")
	}
}

func TestDESK02_SnapshotReplacesPlayers(t *testing.T) {
	w := started(t)
	w.do(PlayerSnapshot{Players: []Player{spotifyPlayer, vlcPlayer}})
	w.do(PlayerSnapshot{Players: []Player{vlcPlayer}}) // after a reconnect
	if len(w.m.players) != 1 || w.m.players[vlcPlayer.BusName] != vlcPlayer {
		t.Errorf("players = %v, want only VLC", w.m.players)
	}
	if len(effects(w.last)) != 0 {
		t.Errorf("player events produced %v", w.last)
	}
}
