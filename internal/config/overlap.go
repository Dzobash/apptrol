package config

import (
	"fmt"
	"strings"
)

// overlaps warns about apps of the active layout that can catch the same
// streams or devices (CFG-12). Such a stream goes to only one app: the one on
// the first control, in the order slider1–slider8, then knob1–knob8. That is
// fine when intended, but a surprise otherwise, because the other app's
// control then does nothing for it.
//
// Two fragments overlap when one contains the other: every name that contains
// the longer one also contains the shorter one.
func overlaps(cfg *Config) []Warning {
	var warnings []Warning
	as := cfg.Layout().Assignments // sorted: sliders, then knobs
	for i, first := range as {
		a := cfg.Apps[first.AppID]
		for _, second := range as[i+1:] {
			b := cfg.Apps[second.AppID]
			// First-control ownership applies within each target kind
			// (CFG-12, OUT-04, ADR 0034).
			if a.Type != b.Type {
				continue
			}
			if why := sharedFragment(a.Match, b.Match); why != "" {
				kind, what := "apps", "streams"
				if a.Type == TypeInput {
					kind, what = "inputs", "input devices"
				}
				if a.Type == TypeOutput {
					kind, what = "outputs", "output devices"
				}
				warnings = append(warnings, warnf(
					"%s %q (%s) and %q (%s) can match the same %s (%s); those go to %q on %s",
					kind, a.ID, first.Control, b.ID, second.Control, what, why, a.ID, first.Control))
			}
		}
	}
	return warnings
}

// sharedFragment explains the first overlap between two match lists, or
// returns "" if there is none.
func sharedFragment(a, b []string) string {
	for _, x := range a {
		lx := strings.ToLower(x)
		for _, y := range b {
			ly := strings.ToLower(y)
			switch {
			case lx == ly:
				return fmt.Sprintf("both match %q", x)
			case strings.Contains(ly, lx):
				return fmt.Sprintf("%q is part of %q", x, y)
			case strings.Contains(lx, ly):
				return fmt.Sprintf("%q is part of %q", y, x)
			}
		}
	}
	return ""
}
