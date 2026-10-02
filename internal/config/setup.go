package config

import "github.com/Dzobash/apptrol/internal/mixer"

// Setup returns the part of the configuration the mixer needs: every app and
// the assignments of the active layout.
func (c *Config) Setup() mixer.Setup {
	s := mixer.Setup{Layout: DefaultLayout, Targets: map[string]mixer.Target{}, Assignments: map[mixer.Control]string{}}
	for id, app := range c.Apps {
		kind := mixer.App
		if app.Type == TypeInput {
			kind = mixer.Input
		}
		t := mixer.Target{ID: id, Name: app.Name, Kind: kind, Match: append([]string(nil), app.Match...),
			MaxVolume: float64(app.MaxVolume) / 100}
		if kind == mixer.Input {
			t.TalkOverVolume = mixer.DefaultTalkOverVolume // CFG-16; the file cannot set it yet
		}
		s.Targets[id] = t
	}
	for _, a := range c.Layout().Assignments {
		kind := mixer.Slider
		if a.Control.Kind == Knob {
			kind = mixer.Knob
		}
		s.Assignments[mixer.Control{Kind: kind, Column: a.Control.Column}] = a.AppID
	}
	return s
}
