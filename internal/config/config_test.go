package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// minimal is the smallest valid configuration.
const minimal = `
[apps.spotify]
match = ["spotify"]

[layouts.default]
slider1 = "spotify"
`

func mustParse(t *testing.T, src string) (*Config, []string) {
	t.Helper()
	cfg, warnings, err := Parse("test.toml", []byte(src))
	if err != nil {
		t.Fatalf("Parse: unexpected error:\n%v", err)
	}
	return cfg, warnings
}

// problemsOf parses src, expects a *ValidationError and returns its problems.
func problemsOf(t *testing.T, src string) []string {
	t.Helper()
	_, _, err := Parse("test.toml", []byte(src))
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("Parse: want *ValidationError, got %v", err)
	}
	return ve.Problems
}

func requireProblem(t *testing.T, problems []string, want string) {
	t.Helper()
	for _, p := range problems {
		if strings.Contains(p, want) {
			return
		}
	}
	t.Errorf("no problem containing %q in:\n  %s", want, strings.Join(problems, "\n  "))
}

func TestExampleConfigIsValid(t *testing.T) {
	cfg, _, err := Load(filepath.Join("..", "..", "examples", "config.toml"))
	if err != nil {
		t.Fatalf("examples/config.toml must be valid:\n%v", err)
	}
	if len(cfg.Layout().Assignments) == 0 {
		t.Error("example layout has no assignments")
	}
	if cfg.Apps["mic"].Type != TypeInput {
		t.Errorf("example app mic: type = %q, want %q", cfg.Apps["mic"].Type, TypeInput)
	}
}

func TestDefaults(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/xdg/state")
	cfg, _ := mustParse(t, minimal)

	if cfg.Controller.Port != "nanoKONTROL2" {
		t.Errorf("controller.port = %q, want nanoKONTROL2", cfg.Controller.Port)
	}
	l := cfg.Log
	if l.Level != "warn" || l.Journald.Format != "text" || l.File.Format != "json" {
		t.Errorf("log defaults = %+v", l)
	}
	if len(l.Outputs) != 1 || l.Outputs[0] != OutputJournald {
		t.Errorf("log.outputs = %v, want [journald]", l.Outputs)
	}
	if l.File.Path != "/xdg/state/apptrol/apptrol.log" {
		t.Errorf("log.file.path = %q (LOG-06)", l.File.Path)
	}
	if l.File.MaxSize != 10<<20 || l.File.MaxFiles != 5 {
		t.Errorf("rotation = %d bytes, %d files", l.File.MaxSize, l.File.MaxFiles)
	}
	app := cfg.Apps["spotify"]
	if app.Name != "spotify" || app.Type != TypeApp {
		t.Errorf("app defaults: name %q, type %q", app.Name, app.Type)
	}
}

func TestCFG01_DefaultPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg/config")
	p, err := DefaultPath()
	if err != nil || p != "/xdg/config/apptrol/config.toml" {
		t.Errorf("DefaultPath() = %q, %v", p, err)
	}

	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)
	p, err = DefaultPath()
	if err != nil || p != filepath.Join(home, ".config", "apptrol", "config.toml") {
		t.Errorf("DefaultPath() without XDG_CONFIG_HOME = %q, %v", p, err)
	}

	// A relative XDG_CONFIG_HOME is invalid per the XDG spec and must be ignored.
	t.Setenv("XDG_CONFIG_HOME", "relative/dir")
	p, _ = DefaultPath()
	if !strings.HasPrefix(p, home) {
		t.Errorf("relative XDG_CONFIG_HOME was used: %q", p)
	}
}

func TestSTATE06_DefaultStateDir(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/xdg/state")
	if d, _ := DefaultStateDir(); d != "/xdg/state/apptrol" {
		t.Errorf("DefaultStateDir() = %q", d)
	}
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", home)
	if d, _ := DefaultStateDir(); d != filepath.Join(home, ".local", "state", "apptrol") {
		t.Errorf("DefaultStateDir() = %q", d)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, _, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Load(missing) error = %v, want ErrNotFound", err)
	}
}

func TestLoad_FromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(minimal), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(path)
	if err != nil || cfg.Apps["spotify"].ID != "spotify" {
		t.Errorf("Load = %+v, %v", cfg, err)
	}
}

func TestCFG02_AppsDefinedOnceAndReferencedByID(t *testing.T) {
	cfg, _ := mustParse(t, `
[apps.discord]
name  = "Discord"
match = ["discord", "vesktop"]

[apps.mic]
name  = "Microphone"
type  = "input"
match = ["GoXLR"]

[layouts.default]
slider3 = "discord"
slider8 = "mic"
`)
	d := cfg.Apps["discord"]
	if d.Name != "Discord" || len(d.Match) != 2 || d.Match[1] != "vesktop" {
		t.Errorf("discord = %+v", d)
	}
	if got, ok := cfg.Layout().Assignment(Control{Slider, 8}); !ok || got != "mic" {
		t.Errorf("slider8 -> %q, %v", got, ok)
	}
	if cfg.Apps["mic"].Type != TypeInput {
		t.Errorf("mic type = %q", cfg.Apps["mic"].Type)
	}
}

