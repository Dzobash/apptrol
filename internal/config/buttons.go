package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Button modes and if_running values (CFG-13, ADR 0019, ADR 0020).
const (
	ModeOff        = "off"
	ModePlayPause  = "play_pause"   // R on an app column
	ModeMute       = "mute"         // M on an input column
	ModeHoldToTalk = "hold_to_talk" // M on an input column
	ModeCough      = "cough"        // S on an input column
	ModeTalkOver   = "talk_over"    // S on an input column

	IfRunningStart = "start"
	IfRunningSkip  = "skip"
)

// DefaultTalkOverVolume is the talk-over volume of an input that sets none (CFG-16).
const DefaultTalkOverVolume = 25

// Transport buttons that can start apps.
var launcherButtons = []string{"record", "marker_set", "marker_prev", "marker_next"}

// Button is one entry of [layouts.<name>.buttons] (CFG-13): a launcher (App
// or Command set) or a Mode.
type Button struct {
	Name      string   // e.g. "m8", "r3", "record"
	Mode      string   // "" for a launcher
	TalkOver  bool     // on M: apps go down while the input is live through M
	App       string   // a desktop ID
	Command   []string // a program and its arguments
	IfRunning string   // IfRunningStart or IfRunningSkip; launchers only
	// WhenLocked lets a launcher start its app while the screen is locked or
	// another user's session is in front (LAUNCH-14); launchers only.
	WhenLocked bool
}

// Launcher reports whether the button starts an app.
func (b Button) Launcher() bool { return b.App != "" || len(b.Command) > 0 }

// LauncherApps returns the desktop IDs the launchers start, by layout and
// button name; launchers with a command are left out. `apptrol check` and
// the service warn about IDs that are not installed (LAUNCH-10).
func (c *Config) LauncherApps() map[string]map[string]string {
	out := map[string]map[string]string{}
	for name, l := range c.Layouts {
		for button, b := range l.Buttons {
			if b.App == "" {
				continue
			}
			if out[name] == nil {
				out[name] = map[string]string{}
			}
			out[name][button] = b.App
		}
	}
	return out
}

// WhenLockedLaunchers returns the launchers allowed while the screen is
// locked, as "layouts.<layout>.buttons.<button>", sorted. Each is a warning
// on every load (LAUNCH-14).
func (c *Config) WhenLockedLaunchers() []string {
	var out []string
	for name, l := range c.Layouts {
		for button, b := range l.Buttons {
			if b.WhenLocked && b.Launcher() {
				out = append(out, fmt.Sprintf("layouts.%s.buttons.%s", name, button))
			}
		}
	}
	sort.Strings(out)
	return out
}

// Column returns the column of r1–r8, s1–s8 and m1–m8, and the button letter;
// ok is false for other buttons.
func (b Button) Column() (letter byte, col int, ok bool) { return buttonColumn(b.Name) }

func buttonColumn(name string) (letter byte, col int, ok bool) {
	if len(name) != 2 || !strings.ContainsRune("rsm", rune(name[0])) {
		return 0, 0, false
	}
	n, err := strconv.Atoi(name[1:])
	if err != nil || n < 1 || n > NumColumns {
		return 0, 0, false
	}
	return name[0], n, true
}

type rawButton struct {
	Mode       *string   `toml:"mode"`
	TalkOver   *bool     `toml:"talk_over"`
	App        *string   `toml:"app"`
	Command    *[]string `toml:"command"`
	IfRunning  *string   `toml:"if_running"`
	WhenLocked *bool     `toml:"when_locked"`
}

