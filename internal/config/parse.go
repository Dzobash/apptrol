package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// rawConfig mirrors the file layout. Pointers distinguish "missing" from "empty".
type rawConfig struct {
	Controller struct {
		Port *string `toml:"port"`
	} `toml:"controller"`
	Log struct {
		Level    *string   `toml:"level"`
		Outputs  *[]string `toml:"outputs"`
		Journald struct {
			Format *string `toml:"format"`
		} `toml:"journald"`
		File struct {
			Path     *string `toml:"path"`
			Format   *string `toml:"format"`
			MaxSize  *string `toml:"max_size"`
			MaxFiles *int    `toml:"max_files"`
		} `toml:"file"`
	} `toml:"log"`
	Apps map[string]rawApp `toml:"apps"`
	// A layout holds "control = app" entries and a buttons table; each
	// value is decoded once its type is known (see decodeLayouts).
	Layouts map[string]map[string]toml.Primitive `toml:"layouts"`
	Media   struct {
		Player *string `toml:"player"`
	} `toml:"media"`
}

// rawLayout is one [layouts.<name>] after decodeLayouts.
type rawLayout struct {
	controls map[string]string
	buttons  map[string]rawButton
}

type rawApp struct {
	Name      *string   `toml:"name"`
	Type      *string   `toml:"type"`
	Match     *[]string `toml:"match"`
	MaxVolume *int      `toml:"max_volume"`
	TalkOver  *int      `toml:"talk_over_volume"`
}

// Defaults (docs/config.md).
const (
	defaultPort        = "nanoKONTROL2"
	defaultLevel       = "warn" // quiet in daily use; info shows what Apptrol does (LOG-02)
	defaultJournaldFmt = "text"
	defaultFileFmt     = "json"
	defaultMaxSize     = 10 << 20
	defaultMaxFiles    = 5
	defaultMaxVolume   = 100
)

// MaxVolumeLimit is the highest allowed max_volume in percent. Above it, the
// software amplification distorts badly; desktop mixers stop there too.
const MaxVolumeLimit = 150

var (
	idPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	controlPattern = regexp.MustCompile(`^(slider|knob)([1-9][0-9]*)$`)
	sizePattern    = regexp.MustCompile(`^\s*(\d+)\s*(B|KB|MB|GB)?\s*$`)

	validLevels       = []string{"debug", "info", "warn", "error"}
	validOutputs      = []string{OutputJournald, OutputFile}
	validJournaldFmts = []string{"text", "logfmt"}
	validFileFmts     = []string{"text", "json", "logfmt"}
	validTypes        = []string{TypeApp, TypeInput}
)

// problems collects validation messages.
type problems []string

func (p *problems) add(format string, args ...any) { *p = append(*p, fmt.Sprintf(format, args...)) }

