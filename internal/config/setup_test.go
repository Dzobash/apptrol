package config

import (
	"reflect"
	"testing"

	"github.com/Dzobash/apptrol/internal/mixer"
)

func TestSetup(t *testing.T) {
	cfg, _, err := Parse("test.toml", []byte(`
[apps.music]
name  = "Music"
match = ["spotify"]
max_volume = 150

[apps.mic]
name  = "Mic"
type  = "input"
match = ["GoXLR"]

[apps.unused]
name  = "Unused"
match = ["x"]

[layouts.default]
slider1 = "music"
knob8   = "mic"
`))
	if err != nil {
		t.Fatal(err)
	}
	want := mixer.Setup{
		Targets: map[string]mixer.Target{
			"music":  {ID: "music", Name: "Music", Kind: mixer.App, Match: []string{"spotify"}, MaxVolume: 1.5},
			"mic":    {ID: "mic", Name: "Mic", Kind: mixer.Input, Match: []string{"GoXLR"}, MaxVolume: 1},
			"unused": {ID: "unused", Name: "Unused", Kind: mixer.App, Match: []string{"x"}, MaxVolume: 1},
		},
		Assignments: map[mixer.Control]string{
			{Kind: mixer.Slider, Column: 1}: "music",
			{Kind: mixer.Knob, Column: 8}:   "mic",
		},
	}
	if got := cfg.Setup(); !reflect.DeepEqual(got, want) {
		t.Errorf("Setup() =\n%+v\nwant\n%+v", got, want)
	}
}
