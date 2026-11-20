package config

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Blocked commands (LAUNCH-12, ADR 0022): launcher
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
	// wrapperSpecs lists commands that execute a wrapped program and the flags
	// they accept before it (LAUNCH-12, ADR 0022). Only flags guaranteed to take
	// zero separate words go in noArg, and only flags with a required argument go
	// in withArg; unknown or optional-argument flags without "=" are checked
	// conservatively as taking either zero or one word.
	wrapperSpecs = map[string]wrapperSpec{
		"env": {
			noArg: setOf(
				"-i", "--ignore-environment",
				"-0", "--null",
				"-v", "--debug",
				"-S", "--split-string",
				"--list-signal-handling",
				"--block-signal", "--default-signal", "--ignore-signal",
			),
			withArg:     setOf("-u", "--unset", "-C", "--chdir", "-a", "--argv0"),
			splitString: true,
		},
		"nohup": {
			noArg: setOf("--help", "--version"),
		},
		"nice": {
			withArg:     setOf("-n", "--adjustment"),
			numericFlag: true,
		},
		"exec": {
			noArg:   setOf("-c", "-l"),
			withArg: setOf("-a"),
		},
		"command": {
			noArg: setOf("-p", "-v", "-V"),
		},
		"time": {
			noArg:   setOf("-p", "--portability", "-v", "--verbose", "-a", "--append", "-q", "--quiet"),
			withArg: setOf("-f", "--format", "-o", "--output"),
		},
		"setsid": {
			noArg: setOf("-c", "--ctty", "-f", "--fork", "-w", "--wait"),
		},
		"pkexec": {
			noArg:   setOf("--disable-internal-agent", "--keep-cwd"),
			withArg: setOf("--user"),
		},
		"run0": {
			noArg: setOf("--no-ask-password", "--pty", "--pipe", "--slice-inherit", "--empower", "--via-shell", "--lightweight"),
			withArg: setOf(
				"-u", "--user", "-g", "--group", "-p", "--property", "-D",
				"--unit", "--description", "--slice", "--setenv",
				"--chdir", "--working-directory", "--machine",
				"--nice", "--umask", "--shell-prompt-prefix", "--area",
			),
		},
		"kdesu": {
			noArg:       setOf("-d", "-n", "-s", "-t", "--noignorebutton"),
			withArg:     setOf("-u", "-i", "-p", "-f", "--attach"),
			commandFlag: true,
		},
		"lxqt-sudo": {
			noArg: setOf("-s", "-d", "--sudo", "--su", "--doas"),
		},
		"xargs": {
			noArg: setOf(
				"-0", "--null", "-o", "--open-tty", "-p", "--interactive",
				"-r", "--no-run-if-empty", "-t", "--verbose", "-x", "--exit",
			),
			withArg: setOf(
				"-a", "--arg-file", "-d", "--delimiter", "-E", "-I", "-L",
				"-n", "--max-args", "-P", "--max-procs", "-R", "--max-replace-args",
				"-s", "--max-chars", "--process-slot-var",
			),
		},
		"systemd-run": {
			noArg: setOf(
				"-t", "-P", "-G", "-q", "-d", "-S", "-r",
				"--user", "--system", "--scope", "--wait", "--collect",
				"--pty", "--pipe", "--remain-after-exit", "--send-sighup",
				"--quiet", "--no-block", "--no-ask-password", "--same-dir",
				"--shell", "--slice-inherit",
			),
			withArg: setOf(
				"-u", "--unit", "-p", "--property", "-E", "--setenv",
				"-H", "--host", "-M", "--machine",
				"--description", "--slice", "--uid", "--gid", "--nice",
				"--working-directory", "--root-directory", "--service-type",
				"--on-active", "--on-boot", "--on-startup",
				"--on-unit-active", "--on-unit-inactive", "--on-calendar",
				"--timer-property", "--path-property", "--socket-property",
			),
		},
	}
)

// wrapperSpec describes the flags a wrapper command understands before the
// wrapped program (LAUNCH-12, ADR 0022).
type wrapperSpec struct {
	noArg       map[string]bool // flags that never consume the next word
	withArg     map[string]bool // flags that always consume the next word
	splitString bool            // env: lone "-" is -i; -S / --split-string holds command words
	numericFlag bool            // nice: obsolete -<digits> priority flag
	commandFlag bool            // kdesu: -c introduces a command string
}

