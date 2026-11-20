// Package logging sets up Apptrol's log outputs (ADR 0008).
//
// Two outputs are available, separately or together (LOG-03):
//
//   - journald: when running under systemd, each line on stdout carries a
//     severity prefix so journald files it at the right priority (LOG-05);
//     started from a terminal, it prints readable, colored text instead.
//   - file: a log file with size-based rotation (LOG-06, LOG-07).
//
// Each output has its own format (LOG-04). Level and outputs can be changed at
// runtime with Reconfigure without replacing the *slog.Logger (LOG-08).
package logging

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"

	"github.com/Dzobash/apptrol/internal/config"
	"github.com/Dzobash/apptrol/internal/logattr"
)

// Options controls where output goes. The zero value uses the real stdout and
// detects systemd automatically.
type Options struct {
	// Stdout receives the journald/terminal output. Default: os.Stdout.
	Stdout io.Writer
	// Journal forces journald mode (true) or terminal mode (false).
	// Nil detects it from JOURNAL_STREAM (see IsJournalStream).
	Journal *bool
	// Level, if set, is used instead of the configured level for the whole
	// run, including every Reconfigure (--log-level, LOG-16).
	Level *slog.Level
}

// Manager owns the log outputs. Its Logger stays valid across Reconfigure.
type Manager struct {
	opts    Options
	journal bool
	level   slog.LevelVar

	// applyMu serializes Reconfigure and Close from picking the file to
	// retiring the old one, so two reloads never install a file the other
	// retired (LOG-08, ADR 0008, #250).
	applyMu sync.Mutex
	// draining holds files a reload replaced while records were still being
	// written to them, by path, so a reload back to that path takes the same
	// file again: one file at a time rotates a path. Guarded by applyMu.
	draining map[string]*rotatingFile

	mu       sync.RWMutex
	handlers []slog.Handler
	file     *rotatingFile

	logger *slog.Logger
}

// New creates the outputs described by cfg. It always returns a usable
// Manager: if an output cannot be set up (for example the log file cannot be
// opened), the others are used — falling back to the journald/terminal output
// if nothing else is left — and the problem is returned as the error, for the
// caller to log.
func New(cfg config.Log, opts Options) (*Manager, error) {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	m := &Manager{opts: opts}
	if opts.Journal != nil {
		m.journal = *opts.Journal
	} else {
		m.journal = IsJournalStream(os.Getenv("JOURNAL_STREAM"), os.Stdout)
	}
	m.logger = slog.New(&dynamicHandler{m: m})
	err := m.apply(cfg, true)
	return m, err
}

// Logger returns the logger. It follows every later Reconfigure.
func (m *Manager) Logger() *slog.Logger { return m.logger }

// Reconfigure applies a new logging configuration (LOG-08). If an output
// cannot be set up, the remaining ones are used and the error is returned.
func (m *Manager) Reconfigure(cfg config.Log) error { return m.apply(cfg, false) }

// Close flushes and closes the log file, if any; a record still being written
// finishes first (LOG-08, ADR 0008, #250).
func (m *Manager) Close() error {
	m.applyMu.Lock()
	defer m.applyMu.Unlock()
	m.mu.Lock()
	file := m.file
	m.handlers = nil
	m.file = nil
	m.mu.Unlock()
	if file == nil {
		return nil
	}
	err := file.retire()
	m.trackDraining(file, nil)
	return err
}

func (m *Manager) apply(cfg config.Log, initial bool) error {
	m.applyMu.Lock()
	defer m.applyMu.Unlock()
	level, err := effectiveLevel(cfg.Level, m.opts.Level)
	if err != nil {
		return err
	}

	var (
		handlers []slog.Handler
		file     *rotatingFile
		errs     []error
		working  int // handlers that can write now
	)
	for _, out := range cfg.Outputs {
		switch out {
		case config.OutputJournald:
			h, err := m.stdoutHandler(cfg.Journald.Format)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			handlers = append(handlers, h)
			working++
		case config.OutputFile:
			// A handler with an error is a file that cannot be opened now but
			// stays attached, so the next record can reopen it (#250).
			f, h, err := m.fileHandler(cfg.File)
			if err != nil {
				errs = append(errs, err)
			}
			if h == nil {
				continue
			}
			file = f
			handlers = append(handlers, h)
			if err == nil {
				working++
			}
		default:
			errs = append(errs, fmt.Errorf("unknown log output %q", out))
		}
	}
	if working == 0 {
		// Never end up without any output: fall back to journald/terminal.
		h, err := m.stdoutHandler("text")
		if err != nil {
			return errors.Join(append(errs, err)...)
		}
		handlers = append(handlers, h)
		if !initial || len(errs) > 0 {
			errs = append(errs, errors.New("no log output could be set up; logging to the journal/terminal instead"))
		}
	}

	m.mu.Lock()
	old := m.file
	m.handlers = handlers
	m.file = file
	m.level.Set(level)
	m.mu.Unlock()

	// A record still being written to the old file (dynamicHandler.Handle)
	// finishes before retire closes it, so none is lost (LOG-08, ADR 0008, #250).
	if old != nil && old != file {
		if err := old.retire(); err != nil {
			errs = append(errs, fmt.Errorf("closing previous log file: %w", err))
		}
	}
	m.trackDraining(old, file)
	return errors.Join(errs...)
}

// trackDraining forgets closed and reused files and remembers old while its
// last records are still being written (LOG-08, ADR 0008, #250).
func (m *Manager) trackDraining(old, current *rotatingFile) {
	for path, f := range m.draining {
		if f == current || !f.draining() {
			delete(m.draining, path)
		}
	}
	if old != nil && old != current && old.draining() {
		if m.draining == nil {
			m.draining = map[string]*rotatingFile{}
		}
		m.draining[old.path] = old
	}
}

