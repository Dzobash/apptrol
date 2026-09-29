package config

import (
	"errors"
	"strings"
	"testing"
)

func TestCTRL03_MaxVolume(t *testing.T) {
	cfg, _, err := Parse("t.toml", []byte(`
[apps.a]
match = ["a"]
max_volume = 150
[apps.b]
match = ["b"]
max_volume = 1
[apps.c]
match = ["c"]
[layouts.default]
slider1 = "a"
slider2 = "b"
slider3 = "c"
`))
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]int{"a": 150, "b": 1, "c": 100} {
		if got := cfg.Apps[id].MaxVolume; got != want {
			t.Errorf("apps.%s.max_volume = %d, want %d", id, got, want)
		}
	}
}

func TestCTRL03_MaxVolumeOutOfRange(t *testing.T) {
	for value, want := range map[string]string{
		"0":      "apps.a.max_volume: 0 is out of range (1–150, in percent)",
		"151":    "apps.a.max_volume: 151 is out of range",
		"-5":     "apps.a.max_volume: -5 is out of range",
		`"150%"`: "max_volume",
	} {
		_, _, err := Parse("t.toml", []byte("[apps.a]\nmatch = [\"a\"]\nmax_volume = "+value+"\n[layouts.default]\nslider1 = \"a\"\n"))
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("max_volume = %s: err = %v, want a validation error", value, err)
			continue
		}
		requireProblem(t, ve.Problems, want)
	}
}

func TestCFG12_OverlapWarnings(t *testing.T) {
	cfg := `
[apps.browser]
match = ["Firefox", "vivaldi"]
[apps.fire]
match = ["fire"]
[apps.twin]
match = ["vivaldi"]
[apps.other]
match = ["spotify"]
[apps.mic]
type  = "input"
match = ["goxlr"]
[apps.mic2]
type  = "input"
match = ["GoXLR Mini"]
[apps.firecam]
type  = "input"
match = ["fire"]
[apps.spare]
match = ["firefox"]

[layouts.default]
knob1   = "fire"
slider2 = "browser"
slider3 = "twin"
slider4 = "other"
slider7 = "mic2"
slider8 = "mic"
knob2   = "firecam"
`
	_, warnings, err := Parse("t.toml", []byte(cfg))
	if err != nil {
		t.Fatal(err)
	}
	var overlapsFound []string
	for _, w := range warnings {
		if strings.Contains(w, "can match the same") {
			overlapsFound = append(overlapsFound, w)
		}
	}
	want := []string{
		`apps "browser" (slider2) and "twin" (slider3) can match the same streams (both match "vivaldi"); those go to "browser" on slider2`,
		`apps "browser" (slider2) and "fire" (knob1) can match the same streams ("fire" is part of "Firefox"); those go to "browser" on slider2`,
		`apps "mic2" (slider7) and "mic" (slider8) can match the same input devices ("goxlr" is part of "GoXLR Mini"); those go to "mic2" on slider7`,
	}
	if strings.Join(overlapsFound, "\n") != strings.Join(want, "\n") {
		t.Errorf("warnings:\n%s\nwant:\n%s", strings.Join(overlapsFound, "\n"), strings.Join(want, "\n"))
	}
}
