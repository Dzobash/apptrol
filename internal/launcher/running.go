package launcher

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	sd "github.com/coreos/go-systemd/v22/dbus"
	"github.com/coreos/go-systemd/v22/unit"
)

// Whether an app already runs, for if_running = "skip" (LAUNCH-06,
// LAUNCH-07, ADR 0019 point 6 and its notes). Checked when the button is
// pressed, in two steps:
//
//  1. the systemd user manager's active units with the app's ID in their
//     name: KDE's app-<id>@….service and app-<id>-….scope, Flatpak's
//     app-flatpak-<id>-….scope, Snap's snap.<snap>.<app>-….scope;
//  2. the user's running processes, by the program name of Exec or command.
//
// Tested on the reference system (ADR 0019, note): a Steam game shortcut
// runs `steam steam://rungameid/<n>`, and Steam can live in the game's unit
// after the game has ended, so neither step works for it; Steam itself never
// starts a running game twice, so Steam games are passed on unchecked.
// Programs that only start another one (flatpak, a shell, …) say nothing
// about which app runs, so step 2 is skipped for them.

// Ways an app was found running (apptrol.launcher.running_found_by).
const (
	foundByUnit    = "unit"
	foundByProcess = "process"
)

// wrappers start another program; their name does not identify the app.
var wrappers = map[string]bool{
	"flatpak": true, "env": true, "sh": true, "bash": true, "dash": true, "zsh": true, "fish": true,
	"java": true, "xdg-open": true, "gtk-launch": true, "gio": true, "kioclient": true, "kioclient5": true,
	"kde-open": true, "kde-open5": true, "snap": true, "nohup": true, "pkexec": true,
}

func isWrapper(program string) bool {
	return wrappers[program] || strings.HasPrefix(program, "python") || strings.HasPrefix(program, "perl")
}

// isSteamURL reports whether the command hands a steam:// link to Steam,
// as Steam's game shortcuts do.
func isSteamURL(argv []string) bool {
	if len(argv) < 2 || filepath.Base(argv[0]) != "steam" {
		return false
	}
	for _, a := range argv[1:] {
		if strings.HasPrefix(a, "steam://") {
			return true
		}
	}
	return false
}

// unitPatterns are the unit names an app with this ID runs in. KDE writes
// the ID as systemd-escape does (a "-" becomes "\x2d"); Flatpak and Snap use
// it as it is, so both spellings are searched.
func unitPatterns(id string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, name := range []string{globEscape(unit.UnitNameEscape(id)), globEscape(id)} {
		add("app-" + name + "@*.service")   // KDE, Apptrol without a launcher prefix
		add("app-*-" + name + "@*.service") // with a launcher prefix: app-apptrol-…
		add("app-" + name + "-*.scope")
		add("app-*-" + name + "-*.scope") // app-flatpak-<id>-….scope
	}
	if snap, app, ok := strings.Cut(id, "_"); ok && snap != "" && app != "" {
		add("snap." + globEscape(snap) + "." + globEscape(app) + "-*.scope")
	}
	return out
}

// globEscape protects the characters systemd's unit patterns treat as
// special, so an escaped "\x2d" is matched as it is written.
func globEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`).Replace(s)
}

// runningProcess returns the name of a program of the current user that
// runs under one of the names, or "".
func runningProcess(procDir, program string) string {
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return ""
	}
	uid, self := uint32(os.Getuid()), os.Getpid()
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		dir := filepath.Join(procDir, e.Name())
		info, err := os.Stat(dir)
		if err != nil {
			continue
		}
		if st, ok := info.Sys().(*syscall.Stat_t); !ok || st.Uid != uid {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join(dir, "cmdline"))
		if err != nil || len(cmdline) == 0 {
			continue
		}
		argv0, _, _ := strings.Cut(string(cmdline), "\x00")
		if filepath.Base(argv0) == program {
			return program
		}
	}
	return ""
}

// runningCheck is the outcome of checking whether an app runs.
type runningCheck struct {
	running bool
	foundBy string // foundByUnit or foundByProcess
	what    string // the unit or the program found
	checked string // what was looked at, for the log
	steam   bool   // a Steam game: not checked, Steam decides
}

// checkRunning looks for an app with the desktop ID (or a command's unit
// name) and program.
func (s *Starter) checkRunning(ctx context.Context, id string, argv []string) runningCheck {
	if isSteamURL(argv) {
		return runningCheck{steam: true}
	}
	patterns := unitPatterns(id)
	c := runningCheck{checked: "units " + strings.Join(patterns, ", ")}
	sys, err := s.conn(ctx)
	var names []sd.UnitStatus
	if err == nil {
		names, err = sys.ListUnitsByPatternsContext(ctx, []string{"active"}, patterns)
	}
	switch {
	case err != nil:
		// Not knowing is not "not running": say so, and connect again next time.
		s.dropConn()
		c.checked += " (could not ask systemd: " + err.Error() + ")"
	case len(names) > 0:
		return runningCheck{running: true, foundBy: foundByUnit, what: names[0].Name}
	}
	if len(argv) == 0 {
		return c // started over D-Bus: no program to look for
	}
	program := filepath.Base(argv[0])
	if isWrapper(program) {
		c.checked += "; no process check: " + program + " only starts another program"
		return c
	}
	c.checked += "; processes named " + program
	if p := runningProcess(s.procDir, program); p != "" {
		return runningCheck{running: true, foundBy: foundByProcess, what: p}
	}
	return c
}
