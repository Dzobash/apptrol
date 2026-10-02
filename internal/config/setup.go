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

// Setup returns the part of the configuration the mixer needs: every app,
// the assignments of the active layout and its M and S buttons.
func (c *Config) Setup() mixer.Setup {
	s := mixer.Setup{Layout: DefaultLayout, Targets: map[string]mixer.Target{}, Assignments: map[mixer.Control]string{},
		Buttons: map[mixer.LED]mixer.Button{}}
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
