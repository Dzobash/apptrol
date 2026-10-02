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
	// lastPlaying orders players by when they last started playing while
	// Apptrol ran (MEDIA-07); 0 = not since Apptrol started.
	lastPlaying int
}

// StatusPlaying and StatusPaused are MPRIS PlaybackStatus values.
const (
	StatusPlaying = "Playing"
	StatusPaused  = "Paused"
)

// playerName is the bus name without the MPRIS prefix and without an
// ".instance…" suffix: "org.mpris.MediaPlayer2.chromium.instance1234" is
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
	if known {
		info.lastPlaying = before.lastPlaying
	}
	if p.Status == StatusPlaying && (!known || before.Status != StatusPlaying) {
		info.lastPlaying = m.nextPlaySeq()
	}
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
			if e.Status == StatusPlaying && p.Status != StatusPlaying {
				p.lastPlaying = m.nextPlaySeq()
			}
			p.Status = e.Status
		}
	}
	m.syncLEDs(a, false) // a player playing, pausing or going changes the R LED (MEDIA-09)
}

func (m *Mixer) nextPlaySeq() int {
	m.playSeq++
	return m.playSeq
}

// ---- R on app columns (MEDIA-07 to MEDIA-09) -----------------------------------

// rMode returns what R on the column does: the configured mode, or by default
// play_pause on an app column and off elsewhere.
func (m *Mixer) rMode(col int) Mode {
	if mode := m.setup.Buttons[LED{Button: ButtonR, Column: col}].Mode; mode != ModeDefault {
		return mode
	}
	if id, ok := m.setup.Assignments[Control{Slider, col}]; ok && m.setup.Targets[id].Kind == App {
		return ModePlayPause
	}
	return ModeOff
}

// appPlayers returns the players matched to target id, by bus name.
func (m *Mixer) appPlayers(id string) []*playerInfo {
	var out []*playerInfo
	for _, p := range m.players {
		if p.target == id {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BusName < out[j].BusName })
	return out
}

// rPressed handles R on a column.
func (m *Mixer) rPressed(a *actions, col int, button string) {
	c := Control{Slider, col}
	switch m.rMode(col) {
	case ModePlayPause:
		m.playPause(a, c, button)
	case ModeLauncher:
		m.launch(a, LED{Button: ButtonR, Column: col})
	default:
		a.notice(slog.LevelDebug, "button has no function: its mode is off", m.about(c, logattr.KeyButton, button)...)
	}
}

// playPause pauses every playing player of the column's app; if none plays,
// it plays the paused one that played most recently, or the first paused one
// by name. A stopped player is never started, as with the keyboard's
// play / pause key (MEDIA-07, MEDIA-08).
func (m *Mixer) playPause(a *actions, c Control, button string) {
	id := m.setup.Assignments[c]
	players := m.appPlayers(id)
	if len(players) == 0 {
		a.notice(slog.LevelDebug, "button has no function: no media player for this app", m.about(c, logattr.KeyButton, button)...)
		return
	}
	var resume *playerInfo
	paused := false
	for _, p := range players {
		switch p.Status {
		case StatusPlaying:
			a.add(PlayerCommand{BusName: p.BusName, Command: CommandPause})
			a.notice(slog.LevelInfo, "paused", m.about(c, logattr.KeyButton, button, logattr.KeyPlayerBusName, p.BusName)...)
			paused = true
		case StatusPaused:
			if resume == nil || p.lastPlaying > resume.lastPlaying {
				resume = p
			}
		}
	}
	switch {
	case paused:
	case resume == nil:
		a.notice(slog.LevelDebug, "button has no function: no paused media player for this app", m.about(c, logattr.KeyButton, button)...)
	default:
		a.add(PlayerCommand{BusName: resume.BusName, Command: CommandPlay})
		a.notice(slog.LevelInfo, "playing", m.about(c, logattr.KeyButton, button, logattr.KeyPlayerBusName, resume.BusName)...)
	}
}

// rLit reports whether the R LED of an app column is lit: in play_pause mode,
// while one of the app's media players reports Playing (MEDIA-09). Lit means
// pressing R pauses; dark means it resumes, or does nothing. The player's
// status arrives right after a press, while apps cork their stream only
// seconds after pausing (ADR 0018, note of 2026-10-02).
func (m *Mixer) rLit(col int, id string) bool {
	if m.rMode(col) != ModePlayPause {
		return false
	}
	for _, p := range m.appPlayers(id) {
		if p.Status == StatusPlaying {
			return true
		}
	}
	return false
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
