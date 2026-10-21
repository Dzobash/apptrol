// Package systemd tells systemd how Apptrol is doing, for the Status line that
// `systemctl --user status apptrol` shows (SVC-13, ADR 0007).
//
// It uses go-systemd's daemon package, which is pure Go; only sdjournal needs
// cgo and is never imported (ADR 0013, ADR 0017). OS-specific code stays in an
// adapter like this one, so the core builds for other systems (NFR-11).
package systemd

import (
	"log/slog"
	"sync"
	"time"

	"github.com/coreos/go-systemd/v22/daemon"

	"github.com/Dzobash/apptrol/internal/logattr"
)

// Notifier sends the Status line to systemd. Writing to systemd's socket has
// no time limit and blocks when systemd stops reading, so SetStatus never
// writes itself: a sender goroutine does, and the service's event loop never
// waits for systemd (SVC-13, ADR 0007, ADR 0015).
type Notifier struct {
	log *slog.Logger
	// send is daemon.SdNotify; a test replaces it. It reports false, with no
	// error, when Apptrol was not started by systemd (no NOTIFY_SOCKET, for
	// example when it runs in a terminal): then nothing is sent.
	send func(state string) (bool, error)

	mu      sync.Mutex
	pending string        // the newest text not sent yet
	has     bool          // pending is set
	running bool          // a sender goroutine is working
	idle    chan struct{} // closed when that goroutine has nothing left to send

	notStartedLogged bool // only touched by the sender goroutine
}

// New returns a Notifier that logs to log.
func New(log *slog.Logger) *Notifier {
	return &Notifier{
		log: log.With(logattr.Component(logattr.Systemd)),
		send: func(state string) (bool, error) {
			return daemon.SdNotify(false, state)
		},
	}
}

// SetStatus hands the unit's Status line to the sender and returns at once;
// text must be one line. If an earlier text has not been sent yet, only the
// newest is: the Status line shows the current state, not a history. systemd
// drops the message, without an error, unless the unit says
// NotifyAccess=main, which is why it does (ADR 0007).
func (n *Notifier) SetStatus(text string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.pending, n.has = text, true
	if n.running {
		return
	}
	n.running = true
	n.idle = make(chan struct{})
	go n.drain(n.idle)
}

// Flush waits until everything handed over has been sent, but at most wait,
// so a stuck systemd cannot hold up the shutdown: the service calls it once,
// for the last text ("stopping").
func (n *Notifier) Flush(wait time.Duration) {
	n.mu.Lock()
	idle, running := n.idle, n.running
	n.mu.Unlock()
	if !running {
		return
	}
	select {
	case <-idle:
	case <-time.After(wait):
	}
}

// drain sends the pending text until there is none, then ends. A new sender
// is started by the next SetStatus.
func (n *Notifier) drain(idle chan struct{}) {
	for {
		n.mu.Lock()
		if !n.has {
			n.running = false
			close(idle)
			n.mu.Unlock()
			return
		}
		text := n.pending
		n.has = false
		n.mu.Unlock()
		n.deliver(text)
	}
}

// deliver sends one text and logs what happened. A failure is a warning only:
// Apptrol runs the same without the Status line.
func (n *Notifier) deliver(text string) {
	sent, err := n.send("STATUS=" + text)
	switch {
	case err != nil:
		n.log.Warn("could not send the status line", logattr.KeyStatus, text, logattr.Error(logattr.ErrStatusNotSent, err))
	case sent:
		n.log.Debug("status line sent", logattr.KeyStatus, text)
	case !n.notStartedLogged:
		n.notStartedLogged = true
		n.log.Debug("status line not sent; Apptrol was not started by systemd", logattr.KeyStatus, text)
	}
}
