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
			{Device: mixer.Device{Name: "alsa_input.webcam", Description: "Desk Webcam"}, Volume: 0.8},
			{Device: mixer.Device{Name: "alsa_input.goxlr", Description: "GoXLR Mini Mic"}, Volume: 1},
		},
		DefaultSink: "alsa_output.speakers",
		Sinks: []pulse.SinkInfo{
			{Device: mixer.Device{Name: "alsa_output.speakers", Description: "Built-in Speakers"}, Index: 2, Volume: 0.45},
			{Device: mixer.Device{Name: "alsa_output.goxlr-music", Description: "GoXLR Music"}, Index: 1, Volume: 1, Muted: true},
		},
	}
}

const listConfig = `
[apps.spotify]
name  = "Spotify"
match = ["spotify"]

[inputs.mic]
name  = "Microphone"
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
		"Desk Webcam (default)  alsa_input.webcam  80%     -",
		"GoXLR Mini Mic         alsa_input.goxlr   100%    slider8 (Microphone)",
		"Controls are from " + path,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
}

// Output devices are listed with description, name and volume, and * marks
// the system's default output (OUT-12, ADR 0027).
func TestOUT12_ListShowsOutputDevices(t *testing.T) {
	var out bytes.Buffer
	if err := printListing(&out, testListing(), nil, "", sectionAll); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"Output devices (* = the system's default). Put part of DESCRIPTION or NAME into an output's match list:",
		"    DESCRIPTION        NAME                     VOLUME      CONTROL\n",
		"  * Built-in Speakers  alsa_output.speakers     45%         -\n",
		"    GoXLR Music        alsa_output.goxlr-music  100% muted  -\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "*") != 2 { // the heading and one output
		t.Errorf("more than one output marked as the default:\n%s", got)
	}
}

// The CONTROL column names the control each output device is on, both
// controls when two fixed outputs match one device (OUT-12, OUT-04).
func TestOUT12_ListShowsOutputControl(t *testing.T) {
	path := writeConfig(t, `
[outputs.speakers]
name  = "Speakers"
match = ["speakers"]

[outputs.built-in]
name  = "Built-in"
match = ["built-in"]

[outputs.music]
name  = "Music"
match = ["GOXLR MUSIC"]

[outputs.main]
device = "default"

[layouts.default]
slider7 = "speakers"
knob7   = "built-in"
slider6 = "music"
slider5 = "main"
`)
	var out bytes.Buffer
	if err := cmdList(path, &out, func() (*pulse.Listing, error) { return testListing(), nil }); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		// The default output is also slider7's speakers; slider5 comes
		// first, so it owns them (OUT-03, OUT-04, ADR 0038).
		"  * Built-in Speakers  alsa_output.speakers     45%         slider5 (main)\n",
		"    GoXLR Music        alsa_output.goxlr-music  100% muted  slider6 (Music)\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "slider7") {
		t.Errorf("the control that yields is shown as holding the device:\n%s", got)
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
	if err := printListing(&out, &pulse.Listing{Server: "x"}, nil, "", sectionAll); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Nothing is playing", "No input devices found", "No output devices found"} {
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

// Each section can be asked for alone: it is the same section `apptrol list`
// prints, without the others, with `*` on the default output (LAUNCH-16).
func TestLAUNCH16_ListSectionAlone(t *testing.T) {
	tests := []struct {
		name    string
		section listSection
		want    []string
		notWant []string
	}{
		{
			name:    "outputs",
			section: sectionOutputs,
			want: []string{
				"Output devices (* = the system's default). Put part of DESCRIPTION or NAME into an output's match list:\n",
				"    DESCRIPTION        NAME                     VOLUME      CONTROL\n",
				"  * Built-in Speakers  alsa_output.speakers     45%         -\n",
				"    GoXLR Music        alsa_output.goxlr-music  100% muted  -\n",
			},
			notWant: []string{"Audio server", "Playing apps", "Input devices", "Nothing is playing"},
		},
		{
			name:    "inputs",
			section: sectionInputs,
			want: []string{
				"Input devices. Put part of DESCRIPTION or NAME into an input's match list:\n",
				"  DESCRIPTION            NAME               VOLUME  CONTROL\n",
				"  Desk Webcam (default)  alsa_input.webcam  80%     -\n",
				"  GoXLR Mini Mic         alsa_input.goxlr   100%    -\n",
			},
			notWant: []string{"Audio server", "Playing apps", "Output devices"},
		},
		{
			name:    "playing",
			section: sectionPlaying,
			want: []string{
				// The standalone command points to the launcher command;
				// `apptrol list` keeps the shorter heading (LAUNCH-16).
				"Playing apps. Put part of NAME or BINARY into an app's match list (`apptrol list apps` shows the desktop IDs a launcher uses):\n",
				"  NAME     BINARY  VOLUME      OUTPUT       CONTROL\n",
				"  Spotify  -       71%         GoXLR Music  -\n",
			},
			notWant: []string{"Audio server", "an app's match list:", "Input devices", "Output devices"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := printListing(&out, testListing(), nil, "", tt.section); err != nil {
				t.Fatal(err)
			}
			got := out.String()
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("output lacks %q:\n%s", want, got)
				}
			}
			for _, not := range tt.notWant {
				if strings.Contains(got, not) {
					t.Errorf("output contains %q:\n%s", not, got)
				}
			}
		})
	}
}

// `apptrol list` keeps printing every section with today's headings; only the
// standalone `list playing` grew the pointer to `apptrol list apps` (LAUNCH-16).
func TestLAUNCH16_ListKeepsEverySection(t *testing.T) {
	var out bytes.Buffer
	if err := printListing(&out, testListing(), nil, "", sectionAll); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"Audio server: PulseAudio (on PipeWire 1.6.0)",
		"Playing apps. Put part of NAME or BINARY into an app's match list:\n",
		"Input devices. Put part of DESCRIPTION or NAME into an input's match list:\n",
		"Output devices (* = the system's default). Put part of DESCRIPTION or NAME into an output's match list:\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "apptrol list apps") {
		t.Errorf("`apptrol list` grew the standalone playing heading:\n%s", got)
	}
}

// A section asked for alone carries the same note about the configuration as
// `apptrol list`, because its CONTROL column depends on it (LAUNCH-16, CFG-10).
func TestLAUNCH16_ListSectionWithoutConfig(t *testing.T) {
	var out bytes.Buffer
	path := filepath.Join(t.TempDir(), "none.toml")
	if err := cmdListSection(path, &out, func() (*pulse.Listing, error) { return testListing(), nil }, sectionOutputs); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no controls are shown") {
		t.Errorf("no note about the configuration:\n%s", out.String())
	}
}

func TestLAUNCH16_ListSectionServerError(t *testing.T) {
	err := cmdListSection("", &bytes.Buffer{}, func() (*pulse.Listing, error) { return nil, errors.New("refused") }, sectionInputs)
	if err == nil || !strings.Contains(err.Error(), "refused") || !strings.Contains(err.Error(), "pipewire-pulse") {
		t.Errorf("err = %v", err)
	}
}
