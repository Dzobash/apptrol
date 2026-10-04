package config

import "github.com/Dzobash/apptrol/internal/mixer"

// mixerModes maps the column buttons' modes to the mixer's (INPUT-01,
// MEDIA-07). A launcher on R becomes ModeLauncher.
var mixerModes = map[string]mixer.Mode{
	ModeOff:        mixer.ModeOff,
	ModeMute:       mixer.ModeMute,
	ModeHoldToTalk: mixer.ModeHoldToTalk,
	ModeCough:      mixer.ModeCough,
	ModeTalkOver:   mixer.ModeTalkOver,
	ModePlayPause:  mixer.ModePlayPause,
}

// launcherTransport maps the launcher buttons' names to the controller's.
var launcherTransport = map[string]mixer.TransportButton{
	"record": mixer.Record, "marker_set": mixer.MarkerSet, "marker_prev": mixer.MarkerPrev, "marker_next": mixer.MarkerNext,
}

// launcherLED returns the mixer's name for a launcher button: a transport
// button, or R on a column.
func launcherLED(name string) (mixer.LED, bool) {
	if t, ok := launcherTransport[name]; ok {
		return mixer.LED{Transport: t}, true
	}
	if letter, col, ok := buttonColumn(name); ok && letter == 'r' {
		return mixer.LED{Button: mixer.ButtonR, Column: col}, true
	}
	return mixer.LED{}, false
}

// Setup returns the part of the configuration the mixer needs: every app,
// the assignments of the active layout and its M and S buttons.
func (c *Config) Setup() mixer.Setup {
	s := mixer.Setup{Layout: DefaultLayout, Targets: map[string]mixer.Target{}, Assignments: map[mixer.Control]string{},
		Buttons: map[mixer.LED]mixer.Button{}, MediaPlayer: c.Media.Player, Launchers: map[mixer.LED]mixer.Launch{}}
	for id, app := range c.Apps {
		kind := mixer.App
		if app.Type == TypeInput {
			kind = mixer.Input
		}
		s.Targets[id] = mixer.Target{ID: id, Name: app.Name, Kind: kind, Match: append([]string(nil), app.Match...),
			MaxVolume: float64(app.MaxVolume) / 100, TalkOverVolume: app.TalkOverVolume}
	}
	for _, a := range c.Layout().Assignments {
		kind := mixer.Slider
		if a.Control.Kind == Knob {
			kind = mixer.Knob
		}
		s.Assignments[mixer.Control{Kind: kind, Column: a.Control.Column}] = a.AppID
	}
	for _, b := range c.Layout().Buttons {
		if b.Launcher() {
			if l, ok := launcherLED(b.Name); ok {
				s.Launchers[l] = mixer.Launch{DesktopID: b.App, Command: append([]string(nil), b.Command...),
					SkipIfRunning: b.IfRunning == IfRunningSkip, WhenLocked: b.WhenLocked}
			}
		}
		letter, col, ok := b.Column()
		mode, known := mixerModes[b.Mode]
		if b.Launcher() {
			mode, known = mixer.ModeLauncher, true
		}
		if !ok || !known {
			continue
		}
		button := map[byte]mixer.ButtonKind{'r': mixer.ButtonR, 's': mixer.ButtonS, 'm': mixer.ButtonM}[letter]
		s.Buttons[mixer.LED{Button: button, Column: col}] = mixer.Button{Mode: mode, TalkOver: b.TalkOver}
	}
	return s
}
