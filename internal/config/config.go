// Package config loads and validates Apptrol's TOML configuration file.
//
// The format is documented in docs/config.md. Loading never partially succeeds:
// Load returns either a fully validated *Config or an error listing every problem
// found, so a broken file can never replace a working configuration (CFG-07).
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultLayout is the layout used in Phase 1 (CFG-03).
const DefaultLayout = "default"

// Target types distinguish the three tables after loading into one map
// (CFG-18, CFG-20, ADR 0026).
const (
	TypeApp    = "app"    // playback streams of an application
	TypeInput  = "input"  // a capture device, such as a microphone
	TypeOutput = "output" // a playback device, such as speakers
)

// DeviceDefault selects the audio server's default output instead of matching
// a fixed device (CFG-20, ADR 0027 point 2).
const DeviceDefault = "default"

// Log outputs and formats (LOG-03, LOG-04).
const (
	OutputJournald = "journald"
	OutputFile     = "file"
)

// Config is a loaded and validated configuration.
type Config struct {
	Controller Controller
	Log        Log
	// Apps holds apps, inputs and outputs by unique id (CFG-18, ADR 0026).
	Apps map[string]App
	// Layouts by name. Phase 1 uses only DefaultLayout.
	Layouts map[string]Layout
	Media   Media
}

// Media configures the media keys (ADR 0018).
type Media struct {
	// Player is the id of the app the media keys are pinned to; "" = the
	// player that most recently started playing (CFG-15, MEDIA-06).
	Player string
}

// Controller identifies the MIDI controller.
type Controller struct {
	// Port is matched case-insensitively against the controller's ALSA card id (HW-05).
	Port string
	// AtLoginScreen says whether the controller is kept or let go while the
	// login screen is in front: AtLoginScreenKeep or AtLoginScreenRelease
	// (SVC-10, CFG-24, ADR 0029).
	AtLoginScreen string
}

// The values of [controller] at_login_screen (CFG-24).
const (
	AtLoginScreenKeep    = "keep"    // the default: sliders keep working at the login screen
	AtLoginScreenRelease = "release" // let go, as for another user's session
)

// Log configures logging (LOG-01 … LOG-08).
type Log struct {
	Level    string // debug | info | warn | error
	Outputs  []string
	Journald JournaldOutput
	File     FileOutput
}

// JournaldOutput configures the journald / terminal output.
type JournaldOutput struct {
	Format string // text | logfmt
}

// FileOutput configures the log file output.
type FileOutput struct {
	Path     string // absolute, with ~ expanded
	Format   string // text | json | logfmt
	MaxSize  int64  // bytes
	MaxFiles int
}

// App is a target: application streams, an input or an output. All three use
// the same layout references and volume limit (CFG-18, CFG-20, ADR 0026).
type App struct {
	ID    string
	Name  string   // display name
	Type  string   // TypeApp, TypeInput or TypeOutput
	Match []string // case-insensitive fragments (CFG-04, CFG-05, CFG-20; ADR 0026)
	// Device is DeviceDefault for a default-following output, otherwise empty.
	// A validated output has either Device or a nonempty Match list; apps and
	// inputs always use Match (CFG-20, ADR 0027 point 2).
	Device string
	// MaxVolume is the volume in percent at the top of the control, 1–150
	// (CTRL-03). Default 100.
	MaxVolume int
	// TalkOverVolume is, on an input, the volume in percent (0–100) apps go
	// down to during talk-over (CFG-16). Default DefaultTalkOverVolume.
	TalkOverVolume int
}

// Layout assigns apps to controls.
type Layout struct {
	Name        string
	Assignments []Assignment      // sorted: sliders 1–8, then knobs 1–8
	Buttons     map[string]Button // [layouts.<name>.buttons] by button name (CFG-13)
}

// Assignment binds one control to one app.
type Assignment struct {
	Control Control
	AppID   string
}

// ControlKind is a kind of physical control.
type ControlKind int

// Control kinds.
const (
	Slider ControlKind = iota
	Knob
)

// NumColumns is the number of slider/knob columns on the controller.
const NumColumns = 8

func (k ControlKind) String() string {
	if k == Knob {
		return "knob"
	}
	return "slider"
}

// Control identifies a slider or knob by kind and column (1–8).
type Control struct {
	Kind   ControlKind
	Column int
}

func (c Control) String() string { return fmt.Sprintf("%s%d", c.Kind, c.Column) }

// Assignment returns the app assigned to control c in the layout, if any.
func (l Layout) Assignment(c Control) (string, bool) {
	for _, a := range l.Assignments {
		if a.Control == c {
			return a.AppID, true
		}
	}
	return "", false
}

// Layout returns the layout used in Phase 1.
func (c *Config) Layout() Layout { return c.Layouts[DefaultLayout] }

// ValidationError lists every problem found in a configuration file.
type ValidationError struct {
	Path     string
	Problems []Problem
}

// Problem is one reason a configuration is invalid. Text is the line for
// people, as `apptrol check` prints it and the log keeps in exception.message.
// A problem someone may want to find in the log by its subject also carries
// Attrs, logged with it (ADR 0016), like a Warning does; e.g. an id defined in
// two tables (CFG-18, ADR 0026 point 7):
//
//	Text:  inputs.mic: the same id is defined in two tables ([apps] and [inputs])
//	Attrs: apptrol.app.id=mic
type Problem struct {
	Text  string
	Attrs []slog.Attr // attribute names from internal/logattr; most problems have none
}

func (p Problem) String() string { return p.Text }

// Texts returns the text of every problem, in order.
func (e *ValidationError) Texts() []string {
	texts := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		texts[i] = p.Text
	}
	return texts
}

func (e *ValidationError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d problem", e.Path, len(e.Problems))
	if len(e.Problems) != 1 {
		b.WriteString("s")
	}
	for _, p := range e.Problems {
		b.WriteString("\n  - ")
		b.WriteString(p.Text)
	}
	return b.String()
}

// ErrNotFound is returned (wrapped) when the configuration file does not exist.
var ErrNotFound = errors.New("configuration file not found")

// DefaultPath returns $XDG_CONFIG_HOME/apptrol/config.toml, falling back to
// ~/.config/apptrol/config.toml (CFG-01).
func DefaultPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" && filepath.IsAbs(dir) {
		return filepath.Join(dir, "apptrol", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding the home directory: %w", err)
	}
	return filepath.Join(home, ".config", "apptrol", "config.toml"), nil
}

// DefaultStateDir returns $XDG_STATE_HOME/apptrol, falling back to
// ~/.local/state/apptrol (STATE-06, LOG-06).
func DefaultStateDir() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" && filepath.IsAbs(dir) {
		return filepath.Join(dir, "apptrol"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding the home directory: %w", err)
	}
	return filepath.Join(home, ".local", "state", "apptrol"), nil
}

// Load reads and validates the configuration file at path. It returns the
// configuration and non-fatal warnings, or an error. Validation problems are
// returned as *ValidationError; a missing file wraps ErrNotFound.
func Load(path string) (*Config, []Warning, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, fmt.Errorf("%s: %w", path, ErrNotFound)
		}
		return nil, nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return Parse(path, data)
}

// Parse validates configuration data. path is only used in messages.
func Parse(path string, data []byte) (*Config, []Warning, error) {
	return parse(path, data)
}

// AppIDs returns all target ids, including inputs and outputs, sorted so
// diagnostics are stable (CFG-18, CFG-20, ADR 0026).
func (c *Config) AppIDs() []string {
	ids := make([]string, 0, len(c.Apps))
	for id := range c.Apps {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