func parse(path string, data []byte) (*Config, []string, error) {
	var raw rawConfig
	md, err := toml.Decode(string(data), &raw)
	if err != nil {
		return nil, nil, &ValidationError{Path: path, Problems: []string{tomlError(err)}}
	}

	var errs problems
	var warnings []string
	layouts := decodeLayouts(md, raw.Layouts, &errs)

	// Unknown keys are almost always typos; reject them (CFG-07).
	for _, k := range md.Undecoded() {
		errs.add("%s: unknown setting", k.String())
	}

	cfg := &Config{
		Apps:    map[string]App{},
		Layouts: map[string]Layout{},
	}

	// [controller]
	cfg.Controller.Port = defaultPort
	if raw.Controller.Port != nil {
		cfg.Controller.Port = strings.TrimSpace(*raw.Controller.Port)
		if cfg.Controller.Port == "" {
			errs.add("controller.port: must not be empty")
		}
	}

	// [log]
	parseLog(&raw, &cfg.Log, &errs)

	// [apps.<id>]
	for id, ra := range raw.Apps {
		if !idPattern.MatchString(id) {
			errs.add("apps.%s: id may only contain letters, digits, - and _ (up to 64 characters)", id)
			continue
		}
		app := App{ID: id, Name: id, Type: TypeApp, MaxVolume: defaultMaxVolume}
		if ra.Name != nil && strings.TrimSpace(*ra.Name) != "" {
			app.Name = strings.TrimSpace(*ra.Name)
		}
		if ra.Type != nil {
			app.Type = *ra.Type
			if !contains(validTypes, app.Type) {
				errs.add("apps.%s.type: %q is not valid (use %s)", id, app.Type, orList(validTypes))
			}
		}
		if ra.Match == nil {
			errs.add("apps.%s.match: missing; list at least one name fragment", id)
		} else {
			for i, m := range *ra.Match {
				m = strings.TrimSpace(m)
				if m == "" {
					errs.add("apps.%s.match[%d]: empty fragment", id, i)
					continue
				}
				app.Match = append(app.Match, m)
			}
			if len(*ra.Match) == 0 {
				errs.add("apps.%s.match: empty; list at least one name fragment", id)
			}
		}
		if ra.MaxVolume != nil {
			app.MaxVolume = *ra.MaxVolume
			if app.MaxVolume < 1 || app.MaxVolume > MaxVolumeLimit {
				errs.add("apps.%s.max_volume: %d is out of range (1–%d, in percent)", id, app.MaxVolume, MaxVolumeLimit)
			}
		}
		if ra.TalkOver != nil {
			app.TalkOverVolume = *ra.TalkOver
			switch {
			case app.Type != TypeInput:
				errs.add("apps.%s.talk_over_volume: only an input (type = %q) has it", id, TypeInput)
			case app.TalkOverVolume < 0 || app.TalkOverVolume > 100:
				errs.add("apps.%s.talk_over_volume: %d is out of range (0–100, in percent)", id, app.TalkOverVolume)
			}
		} else if app.Type == TypeInput {
			app.TalkOverVolume = DefaultTalkOverVolume
		}
		cfg.Apps[id] = app
	}

	// [media]
	if p := raw.Media.Player; p != nil {
		cfg.Media.Player = *p
		switch app, ok := cfg.Apps[*p]; {
		case !ok:
			errs.add("media.player: app %q is not defined in [apps]", *p)
		case app.Type != TypeApp:
			errs.add("media.player: %q is an input; name an app that plays media", *p)
		}
	}

	// [layouts.<name>]
	if len(raw.Layouts) == 0 {
		errs.add("layouts.%s: missing; assign apps to controls there", DefaultLayout)
	}
	names := make([]string, 0, len(layouts))
	for name := range layouts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		layout := Layout{Name: name}
		usedBy := map[string]string{} // app id -> control name
		controls := make([]string, 0, len(layouts[name].controls))
		for c := range layouts[name].controls {
			controls = append(controls, c)
		}
		sort.Strings(controls)
		for _, key := range controls {
			appID := layouts[name].controls[key]
			ctl, validControl := parseControl(key)
			if !validControl {
				errs.add("layouts.%s.%s: unknown control (use slider1–slider%d or knob1–knob%d)", name, key, NumColumns, NumColumns)
			}
			_, validApp := cfg.Apps[appID]
			if _, defined := raw.Apps[appID]; !defined {
				errs.add("layouts.%s.%s: app %q is not defined in [apps]", name, key, appID)
			}
			if !validControl || !validApp {
				continue
			}
			if prev, dup := usedBy[appID]; dup {
				errs.add("layouts.%s.%s: app %q is already assigned to %s (each app may be on one control per layout)", name, key, appID, prev)
				continue
			}
			usedBy[appID] = key
			layout.Assignments = append(layout.Assignments, Assignment{Control: ctl, AppID: appID})
		}
		sortAssignments(layout.Assignments)
		layout.Buttons = parseButtons(name, layouts[name].buttons, layout, cfg.Apps, &errs)
		cfg.Layouts[name] = layout
		if name != DefaultLayout {
			warnings = append(warnings, fmt.Sprintf("layouts.%s: only the %q layout is used in this version; %q is ignored", name, DefaultLayout, name))
		}
	}
	if _, ok := raw.Layouts[DefaultLayout]; !ok && len(raw.Layouts) > 0 {
		errs.add("layouts.%s: missing; this version uses the layout named %q", DefaultLayout, DefaultLayout)
	}

	// Apps defined but not used anywhere: harmless, but worth knowing.
	for _, id := range cfg.AppIDs() {
		used := false
		for _, l := range cfg.Layouts {
			for _, a := range l.Assignments {
				if a.AppID == id {
					used = true
				}
			}
		}
		if !used {
			warnings = append(warnings, fmt.Sprintf("apps.%s: not assigned to any control", id))
		}
	}

	if len(errs) > 0 {
		sort.Strings(errs)
		return nil, nil, &ValidationError{Path: path, Problems: errs}
	}
	warnings = append(warnings, overlaps(cfg)...)
	for _, at := range cfg.WhenLockedLaunchers() {
		warnings = append(warnings, at+": when_locked = true: this button starts its app also while the screen is locked, for anyone at the controller")
	}
	return cfg, warnings, nil
}

