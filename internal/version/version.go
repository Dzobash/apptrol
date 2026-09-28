// Package version holds build information set at link time.
//
// The values are injected with -ldflags, for example:
//
//	go build -ldflags "-X github.com/Dzobash/apptrol/internal/version.Version=v0.1.0"
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Set via -ldflags at build time. The defaults apply to plain `go build` / `go run`.
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// Info returns the build information, filling in the commit and date from the
// Go build metadata (VCS stamping) when they were not set via -ldflags.
func Info() (version, commit, date string) {
	version, commit, date = Version, Commit, Date
	if commit != "" && date != "" {
		return
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if commit == "" {
				commit = s.Value
			}
		case "vcs.time":
			if date == "" {
				date = s.Value
			}
		}
	}
	return
}

// String returns a one-line description, e.g.
// "apptrol v0.1.0 (commit 1a2b3c4, built 2026-10-01T12:00:00Z, go1.24.7 linux/amd64)".
func String() string {
	v, c, d := Info()
	if len(c) > 7 {
		c = c[:7]
	}
	if c == "" {
		c = "unknown"
	}
	if d == "" {
		d = "unknown"
	}
	return fmt.Sprintf("apptrol %s (commit %s, built %s, %s %s/%s)",
		v, c, d, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