// flagArity classifies how many extra words a wrapper option consumes
// (LAUNCH-12, ADR 0022).
type flagArity int

const (
	flagNoArg   flagArity = iota // option consumes no following word
	flagWithArg                  // option consumes the next word as its value
	flagUnknown                  // unknown option: may consume zero or one word
)

// scanState tracks all possible parser positions at the current word so an
// unknown wrapper flag can be explored as both zero-arg and one-arg without
// letting a blocked program slip past (LAUNCH-12, ADR 0022).
type scanState struct {
	expectProgram bool            // current word is in program position
	inArgs        bool            // already past a non-wrapper program
	wrapperOpt    map[string]bool // active wrappers currently reading options
	wrapperArg    map[string]bool // active wrappers expecting an option value
}

// addOpt records wrap as accepting further options on the next word (LAUNCH-12, ADR 0022).
func (s *scanState) addOpt(wrap string) {
	if s.wrapperOpt == nil {
		s.wrapperOpt = map[string]bool{}
	}
	s.wrapperOpt[wrap] = true
}

// addArg records wrap as consuming the next word as an option value (LAUNCH-12, ADR 0022).
func (s *scanState) addArg(wrap string) {
	if s.wrapperArg == nil {
		s.wrapperArg = map[string]bool{}
	}
	s.wrapperArg[wrap] = true
}

// setOf builds a lookup set from a list of flag names (LAUNCH-12, ADR 0022).
func setOf(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, item := range items {
		m[item] = true
	}
	return m
}

// isAssignment reports whether a word sets a variable, e.g. LANG=C (LAUNCH-12, ADR 0022).
func isAssignment(w string) bool {
	name, _, ok := strings.Cut(w, "=")
	return ok && name != "" && !strings.ContainsAny(name, "/-.")
}

// isASCIIUnsigned reports whether s is a nonempty string of ASCII digits,
// used for nice's obsolete -<digits> priority flag (LAUNCH-12, ADR 0022).
func isASCIIUnsigned(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// blockedCommand returns why a launcher command is refused, or "" (LAUNCH-12, ADR 0022).
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
	// First check the raw argv list so multi-word wrapper option arguments
	// (e.g. systemd-run --description "my job" rm -rf /) keep their boundaries
	// (LAUNCH-12, ADR 0022).
	if reason := checkWords(args); reason != "" {
		return reason
	}
	// Then check the words of any shell text in them, split into segments at
	// ; | & ` and $( so "a; rm -rf /" is seen as two commands. Only a word in
	// program position counts, so a message that merely contains "su" passes
	// (LAUNCH-12, ADR 0022).
	for _, segment := range separators.Split(text, -1) {
		if reason := checkWords(strings.Fields(segment)); reason != "" {
			return reason
		}
	}
	return ""
}

// checkWords scans one command word list, tracking program position, wrapper
// options and wrapper option arguments (LAUNCH-12, ADR 0022).
func checkWords(words []string) string {
	cur := scanState{expectProgram: true}
	for i, w := range words {
		rest := words[i+1:]
		var next scanState

		if cur.expectProgram {
			if reason := checkProgramWord(w, rest, &next); reason != "" {
				return reason
			}
		}
		for wrap := range cur.wrapperArg {
			next.addOpt(wrap)
		}
		for wrap := range cur.wrapperOpt {
			spec := wrapperSpecs[wrap]
			if inner, ok := extractSplitString(w, spec); ok {
				if inner == "" {
					next.addOpt(wrap)
					continue
				}
				if reason := applyWrapperWord(wrap, spec, inner, rest, &next); reason != "" {
					return reason
				}
				continue
			}
			if cmd, hasCmd, ok := extractCommandFlag(w, spec); ok {
				if !hasCmd {
					next.expectProgram = true
					continue
				}
				if reason := checkProgramWord(cmd, rest, &next); reason != "" {
					return reason
				}
				continue
			}
			if reason := applyWrapperWord(wrap, spec, w, rest, &next); reason != "" {
				return reason
			}
		}
		if cur.inArgs {
			// After "-c" (sh -c, bash -c), the next word is a program again
			// (LAUNCH-12, ADR 0022).
			if w == "-c" {
				next.expectProgram = true
			} else {
				next.inArgs = true
			}
		}
		cur = next
	}
	return ""
}