func TestCFG02_InvalidAppIDAndType(t *testing.T) {
	ps := problemsOf(t, `
[apps."bad id"]
match = ["x"]

[apps.x]
type  = "speaker"
match = ["x"]

[layouts.default]
slider1 = "x"
`)
	requireProblem(t, ps, `apps.bad id: id may only contain`)
	requireProblem(t, ps, `apps.x.type: "speaker" is not valid`)
}

func TestCFG03_DefaultLayoutRequired(t *testing.T) {
	requireProblem(t, problemsOf(t, `
[apps.a]
match = ["a"]
`), "layouts.default: missing")

	requireProblem(t, problemsOf(t, `
[apps.a]
match = ["a"]
[layouts.gaming]
slider1 = "a"
`), "layouts.default: missing")
}

func TestCFG03_OtherLayoutsAreIgnoredWithWarning(t *testing.T) {
	cfg, warnings := mustParse(t, minimal+`
[layouts.gaming]
knob1 = "spotify"
`)
	if len(cfg.Layout().Assignments) != 1 {
		t.Errorf("default layout assignments = %v", cfg.Layout().Assignments)
	}
	requireProblem(t, warnings, `layouts.gaming: only the "default" layout is used`)
}

func TestCFG04_MatchFragments(t *testing.T) {
	cfg, _ := mustParse(t, `
[apps.b]
match = [" firefox ", "vivaldi"]
[layouts.default]
knob2 = "b"
`)
	if got := cfg.Apps["b"].Match; len(got) != 2 || got[0] != "firefox" {
		t.Errorf("match = %q, want trimmed fragments", got)
	}
	requireProblem(t, problemsOf(t, `
[apps.b]
match = ["ok", "  "]
[layouts.default]
knob2 = "b"
`), "apps.b.match[1]: empty fragment")
	requireProblem(t, problemsOf(t, `
[apps.b]
name = "B"
[layouts.default]
knob2 = "b"
`), "apps.b.match: missing")
}

func TestCFG07_SyntaxErrorReportsPosition(t *testing.T) {
	ps := problemsOf(t, "[apps.a]\nmatch = [\"a\"\n")
	requireProblem(t, ps, "line ")
}

func TestCFG07_WrongValueTypeIsRejected(t *testing.T) {
	ps := problemsOf(t, `
[apps.a]
match = "not a list"
[layouts.default]
slider1 = "a"
`)
	if len(ps) != 1 {
		t.Errorf("want one problem, got %q", ps)
	}
	requireProblem(t, ps, "apps.a.match: expected a list")

	requireProblem(t, problemsOf(t, "[log.file]\nmax_files = \"five\"\n"+minimal),
		"log.file.max_files: expected a whole number, found text")
}

func TestCFG07_UnknownSettingsAreRejected(t *testing.T) {
	ps := problemsOf(t, minimal+`
[log]
verbosity = "debug"
`)
	requireProblem(t, ps, "log.verbosity: unknown setting")
}

func TestCFG07_AllProblemsReportedAtOnce(t *testing.T) {
	_, _, err := Parse("my.toml", []byte(`
[log]
level = "loud"
[apps.a]
match = []
[layouts.default]
slider9 = "a"
knob1 = "ghost"
`))
	var ve *ValidationError
	if !errors.As(err, &ve) || len(ve.Problems) < 4 {
		t.Fatalf("want at least 4 problems, got %v", err)
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "my.toml: ") || !strings.Contains(msg, "\n  - ") {
		t.Errorf("error message format:\n%s", msg)
	}
}

func TestCFG08_Validation(t *testing.T) {
	tests := []struct {
		name, layout, want string
	}{
		{"slider beyond 8", `slider9 = "a"`, "layouts.default.slider9: unknown control"},
		{"knob zero", `knob0 = "a"`, "layouts.default.knob0: unknown control"},
		{"unknown kind", `fader1 = "a"`, "layouts.default.fader1: unknown control"},
		{"undefined app", `slider1 = "ghost"`, `app "ghost" is not defined`},
		{"same app twice", "slider1 = \"a\"\nknob1 = \"a\"", `app "a" is already assigned to`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ps := problemsOf(t, "[apps.a]\nmatch = [\"a\"]\n[layouts.default]\n"+tt.layout+"\n")
			requireProblem(t, ps, tt.want)
		})
	}

	requireProblem(t, problemsOf(t, `
[apps.a]
match = []
[layouts.default]
slider1 = "a"
`), "apps.a.match: empty")
}

func TestAssignmentsAreSorted(t *testing.T) {
	cfg, _ := mustParse(t, `
[apps.a]
match = ["a"]
[apps.b]
match = ["b"]
[apps.c]
match = ["c"]
[layouts.default]
knob1 = "a"
slider8 = "b"
slider2 = "c"
`)
	var got []string
	for _, a := range cfg.Layout().Assignments {
		got = append(got, a.Control.String())
	}
	if strings.Join(got, ",") != "slider2,slider8,knob1" {
		t.Errorf("order = %v", got)
	}
	if _, ok := cfg.Layout().Assignment(Control{Knob, 5}); ok {
		t.Error("knob5 should be unassigned")
	}
}

