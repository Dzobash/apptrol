package launcher

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ParseExec splits a desktop file's Exec value into a program and its
// arguments, as the Desktop Entry specification says (LAUNCH-03):
//
//   - the escapes of a string value first: \s \n \t \r \\
//   - then arguments split at spaces; an argument in double quotes may hold
//     spaces, and \" \` \$ \\ stand for the character itself
//   - field codes in unquoted arguments: %f %F %u %U (files and links;
//     Apptrol opens none) are removed, %c becomes the app's name, %i and %k
//     are dropped, the deprecated %d %D %n %N %v %m are removed, %% is %.
//     An argument that was only a removed field code is dropped.
func ParseExec(exec, name string) ([]string, error) {
	type arg struct {
		text   string
		quoted bool
	}
	var args []arg
	s := []rune(unescape(exec))
	var cur strings.Builder
	inArg, quoted := false, false
	flush := func() {
		if inArg {
			args = append(args, arg{cur.String(), quoted})
		}
		cur.Reset()
		inArg, quoted = false, false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			// A quoted argument runs to the next unescaped quote.
			inArg, quoted = true, true
			for i++; ; i++ {
				if i >= len(s) {
					return nil, errors.New("exec: a quote is not closed")
				}
				if s[i] == '\\' && i+1 < len(s) && strings.ContainsRune("\"`$\\", s[i+1]) {
					i++
					cur.WriteRune(s[i])
					continue
				}
				if s[i] == '"' {
					break
				}
				cur.WriteRune(s[i])
			}
		case ' ', '\t':
			flush()
		default:
			inArg = true
			cur.WriteRune(c)
		}
	}
	flush()

	var out []string
	for _, a := range args {
		if a.quoted {
			out = append(out, a.text)
			continue
		}
		text, onlyCode, err := expandFieldCodes(a.text, name)
		if err != nil {
			return nil, err
		}
		if onlyCode && text == "" {
			continue
		}
		out = append(out, text)
	}
	if len(out) == 0 {
		return nil, errors.New("exec: no program")
	}
	return out, nil
}

// expandFieldCodes replaces the field codes in one unquoted argument.
// onlyCode reports that the argument held field codes and nothing else.
func expandFieldCodes(s, name string) (text string, onlyCode bool, err error) {
	var b strings.Builder
	onlyCode = true
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			b.WriteByte(s[i])
			onlyCode = false
			continue
		}
		if i+1 >= len(s) {
			return "", false, fmt.Errorf("exec: %q ends with a single %%", s)
		}
		i++
		switch s[i] {
		case '%':
			b.WriteByte('%')
			onlyCode = false
		case 'c':
			b.WriteString(name)
		case 'f', 'F', 'u', 'U', 'i', 'k', 'd', 'D', 'n', 'N', 'v', 'm':
			// removed: no files to open, no icon, no desktop file path
		default:
			return "", false, fmt.Errorf("exec: unknown field code %%%c", s[i])
		}
	}
	return b.String(), onlyCode, nil
}

// unescape applies the escapes of a desktop file's string values.
func unescape(v string) string {
	if !strings.Contains(v, `\`) {
		return v
	}
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		if v[i] != '\\' || i+1 >= len(v) {
			b.WriteByte(v[i])
			continue
		}
		i++
		switch v[i] {
		case 's':
			b.WriteByte(' ')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '\\':
			b.WriteByte('\\')
		default: // not a string escape: kept for the quoting rules
			b.WriteByte('\\')
			b.WriteByte(v[i])
		}
	}
	return b.String()
}

// ProgramName is the name of the program an Exec value runs, e.g. "firefox"
// for "/snap/bin/firefox %u"; "" if it cannot be parsed. It is used to find
// a running app by its process (LAUNCH-07).
func ProgramName(exec string) string {
	args, err := ParseExec(exec, "")
	if err != nil {
		return ""
	}
	return filepath.Base(args[0])
}
