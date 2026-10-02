package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Dzobash/apptrol/internal/config"
	"github.com/Dzobash/apptrol/internal/logattr"
)

// syncBuffer is a bytes.Buffer safe for concurrent writes.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func boolPtr(b bool) *bool { return &b }

func baseConfig() config.Log {
	return config.Log{
		Level:    "info",
		Outputs:  []string{config.OutputJournald},
		Journald: config.JournaldOutput{Format: "text"},
		File:     config.FileOutput{Format: "json", MaxSize: 10 << 20, MaxFiles: 5},
	}
}

func newManager(t *testing.T, cfg config.Log, journal bool) (*Manager, *syncBuffer) {
	t.Helper()
	out := &syncBuffer{}
	m, err := New(cfg, Options{Stdout: out, Journal: boolPtr(journal)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m, out
}

func lines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func TestLOG02_LevelFiltersMessages(t *testing.T) {
	tests := []struct {
		level string
		want  []string
	}{
		{"debug", []string{"d", "i", "w", "e"}},
		{"info", []string{"i", "w", "e"}},
		{"warn", []string{"w", "e"}},
		{"error", []string{"e"}},
	}
	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			cfg := baseConfig()
			cfg.Level = tt.level
			m, out := newManager(t, cfg, true)
			l := m.Logger()
			l.Debug("d")
			l.Info("i")
			l.Warn("w")
			l.Error("e")
			got := lines(out.String())
			if len(got) != len(tt.want) {
				t.Fatalf("got %d lines, want %d:\n%s", len(got), len(tt.want), out.String())
			}
			for i, w := range tt.want {
				if !strings.HasSuffix(got[i], " "+w) {
					t.Errorf("line %d = %q, want message %q", i, got[i], w)
				}
			}
		})
	}
}

func TestLOG05_JournaldPriorityPrefixes(t *testing.T) {
	cfg := baseConfig()
	cfg.Level = "debug"
	m, out := newManager(t, cfg, true)
	l := m.Logger()
	l.Debug("d")
	l.Info("i", "app", "spotify")
	l.Warn("w")
	l.Error("e")

	want := []string{"<7>", "<6>", "<4>", "<3>"}
	got := lines(out.String())
	if len(got) != 4 {
		t.Fatalf("want 4 lines, got:\n%s", out.String())
	}
	for i, p := range want {
		if !strings.HasPrefix(got[i], p) {
			t.Errorf("line %d = %q, want prefix %s", i, got[i], p)
		}
	}
	if !strings.Contains(got[1], "app=spotify") {
		t.Errorf("attributes missing: %q", got[1])
	}
	// journald adds its own timestamps: none in the line.
	if strings.Contains(got[1], "T") && strings.Contains(got[1], ":") && strings.Count(got[1], "-") >= 2 {
		t.Errorf("journald line looks like it has a timestamp: %q", got[1])
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Error("journald output must not contain color codes")
	}
}

func TestLOG13_EveryRecordIsOneLine(t *testing.T) {
	for _, format := range []string{"text", "logfmt", "json"} {
		t.Run(format, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "apptrol.log")
			cfg := baseConfig()
			cfg.Outputs = []string{config.OutputFile}
			cfg.File = config.FileOutput{Path: path, Format: format, MaxSize: 1 << 20, MaxFiles: 1}
			m, _ := newManager(t, cfg, true)
			l := m.Logger().With(logattr.Component(logattr.Config))
			l.Error("config invalid:\n  - problem one",
				logattr.Error(logattr.ErrConfigInvalid, errors.New("file: 2 problems\n  - a\n  - b")),
				"plain", fmt.Errorf("wrapped:\n%w", errors.New("inner")))
			if err := m.Close(); err != nil {
				t.Fatal(err)
			}
			got := lines(readFile(t, path))
			if len(got) != 1 {
				t.Fatalf("%d lines:\n%s", len(got), strings.Join(got, "\n"))
			}
			for _, want := range []string{"apptrol.component", "config", "error.type", "config_invalid",
				"exception.message", "file: 2 problems; a; b", "config invalid:; problem one", "wrapped:; inner"} {
				if !strings.Contains(got[0], want) {
					t.Errorf("record lacks %q: %s", want, got[0])
				}
			}
		})
	}
}

