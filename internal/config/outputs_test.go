package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// outputsBase exercises both selectors on different kinds of control (CFG-20,
// ADR 0026, ADR 0027), without needing an audio server.
const outputsBase = `
[outputs.main]
name = "Main volume"
device = "default"
[outputs.speakers]
match = [" Test speaker "]
max_volume = 80
[layouts.default]
slider1 = "main"
knob2 = "speakers"
`

func TestCFG20_OutputsLoadWithEitherSelector(t *testing.T) {
	cfg, warnings := mustParseWarnings(t, outputsBase)
	// Only the default output is inactive; the fixed one works (OUT-01, OUT-03).
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want one inactive warning for the default output", warnings)
	}
	w := warnings[0]
	if w.Message != "following the default output is not implemented yet; this target is inactive" || w.Text != "outputs.main: "+w.Message {
		t.Errorf("warning = %+v", w)
	}
	if attrs := attrsOf(w); len(attrs) != 2 || attrs["apptrol.app.id"] != "main" || attrs["apptrol.app.type"] != TypeOutput {
		t.Errorf("warning attributes = %v", attrs)
	}
	main, fixed := cfg.Apps["main"], cfg.Apps["speakers"]
	if main.Device != DeviceDefault || main.Type != TypeOutput || main.Name != "Main volume" || main.MaxVolume != 100 || len(main.Match) != 0 {
		t.Errorf("default output = %+v", main)
	}
	if fixed.Device != "" || fixed.Type != TypeOutput || fixed.Name != "speakers" || fixed.MaxVolume != 80 || len(fixed.Match) != 1 || fixed.Match[0] != "Test speaker" {
		t.Errorf("fixed output = %+v", fixed)
	}
	if got := strings.Join(cfg.AppIDs(), ","); got != "main,speakers" {
		t.Errorf("AppIDs = %s", got)
	}
	if id, ok := cfg.Layout().Assignment(Control{Kind: Knob, Column: 2}); !ok || id != "speakers" {
		t.Errorf("knob2 = %q, %v", id, ok)
	}
}

func TestCFG20_RejectsInvalidSelectors(t *testing.T) {
	for _, tt := range []struct{ name, settings, want string }{
		{"neither", "", "exactly one of match and device"},
		{"both", "match = [\"speaker\"]\ndevice = \"default\"", "exactly one of match and device"},
		{"both empty", "match = []\ndevice = \"\"", "exactly one of match and device"},
		{"empty match", "match = []", "outputs.main.match: empty"},
		{"blank fragment", "match = [\"  \"]", "outputs.main.match[0]: empty fragment"},
		{"invalid device", "device = \"speakers\"", "outputs.main.device:"},
		{"empty device", "device = \"\"", "outputs.main.device:"},
		{"case sensitive device", "device = \"Default\"", "outputs.main.device:"},
		{"padded device", "device = \" default \"", "outputs.main.device:"},
		{"wrong device type", "device = true", "outputs.main.device"},
		{"wrong match type", "match = \"speaker\"", "outputs.main.match"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			requireProblem(t, problemsOf(t, "[outputs.main]\n"+tt.settings+"\n[layouts.default]\nslider1 = \"main\"\n"), tt.want)
		})
	}
}

func TestCFG20_DeviceRejectedOutsideOutputs(t *testing.T) {
	for _, table := range []string{"apps", "inputs"} {
		for _, device := range []string{"default", ""} {
			src := fmt.Sprintf("[%s.target]\nmatch = [\"test\"]\ndevice = %q\n[layouts.default]\nslider1 = \"target\"\n", table, device)
			requireProblem(t, problemsOf(t, src), table+".target.device: only an output has it")
		}
	}
	requireProblem(t, problemsOf(t, "[apps.mic]\ntype = \"input\"\nmatch = [\"test\"]\ndevice = \"default\"\n[layouts.default]\n"), "apps.mic.device: only an output has it")
}