// applyWrapperWord advances a wrapperOpt branch by one word w, skipping
// wrapper flags and option values until the wrapped program or "--"
// (LAUNCH-12, ADR 0022).
func applyWrapperWord(wrap string, spec wrapperSpec, w string, rest []string, next *scanState) string {
	switch {
	case w == "--":
		next.expectProgram = true
		return ""
	case spec.splitString && w == "-":
		next.addOpt(wrap)
		return ""
	case strings.HasPrefix(w, "-") && len(w) > 1:
		switch classifyWrapperFlag(w, spec) {
		case flagNoArg:
			next.addOpt(wrap)
		case flagWithArg:
			next.addArg(wrap)
		default:
			// Unknown wrapper flag without "=": could take zero or one word,
			// so keep both branches alive so a blocked program cannot slip
			// past (LAUNCH-12, ADR 0022).
			next.addOpt(wrap)
			next.addArg(wrap)
		}
		return ""
	default:
		return checkProgramWord(w, rest, next)
	}
}

// classifyWrapperFlag determines whether a wrapper option consumes zero, one
// or an unknown number of following words (LAUNCH-12, ADR 0022).
func classifyWrapperFlag(w string, spec wrapperSpec) flagArity {
	if strings.HasPrefix(w, "--") {
		if strings.Contains(w, "=") {
			return flagNoArg
		}
		if spec.noArg[w] {
			return flagNoArg
		}
		if spec.withArg[w] {
			return flagWithArg
		}
		return flagUnknown
	}
	if spec.numericFlag && isASCIIUnsigned(w[1:]) {
		return flagNoArg
	}
	for j := 1; j < len(w); j++ {
		short := "-" + w[j:j+1]
		if spec.noArg[short] {
			continue
		}
		if spec.withArg[short] {
			if j+1 < len(w) {
				// Attached option argument, e.g. "-uVAR" or "-n10".
				return flagNoArg
			}
			return flagWithArg
		}
		return flagUnknown
	}
	return flagNoArg
}

// extractSplitString extracts an inline word attached to env's
// --split-string=<word> or -S<word> / -<noArg>S<word> (LAUNCH-12, ADR 0022).
func extractSplitString(w string, spec wrapperSpec) (string, bool) {
	if !spec.splitString {
		return "", false
	}
	if inner, ok := strings.CutPrefix(w, "--split-string="); ok {
		return inner, true
	}
	if !strings.HasPrefix(w, "-") || strings.HasPrefix(w, "--") || len(w) <= 2 {
		return "", false
	}
	for j := 1; j < len(w); j++ {
		if w[j] == 'S' {
			if j+1 < len(w) {
				return w[j+1:], true
			}
			return "", false
		}
		if !spec.noArg["-"+w[j:j+1]] {
			return "", false
		}
	}
	return "", false
}

// extractCommandFlag recognizes kdesu's -c (alone, clustered after no-arg
// flags, or with an attached command word) so the command after it is checked
// in program position (LAUNCH-12, ADR 0022).
func extractCommandFlag(w string, spec wrapperSpec) (cmd string, hasCmd, ok bool) {
	if !spec.commandFlag || !strings.HasPrefix(w, "-") || strings.HasPrefix(w, "--") || len(w) < 2 {
		return "", false, false
	}
	for j := 1; j < len(w); j++ {
		if w[j] == 'c' {
			if j+1 < len(w) {
				return w[j+1:], true, true
			}
			return "", false, true
		}
		if !spec.noArg["-"+w[j:j+1]] {
			return "", false, false
		}
	}
	return "", false, false
}

// checkProgramWord checks a word in program position and records the next
// state for assignments, nested wrappers or ordinary arguments (LAUNCH-12, ADR 0022).
func checkProgramWord(w string, rest []string, next *scanState) string {
	if isAssignment(w) {
		next.expectProgram = true
		return ""
	}
	name := filepath.Base(w)
	if reason := blockedProgram(name, rest); reason != "" {
		return reason
	}
	if _, ok := wrapperSpecs[name]; ok {
		next.addOpt(name)
		return ""
	}
	next.inArgs = true
	return ""
}

// blockedProgram checks a resolved program basename and its trailing words
// against the denylist groups (LAUNCH-12, ADR 0022).
func blockedProgram(name string, rest []string) string {
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
