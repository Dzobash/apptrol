package logattr

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestLOG11_ValidKey(t *testing.T) {
	for _, k := range []string{"error.type", "apptrol.volume_percent", "a", "apptrol.midi.cc2"} {
		if !ValidKey(k) {
			t.Errorf("%q rejected", k)
		}
	}
	for _, k := range []string{"", "Error.type", "apptrol..x", "apptrol.", ".x", "retry-in", "x._y", "x.y_"} {
		if ValidKey(k) {
			t.Errorf("%q accepted", k)
		}
	}
}

// Every attribute name this package defines follows the naming rules (LOG-11).
func TestLOG11_AllKeysValid(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "logattr.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	ast.Inspect(f, func(node ast.Node) bool {
		vs, ok := node.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range vs.Names {
			if !strings.HasPrefix(name.Name, "Key") || i >= len(vs.Values) {
				continue
			}
			lit, ok := vs.Values[i].(*ast.BasicLit)
			if !ok {
				continue
			}
			key, _ := strconv.Unquote(lit.Value)
			n++
			if !ValidKey(key) {
				t.Errorf("%s = %q breaks the naming rules", name.Name, key)
			}
			if !strings.HasPrefix(key, "apptrol.") && !otelKeys[key] {
				t.Errorf("%s = %q: use the apptrol namespace, or list it here as an OpenTelemetry name", name.Name, key)
			}
		}
		return true
	})
	if n < 10 {
		t.Fatalf("found only %d keys; did the file change shape?", n)
	}
}

// otelKeys are the names taken from the OpenTelemetry semantic conventions.
var otelKeys = map[string]bool{
	"error.type": true, "exception.message": true, "file.path": true, "url.full": true,
	"service.name": true, "service.version": true, "process.executable.name": true,
}

