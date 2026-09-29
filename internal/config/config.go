// Package config loads and validates Apptrol's TOML configuration file.
//
// The format is documented in docs/config.md. Loading never partially succeeds:
// Load returns either a fully validated *Config or an error listing every problem
// found, so a broken file can never replace a working configuration (CFG-07).
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultLayout is the layout used in Phase 1 (CFG-03).
const DefaultLayout = "default"

// App types (CFG-02).
const (
	TypeApp   = "app"   // playback streams of an application
	TypeInput = "input" // a capture device, such as a microphone
)

// Log outputs and formats (LOG-03, LOG-04).
const (
	OutputJournald = "journald"
	OutputFile     = "file"
)

// Config is a loaded and validated configuration.
type Config struct {
	Controller Controller
	Log        Log
	// Apps by id.
	Apps map[string]App
	// Layouts by name. Phase 1 uses only DefaultLayout.
	Layouts map[string]Layout
}

// Controller identifies the MIDI controller.
type Controller struct {
	// Port is matched case-insensitively against the controller's ALSA card id (HW-05).
	Port string
}

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

// App is something a control acts on: the playback streams of an application,
// or a capture device.
type App struct {
	ID    string
	Name  string   // display name
	Type  string   // TypeApp or TypeInput
	Match []string // case-insensitive fragments (CFG-04, CFG-05)
}

// Layout assigns apps to controls.
type Layout struct {
	Name        string
	Assignments []Assignment // sorted: sliders 1–8, then knobs 1–8
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
	Problems []string
}

func (e *ValidationError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d problem", e.Path, len(e.Problems))
	if len(e.Problems) != 1 {
		b.WriteString("s")
	}
	for _, p := range e.Problems {
		b.WriteString("\n  - ")
		b.WriteString(p)
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
func Load(path string) (*Config, []string, error) {
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
func Parse(path string, data []byte) (*Config, []string, error) {
	return parse(path, data)
}

// AppIDs returns the ids of all apps, sorted.
func (c *Config) AppIDs() []string {
	ids := make([]string, 0, len(c.Apps))
	for id := range c.Apps {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