// decodeLayouts decodes each [layouts.<name>]: "control = app" entries as
// text, and the buttons table (CFG-13). It runs before the check for unknown
// keys, which only sees what was decoded.
func decodeLayouts(md toml.MetaData, raw map[string]map[string]toml.Primitive, errs *problems) map[string]rawLayout {
	out := map[string]rawLayout{}
	for name, entries := range raw {
		l := rawLayout{controls: map[string]string{}}
		for key, prim := range entries {
			if key == "buttons" {
				if err := md.PrimitiveDecode(prim, &l.buttons); err != nil {
					errs.add("%s", valueError(err))
				}
				continue
			}
			var appID string
			if err := md.PrimitiveDecode(prim, &appID); err != nil {
				errs.add("layouts.%s.%s: expected an app id in quotes, e.g. %s = \"spotify\"", name, key, key)
				continue
			}
			l.controls[key] = appID
		}
		out[name] = l
	}
	return out
}

var mismatchPattern = regexp.MustCompile(`line (\d+) \(last key "([^"]+)"\): type mismatch for [^:]+: expected (\w+) but found (\w+)`)

// valueError describes a value of the wrong type inside a button table, with
// its line and key.
func valueError(err error) string {
	if m := mismatchPattern.FindStringSubmatch(err.Error()); m != nil {
		return fmt.Sprintf("line %s: %s: expected %s, found %s", m[1], m[2], describeGoType(m[3]), describeTOMLType(m[4]))
	}
	return tomlError(err)
}

func parseLog(raw *rawConfig, l *Log, errs *problems) {
	l.Level = defaultLevel
	if raw.Log.Level != nil {
		l.Level = *raw.Log.Level
		if !contains(validLevels, l.Level) {
			errs.add("log.level: %q is not valid (use %s)", l.Level, orList(validLevels))
		}
	}

	l.Outputs = []string{OutputJournald}
	if raw.Log.Outputs != nil {
		l.Outputs = nil
		seen := map[string]bool{}
		for _, o := range *raw.Log.Outputs {
			if !contains(validOutputs, o) {
				errs.add("log.outputs: %q is not valid (use %s)", o, orList(validOutputs))
				continue
			}
			if !seen[o] {
				seen[o] = true
				l.Outputs = append(l.Outputs, o)
			}
		}
		if len(*raw.Log.Outputs) == 0 {
			errs.add("log.outputs: empty; use %q, %q or both", OutputJournald, OutputFile)
		}
	}

	l.Journald.Format = defaultJournaldFmt
	if f := raw.Log.Journald.Format; f != nil {
		l.Journald.Format = *f
		if !contains(validJournaldFmts, *f) {
			errs.add("log.journald.format: %q is not valid (use %s)", *f, orList(validJournaldFmts))
		}
	}

	l.File.Format = defaultFileFmt
	if f := raw.Log.File.Format; f != nil {
		l.File.Format = *f
		if !contains(validFileFmts, *f) {
			errs.add("log.file.format: %q is not valid (use %s)", *f, orList(validFileFmts))
		}
	}

	if p := raw.Log.File.Path; p != nil && strings.TrimSpace(*p) != "" {
		expanded, err := expandHome(strings.TrimSpace(*p))
		switch {
		case err != nil:
			errs.add("log.file.path: %v", err)
		case !filepath.IsAbs(expanded):
			errs.add("log.file.path: %q must be an absolute path or start with ~/", *p)
		default:
			l.File.Path = filepath.Clean(expanded)
		}
	} else if dir, err := DefaultStateDir(); err == nil {
		l.File.Path = filepath.Join(dir, "apptrol.log")
	}

	l.File.MaxSize = defaultMaxSize
	if s := raw.Log.File.MaxSize; s != nil {
		n, err := parseSize(*s)
		if err != nil {
			errs.add("log.file.max_size: %v", err)
		} else {
			l.File.MaxSize = n
		}
	}

	l.File.MaxFiles = defaultMaxFiles
	if n := raw.Log.File.MaxFiles; n != nil {
		l.File.MaxFiles = *n
		if *n < 0 || *n > 100 {
			errs.add("log.file.max_files: %d is out of range (0–100)", *n)
		}
	}
}