func TestLOG13_OneLine(t *testing.T) {
	for in, want := range map[string]string{
		"plain":                          "plain",
		"file: 2 problems\n  - a\n  - b": "file: 2 problems; a; b",
		"x\r\n\ny\n":                     "x; y",
	} {
		if got := OneLine(in); got != want {
			t.Errorf("OneLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLOG12_Error(t *testing.T) {
	a := Error(ErrConfigInvalid, errors.New("line 1\nline 2"))
	got := map[string]string{}
	for _, x := range a.Value.Group() {
		got[x.Key] = x.Value.String()
	}
	if a.Key != "" || got[KeyErrorType] != "config_invalid" || got[KeyExceptionMessage] != "line 1; line 2" {
		t.Errorf("Error = %v", a)
	}
	if n := len(Error(ErrConfigRemoved, nil).Value.Group()); n != 1 {
		t.Errorf("Error without an error value has %d attributes, want 1", n)
	}
	if !Component(Audio).Equal(slog.String("apptrol.component", "audio")) {
		t.Error("Component")
	}
}

// logMethods are the calls that write a log record: the message comes first,
// then attributes.
var logMethods = map[string]bool{"Debug": true, "Info": true, "Warn": true, "Error": true, "notice": true, "report": true}

// TestADR0016_LogCallsUseNamedKeys checks every log call in Apptrol's code:
// the message is a fixed text on one line, and attribute names come from this
// package, never from a string written at the call (LOG-11, LOG-13).
func TestADR0016_LogCallsUseNamedKeys(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	calls := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != root && skipLogCheckDir(d.Name()) {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !logMethods[sel.Sel.Name] || len(call.Args) == 0 {
				return true
			}
			args := call.Args
			if sel.Sel.Name == "notice" || sel.Sel.Name == "report" {
				args = args[1:] // level first
			}
			if len(args) == 0 {
				return true
			}
			msg, ok := args[0].(*ast.BasicLit)
			if !ok || msg.Kind != token.STRING {
				if _, isBin := args[0].(*ast.BinaryExpr); !isBin {
					return true // not a log call (e.g. errors.Error)
				}
			}
			calls++
			pos := fset.Position(call.Pos())
			if ok && strings.Contains(msg.Value, `\n`) {
				t.Errorf("%s: message has a line break", pos)
			}
			for i := 1; i < len(args); i++ {
				lit, isLit := args[i].(*ast.BasicLit)
				if !isLit || lit.Kind != token.STRING {
					continue
				}
				if prev, isSel := args[i-1].(*ast.SelectorExpr); isSel && strings.HasPrefix(prev.Sel.Name, "Key") {
					continue // a fixed value after a named key
				}
				t.Errorf("%s: attribute name %s written at the call; add a Key constant to logattr", pos, lit.Value)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls < 20 {
		t.Fatalf("found only %d log calls; is the check still looking at the code?", calls)
	}
}

// TestLogDocListsEverything keeps docs/log-attributes.md complete: every attribute
// name and error type defined here is explained there.
func TestLogDocListsEverything(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "log-attributes.md"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := parser.ParseFile(token.NewFileSet(), "logattr.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	ast.Inspect(f, func(node ast.Node) bool {
		vs, ok := node.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range vs.Names {
			if i >= len(vs.Values) || !strings.HasPrefix(name.Name, "Key") && !strings.HasPrefix(name.Name, "Err") {
				continue
			}
			lit, ok := vs.Values[i].(*ast.BasicLit)
			if !ok {
				continue
			}
			v, _ := strconv.Unquote(lit.Value)
			n++
			if !strings.Contains(string(doc), "| `"+v+"` |") {
				t.Errorf("docs/log-attributes.md does not explain %s (%q) in a table", name.Name, v)
			}
		}
		return true
	})
	if n < 30 {
		t.Fatalf("found only %d names", n)
	}
}

// TestLOG11_NoNameIsThePrefixOfAnother: log stores such as Elasticsearch turn
// dotted names into nested objects, so "a.b" cannot have a value when "a.b.c"
// exists.
func TestLOG11_NoNameIsThePrefixOfAnother(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "logattr.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	ast.Inspect(f, func(node ast.Node) bool {
		if vs, ok := node.(*ast.ValueSpec); ok {
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				if lit, ok := vs.Values[i].(*ast.BasicLit); ok && strings.HasPrefix(name.Name, "Key") {
					k, _ := strconv.Unquote(lit.Value)
					keys = append(keys, k)
				}
			}
		}
		return true
	})
	keys = append(keys, "time", "level", "msg") // added by the log formats
	for _, a := range keys {
		for _, b := range keys {
			if strings.HasPrefix(b, a+".") {
				t.Errorf("%q is the start of %q", a, b)
			}
		}
	}
}

// skipLogCheckDir keeps other checkouts and build output out of the log audit
// (LOG-11, LOG-13, ADR 0016, #130).
func skipLogCheckDir(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "dist"
}

// TestLOG13_LogCheckSkipsNonPackageDirs protects the audit from unrelated code
// in nested worktrees (LOG-11, LOG-13, ADR 0016, #130).
func TestLOG13_LogCheckSkipsNonPackageDirs(t *testing.T) {
	for _, name := range []string{".worktrees", ".claude", "_x", ".git", "dist", ".future", "_future"} {
		t.Run(name, func(t *testing.T) {
			if !skipLogCheckDir(name) {
				t.Errorf("directory %q must be skipped", name)
			}
		})
	}
	for _, name := range []string{"internal", "cmd", "distribution"} {
		if skipLogCheckDir(name) {
			t.Errorf("directory %q must be checked", name)
		}
	}
}

// TestQA01_FormatSkipsNonPackageDirs runs the real recipe so unrelated worktrees
// cannot break formatting checks while ordinary files remain checked (QA-01, QA-11, #130).
func TestQA01_FormatSkipsNonPackageDirs(t *testing.T) {
	makefile, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Makefile"), makefile, 0600); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{".worktrees", ".claude/worktrees", "_x", ".git", "dist", "internal/.future", "internal/_future"} {
		path := filepath.Join(root, dir)
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "bad.go"), []byte("package fixture;var x=1\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run := func() ([]byte, error) {
		cmd := exec.Command("make", "--no-print-directory", "fmt")
		cmd.Dir = root
		return cmd.CombinedOutput()
	}
	if out, err := run(); err != nil {
		t.Fatalf("excluded directories broke fmt: %v\n%s", err, out)
	}
	path := filepath.Join(root, "internal", "bad.go")
	if err := os.WriteFile(path, []byte("package fixture;var x=1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := run(); err == nil || !strings.Contains(string(out), "Needs gofmt:") || !strings.Contains(string(out), "internal/bad.go") {
		t.Fatalf("ordinary unformatted file must fail with Needs gofmt: err=%v\n%s", err, out)
	}
}