func TestLOG05_JournaldOneLinePerRecord(t *testing.T) {
	m, out := newManager(t, baseConfig(), true)
	m.Logger().Error("config invalid:\n  - problem one\n  - problem two")
	got := lines(out.String())
	if len(got) != 1 || !strings.HasPrefix(got[0], "<3>") {
		t.Errorf("journal lines = %q", got)
	}
}

func TestLOG04_JournaldLogfmt(t *testing.T) {
	cfg := baseConfig()
	cfg.Journald.Format = "logfmt"
	m, out := newManager(t, cfg, true)
	m.Logger().Info("controller connected", "device", "hw:1,0,0")
	got := out.String()
	for _, want := range []string{"<6>", "level=info", `msg="controller connected"`, "device=hw:1,0,0"} {
		if !strings.Contains(got, want) {
			t.Errorf("logfmt output %q missing %q", got, want)
		}
	}
}

func TestLOG04_JournaldRejectsJSON(t *testing.T) {
	cfg := baseConfig()
	cfg.Journald.Format = "json"
	m, err := New(cfg, Options{Stdout: &syncBuffer{}, Journal: boolPtr(true)})
	if err == nil {
		t.Fatal("want an error for json on journald")
	}
	if m == nil || m.Logger() == nil {
		t.Fatal("New must still return a usable manager")
	}
}

func TestLOG05_TerminalModeHasTimestamps(t *testing.T) {
	m, out := newManager(t, baseConfig(), false)
	m.Logger().Info("hello")
	got := out.String()
	if strings.HasPrefix(got, "<6>") {
		t.Errorf("terminal output must not carry journald prefixes: %q", got)
	}
	if !strings.Contains(got, "hello") || !strings.Contains(got, "INFO") {
		t.Errorf("terminal output = %q", got)
	}
	// RFC 3339 timestamp with milliseconds, e.g. 2026-09-29T15:04:05.123+02:00
	ts := strings.Fields(got)[0]
	if _, err := time.Parse(time.RFC3339, ts); err != nil || len(ts) < len("2006-01-02T15:04:05.000Z") || ts[19] != '.' {
		t.Errorf("want a timestamp with milliseconds first, got %q", got)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLOG06_FileOutputJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "dir", "apptrol.log")
	cfg := baseConfig()
	cfg.Outputs = []string{config.OutputFile}
	cfg.File.Path = path
	m, out := newManager(t, cfg, true)
	m.Logger().Warn("stream matched nothing", "app", "discord")
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}

	if out.String() != "" {
		t.Errorf("file-only config wrote to stdout: %q", out.String())
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(readFile(t, path))), &rec); err != nil {
		t.Fatalf("file line is not JSON: %v", err)
	}
	if rec["msg"] != "stream matched nothing" || rec["level"] != "warn" || rec["app"] != "discord" || rec["time"] == nil {
		t.Errorf("JSON record = %v", rec)
	}
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o640 {
		t.Errorf("log file mode = %v, %v", st.Mode().Perm(), err)
	}
}

func TestLOG03_BothOutputsWithOwnFormats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apptrol.log")
	cfg := baseConfig()
	cfg.Outputs = []string{config.OutputJournald, config.OutputFile}
	cfg.File.Path = path
	cfg.File.Format = "logfmt"
	m, out := newManager(t, cfg, true)
	m.Logger().Info("started", "version", "v0.1.0")
	_ = m.Close()

	if !strings.HasPrefix(out.String(), "<6>") {
		t.Errorf("journald output = %q", out.String())
	}
	file := readFile(t, path)
	if !strings.Contains(file, "level=info") || !strings.Contains(file, "version=v0.1.0") || !strings.Contains(file, "time=") {
		t.Errorf("logfmt file = %q", file)
	}
}

