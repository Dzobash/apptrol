package mixer

import (
	"log/slog"
	"sort"
	"strings"

	"github.com/Dzobash/apptrol/internal/logattr"
)

// Media players and the controls they belong to (MEDIA-02, MEDIA-03,
// ADR 0018). The desktop adapter reports the players; this file decides.

// mprisPrefix starts every media player's bus name.
const mprisPrefix = "org.mpris.MediaPlayer2."

// playerInfo is a player and the app target it belongs to ("" if none).
type playerInfo struct {
	Player
	target    string
	control   Control // the target's control when it was matched
	matchedBy string  // bus_name, identity or desktop_entry
}

// playerName is the bus name without the MPRIS prefix and without an
// ".instance…" suffix: "org.mpris.MediaPlayer2.chromium.instance6652" is
// "chromium". The instance number changes on every start, so it would only
// match by accident.
func playerName(busName string) string {
	name := strings.TrimPrefix(busName, mprisPrefix)
	if i := strings.Index(name, ".instance"); i >= 0 {
		name = name[:i]
	}
	return name
}

// ignoredReason says why a player is never matched, or "" (MEDIA-02):
// players on other devices, proxies and duplicates. It is checked before
// matching, because e.g. a phone's Spotify reports "Spotify" too.
func ignoredReason(busName string) string {
	switch name := strings.TrimPrefix(busName, mprisPrefix); {
	case strings.HasPrefix(name, "kdeconnect."):
		return "other_device"
	case name == "playerctld":
		return "proxy"
	case name == "plasma-browser-integration":
		return "duplicate"
	}
	return ""
}

// matchPlayer returns the app target a player belongs to and what matched:
// a fragment of the app's match list in the player's name, Identity or
// DesktopEntry (MEDIA-03). Several matches go to the first control, sliders
// 1–8, then knobs 1–8, as for streams (CFG-12). Inputs have no players.
func (m *Mixer) matchPlayer(p Player) (id, matchedBy string) {
	fields := []struct{ by, value string }{
		{"bus_name", playerName(p.BusName)},
		{"identity", p.Identity},
		{"desktop_entry", p.DesktopEntry},
	}
	for _, c := range m.assignedControls() {
		id := m.setup.Assignments[c]
		if m.setup.Targets[id].Kind != App {
			continue
		}
		for _, f := range fields {
			value := strings.ToLower(f.value)
			for _, frag := range m.fragments[id] {
				if value != "" && strings.Contains(value, frag) {
					return id, f.by
				}
			}
		}
	}
	return "", ""
}

// playerAttrs are the log attributes of a player; DesktopEntry only when the
// player has one.
func playerAttrs(p Player) []any {
	attrs := []any{logattr.KeyPlayerBusName, p.BusName, logattr.KeyPlayerIdentity, p.Identity}
	if p.DesktopEntry != "" {
		attrs = append(attrs, logattr.KeyPlayerDesktopEntry, p.DesktopEntry)
	}
	return attrs
}

// placePlayer matches a player and logs the decision with its reason
// (LOG-15). With quiet set, a player whose target did not change is not
// logged again (configuration reloads).
func (m *Mixer) placePlayer(a *actions, p Player, quiet bool) {
	before, known := m.players[p.BusName]
	info := &playerInfo{Player: p}
	if reason := ignoredReason(p.BusName); reason != "" {
		m.players[p.BusName] = info
		if !quiet || !known {
			a.notice(slog.LevelDebug, "media player ignored", append(playerAttrs(p), logattr.KeyPlayerIgnoredReason, reason)...)
		}
		return
	}
	info.target, info.matchedBy = m.matchPlayer(p)
	info.control = m.controlOf[info.target]
	m.players[p.BusName] = info
	if quiet && known && before.target == info.target && before.control == info.control {
		return
	}
	if info.target == "" {
		a.notice(slog.LevelDebug, "media player not matched", playerAttrs(p)...)
		return
	}
	a.notice(slog.LevelInfo, "media player matched",
		m.aboutTarget(info.target, append(playerAttrs(p), logattr.KeyPlayerMatchedBy, info.matchedBy)...)...)
}

// playersChanged applies a player event.
func (m *Mixer) playersChanged(a *actions, ev Event) {
	switch e := ev.(type) {
	case PlayerSnapshot:
		m.players = map[string]*playerInfo{}
		for _, p := range e.Players {
			m.placePlayer(a, p, false)
		}
	case PlayerAdded:
		m.placePlayer(a, e.Player, false)
	case PlayerRemoved:
		delete(m.players, e.BusName)
	case PlayerStatusChanged:
		if p, ok := m.players[e.BusName]; ok {
			p.Status = e.Status
		}
	}
}

// rematchPlayers matches every player again after a configuration change,
// logging only those whose control changed.
func (m *Mixer) rematchPlayers(a *actions) {
	names := make([]string, 0, len(m.players))
	for n := range m.players {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		m.placePlayer(a, m.players[n].Player, true)
	}
}
