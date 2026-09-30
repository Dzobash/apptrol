// Package state saves and restores what Apptrol remembers between runs: the
// last known position of each control, the user mutes (STATE-01) and the solo
// (STATE-04).
//
// The file is JSON, written atomically (STATE-02), at
// $XDG_STATE_HOME/apptrol/state.json (STATE-06).
//
//	{
//	  "version": 1,
//	  "layouts": {
//	    "default": {
//	      "slider1": { "position": 90, "solo": true },
//	      "slider8": { "position": 100, "muted": true }
//	    }
//	  }
//	}
//
// "solo" was added in 0.1.0-rc3 without a new version: older versions ignore it.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Dzobash/apptrol/internal/mixer"
)

// FileName is the name of the state file inside the state directory.
const FileName = "state.json"

// Layout is the layout whose state is saved. Phase 1 has only this one; the
// file format already keeps state per layout (STATE-01).
const Layout = "default"

// formatVersion is written to every file. Files with a higher version come
// from a newer Apptrol and are not read.
const formatVersion = 1

type fileJSON struct {
	Version int                               `json:"version"`
	Layouts map[string]map[string]controlJSON `json:"layouts"`
}

type controlJSON struct {
	Position *int `json:"position,omitempty"` // absent: position unknown
	Muted    bool `json:"muted,omitempty"`
	Solo     bool `json:"solo,omitempty"`
}

// Load reads the state file. A missing file returns an error that wraps
// fs.ErrNotExist (the first start). A file that cannot be read or decoded
// returns an error; the caller then starts with an empty state (STATE-05).
// Single entries that make no sense are skipped and reported as warnings.
func Load(path string) (mixer.State, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return emptyState(), nil, err
	}
	return Decode(data)
}

// Decode parses the contents of a state file; see Load.
func Decode(data []byte) (mixer.State, []string, error) {
	var f fileJSON
	if err := json.Unmarshal(data, &f); err != nil {
		return emptyState(), nil, fmt.Errorf("the state file is damaged: %w", err)
	}
	switch {
	case f.Version == 0:
		return emptyState(), nil, errors.New("the state file has no version; it was not written by Apptrol")
	case f.Version > formatVersion:
		return emptyState(), nil, fmt.Errorf("the state file has version %d; it was written by a newer Apptrol", f.Version)
	}

	st := emptyState()
	var warnings []string
	soloCount := 0
	for name, c := range f.Layouts[Layout] {
		ctl, ok := mixer.ParseControl(name)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("state: ignoring unknown control %q", name))
			continue
		}
		if c.Position != nil {
			if p := *c.Position; p < 0 || p > mixer.MaxValue {
				warnings = append(warnings, fmt.Sprintf("state: ignoring position %d of %s (must be 0–%d)", p, name, mixer.MaxValue))
			} else {
				st.Positions[ctl] = p
			}
		}
		if c.Muted {
			if ctl.Kind != mixer.Slider {
				warnings = append(warnings, fmt.Sprintf("state: ignoring mute of %s (only slider columns can be muted)", name))
			} else {
				st.Muted[ctl] = true
			}
		}
		if c.Solo {
			if ctl.Kind != mixer.Slider {
				warnings = append(warnings, fmt.Sprintf("state: ignoring solo of %s (only slider columns can be soloed)", name))
			} else {
				soloCount++
				if st.Solo == 0 || ctl.Column < st.Solo {
					st.Solo = ctl.Column // several: keep the lowest, as the map order is random
				}
			}
		}
	}
	if soloCount > 1 {
		warnings = append(warnings, fmt.Sprintf("state: several controls are soloed; keeping slider%d", st.Solo))
	}
	return st, warnings, nil
}

// Encode returns the file contents for st.
func Encode(st mixer.State) []byte {
	controls := map[string]controlJSON{}
	for c, p := range st.Positions {
		p := p
		e := controls[c.String()]
		e.Position = &p
		controls[c.String()] = e
	}
	for c, on := range st.Muted {
		if on {
			e := controls[c.String()]
			e.Muted = true
			controls[c.String()] = e
		}
	}
	if st.Solo != 0 {
		name := mixer.Control{Kind: mixer.Slider, Column: st.Solo}.String()
		e := controls[name]
		e.Solo = true
		controls[name] = e
	}
	f := fileJSON{Version: formatVersion, Layouts: map[string]map[string]controlJSON{Layout: controls}}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		// Only plain ints, bools and strings are encoded; this cannot fail.
		panic(err)
	}
	return append(data, '\n')
}

// Save writes st to path atomically (STATE-02): it writes a temporary file in
// the same directory, flushes it to disk and renames it over the old file, so
// a crash leaves either the old or the new file, never a half-written one.
// The directory is created if missing.
func Save(path string, st mixer.State) error {
	return writeAtomic(path, Encode(st))
}

func writeAtomic(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating the state directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".state-*.tmp") // created with mode 0600
	if err != nil {
		return fmt.Errorf("saving state: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}
	// Make the rename itself durable. Not every file system supports this;
	// the file is already in place, so a failure here is ignored.
	if d, derr := os.Open(dir); derr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// IsFirstStart reports whether err from Load means there is no state file yet,
// which is normal on the first start and needs no warning (STATE-05).
func IsFirstStart(err error) bool { return errors.Is(err, fs.ErrNotExist) }

func emptyState() mixer.State {
	return mixer.State{Positions: map[mixer.Control]int{}, Muted: map[mixer.Control]bool{}}
}