func (m *Manager) stdoutHandler(format string) (slog.Handler, error) {
	if m.journal {
		return newJournalHandler(m.opts.Stdout, format)
	}
	return newTerminalHandler(m.opts.Stdout, format)
}

func (m *Manager) fileHandler(fc config.FileOutput) (*rotatingFile, slog.Handler, error) {
	// Reuse the open file when the path has not changed; the limits may.
	m.mu.RLock()
	cur := m.file
	m.mu.RUnlock()
	if cur == nil || cur.path != fc.Path {
		// A file for this path still finishing its records from an earlier
		// reload is taken back instead of opening a second one (#250).
		if d := m.draining[fc.Path]; d != nil && d.revive() {
			cur = d
		}
	}
	// A file whose reopen failed is opened again here. If that still fails,
	// the file is returned with its handler and the error: the reload reports
	// it and falls back to the journal, and the next record tries to reopen the
	// file, so restoring its folder is enough. A closed file is replaced
	// (LOG-08, ADR 0008, #250).
	var (
		f        *rotatingFile
		reuseErr error
	)
	if cur != nil && cur.path == fc.Path {
		switch err := cur.reuse(fc.MaxSize, fc.MaxFiles); {
		case err == nil:
			f = cur
		case errors.Is(err, os.ErrClosed):
			// opened anew below
		default:
			f, reuseErr = cur, fmt.Errorf("log file: %w", err)
		}
	}
	if f == nil {
		var err error
		f, err = openRotatingFile(fc.Path, fc.MaxSize, fc.MaxFiles)
		if err != nil {
			return nil, nil, fmt.Errorf("log file: %w", err)
		}
	}
	h, err := newFileHandler(f, fc.Format)
	if err != nil {
		if f != cur {
			_ = f.Close()
		} else if f != m.file {
			_ = f.retire() // revived above, but not used after all
		}
		return nil, nil, err
	}
	return f, h, reuseErr
}

// ParseLevel converts a configured level name to a slog level (LOG-02).
func ParseLevel(s string) (slog.Level, error) {
	switch s {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("unknown log level %q", s)
}

// effectiveLevel returns the level to use: the override from --log-level if
// set, otherwise the configured one (LOG-16). The configured level is not even
// parsed when there is an override; configuration validation reports a bad one.
func effectiveLevel(configured string, override *slog.Level) (slog.Level, error) {
	if override != nil {
		return *override, nil
	}
	return ParseLevel(configured)
}

// dynamicHandler forwards records to the Manager's current outputs, so that a
// *slog.Logger created once keeps working after Reconfigure.
type dynamicHandler struct {
	m      *Manager
	attrs  []slog.Attr
	groups []string
}

func (h *dynamicHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.m.level.Level()
}

// Handle writes r to every current output. It holds the lock only to pick the
// outputs, and marks the file as in use until the record is written, so a
// Reconfigure or Close that replaces the file closes it only after this record,
// and a slow output or a value that logs or reloads while it is formatted
// blocks no reload (LOG-08, ADR 0008, #250).
func (h *dynamicHandler) Handle(ctx context.Context, r slog.Record) error {
	r = oneLine(r)
	h.m.mu.RLock()
	outputs, file := h.m.handlers, h.m.file
	if file != nil {
		file.acquire()
	}
	h.m.mu.RUnlock()
	if file != nil {
		defer file.release()
	}

	var errs []error
	for _, out := range outputs {
		for _, g := range h.groups {
			out = out.WithGroup(g)
		}
		if len(h.attrs) > 0 {
			out = out.WithAttrs(h.attrs)
		}
		if err := out.Handle(ctx, r.Clone()); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (h *dynamicHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	n := *h
	n.attrs = append(append([]slog.Attr(nil), h.attrs...), flatten(nil, attrs)...)
	return &n
}

func (h *dynamicHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	n := *h
	n.groups = append(append([]string(nil), h.groups...), name)
	return &n
}

// oneLine returns r with every line break in the message and in string values
// replaced, and with groups without a name inlined, so that each record is
// one line in every format (LOG-13, ADR 0016).
func oneLine(r slog.Record) slog.Record {
	var attrs []slog.Attr
	r.Attrs(func(a slog.Attr) bool {
		attrs = flatten(attrs, []slog.Attr{a})
		return true
	})
	n := slog.NewRecord(r.Time, r.Level, logattr.OneLine(r.Message), r.PC)
	n.AddAttrs(attrs...)
	return n
}

// flatten appends attrs to dst, inlining groups without a name (slog's rule,
// which not every output follows) and joining the lines of string values.
func flatten(dst, attrs []slog.Attr) []slog.Attr {
	for _, a := range attrs {
		a.Value = a.Value.Resolve()
		switch {
		case a.Value.Kind() == slog.KindGroup && a.Key == "":
			dst = flatten(dst, a.Value.Group())
		case a.Value.Kind() == slog.KindString:
			dst = append(dst, slog.String(a.Key, logattr.OneLine(a.Value.String())))
		case a.Key == "" && a.Value.Any() == nil:
			// an empty attribute; slog handlers ignore it
		case a.Value.Kind() == slog.KindAny && isError(a.Value.Any()):
			dst = append(dst, slog.String(a.Key, logattr.OneLine(a.Value.Any().(error).Error())))
		default:
			dst = append(dst, a)
		}
	}
	return dst
}

func isError(v any) bool { _, ok := v.(error); return ok }
