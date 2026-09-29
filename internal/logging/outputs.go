package logging

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	charm "github.com/charmbracelet/log"
	"github.com/muesli/termenv"
)

const timeFormat = time.RFC3339

func charmFormatter(format string) (charm.Formatter, error) {
	switch format {
	case "text", "":
		return charm.TextFormatter, nil
	case "json":
		return charm.JSONFormatter, nil
	case "logfmt":
		return charm.LogfmtFormatter, nil
	}
	return 0, fmt.Errorf("unknown log format %q", format)
}

func newCharm(w io.Writer, format string, timestamps, color bool) (*charm.Logger, error) {
	f, err := charmFormatter(format)
	if err != nil {
		return nil, err
	}
	l := charm.NewWithOptions(w, charm.Options{
		Level:           charm.DebugLevel, // filtering happens in dynamicHandler
		Formatter:       f,
		ReportTimestamp: timestamps,
		TimeFormat:      timeFormat,
	})
	if !color {
		l.SetColorProfile(termenv.Ascii)
	}
	return l, nil
}

// newTerminalHandler prints readable output when Apptrol is started by hand:
// timestamps, and colors when stdout is a terminal (LOG-05).
func newTerminalHandler(w io.Writer, format string) (slog.Handler, error) {
	color := false
	if f, ok := w.(*os.File); ok {
		color = termenv.NewOutput(f).Profile != termenv.Ascii
	}
	return newCharm(w, format, true, color)
}

// newFileHandler writes to the log file: always with timestamps, never colors.
func newFileHandler(w io.Writer, format string) (slog.Handler, error) {
	return newCharm(w, format, true, false)
}

// journalHandler writes one line per record to stdout, prefixed with the
// syslog priority ("<6>") that journald reads (sd-daemon(3)). journald adds
// its own timestamps, so none are written (LOG-05).
type journalHandler struct {
	w  io.Writer
	mu *sync.Mutex
	// inner formats into buf; guarded by mu.
	inner slog.Handler
	buf   *bytes.Buffer
}

func newJournalHandler(w io.Writer, format string) (slog.Handler, error) {
	if format == "json" {
		return nil, fmt.Errorf("log format %q is not available for journald (use text or logfmt)", format)
	}
	buf := &bytes.Buffer{}
	inner, err := newCharm(buf, format, false, false)
	if err != nil {
		return nil, err
	}
	return &journalHandler{w: w, mu: &sync.Mutex{}, inner: inner, buf: buf}, nil
}

func (h *journalHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *journalHandler) Handle(ctx context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.buf.Reset()
	if err := h.inner.Handle(ctx, r); err != nil {
		return err
	}
	prefix := "<" + strconv.Itoa(Priority(r.Level)) + ">"
	var out bytes.Buffer
	for _, line := range strings.Split(strings.TrimRight(h.buf.String(), "\n"), "\n") {
		out.WriteString(prefix)
		out.WriteString(line)
		out.WriteByte('\n')
	}
	_, err := h.w.Write(out.Bytes())
	return err
}

func (h *journalHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	n := *h
	n.inner = h.inner.WithAttrs(attrs)
	return &n
}

func (h *journalHandler) WithGroup(name string) slog.Handler {
	n := *h
	n.inner = h.inner.WithGroup(name)
	return &n
}

// Priority maps a log level to its syslog priority: debug 7, info 6,
// warning 4, error 3.
func Priority(l slog.Level) int {
	switch {
	case l >= slog.LevelError:
		return 3
	case l >= slog.LevelWarn:
		return 4
	case l >= slog.LevelInfo:
		return 6
	}
	return 7
}

// IsJournalStream reports whether f is the stream systemd connected to the
// journal. systemd sets JOURNAL_STREAM to "<device>:<inode>" of that stream;
// comparing it with f avoids treating a redirected stdout as the journal.
func IsJournalStream(env string, f *os.File) bool {
	dev, ino, ok := strings.Cut(env, ":")
	if !ok || f == nil {
		return false
	}
	wantDev, err1 := strconv.ParseUint(dev, 10, 64)
	wantIno, err2 := strconv.ParseUint(ino, 10, 64)
	if err1 != nil || err2 != nil {
		return false
	}
	var st syscall.Stat_t
	if err := syscall.Fstat(int(f.Fd()), &st); err != nil {
		return false
	}
	return uint64(st.Dev) == wantDev && st.Ino == wantIno //nolint:unconvert // Dev's type differs between architectures
}