func TestLOG08_ReconfigureWithoutNewLogger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apptrol.log")
	m, out := newManager(t, baseConfig(), true)
	l := m.Logger().With("component", "test")

	l.Debug("hidden")
	cfg := baseConfig()
	cfg.Level = "debug"
	cfg.Outputs = []string{config.OutputJournald, config.OutputFile}
	cfg.File.Path = path
	if err := m.Reconfigure(cfg); err != nil {
		t.Fatalf("Reconfigure: %v", err)
	}
	l.Debug("visible")

	if strings.Contains(out.String(), "hidden") {
		t.Error("debug message logged before the level was raised")
	}
	if !strings.Contains(out.String(), "visible") || !strings.Contains(out.String(), "component=test") {
		t.Errorf("stdout after reconfigure = %q", out.String())
	}
	if !strings.Contains(readFile(t, path), "visible") {
		t.Error("file output added by Reconfigure did not receive the message")
	}

	// Removing the file output closes the file.
	if err := m.Reconfigure(baseConfig()); err != nil {
		t.Fatal(err)
	}
	l.Info("after")
	if strings.Contains(readFile(t, path), "after") {
		t.Error("file output still active after it was removed")
	}
}

func TestLOG08_ReconfigureKeepsOpenFileForSamePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apptrol.log")
	cfg := baseConfig()
	cfg.Outputs = []string{config.OutputFile}
	cfg.File.Path = path
	m, _ := newManager(t, cfg, true)
	first := m.file
	cfg.File.MaxFiles = 2
	if err := m.Reconfigure(cfg); err != nil {
		t.Fatal(err)
	}
	if m.file != first {
		t.Error("file was reopened although the path did not change")
	}
	if m.file.maxFiles != 2 {
		t.Errorf("maxFiles = %d, want the new limit 2", m.file.maxFiles)
	}
}

func TestFileOutputFailureFallsBackToJournal(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := baseConfig()
	cfg.Outputs = []string{config.OutputFile}
	cfg.File.Path = filepath.Join(blocker, "apptrol.log") // parent is a file: cannot be created
	out := &syncBuffer{}
	m, err := New(cfg, Options{Stdout: out, Journal: boolPtr(true)})
	if err == nil {
		t.Fatal("want an error when the log file cannot be opened")
	}
	if !strings.Contains(err.Error(), "logging to the journal/terminal instead") {
		t.Errorf("error = %v", err)
	}
	m.Logger().Info("still logged")
	if !strings.Contains(out.String(), "still logged") {
		t.Error("nothing logged after the file output failed")
	}
	_ = m.Close()
}

func TestReconfigureRejectsUnknownLevel(t *testing.T) {
	m, _ := newManager(t, baseConfig(), true)
	cfg := baseConfig()
	cfg.Level = "loud"
	if err := m.Reconfigure(cfg); err == nil {
		t.Error("want an error for an unknown level")
	}
}

func newManagerWithLevel(t *testing.T, cfg config.Log, level slog.Level) (*Manager, *syncBuffer) {
	t.Helper()
	out := &syncBuffer{}
	m, err := New(cfg, Options{Stdout: out, Journal: boolPtr(true), Level: &level})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m, out
}

func TestLOG16_OverrideWinsOverConfiguredLevel(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		override   slog.Level
		want       []string
	}{
		{"debug over info", "info", slog.LevelDebug, []string{"d", "i", "w"}},
		{"warn over debug", "debug", slog.LevelWarn, []string{"w"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.Level = tt.configured
			m, out := newManagerWithLevel(t, cfg, tt.override)
			l := m.Logger()
			l.Debug("d")
			l.Info("i")
			l.Warn("w")
			got := lines(out.String())
			if len(got) != len(tt.want) {
				t.Fatalf("got %d lines, want %d:\n%s", len(got), len(tt.want), out.String())
			}
			for i, w := range tt.want {
				if !strings.HasSuffix(got[i], " "+w) {
					t.Errorf("line %d = %q, want message %q", i, got[i], w)
				}
			}
		})
	}
}

