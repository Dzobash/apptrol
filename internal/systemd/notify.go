// Package systemd tells systemd how Apptrol is doing, for the Status line that
// `systemctl --user status apptrol` shows (SVC-13, ADR 0007).
//
// It uses go-systemd's daemon package, which is pure Go; only sdjournal needs
// cgo and is never imported (ADR 0013, ADR 0017). OS-specific code stays in an
// adapter like this one, so the core builds for other systems (NFR-11).
package systemd

import "github.com/coreos/go-systemd/v22/daemon"

// Notifier sends the Status line to systemd.
type Notifier struct{}

// SetStatus sets the unit's Status line to text, which must be one line. It
// reports false, with no error, when Apptrol was not started by systemd (no
// NOTIFY_SOCKET, for example when it runs in a terminal): then nothing is
// sent (SVC-13). systemd drops the message, without an error, unless the unit
// says NotifyAccess=main, which is why it does (ADR 0007).
func (Notifier) SetStatus(text string) (sent bool, err error) {
	return daemon.SdNotify(false, "STATUS="+text)
}
