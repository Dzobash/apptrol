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

// A fixed output reaches the mixer as an Output with its control; one that
// follows the default stays out, with its assignment, until it is built
// (CFG-20, OUT-01, OUT-03, ADR 0026 implementation note, ADR 0027).
func TestCFG20_SetupPassesFixedOutputsButNotDefault(t *testing.T) {
	cfg, _ := mustParse(t, outputsBase+`
[apps.music]
match = ["Test speaker"]
[inputs.mic]
match = ["Test microphone"]
`)
	setup := cfg.Setup()
	if target, ok := setup.Targets["main"]; ok {
		t.Errorf("the default output reached the mixer as %+v", target)
	}
	want := mixer.Target{ID: "speakers", Name: "speakers", Kind: mixer.Output, Match: []string{"Test speaker"}, MaxVolume: 0.8}
	if got := setup.Targets["speakers"]; !reflect.DeepEqual(got, want) {
		t.Errorf("fixed output = %+v, want %+v", got, want)
	}
	wantAssignments := map[mixer.Control]string{{Kind: mixer.Knob, Column: 2}: "speakers"}
	if !reflect.DeepEqual(setup.Assignments, wantAssignments) {
		t.Errorf("assignments = %v, want %v", setup.Assignments, wantAssignments)
	}
	if len(setup.Targets) != 3 || setup.Targets["music"].Kind != mixer.App || setup.Targets["mic"].Kind != mixer.Input {
		t.Errorf("app and input must remain in setup: %v", setup.Targets)
	}
	if len(cfg.Layout().Assignments) != 2 || len(cfg.Apps) != 4 {
		t.Fatal("Setup changed the parsed config")
	}
}

// An output acts on an output device, never on a playback stream, even one
// named like its match: neither its control nor its buttons nor a reload
// touch the stream (OUT-01, CTRL-06, ADR 0027).
func TestOUT01_OutputNeverControlsAPlaybackStream(t *testing.T) {
	cfg, _ := mustParse(t, `[outputs.speakers]
match = ["speaker"]
[layouts.default]
slider1 = "speakers"
`)
	m := mixer.New(cfg.Setup(), mixer.State{})
	events := []mixer.Event{
		mixer.AudioSnapshot{Streams: []mixer.Stream{{ID: 1, AppName: "speaker"}},
			Outputs: []mixer.Device{{Name: "alsa_output.test-speaker", Description: "Test speaker"}}},
		mixer.ControlMoved{Control: mixer.Control{Kind: mixer.Slider, Column: 1}, Value: 100},
		mixer.ButtonPressed{Button: mixer.ButtonM, Column: 1},
		mixer.ButtonPressed{Button: mixer.ButtonS, Column: 1},
		mixer.StreamAdded{Stream: mixer.Stream{ID: 2, AppName: "speaker", Binary: "speaker"}},
		mixer.ConfigChanged{Setup: cfg.Setup()},
		mixer.ControlMoved{Control: mixer.Control{Kind: mixer.Slider, Column: 1}, Value: 20},
	}
	outputSet := false
	for _, event := range events {
		for _, action := range m.Handle(event) {
			switch action.(type) {
			case mixer.SetStreamVolume, mixer.SetStreamMute:
				t.Errorf("%T caused output target to control playback stream: %+v", event, action)
			case mixer.SetOutputVolume:
				outputSet = true
			}
		}
	}
	if !outputSet {
		t.Error("the slider never set the output device")
	}
}
