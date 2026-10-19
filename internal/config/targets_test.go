package config

import (
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/Dzobash/apptrol/internal/logattr"
)

// targetsBase has one app and one input in their own tables (CFG-18).
const targetsBase = `
[apps.tone-a]
name  = "Test tone A"
match = ["tone-a"]

[inputs.mic]
name  = "Headset"
match = ["Headset"]

[layouts.default]
slider1 = "tone-a"
slider8 = "mic"
`

// oldFormWarnings returns the warnings about an old form (CFG-19).
func oldFormWarnings(ws []Warning) []Warning {
	var out []Warning
	for _, w := range ws {
		if w.Message != "" {
			out = append(out, w)
		}
	}
	return out
}

func attrsOf(w Warning) map[string]string {
	m := map[string]string{}
	for _, a := range w.Attrs {
		m[a.Key] = a.Value.String()
	}
	return m
}

func TestCFG18_InputsHaveTheirOwnTable(t *testing.T) {
	cfg, warnings := mustParseWarnings(t, targetsBase)
	if got := cfg.Apps["mic"]; got.Type != TypeInput || got.Name != "Headset" || got.TalkOverVolume != DefaultTalkOverVolume {
		t.Errorf("inputs.mic = %+v, want an input with the default talk_over_volume", got)
	}
	if got := cfg.Apps["tone-a"]; got.Type != TypeApp {
		t.Errorf("apps.tone-a: type %q, want %q", got.Type, TypeApp)
	}
	if len(warnings) != 0 {
		t.Errorf("the new form gets no warnings, got %v", warnings)
	}
}

func TestCFG18_SameIDInTwoTablesIsRejected(t *testing.T) {
	ps := problemsOf(t, targetsBase+`
[apps.mic]
match = ["mic"]
`)
	want := `inputs.mic: the same id is defined in two tables ([apps] and [inputs])`
	if len(ps) != 1 || ps[0] != want {
		t.Errorf("problems = %q, want only %q", ps, want)
	}
}

// The duplicate id is an error the log can be searched for by its id, as ADR
// 0026 point 7 lists it; other problems carry no attributes (CFG-18).
func TestCFG18_SameIDInTwoTablesCarriesItsID(t *testing.T) {
	_, _, err := Parse("test.toml", []byte(targetsBase+`
[apps.mic]
match = ["mic"]

[apps.bad]
`))
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("Parse: want *ValidationError, got %v", err)
	}
	var dup *Problem
	for i, p := range ve.Problems {
		if strings.Contains(p.Text, "the same id is defined in two tables") {
			dup = &ve.Problems[i]
		} else if len(p.Attrs) != 0 {
			t.Errorf("%q has attributes %v, want none", p.Text, p.Attrs)
		}
	}
	if dup == nil {
		t.Fatalf("no duplicate-id problem in %q", ve.Texts())
	}
	want := []slog.Attr{slog.String(logattr.KeyAppID, "mic")}
	if len(dup.Attrs) != 1 || !dup.Attrs[0].Equal(want[0]) {
		t.Errorf("attrs = %v, want %v", dup.Attrs, want)
	}
	if !strings.Contains(err.Error(), "\n  - inputs.mic: the same id is defined in two tables ([apps] and [inputs])") {
		t.Errorf("error text for apptrol check:\n%s", err)
	}
}

func TestCFG18_TalkOverVolumeOnlyInInputs(t *testing.T) {
	src := strings.Replace(targetsBase, `match = ["Headset"]`, "match = [\"Headset\"]\ntalk_over_volume = 10", 1)
	cfg, _ := mustParse(t, src)
	if v := cfg.Apps["mic"].TalkOverVolume; v != 10 {
		t.Errorf("inputs.mic.talk_over_volume = %d, want 10", v)
	}
	src = strings.Replace(targetsBase, `match = ["tone-a"]`, "match = [\"tone-a\"]\ntalk_over_volume = 10", 1)
	requireProblem(t, problemsOf(t, src), "apps.tone-a.talk_over_volume: only an input has it; inputs go in [inputs.tone-a]")
}

func TestCFG18_ProblemsNameTheInputsTable(t *testing.T) {
	src := strings.Replace(targetsBase, `match = ["Headset"]`, "match = []\nmax_volume = 0", 1)
	ps := problemsOf(t, src)
	requireProblem(t, ps, "inputs.mic.match: empty")
	requireProblem(t, ps, "inputs.mic.max_volume: 0 is out of range")
}

func TestCFG18_UndefinedTargetNamesAllTables(t *testing.T) {
	requireProblem(t, problemsOf(t, targetsBase+"knob1 = \"ghost\"\n"),
		`layouts.default.knob1: app "ghost" is not defined in [apps], [inputs] or [outputs]`)
}

