package mixer

import (
	"log/slog"
	"sort"
	"strings"

	"github.com/Dzobash/apptrol/internal/logattr"
)

// The media keys ◀◀ ▶▶ ■ ▶ (MEDIA-05, MEDIA-06, ADR 0018). Unlike R they are
// not tied to a control: they act on any player that is not ignored.

// launch starts the app of a launcher button; without one, the button does
// nothing (LAUNCH-01). Only while the screen is unlocked, or, with
// when_locked, also while it is locked or another session is in front; never
// while it cannot be told (LAUNCH-13, LAUNCH-14, ADR 0024). Every press while
// locked is a warning (LAUNCH-15). The launcher logs what it starts.
func (m *Mixer) launch(a *actions, b LED) {
	l, ok := m.setup.Launchers[b]
	if !ok {
		a.notice(slog.LevelDebug, "button has no function: no launcher configured", logattr.KeyButton, b.String())
		return
	}
	switch {
	case m.screen == ScreenUnlocked:
	case m.screenLocked() && l.WhenLocked:
		a.notice(slog.LevelWarn, "launcher pressed while the screen is locked; started (when_locked)",
			m.lockedLaunch(b, l)...)
	case m.screenLocked():
		a.notice(slog.LevelWarn, "launcher pressed while the screen is locked; nothing started",
			m.lockedLaunch(b, l)...)
		return
	default:
		a.notice(slog.LevelInfo, "launcher not started: the screen is not known to be unlocked",
			logattr.KeyButton, b.String(), logattr.KeyScreenState, string(m.screen))
		return
	}
	a.add(LaunchApp{Button: b, Launch: l})
}

// lockedLaunch returns the attributes of a launcher pressed while locked.
func (m *Mixer) lockedLaunch(b LED, l Launch) []any {
	attrs := []any{logattr.KeyButton, b.String(), logattr.KeyScreenState, string(m.screen)}
	if l.DesktopID != "" {
		return append(attrs, logattr.KeyLauncherDesktopID, l.DesktopID)
	}
	return append(attrs, logattr.KeyLauncherCommand, strings.Join(l.Command, " "))
}

// screenLocked reports whether the screen is known to be locked, or another
// session is in front.
func (m *Mixer) screenLocked() bool { return m.screen == ScreenLocked || m.screen == ScreenInactive }

// lockedPress warns about a button pressed while the screen is locked
// (LAUNCH-15). Launchers warn in launch, with what they did; sliders and
// knobs are not reported.
func (m *Mixer) lockedPress(a *actions, b LED) {
	if !m.screenLocked() {
		return
	}
	if _, launcher := m.setup.Launchers[b]; launcher {
		return
	}
	a.notice(slog.LevelWarn, "button pressed while the screen is locked",
		logattr.KeyButton, b.String(), logattr.KeyScreenState, string(m.screen))
}

// mediaKeyCommands maps the media keys to player commands; ▶ is decided by
// the player's status.
var mediaKeyCommands = map[TransportButton]string{
	Stop:        CommandStop,
	Rewind:      CommandPrevious,
	FastForward: CommandNext,
}

// mediaKeyPlayer returns the player the media keys act on and how it was
// chosen: with [media] player set, the most recent of that app's players,
// else the most recent of all (MEDIA-06). "Most recent" is the player that
// last started playing while Apptrol ran; without that, a paused one before
// a stopped one, then by name. nil if there is none; a pinned app without a
// player gets nil too, never another app's player.
func (m *Mixer) mediaKeyPlayer() (p *playerInfo, selection string) {
	selection = "most_recent"
	pinned := m.setup.MediaPlayer
	if pinned != "" {
		selection = "pinned"
	}
	var candidates []*playerInfo
	for _, info := range m.players {
		if ignoredReason(info.BusName) != "" {
			continue
		}
		if pinned != "" && !m.playerMatches(info.Player, pinned) {
			continue
		}
		candidates = append(candidates, info)
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.lastPlaying != b.lastPlaying {
			return a.lastPlaying > b.lastPlaying
		}
		if (a.Status == StatusPaused) != (b.Status == StatusPaused) {
			return a.Status == StatusPaused
		}
		return a.BusName < b.BusName
	})
	if len(candidates) == 0 {
		return nil, selection
	}
	return candidates[0], selection
}

// playerMatches reports whether a player belongs to app id by its match
// list, whether or not the app is on a control: a pinned app need not be.
func (m *Mixer) playerMatches(p Player, id string) bool {
	for _, value := range []string{playerName(p.BusName), p.Identity, p.DesktopEntry} {
		value = strings.ToLower(value)
		for _, frag := range m.fragments[id] {
			if value != "" && strings.Contains(value, frag) {
				return true
			}
		}
	}
	return false
}

// mediaKeyPlaying reports whether the media-key player plays: the ▶ LED is
// lit then (LED-06).
func (m *Mixer) mediaKeyPlaying() bool {
	p, _ := m.mediaKeyPlayer()
	return p != nil && p.Status == StatusPlaying
}

// launcherButtons are the transport buttons that can start apps (LAUNCH-01).
var launcherButtons = map[TransportButton]bool{Record: true, MarkerSet: true, MarkerPrev: true, MarkerNext: true}

// transportPressed handles the transport buttons: media keys and
// launchers; Track and Cycle (layouts) come later.
func (m *Mixer) transportPressed(a *actions, b TransportButton) {
	if launcherButtons[b] {
		m.launch(a, LED{Transport: b})
		return
	}
	command, isMediaKey := mediaKeyCommands[b]
	if b != Play && !isMediaKey {
		a.notice(slog.LevelDebug, "button has no function yet", logattr.KeyButton, b.String())
		return
	}
	p, selection := m.mediaKeyPlayer()
	if p == nil {
		msg := "button has no function: no media player"
		if selection == "pinned" {
			msg = "button has no function: the pinned app has no media player"
		}
		a.notice(slog.LevelDebug, msg, logattr.KeyButton, b.String(), logattr.KeyPlayerSelection, selection)
		return
	}
	if b == Play {
		// Pause while playing; otherwise Play, also after ■: the player then
		// starts the track again, or does nothing without one (MEDIA-05).
		command = CommandPlay
		if p.Status == StatusPlaying {
			command = CommandPause
		}
	}
	a.add(PlayerCommand{BusName: p.BusName, Command: command})
	attrs := []any{logattr.KeyButton, b.String(), logattr.KeyPlayerBusName, p.BusName,
		logattr.KeyPlayerIdentity, p.Identity, logattr.KeyPlayerCommand, command, logattr.KeyPlayerSelection, selection}
	if p.target != "" {
		attrs = m.aboutTarget(p.target, attrs...)
	}
	a.notice(slog.LevelInfo, "media key", attrs...)
}
