package config

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Blocked commands (LAUNCH-12, ADR 0019 note of 2026-10-02): launcher
// commands that are refused because they are irreversible or catastrophic,
// the core of what security guides and AI coding agents' guardrails block.
// It is a safety net against copy-paste accidents, not security: a command
// is a list of arguments run without a shell, so it is checked reliably, but
// text in "sh -c" can be disguised and is checked on a best-effort basis.
// docs/config.md lists the groups under "Blocked commands".

// blockedSuffix points from every refusal to the documentation.
const blockedSuffix = `; see "Blocked commands" in docs/config.md`

var (
	separators   = regexp.MustCompile("[;|&\n`]+|\\$\\(")
	diskRedirect = regexp.MustCompile(`>\s*/dev/(sd|nvme|hd|vd|mmcblk|xvd)`)
	// A function that pipes itself into itself in the background: f(){ f|f& }.
	// Go's regular expressions have no back-references, so isForkBomb
	// compares the three names.
	forkBomb    = regexp.MustCompile(`(?:^|[\s;&|])([A-Za-z_:][A-Za-z0-9_:]*)\s*\(\s*\)\s*\{\s*([A-Za-z_:][A-Za-z0-9_:]*)\s*\|\s*([A-Za-z_:][A-Za-z0-9_:]*)\s*&`)
	downloadRun = regexp.MustCompile(`\b(curl|wget)\b[^|;&]*\|\s*(sudo\s+|doas\s+)?(sh|bash|dash|zsh|ksh|fish|python3?|perl)\b`)
	everything  = map[string]bool{"/": true, "/*": true, "~": true, "~/": true, "~/*": true, "$HOME": true, "${HOME}": true,
		"$HOME/": true, "$HOME/*": true, "/home": true, "/home/": true, "/home/*": true}
	terminalRoot = map[string]bool{"sudo": true, "su": true, "doas": true}
	// wrappers run the program that follows them.
	wrappers = map[string]bool{"env": true, "nohup": true, "nice": true, "exec": true, "command": true, "time": true,
		"setsid": true, "pkexec": true, "run0": true, "kdesu": true, "lxqt-sudo": true, "xargs": true, "systemd-run": true}
)

// isAssignment reports whether a word sets a variable, e.g. LANG=C.
func isAssignment(w string) bool {
	name, _, ok := strings.Cut(w, "=")
	return ok && name != "" && !strings.ContainsAny(name, "/-.")
}

// blockedCommand returns why a launcher command is refused, or "".
func blockedCommand(args []string) string {
	text := strings.Join(args, " ")
	switch {
	case isForkBomb(text):
		return "blocked: a fork bomb freezes the computer" + blockedSuffix
	case downloadRun.MatchString(text):
		return `blocked: runs code downloaded from the internet ("curl … | sh")` + blockedSuffix
	case diskRedirect.MatchString(text):
		return "blocked: writing to a disk device destroys its data" + blockedSuffix
	}
	// The arguments, and the words of any shell text in them, in segments
	// split at ; | & ` and $( so "a; rm -rf /" is seen as two commands.
	// Only a word in program position counts, so a message that merely
	// contains "su" is not refused.
	for _, segment := range separators.Split(text, -1) {
		words := strings.Fields(segment)
		program := true // the next word is a program
		for i, w := range words {
			name := filepath.Base(w)
			rest := words[i+1:]
			isProgram := program
			// After a wrapper, "-c" (sh -c, bash -c) or an assignment
			// (LANG=C rm …), the next word is a program again.
			program = wrappers[name] || w == "-c" || (isProgram && isAssignment(w))
			if !isProgram {
				continue
			}
			switch {
			case terminalRoot[name]:
				return `blocked: "` + name + `" asks for the password in a terminal, which a launcher does not have; use "pkexec" or the app's desktop ID instead` + blockedSuffix
			case name == "rm" && hasFlags(rest, "r", "f") && hasTarget(rest):
				return `blocked: deletes all your files ("rm -rf /" or "rm -rf ~")` + blockedSuffix
			case strings.HasPrefix(name, "mkfs") || name == "wipefs":
				return `blocked: "` + name + `" wipes a disk` + blockedSuffix
			case name == "dd" && hasPrefixed(rest, "of=/dev/"):
				return `blocked: "dd" writing to a device destroys its data` + blockedSuffix
			case (name == "chmod" || name == "chown") && hasFlags(rest, "R") && hasTarget(rest):
				return `blocked: "` + name + ` -R" on everything breaks the system's security` + blockedSuffix
			}
		}
	}
	return ""
}

func isForkBomb(text string) bool {
	for _, m := range forkBomb.FindAllStringSubmatch(text, -1) {
		if m[1] == m[2] && m[2] == m[3] {
			return true
		}
	}
	return false
}

// hasFlags reports whether the arguments set every one of the short flags,
// combined ("-rf") or apart ("-r -f"), or by their long names.
func hasFlags(args []string, flags ...string) bool {
	long := map[string]string{"r": "--recursive", "R": "--recursive", "f": "--force"}
	for _, f := range flags {
		found := false
		for _, a := range args {
			if a == long[f] || (strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, f)) ||
				(f == "r" && strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "R")) {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// hasTarget reports whether an argument is the root, the home folder or
// everything in them.
func hasTarget(args []string) bool {
	for _, a := range args {
		if everything[strings.Trim(a, `"'`)] {
			return true
		}
	}
	return false
}

func hasPrefixed(args []string, prefix string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}
