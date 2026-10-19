package config

import (
	"fmt"
	"log/slog"
)

// Warning is something in a configuration worth fixing that does not make it
// invalid, such as an unused app (CFG-12) or an old form (CFG-19).
//
// Text is the line for people, as `apptrol check` prints it. Most warnings
// are logged as "configuration warning" with Text in apptrol.warning. A
// warning that someone may want to find or count in the log has its own fixed
// Message and Attrs instead (ADR 0016; ADR 0026 point 7), e.g. the old form:
//
//	Text:    apps.mic: type = "input" is the old form; move it to [inputs.mic]
//	Message: config uses an old form; move the entry to its own table
//	Attrs:   apptrol.app.id=mic apptrol.config.table=inputs
type Warning struct {
	Text    string
	Message string      // "" = logged as "configuration warning" with Text
	Attrs   []slog.Attr // attribute names from internal/logattr
}

func (w Warning) String() string { return w.Text }

// warnf returns a warning that is logged by its text alone.
func warnf(format string, args ...any) Warning {
	return Warning{Text: fmt.Sprintf(format, args...)}
}