// parseControl turns "slider3" into Control{Slider, 3}.
func parseControl(s string) (Control, bool) {
	m := controlPattern.FindStringSubmatch(s)
	if m == nil {
		return Control{}, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil || n < 1 || n > NumColumns {
		return Control{}, false
	}
	kind := Slider
	if m[1] == "knob" {
		kind = Knob
	}
	return Control{Kind: kind, Column: n}, true
}

func sortAssignments(as []Assignment) {
	sort.Slice(as, func(i, j int) bool {
		if as[i].Control.Kind != as[j].Control.Kind {
			return as[i].Control.Kind < as[j].Control.Kind
		}
		return as[i].Control.Column < as[j].Control.Column
	})
}

// parseSize parses "10MB", "512KB", "1GB" or a plain number of bytes (1 KB = 1024 B).
func parseSize(s string) (int64, error) {
	m := sizePattern.FindStringSubmatch(strings.ToUpper(s))
	if m == nil {
		return 0, fmt.Errorf("%q is not a size (examples: 512KB, 10MB, 1GB)", s)
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is too large", s)
	}
	mult := map[string]int64{"": 1, "B": 1, "KB": 1 << 10, "MB": 1 << 20, "GB": 1 << 30}[m[2]]
	if n > (1<<40)/mult {
		return 0, fmt.Errorf("%q is too large (maximum 1024GB)", s)
	}
	size := n * mult
	if size < 1<<10 {
		return 0, fmt.Errorf("%q is too small (minimum 1KB)", s)
	}
	return size, nil
}

func expandHome(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot expand ~: %w", err)
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~")), nil
}

var typeErrorPattern = regexp.MustCompile(`line (\d+) \(last key "([^"]+)"\): incompatible types: TOML value has type (\w+); destination has type (\w+)`)

// tomlError turns a TOML syntax or type error into a one-line message with its position.
func tomlError(err error) string {
	var pe toml.ParseError
	if errors.As(err, &pe) {
		return fmt.Sprintf("line %d, column %d: %s", pe.Position.Line, pe.Position.Col, pe.Message)
	}
	if m := typeErrorPattern.FindStringSubmatch(err.Error()); m != nil {
		return fmt.Sprintf("line %s: %s: expected %s, found %s", m[1], m[2], describeGoType(m[4]), describeTOMLType(m[3]))
	}
	return strings.TrimPrefix(err.Error(), "toml: ")
}

func describeGoType(t string) string {
	switch t {
	case "slice":
		return `a list, like ["a", "b"]`
	case "string":
		return "text in quotes"
	case "int", "int64", "integer":
		return "a whole number"
	case "struct", "map":
		return "a table ([section])"
	case "table":
		return `a table in braces, e.g. { mode = "mute" }`
	}
	return t
}

func describeTOMLType(t string) string {
	switch t {
	case "string":
		return "text"
	case "integer", "int64":
		return "a number"
	case "array", "slice":
		return "a list"
	case "table", "map":
		return "a table"
	}
	return t
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func orList(items []string) string {
	q := make([]string, len(items))
	for i, it := range items {
		q[i] = strconv.Quote(it)
	}
	if len(q) == 1 {
		return q[0]
	}
	return strings.Join(q[:len(q)-1], ", ") + " or " + q[len(q)-1]
}
