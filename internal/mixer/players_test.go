package mixer

import (
	"log/slog"
	"testing"

	"github.com/Dzobash/apptrol/internal/logattr"
)

// Media players (MEDIA-01 to MEDIA-03, DESK-02). The test layout has Spotify
// on slider1, the browser (firefox, vivaldi) on slider2, Discord on slider3,
// the microphone on slider8 and games (steam) on knob1.

func player(name, identity, desktopEntry string) Player {
	return Player{BusName: mprisPrefix + name, Identity: identity, DesktopEntry: desktopEntry, Status: "Playing"}
}

var (
	spotifyPlayer = player("spotify", "Spotify", "spotify")
	vlcPlayer     = player("vlc", "VLC media player", "vlc")
	phonePlayer   = player("kdeconnect.mpris_0123", "Spotify - <phone name>", "org.kde.kdeconnect.app")
)

// wantPlayer checks which target a player belongs to, and what matched.
func (w *world) wantPlayer(p Player, target, matchedBy string) {
	w.t.Helper()
	info, ok := w.m.players[p.BusName]
	if !ok {
		w.t.Fatalf("player %s unknown", p.BusName)
	}
	if info.target != target || info.matchedBy != matchedBy {
		w.t.Errorf("player %s: target %q by %q, want %q by %q", p.BusName, info.target, info.matchedBy, target, matchedBy)
	}
}

func TestMEDIA01_PlayersAreFollowed(t *testing.T) {
	w := started(t)
	w.do(PlayerSnapshot{Players: []Player{spotifyPlayer}})
	w.do(PlayerAdded{Player: vlcPlayer})
	w.do(PlayerStatusChanged{BusName: spotifyPlayer.BusName, Status: "Paused"})
	w.do(PlayerStatusChanged{BusName: mprisPrefix + "unknown", Status: "Playing"}) // never seen: ignored
	if got := w.m.players[spotifyPlayer.BusName].Status; got != "Paused" {
		t.Errorf("status = %q, want Paused", got)
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
	if len(w.m.players) != 1 {
		t.Errorf("players = %v, want only VLC", w.m.players)
	}
	if len(effects(w.last)) != 0 {
		t.Errorf("player events produced %v", w.last)
	}
}

func TestMEDIA02_OtherDevicesProxiesAndDuplicatesAreIgnored(t *testing.T) {
	w := started(t)
	proxy := player("playerctld", "playerctld", "")
	duplicate := player("plasma-browser-integration", "Firefox", "")
	w.do(PlayerSnapshot{Players: []Player{phonePlayer, proxy, duplicate}})
	for _, p := range []Player{phonePlayer, proxy, duplicate} {
		w.wantPlayer(p, "", "") // the phone says "Spotify", but is never matched
	}
	if got := w.noticeAttr("media player ignored", logattr.KeyPlayerIgnoredReason); got != "other_device" {
		t.Errorf("first ignored reason = %v", got)
	}
	for _, n := range w.notices {
		if n.Msg == "media player ignored" && n.Level != slog.LevelDebug {
			t.Errorf("ignored players are logged at %v, want debug", n.Level)
		}
	}
}

func TestMEDIA03_PlayersMatchByNameIdentityOrDesktopEntry(t *testing.T) {
	w := started(t)
	firefox := player("firefox.instance_1_42", "Mozilla firefox_firefox", "firefox_firefox")
	chromeLike := player("chromium.instance6652", "Vivaldi", "") // a Chromium browser without DesktopEntry
	game := player("bigpicture", "Big Picture", "steam")
	w.do(PlayerSnapshot{Players: []Player{spotifyPlayer, firefox, chromeLike, game}})
	w.wantPlayer(spotifyPlayer, "spotify", "bus_name")
	w.wantPlayer(firefox, "browser", "bus_name") // without ".instance…"
	w.wantPlayer(chromeLike, "browser", "identity")
	w.wantPlayer(game, "games", "desktop_entry") // a knob's app too
	w.wantNotice(slog.LevelInfo, "media player matched")
}

func TestMEDIA03_UnmatchedPlayersAreLoggedAtDebug(t *testing.T) {
	w := started(t)
	other := player("chromium.instance3989", "Wavebox", "") // "chromium" is in no match list
	w.do(PlayerAdded{Player: other})
	w.wantPlayer(other, "", "")
	w.wantNotice(slog.LevelDebug, "media player not matched")
}

func TestMEDIA03_TheFirstControlWins(t *testing.T) {
	w := started(t)
	both := player("spotify", "Spotify", "firefox") // matches slider1 and slider2
	w.do(PlayerAdded{Player: both})
	w.wantPlayer(both, "spotify", "bus_name")
}

func TestMEDIA03_InputsHaveNoPlayers(t *testing.T) {
	w := started(t)
	p := player("goxlr", "GoXLR", "") // the microphone's match fragment
	w.do(PlayerAdded{Player: p})
	w.wantPlayer(p, "", "")
}

func TestMEDIA03_PlayerNameDropsTheInstance(t *testing.T) {
	for bus, want := range map[string]string{
		mprisPrefix + "chromium.instance6652": "chromium",
		mprisPrefix + "vivaldi.instance42":    "vivaldi",
		mprisPrefix + "spotify":               "spotify",
		mprisPrefix + "vlc.instance1":         "vlc",
	} {
		if got := playerName(bus); got != want {
			t.Errorf("playerName(%s) = %q, want %q", bus, got, want)
		}
	}
}

func TestMEDIA03_ConfigChangeMatchesAgain(t *testing.T) {
	w := started(t)
	w.do(PlayerAdded{Player: spotifyPlayer})
	setup := testSetup()
	delete(setup.Assignments, Control{Slider, 1})
	setup.Assignments[Control{Slider, 4}] = "spotify"
	w.notices = nil
	w.do(ConfigChanged{setup})
	w.wantPlayer(spotifyPlayer, "spotify", "bus_name")
	if got := w.noticeAttr("media player matched", logattr.KeyControl); got != "slider4" {
		t.Errorf("matched again on %v, want slider4", got)
	}

	// A reload that changes nothing for the player logs nothing about it.
	w.notices = nil
	w.do(ConfigChanged{setup})
	for _, n := range w.notices {
		if n.Msg == "media player matched" {
			t.Errorf("unchanged player logged again: %v", n)
		}
	}
}
