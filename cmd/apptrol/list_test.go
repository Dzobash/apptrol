package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dzobash/apptrol/internal/audio/pulse"
	"github.com/Dzobash/apptrol/internal/mixer"
)

func testListing() *pulse.Listing {
	return &pulse.Listing{
		Server:        "PulseAudio (on PipeWire 1.6.0) 15.0.0",
		DefaultSource: "alsa_input.webcam",
		Streams: []pulse.StreamInfo{
			{Stream: mixer.Stream{ID: 57, AppName: "Spotify"}, Volume: 0.71, Sink: 1},
			{Stream: mixer.Stream{ID: 60, AppName: "ViberPC", Binary: "Viber"}, Volume: 1, Muted: true, Sink: 9},
		},
		Sources: []pulse.SourceInfo{
			{Device: mixer.Device{Name: "alsa_input.webcam", Description: "C922 Webcam"}, Volume: 0.8},
			{Device: mixer.Device{Name: "alsa_input.goxlr", Description: "GoXLR Mini Mic"}, Volume: 1},
		},
		Outputs: map[uint32]string{1: "GoXLR Music"},
	}
}

const listConfig = `
[apps.spotify]
name  = "Spotify"
match = ["spotify"]

[apps.mic]
name  = "Microphone"
type  = "input"
match = ["goxlr"]

[layouts.default]
slider1 = "spotify"
slider8 = "mic"
`

func TestCFG10_ListShowsNamesAndControls(t *testing.T) {
	path := writeConfig(t, listConfig)
	var out bytes.Buffer
	if err := cmdList(path, &out, func() (*pulse.Listing, error) { return testListing(), nil }); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"Audio server: PulseAudio (on PipeWire 1.6.0)",
		"NAME     BINARY  VOLUME      OUTPUT       CONTROL",
		"Spotify  -       71%         GoXLR Music  slider1 (Spotify)",
		"ViberPC  Viber   100% muted  -            -",
		"C922 Webcam (default)  alsa_input.webcam  80%     -",
		"GoXLR Mini Mic         alsa_input.goxlr   100%    slider8 (Microphone)",
		"Controls are from " + path,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
}

func TestCFG10_ListWithoutConfig(t *testing.T) {
	for name, path := range map[string]string{
		"missing": filepath.Join(t.TempDir(), "none.toml"),
		"invalid": writeConfig(t, "[apps.x]\nname = 1\n"),
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if err := cmdList(path, &out, func() (*pulse.Listing, error) { return testListing(), nil }); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "no controls are shown") {
				t.Errorf("no note about the configuration:\n%s", out.String())
			}
			if strings.Contains(out.String(), "slider1") {
				t.Errorf("controls shown without a valid configuration:\n%s", out.String())
			}
		})
	}
}

func TestCFG10_ListEmpty(t *testing.T) {
	var out bytes.Buffer
	if err := printListing(&out, &pulse.Listing{Server: "x"}, nil, ""); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Nothing is playing", "No input devices found"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestCFG10_ListServerError(t *testing.T) {
	err := cmdList("", &bytes.Buffer{}, func() (*pulse.Listing, error) { return nil, errors.New("refused") })
	if err == nil || !strings.Contains(err.Error(), "refused") || !strings.Contains(err.Error(), "pipewire-pulse") {
		t.Errorf("err = %v", err)
	}
}