func TestCFG18_MediaPlayerMustBeAnApp(t *testing.T) {
	cfg, _ := mustParse(t, targetsBase+"\n[media]\nplayer = \"tone-a\"\n")
	if cfg.Media.Player != "tone-a" {
		t.Errorf("media.player = %q", cfg.Media.Player)
	}
	requireProblem(t, problemsOf(t, targetsBase+"\n[media]\nplayer = \"mic\"\n"), `media.player: "mic" is an input`)
}

func TestCFG18_UnusedInputNamesItsTable(t *testing.T) {
	_, warnings := mustParse(t, targetsBase+"\n[inputs.spare]\nmatch = [\"Line\"]\n")
	requireProblem(t, warnings, "inputs.spare: not assigned to any control")
}

func TestCFG19_OldInputFormIsReadWithAWarning(t *testing.T) {
	cfg, warnings := mustParseWarnings(t, `
[apps.tone-a]
match = ["tone-a"]

[apps.mic]
name  = "Headset"
type  = "input"
match = ["Headset"]
talk_over_volume = 10

[layouts.default]
slider1 = "tone-a"
slider8 = "mic"
`)
	if got := cfg.Apps["mic"]; got.Type != TypeInput || got.TalkOverVolume != 10 {
		t.Errorf("apps.mic = %+v, want an input with talk_over_volume 10", got)
	}
	old := oldFormWarnings(warnings)
	if len(old) != 1 {
		t.Fatalf("old-form warnings = %v, want one", old)
	}
	w := old[0]
	if want := `apps.mic: type = "input" is the old form; move it to [inputs.mic]`; w.Text != want {
		t.Errorf("text = %q, want %q", w.Text, want)
	}
	if want := "config uses an old form; move the entry to its own table"; w.Message != want {
		t.Errorf("message = %q, want %q (ADR 0026 point 7)", w.Message, want)
	}
	if a := attrsOf(w); a["apptrol.app.id"] != "mic" || a["apptrol.config.table"] != "inputs" || len(a) != 2 {
		t.Errorf("attributes = %v, want apptrol.app.id=mic apptrol.config.table=inputs", a)
	}
}

func TestCFG19_OldAppFormIsReadWithAWarning(t *testing.T) {
	cfg, warnings := mustParseWarnings(t, strings.Replace(minimal, `match = ["spotify"]`, "type  = \"app\"\nmatch = [\"spotify\"]", 1))
	if got := cfg.Apps["spotify"].Type; got != TypeApp {
		t.Errorf("type = %q, want %q", got, TypeApp)
	}
	old := oldFormWarnings(warnings)
	if len(old) != 1 || !strings.HasPrefix(old[0].Text, `apps.spotify: type = "app" is the old form; remove the line`) {
		t.Fatalf("old-form warnings = %v, want one for apps.spotify", old)
	}
	if a := attrsOf(old[0]); a["apptrol.app.id"] != "spotify" || a["apptrol.config.table"] != "apps" {
		t.Errorf("attributes = %v", a)
	}
}

func TestCFG19_OneWarningPerOldEntry(t *testing.T) {
	_, warnings := mustParseWarnings(t, `
[apps.mic]
type  = "input"
match = ["Headset"]

[apps.line]
type  = "input"
match = ["Line"]

[layouts.default]
slider7 = "line"
slider8 = "mic"
`)
	old := oldFormWarnings(warnings)
	if len(old) != 2 || attrsOf(old[0])["apptrol.app.id"] != "line" || attrsOf(old[1])["apptrol.app.id"] != "mic" {
		t.Errorf("old-form warnings = %v, want one each for line and mic, sorted", old)
	}
}

func TestCFG19_OtherTypesAreRejected(t *testing.T) {
	requireProblem(t, problemsOf(t, strings.Replace(minimal, `match = ["spotify"]`, "type  = \"speaker\"\nmatch = [\"spotify\"]", 1)),
		`apps.spotify.type: "speaker" is not valid; [apps] holds only apps, and inputs go in [inputs.spotify]`)
	src := strings.Replace(targetsBase, `match = ["Headset"]`, "type  = \"input\"\nmatch = [\"Headset\"]", 1)
	requireProblem(t, problemsOf(t, src), "inputs.mic.type: not used in [inputs]; remove the line")
}

func TestCFG20_OutputTypeInAppsPointsToOutputsTable(t *testing.T) {
	requireProblem(t, problemsOf(t, strings.Replace(minimal, `match = ["spotify"]`, "type  = \"output\"\nmatch = [\"spotify\"]", 1)),
		`apps.spotify.type: "output" is not valid; [apps] holds only apps, and outputs go in [outputs.spotify]`)
}

// Warnings without a message of their own keep being logged by their text.
func TestCFG19_OtherWarningsHaveNoMessage(t *testing.T) {
	_, warnings := mustParseWarnings(t, minimal+"\n[apps.spare]\nmatch = [\"spare\"]\n")
	if len(warnings) != 1 || warnings[0].Message != "" || warnings[0].Attrs != nil {
		t.Errorf("warnings = %+v, want one text warning", warnings)
	}
}