func TestCFG20_OutputRulesAndDiagnostics(t *testing.T) {
	for _, tt := range []struct{ settings, want string }{
		{"type = \"output\"", "outputs.main.type: not used in [outputs]"},
		{"type = \"input\"", "outputs.main.type: not used in [outputs]"},
		{"talk_over_volume = 20", "outputs.main.talk_over_volume: only an input has it"},
		{"max_volume = 0", "outputs.main.max_volume: 0 is out of range"},
		{"max_volume = 151", "outputs.main.max_volume: 151 is out of range"},
		{"unknown = true", "outputs.main.unknown: unknown setting"},
	} {
		src := strings.Replace(outputsBase, "device = \"default\"", "device = \"default\"\n"+tt.settings, 1)
		requireProblem(t, problemsOf(t, src), tt.want)
	}
	for _, table := range []string{"apps", "inputs"} {
		src := outputsBase + fmt.Sprintf("\n[%s.main]\nmatch = [\"test\"]\n", table)
		requireProblem(t, problemsOf(t, src), "the same id is defined in two tables (["+table+"] and [outputs])")
	}
	requireProblem(t, problemsOf(t, outputsBase+"\n[media]\nplayer = \"main\"\n"), `media.player: "main" is an output`)
	requireProblem(t, problemsOf(t, outputsBase+"\n[layouts.default.buttons]\nr1 = { mode = \"play_pause\" }\n"), `holds the output "main"`)
	requireProblem(t, problemsOf(t, outputsBase+"\n[layouts.default.buttons]\nm1 = { mode = \"hold_to_talk\" }\n"), `holds the output "main"`)
	requireProblem(t, problemsOf(t, outputsBase+"\n[layouts.default.buttons]\ns1 = { mode = \"cough\" }\n"), `holds the output "main"`)
	requireProblem(t, problemsOf(t, outputsBase+"slider2 = \"main\"\n"), "already assigned to slider1")
	requireProblem(t, problemsOf(t, outputsBase+"device = \"default\"\n"), "put it under [outputs.<id>]")
	requireProblem(t, problemsOf(t, outputsBase+"slider8 = \"missing\"\n"), "[outputs]")
	for _, setting := range []string{`mode = "off"`, `command = ["true"]`} {
		mustParse(t, outputsBase+"\n[layouts.default.buttons]\nr1 = { "+setting+" }\n")
	}
	_, ws := mustParse(t, outputsBase+"\n[outputs.spare]\ndevice = \"default\"\n")
	requireProblem(t, ws, "outputs.spare: not assigned to any control")
}

func TestOUT04_OutputOverlapWarnsFirstControlWins(t *testing.T) {
	src := strings.Replace(outputsBase, `device = "default"`, `match = ["speaker"]`, 1)
	_, warnings := mustParseWarnings(t, src)
	for _, w := range warnings {
		if strings.Contains(w.Text, "can match the same") {
			if !strings.Contains(w.Text, `those go to "main" on slider1`) {
				t.Fatalf("wrong owner: %v", w)
			}
			return
		}
	}
	t.Fatal("missing output overlap warning")
}

func TestCFG21_MonitorWarningsOnEveryLoad(t *testing.T) {
	for _, table := range []string{"inputs", "outputs", "apps"} {
		for _, old := range []bool{false, true} {
			if old && table != "apps" {
				continue
			}
			src := fmt.Sprintf("[%s.target]\nmatch = [\" Speaker.MONITOR \", \"speaker.monitor.extra\", \"speaker.monitor\"]\n", table)
			if old {
				src += "type = \"input\"\n"
			}
			src += "[layouts.default]\nslider1 = \"target\"\n"
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(src), 0600); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				_, ws, err := Load(path)
				if err != nil {
					t.Fatal(err)
				}
				var monitors []Warning
				for _, w := range ws {
					if strings.Contains(w.Message, "match names a monitor") {
						monitors = append(monitors, w)
					}
				}
				want := 2
				if table == "apps" && !old {
					want = 0
				}
				if len(monitors) != want {
					t.Fatalf("%s old=%v: warnings = %+v, want %d monitor warnings", table, old, ws, want)
				}
				for i, w := range monitors {
					fragment := []string{"Speaker.MONITOR", "speaker.monitor"}[i]
					if w.Message != "match names a monitor, which records an output and is never matched; use the output's name without .monitor" || !strings.Contains(w.Text, table+".target.match") || !strings.Contains(w.Text, fragment) {
						t.Errorf("warning = %+v", w)
					}
					if a := attrsOf(w); a["apptrol.app.id"] != "target" || a["apptrol.app.match"] != fragment || len(a) != 2 {
						t.Errorf("attributes = %v", a)
					}
				}
			}
		}
	}
}

// Device discovery must be available both in the installed example and in the
// reference; these checks protect the required instructions (CFG-22, ADR 0026).
func TestCFG22_DeviceDiscoveryInstructions(t *testing.T) {
	for _, path := range []string{"../../examples/config.toml", "../../docs/config.md"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ToLower(string(data))
		for _, want := range []string{"apptrol list", "pactl list short sources", "pactl list short sinks", "case-insensitive", "name or", "description", "full name", "first", "by name", "warning", "monitors", "never matched", ".monitor"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s lacks device discovery guidance: %q", path, want)
			}
		}
	}
}
