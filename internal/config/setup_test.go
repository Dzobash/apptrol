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

[inputs.mic]
name  = "Mic"
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
			"music": {ID: "music", Name: "Music", Kind: mixer.App, Match: []string{"spotify"}, MaxVolume: 1.5},
			"mic": {ID: "mic", Name: "Mic", Kind: mixer.Input, Match: []string{"GoXLR"}, MaxVolume: 1,
				TalkOverVolume: mixer.DefaultTalkOverVolume}, // CFG-16 default
			"unused": {ID: "unused", Name: "Unused", Kind: mixer.App, Match: []string{"x"}, MaxVolume: 1},
		},
		Layout:    DefaultLayout,
		Buttons:   map[mixer.LED]mixer.Button{},
		Launchers: map[mixer.LED]mixer.Launch{},
		Assignments: map[mixer.Control]string{
			{Kind: mixer.Slider, Column: 1}: "music",
			{Kind: mixer.Knob, Column: 8}:   "mic",
		},
	}
	if got := cfg.Setup(); !reflect.DeepEqual(got, want) {
		t.Errorf("Setup() =\n%+v\nwant\n%+v", got, want)
	}
}

// Before output audio support lands, output matches must not be converted into
// app matches. Keep both output selectors out of the mixer (CFG-20, ADR 0026
// implementation note, ADR 0027); #85 replaces this temporary guard.
func TestCFG20_SetupOmitsOutputsUntilOutputControlExists(t *testing.T) {
	cfg, _ := mustParse(t, outputsBase+`
[apps.music]
match = ["Test speaker"]
[inputs.mic]
match = ["Test microphone"]
`)
	setup := cfg.Setup()
	for _, id := range []string{"main", "speakers"} {
		if target, ok := setup.Targets[id]; ok {
			t.Errorf("output %q reached mixer as %+v", id, target)
		}
	}
	if len(setup.Assignments) != 0 {
		t.Errorf("output assignments reached mixer: %v", setup.Assignments)
	}
	if len(setup.Targets) != 2 || setup.Targets["music"].Kind != mixer.App || setup.Targets["mic"].Kind != mixer.Input {
		t.Errorf("app and input must remain in setup: %v", setup.Targets)
	}
	if len(cfg.Layout().Assignments) != 2 || len(cfg.Apps) != 4 {
		t.Fatal("Setup changed the parsed config")
	}
}

// A fixed output must never control a playback stream with the same name while
// output support is pending (CFG-20, ADR 0026 implementation note, ADR 0027, #85).
func TestCFG20_FixedOutputCannotControlSameNamedPlaybackStream(t *testing.T) {
	cfg, _ := mustParse(t, `[outputs.speakers]
match = ["speaker"]
[layouts.default]
slider1 = "speakers"
`)
	m := mixer.New(cfg.Setup(), mixer.State{})
	events := []mixer.Event{
		mixer.AudioSnapshot{Streams: []mixer.Stream{{ID: 1, AppName: "speaker"}}},
		mixer.ControlMoved{Control: mixer.Control{Kind: mixer.Slider, Column: 1}, Value: 100},
		mixer.ButtonPressed{Button: mixer.ButtonM, Column: 1},
		mixer.ButtonPressed{Button: mixer.ButtonS, Column: 1},
		mixer.ConfigChanged{Setup: cfg.Setup()},
		mixer.ControlMoved{Control: mixer.Control{Kind: mixer.Slider, Column: 1}, Value: 20},
	}
	for _, event := range events {
		for _, action := range m.Handle(event) {
			switch action.(type) {
			case mixer.SetStreamVolume, mixer.SetStreamMute:
				t.Errorf("%T caused output target to control playback stream: %+v", event, action)
			}
		}
	}
}