func TestLOG16_OverrideSurvivesReconfigureOnEveryOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apptrol.log")
	m, out := newManagerWithLevel(t, baseConfig(), slog.LevelDebug)

	// A reload that asks for "warn" and adds the file output.
	cfg := baseConfig()
	cfg.Level = "warn"
	cfg.Outputs = []string{config.OutputJournald, config.OutputFile}
	cfg.File.Path = path
	if err := m.Reconfigure(cfg); err != nil {
		t.Fatalf("Reconfigure: %v", err)
	}
	m.Logger().Debug("still debug")

	if !strings.Contains(out.String(), "still debug") {
		t.Error("the reload replaced the --log-level override on the journal output")
	}
	if !strings.Contains(readFile(t, path), "still debug") {
		t.Error("the --log-level override does not reach the file output")
	}
}

func TestConcurrentLoggingDuringReconfigure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apptrol.log")
	m, _ := newManager(t, baseConfig(), true)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			l := m.Logger().With("goroutine", g)
			for i := 0; i < 200; i++ {
				l.Info("tick", "i", i)
			}
		}(g)
	}
	for i := 0; i < 20; i++ {
		cfg := baseConfig()
		if i%2 == 0 {
			cfg.Outputs = []string{config.OutputJournald, config.OutputFile}
			cfg.File.Path = path
		}
		if err := m.Reconfigure(cfg); err != nil {
			t.Error(err)
		}
	}
	wg.Wait()
}

func TestPriority(t *testing.T) {
	tests := map[slog.Level]int{slog.LevelDebug: 7, slog.LevelInfo: 6, slog.LevelWarn: 4, slog.LevelError: 3, slog.LevelError + 4: 3}
	for level, want := range tests {
		if got := Priority(level); got != want {
			t.Errorf("Priority(%v) = %d, want %d", level, got, want)
		}
	}
}

func TestLOG05_IsJournalStream(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "stream")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var st syscall.Stat_t
	if err := syscall.Fstat(int(f.Fd()), &st); err != nil {
		t.Fatal(err)
	}
	match := fmt.Sprintf("%d:%d", st.Dev, st.Ino)

	if !IsJournalStream(match, f) {
		t.Error("matching JOURNAL_STREAM not recognized")
	}
	for _, env := range []string{"", "garbage", "1:2", fmt.Sprintf("%d:%d", st.Dev, st.Ino+1), "x:1"} {
		if IsJournalStream(env, f) {
			t.Errorf("JOURNAL_STREAM=%q wrongly recognized", env)
		}
	}
}

func TestGroupsReachEveryOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apptrol.log")
	cfg := baseConfig()
	cfg.Outputs = []string{config.OutputJournald, config.OutputFile}
	cfg.File.Path = path
	cfg.File.Format = "logfmt"
	m, out := newManager(t, cfg, true)
	l := m.Logger().WithGroup("mixer")
	if l.Handler() != l.WithGroup("").Handler() {
		t.Error("WithGroup(\"\") should return the same handler")
	}
	l.Info("solo on", "column", 2)
	_ = m.Close()
	if !strings.Contains(out.String(), "mixer") || !strings.Contains(readFile(t, path), "mixer") {
		t.Errorf("group missing:\nstdout: %s\nfile: %s", out.String(), readFile(t, path))
	}
}

func TestTerminalHandlerOnNonTerminalFile(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	m, err := New(baseConfig(), Options{Stdout: w, Journal: boolPtr(false)})
	if err != nil {
		t.Fatal(err)
	}
	m.Logger().Info("to a pipe")
	_ = w.Close()
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	if got := string(buf[:n]); !strings.Contains(got, "to a pipe") || strings.Contains(got, "\x1b[") {
		t.Errorf("pipe output = %q: want the message without color codes", got)
	}
}