func TestUnusedAppWarning(t *testing.T) {
	_, warnings := mustParse(t, minimal+`
[apps.spare]
match = ["spare"]
`)
	requireProblem(t, warnings, "apps.spare: not assigned to any control")
}

func TestLOG_Settings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg, _ := mustParse(t, minimal+`
[log]
level   = "debug"
outputs = ["file", "journald", "file"]
[log.journald]
format = "logfmt"
[log.file]
path      = "~/logs/apptrol.log"
format    = "text"
max_size  = "512KB"
max_files = 0
`)
	l := cfg.Log
	if l.Level != "debug" || l.Journald.Format != "logfmt" || l.File.Format != "text" {
		t.Errorf("log = %+v", l)
	}
	if strings.Join(l.Outputs, ",") != "file,journald" {
		t.Errorf("outputs = %v, want duplicates removed", l.Outputs)
	}
	if l.File.Path != filepath.Join(home, "logs", "apptrol.log") {
		t.Errorf("path = %q, want ~ expanded", l.File.Path)
	}
	if l.File.MaxSize != 512<<10 || l.File.MaxFiles != 0 {
		t.Errorf("rotation = %d, %d", l.File.MaxSize, l.File.MaxFiles)
	}
}

func TestLOG_InvalidSettings(t *testing.T) {
	tests := []struct{ log, want string }{
		{`level = "verbose"`, `log.level: "verbose" is not valid`},
		{`outputs = ["syslog"]`, `log.outputs: "syslog" is not valid`},
		{`outputs = []`, "log.outputs: empty"},
		{"[log.journald]\nformat = \"json\"", `log.journald.format: "json" is not valid`},
		{"[log.file]\nformat = \"xml\"", `log.file.format: "xml" is not valid`},
		{"[log.file]\npath = \"relative/app.log\"", "must be an absolute path"},
		{"[log.file]\nmax_size = \"lots\"", "log.file.max_size"},
		{"[log.file]\nmax_size = \"10\"", "too small"},
		{"[log.file]\nmax_size = \"99999GB\"", "too large"},
		{"[log.file]\nmax_files = -1", "log.file.max_files: -1 is out of range"},
		{"[controller]\nport = \"  \"", "controller.port: must not be empty"},
		{"[controller]\nat_login_screen = \"Release\"", `controller.at_login_screen: "Release" is not valid (use "keep" or "release")`}, // CFG-24
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			src := minimal + "\n[log]\n" + tt.log + "\n"
			if strings.HasPrefix(tt.log, "[controller]") {
				src = minimal + "\n" + tt.log + "\n"
			}
			requireProblem(t, problemsOf(t, src), tt.want)
		})
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{{"1KB", 1 << 10}, {"10mb", 10 << 20}, {" 2 GB ", 2 << 30}, {"4096", 4096}, {"2048B", 2048}}
	for _, tt := range tests {
		if got, err := parseSize(tt.in); err != nil || got != tt.want {
			t.Errorf("parseSize(%q) = %d, %v; want %d", tt.in, got, err, tt.want)
		}
	}
}

func TestControlString(t *testing.T) {
	if s := (Control{Knob, 3}).String(); s != "knob3" {
		t.Errorf("got %q", s)
	}
	if s := (Control{Slider, 1}).String(); s != "slider1" {
		t.Errorf("got %q", s)
	}
}

// FuzzParse checks that no input, however broken, makes Parse panic, and that
// Parse returns either a config or an error, never both or neither (QA-08).
func FuzzParse(f *testing.F) {
	seeds := []string{
		minimal,
		"",
		"[apps.a]\nmatch=[\"a\"]\n[layouts.default]\nslider1=\"a\"\nknob8=\"a\"",
		"[log]\nlevel=1\n",
		"[[apps]]\n",
		"[layouts.default]\nslider99999999999999999999=\"x\"",
		"[log.file]\nmax_size=\"9223372036854775807GB\"",
		"\x00\xff[",
	}
	if data, err := os.ReadFile(filepath.Join("..", "..", "examples", "config.toml")); err == nil {
		seeds = append(seeds, string(data))
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		cfg, _, err := Parse("fuzz.toml", []byte(src))
		if (cfg == nil) == (err == nil) {
			t.Fatalf("cfg=%v err=%v: exactly one must be set", cfg != nil, err)
		}
		if cfg != nil {
			if _, ok := cfg.Layouts[DefaultLayout]; !ok {
				t.Fatal("valid config without default layout")
			}
		}
	})
}

func TestCFG24_AtLoginScreen(t *testing.T) {
	for src, want := range map[string]bool{
		"": false, // default keep
		"[controller]\nat_login_screen = \"keep\"":    false,
		"[controller]\nat_login_screen = \"release\"": true,
	} {
		cfg, _, err := Parse("test.toml", []byte(minimal+"\n"+src+"\n"))
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if got := cfg.Setup().ReleaseAtLoginScreen; got != want {
			t.Errorf("%q: ReleaseAtLoginScreen = %v, want %v", src, got, want)
		}
	}
}