// parseButtons checks the buttons of one layout against its assignments
// (CFG-14) and returns them by name. Every problem is its own message.
func parseButtons(layout string, raw map[string]rawButton, l Layout, apps map[string]App, errs *problems) map[string]Button {
	out := map[string]Button{}
	names := make([]string, 0, len(raw))
	for n := range raw {
		names = append(names, n)
	}
	sort.Strings(names)
	at := func(name string) string { return fmt.Sprintf("layouts.%s.buttons.%s", layout, name) }

	// columnApp returns the app on the column's slider, if any.
	columnApp := func(col int) (App, bool) {
		id, ok := l.Assignment(Control{Kind: Slider, Column: col})
		if !ok {
			return App{}, false
		}
		return apps[id], true
	}

	for _, name := range names {
		rb := raw[name]
		b := Button{Name: name, IfRunning: IfRunningStart}
		letter, col, isColumn := buttonColumn(name)
		if !isColumn && !contains(launcherButtons, name) {
			errs.add("%s: unknown button (use record, marker_set, marker_prev, marker_next, r1–r%d, s1–s%d or m1–m%d)",
				at(name), NumColumns, NumColumns, NumColumns)
			continue
		}
		if rb.Mode != nil {
			b.Mode = *rb.Mode
		}
		if rb.App != nil {
			b.App = strings.TrimSpace(*rb.App)
			if b.App == "" {
				errs.add("%s.app: empty; give a desktop ID (`apptrol list apps` shows them)", at(name))
			}
		}
		if rb.Command != nil {
			b.Command = *rb.Command
			if len(b.Command) == 0 || strings.TrimSpace(b.Command[0]) == "" {
				errs.add("%s.command: give the program and its arguments, e.g. [\"konsole\", \"-e\", \"htop\"]", at(name))
			} else if reason := blockedCommand(b.Command); reason != "" {
				errs.add("%s.command: %s", at(name), reason) // LAUNCH-12
			}
		}
		launcher := rb.App != nil || rb.Command != nil
		switch {
		case rb.App != nil && rb.Command != nil:
			errs.add("%s: set either app or command, not both", at(name))
		case launcher && rb.Mode != nil:
			errs.add("%s: set either a launcher (app or command) or a mode, not both", at(name))
		case !launcher && rb.Mode == nil:
			errs.add("%s: set app, command or mode", at(name))
		}
		if rb.IfRunning != nil {
			b.IfRunning = *rb.IfRunning
			switch {
			case !launcher:
				errs.add("%s.if_running: only a launcher (app or command) has it", at(name))
			case b.IfRunning != IfRunningStart && b.IfRunning != IfRunningSkip:
				errs.add("%s.if_running: %q is not valid (use %s)", at(name), b.IfRunning, orList([]string{IfRunningStart, IfRunningSkip}))
			}
		}
		if rb.WhenLocked != nil {
			b.WhenLocked = *rb.WhenLocked
			if !launcher {
				errs.add("%s.when_locked: only a launcher (app or command) has it", at(name))
			}
		}
		if rb.TalkOver != nil {
			b.TalkOver = *rb.TalkOver
			if letter != 'm' {
				errs.add("%s.talk_over: only an M button has it (m1–m%d)", at(name), NumColumns)
			}
		}

		// What fits this button and column.
		app, assigned := App{}, false
		if isColumn {
			app, assigned = columnApp(col)
		}
		input := assigned && app.Type == TypeInput
		slider := fmt.Sprintf("slider%d", col)
		switch {
		case !isColumn: // record, marker_*
			if rb.Mode != nil {
				errs.add("%s: this button only starts apps; set app or command", at(name))
			}
		case letter == 'r':
			valid := []string{ModePlayPause, ModeOff}
			if input || !assigned {
				valid = []string{ModeOff} // play_pause needs an app (MEDIA-07)
			}
			if rb.Mode != nil && !contains(valid, b.Mode) {
				switch {
				case b.Mode == ModePlayPause && input:
					errs.add("%s: %q needs an app on %s; %s holds the input %q", at(name), b.Mode, slider, slider, app.ID)
				case b.Mode == ModePlayPause && !assigned:
					errs.add("%s: %q needs an app on %s; nothing is assigned there", at(name), b.Mode, slider)
				default:
					errs.add("%s.mode: %q is not valid here (use %s, or app or command to start an app)", at(name), b.Mode, orList(valid))
				}
			}
		default: // s, m: input columns only, no launchers
			valid := []string{ModeCough, ModeTalkOver, ModeOff}
			if letter == 'm' {
				valid = []string{ModeMute, ModeHoldToTalk}
			}
			switch {
			case !input && assigned:
				errs.add("%s: needs an input on %s; %s holds the app %q", at(name), slider, slider, app.ID)
			case !input:
				errs.add("%s: needs an input on %s; nothing is assigned there", at(name), slider)
			case launcher:
				errs.add("%s: this button cannot start apps; set a mode (%s)", at(name), orList(valid))
			case rb.Mode != nil && !contains(valid, b.Mode):
				errs.add("%s.mode: %q is not valid (use %s)", at(name), b.Mode, orList(valid))
			}
		}
		out[name] = b
	}

	// cough has no use with hold-to-talk: letting go of M mutes (ADR 0020).
	for _, b := range out {
		if letter, col, ok := b.Column(); ok && letter == 's' && b.Mode == ModeCough {
			if m := out[fmt.Sprintf("m%d", col)]; m.Mode == ModeHoldToTalk {
				errs.add("%s: %q has no use with %q on m%d: release M%d to mute", at(b.Name), ModeCough, ModeHoldToTalk, col, col)
			}
		}
	}
	return out
}
