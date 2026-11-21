package config

import (
	"path/filepath"
	"regexp"
	"slices"
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
			noArg: setOf("--no-ask-password", "--pty", "--pipe", "--slice-inherit", "--empower", "--via-shell"),
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

// maxScanDepth bounds nested split-string and shell-word expansions so odd
// inputs cannot trigger deep recursion (LAUNCH-12, QA-08, ADR 0022).
const maxScanDepth = 8

// scanState tracks all possible parser positions at the current word so an
// unknown wrapper flag can be explored as both zero-arg and one-arg without
// letting a blocked program slip past (LAUNCH-12, ADR 0022).
type scanState struct {
	expectProgram bool            // current word is in program position
	inArgs        bool            // already past a non-wrapper program
	wrapperOpt    map[string]bool // active wrappers currently reading options
	wrapperArg    map[string]bool // active wrappers expecting an option value
	wrapperSplit  map[string]bool // active wrappers expecting a split-string value (-S / --split-string)
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

// addSplit records wrap as consuming the next word as a whitespace-split
// argument string, e.g. env -S / --split-string (LAUNCH-12, ADR 0022).
func (s *scanState) addSplit(wrap string) {
	if s.wrapperSplit == nil {
		s.wrapperSplit = map[string]bool{}
	}
	s.wrapperSplit[wrap] = true
}

// merge combines another scanState into s so expanding a split-string value
// feeds its resulting states back into the outer word scan (LAUNCH-12, ADR 0022).
func (s *scanState) merge(other scanState) {
	s.expectProgram = s.expectProgram || other.expectProgram
	s.inArgs = s.inArgs || other.inArgs
	for wrap := range other.wrapperOpt {
		s.addOpt(wrap)
	}
	for wrap := range other.wrapperArg {
		s.addArg(wrap)
	}
	for wrap := range other.wrapperSplit {
		s.addSplit(wrap)
	}
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

// joinRest returns a followed by b without allocating when either slice is
// empty (LAUNCH-12, ADR 0022).
func joinRest(a, b []string) []string {
	if len(b) == 0 {
		return a
	}
	if len(a) == 0 {
		return b
	}
	return append(slices.Clone(a), b...)
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
	// (e.g. systemd-run --description "my job" rm -rf /, or
	// systemd-run --description "Run su" app) keep their exact boundaries
	// (LAUNCH-12, ADR 0022).
	if reason := checkWords(args); reason != "" {
		return reason
	}
	// Then check any segments that follow a shell separator (; | & ` or $()
	// anywhere in the arguments, so "a; rm -rf /" is still caught even when
	// placed inside an ordinary argument. Segment 0 was already checked with
	// intact argv boundaries above (LAUNCH-12, ADR 0022).
	segments := separators.Split(text, -1)
	for _, segment := range segments[1:] {
		if reason := checkWords(strings.Fields(segment)); reason != "" {
			return reason
		}
	}
	return ""
}

// checkWords scans one command word list, tracking program position, wrapper
// options and wrapper option arguments (LAUNCH-12, ADR 0022).
func checkWords(words []string) string {
	_, reason := scanWords(words, nil, scanState{expectProgram: true}, 0)
	return reason
}

// scanWords advances start across words, returning the resulting state or the
// first refusal reason. trailing holds any outer arguments that follow words
// when expanding a split-string value (LAUNCH-12, ADR 0022).
func scanWords(words, trailing []string, start scanState, depth int) (scanState, string) {
	cur := start
	for i, w := range words {
		rest := joinRest(words[i+1:], trailing)
		var next scanState

		if cur.expectProgram {
			if reason := checkProgramWord(w, rest, &next, depth); reason != "" {
				return scanState{}, reason
			}
		}
		for wrap := range cur.wrapperArg {
			next.addOpt(wrap)
		}
		for wrap := range cur.wrapperSplit {
			if reason := expandSplitString(wrap, w, rest, &next, depth); reason != "" {
				return scanState{}, reason
			}
		}
		for wrap := range cur.wrapperOpt {
			spec := wrapperSpecs[wrap]
			if payload, hasPayload, exact, ok := extractSplitString(w, spec); ok {
				if hasPayload {
					if reason := expandSplitString(wrap, payload, rest, &next, depth); reason != "" {
						return scanState{}, reason
					}
					continue
				}
				next.addSplit(wrap)
				if !exact {
					// Abbreviated --s… without "=" could also match a future
					// zero-arg flag, so keep wrapperOpt alive too (LAUNCH-12, ADR 0022).
					next.addOpt(wrap)
				}
				continue
			}
			if cmd, hasCmd, ok := extractCommandFlag(w, spec); ok {
				if !hasCmd {
					next.expectProgram = true
					continue
				}
				if reason := checkProgramWord(cmd, rest, &next, depth); reason != "" {
					return scanState{}, reason
				}
				continue
			}
			if reason := applyWrapperWord(wrap, spec, w, rest, &next, depth); reason != "" {
				return scanState{}, reason
			}
		}
		if cur.inArgs {
			// After "-c" (sh -c, bash -c), the next word is a program again.
			// Keep main's behavior for wrapper names inside arguments
			// (e.g. flatpak-spawn --host pkexec …, konsole -e pkexec …) so
			// nested launchers still get checked (LAUNCH-12, ADR 0022).
			switch {
			case w == "-c":
				next.expectProgram = true
			case strings.ContainsAny(w, " \t\n\r\v\f"):
				next.inArgs = true
				if depth < maxScanDepth {
					end, reason := scanWords(strings.Fields(w), rest, scanState{inArgs: true}, depth+1)
					if reason != "" {
						return scanState{}, reason
					}
					next.merge(end)
				}
			default:
				next.inArgs = true
				name := filepath.Base(w)
				if _, ok := wrapperSpecs[name]; ok {
					next.addOpt(name)
				}
			}
		}
		cur = next
	}
	return cur, ""
}

// expandSplitString splits an env -S / --split-string value into words, scans
// them as further options and command words for wrap, and merges the resulting
// state into next (LAUNCH-12, ADR 0022).
func expandSplitString(wrap, payload string, trailing []string, next *scanState, depth int) string {
	// Unwrap nested attached split-string prefixes (e.g. "-S-S…sudo" or
	// "--split-string=--split-string=…sudo") iteratively so arbitrary nesting
	// never runs into maxScanDepth; each step strictly shortens payload
	// (LAUNCH-12, QA-08, ADR 0022).
	spec := wrapperSpecs[wrap]
	for {
		trimmed := strings.TrimLeft(payload, " \t\n\r\v\f")
		inner, has, _, ok := extractSplitString(trimmed, spec)
		if !ok || !has {
			payload = trimmed
			break
		}
		payload = inner
	}
	if depth >= maxScanDepth {
		next.addOpt(wrap)
		return ""
	}
	start := scanState{wrapperOpt: map[string]bool{wrap: true}}
	end, reason := scanWords(strings.Fields(payload), trailing, start, depth+1)
	if reason != "" {
		return reason
	}
	next.merge(end)
	return ""
}

// applyWrapperWord advances a wrapperOpt branch by one word w, skipping
// wrapper flags and option values until the wrapped program or "--"
// (LAUNCH-12, ADR 0022).
func applyWrapperWord(wrap string, spec wrapperSpec, w string, rest []string, next *scanState, depth int) string {
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
		return checkProgramWord(w, rest, next, depth)
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

// extractSplitString recognizes env's -S and --split-string (including GNU
// getopt_long prefixes such as --s=…), with either an attached payload or a
// following payload word (LAUNCH-12, ADR 0022).
func extractSplitString(w string, spec wrapperSpec) (payload string, hasPayload, exact, ok bool) {
	if !spec.splitString {
		return "", false, false, false
	}
	if strings.HasPrefix(w, "--") && len(w) > 2 {
		const splitStringOpt = "split-string"
		opt, val, hasEq := strings.Cut(w[2:], "=")
		if opt != "" && len(opt) <= len(splitStringOpt) && splitStringOpt[:len(opt)] == opt {
			if hasEq {
				return val, true, true, true
			}
			return "", false, opt == "split-string", true
		}
		return "", false, false, false
	}
	if !strings.HasPrefix(w, "-") || strings.HasPrefix(w, "--") || len(w) < 2 {
		return "", false, false, false
	}
	for j := 1; j < len(w); j++ {
		if w[j] == 'S' {
			if j+1 < len(w) {
				return w[j+1:], true, true, true
			}
			return "", false, true, true
		}
		if !spec.noArg["-"+w[j:j+1]] {
			return "", false, false, false
		}
	}
	return "", false, false, false
}

// extractCommandFlag recognizes kdesu's -c and --c / --c=<cmd> (alone,
// clustered after no-arg flags, or with an attached command word) so the
// command after it is checked in program position (LAUNCH-12, ADR 0022).
func extractCommandFlag(w string, spec wrapperSpec) (cmd string, hasCmd, ok bool) {
	if !spec.commandFlag {
		return "", false, false
	}
	if w == "--c" {
		return "", false, true
	}
	if attached, found := strings.CutPrefix(w, "--c="); found {
		return attached, true, true
	}
	if !strings.HasPrefix(w, "-") || strings.HasPrefix(w, "--") || len(w) < 2 {
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
// state for assignments, nested wrappers or ordinary arguments. A word
// containing whitespace or shell separators (e.g. the command string passed to
// "sh -c") is split into segments and words instead of passing the whole string
// to filepath.Base (LAUNCH-12, ADR 0022).
func checkProgramWord(w string, rest []string, next *scanState, depth int) string {
	if strings.ContainsAny(w, " \t\n\r\v\f") || separators.MatchString(w) {
		if depth < maxScanDepth {
			segments := separators.Split(w, -1)
			for idx, segment := range segments {
				var trailing []string
				if idx == len(segments)-1 {
					trailing = rest
				}
				end, reason := scanWords(strings.Fields(segment), trailing, scanState{expectProgram: true}, depth+1)
				if reason != "" {
					return reason
				}
				if idx == len(segments)-1 {
					next.merge(end)
				}
			}
		} else {
			next.inArgs = true
		}
		return ""
	}
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
