// Package session is the adapter for logind, the login manager, on the D-Bus
// system bus (ADR 0023): it learns
// from systemd-logind when the computer wakes up from sleep or hibernation,
// and reports it to the mixer, which sends every LED again (LED-09).
package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/Dzobash/apptrol/internal/logattr"
	"github.com/Dzobash/apptrol/internal/mixer"
)

// logind names (https://www.freedesktop.org/software/systemd/man/latest/org.freedesktop.login1.html).
const (
	login1Name   = "org.freedesktop.login1"
	login1Path   = dbus.ObjectPath("/org/freedesktop/login1")
	login1Iface  = "org.freedesktop.login1.Manager"
	sleepSignal  = "PrepareForSleep"
	systemSocket = "unix:path=/run/dbus/system_bus_socket"
)

// defaultResend: after a wake-up, mixer.SystemResumed is sent at once and
// again after each of these delays. The controller starts again some seconds
// after the wake-up and ignores LED messages while it starts (LED-09).
var defaultResend = []time.Duration{500 * time.Millisecond, 2 * time.Second, 5 * time.Second}

// Watcher keeps a connection to the system bus and reports wake-ups.
type Watcher struct {
	log     *slog.Logger
	address string // "" = from the environment (SystemBusAddress)
	// Wait before reconnecting: starts at retryMin, doubles up to retryMax.
	retryMin, retryMax time.Duration
	resend             []time.Duration // see defaultResend
}

// New returns a Watcher. address is the system bus address; "" finds it as
// SystemBusAddress does.
func New(log *slog.Logger, address string) *Watcher {
	return &Watcher{log: log.With(logattr.Component(logattr.Session)), address: address,
		retryMin: 500 * time.Millisecond, retryMax: 30 * time.Second, resend: defaultResend}
}

// SystemBusAddress returns DBUS_SYSTEM_BUS_ADDRESS, or the standard socket.
func SystemBusAddress() string {
	if a := os.Getenv("DBUS_SYSTEM_BUS_ADDRESS"); a != "" {
		return a
	}
	return systemSocket
}

func (w *Watcher) busAddress() string {
	if w.address != "" {
		return w.address
	}
	return SystemBusAddress()
}

// Run connects and keeps the connection until ctx is canceled, and sends
// mixer.SystemResumed after every wake-up. Without a bus, or when the
// connection breaks, it logs a warning and reconnects. It returns ctx's error.
func (w *Watcher) Run(ctx context.Context, out chan<- mixer.Event) error {
	wait := w.retryMin
	failed := false // the last attempt failed; don't repeat the warning
	for {
		addr := w.busAddress()
		conn, err := dbus.Connect(addr, dbus.WithContext(ctx))
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !failed {
				w.log.Warn("cannot connect to the system bus; LEDs are not sent again after sleep, retrying",
					logattr.Error(logattr.ErrSystemBusUnreachable, err))
				failed = true
			} else {
				w.log.Debug("system bus still unreachable", logattr.Error(logattr.ErrSystemBusUnreachable, err),
					logattr.KeyRetryDelay, wait.Seconds())
			}
		} else {
			wait = w.retryMin
			err = w.session(ctx, conn, addr, out)
			_ = conn.Close()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			w.log.Warn("lost the connection to the system bus; reconnecting", logattr.Error(logattr.ErrSystemBusLost, err))
			failed = true // already reported; retries are logged at debug level
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		wait = min(wait*2, w.retryMax)
	}
}

// session follows logind's sleep signal on one connection until it breaks or
// ctx ends.
func (w *Watcher) session(ctx context.Context, conn *dbus.Conn, addr string, out chan<- mixer.Event) error {
	signals := make(chan *dbus.Signal, 16)
	conn.Signal(signals)
	defer conn.RemoveSignal(signals)
	if err := conn.AddMatchSignalContext(ctx, dbus.WithMatchSender(login1Name),
		dbus.WithMatchObjectPath(login1Path), dbus.WithMatchInterface(login1Iface),
		dbus.WithMatchMember(sleepSignal)); err != nil {
		return fmt.Errorf("subscribe to %s: %w", sleepSignal, err)
	}
	// Logged once subscribed: from here on, no wake-up is missed.
	w.log.Info("connected to the system bus", logattr.KeySessionBusAddress, addr)

	var resend <-chan time.Time // the next repeat of SystemResumed
	var next int                // index into w.resend of the repeat after that
	var resumedAt time.Time
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-conn.Context().Done():
			return errors.New("the system bus closed the connection")
		case sig, ok := <-signals:
			if !ok {
				return errors.New("the system bus closed the connection")
			}
			sleeping, ok := sleepState(sig)
			if !ok {
				continue
			}
			if sleeping {
				w.log.Debug("system going to sleep")
				resend = nil // a wake-up that was not over yet does not matter now
				continue
			}
			w.log.Info("system resumed; sending LEDs again")
			if !send(ctx, out) {
				return ctx.Err()
			}
			resumedAt, next, resend = time.Now(), 0, nil
			if len(w.resend) > 0 {
				resend = time.After(w.resend[0])
			}
		case <-resend:
			if !send(ctx, out) {
				return ctx.Err()
			}
			resend = nil
			if next++; next < len(w.resend) {
				resend = time.After(time.Until(resumedAt.Add(w.resend[next])))
			}
		}
	}
}

// sleepState reads a PrepareForSleep signal: true before sleep, false after
// waking up. ok is false for any other signal.
func sleepState(sig *dbus.Signal) (sleeping, ok bool) {
	if sig.Name != login1Iface+"."+sleepSignal || sig.Path != login1Path || len(sig.Body) != 1 {
		return false, false
	}
	sleeping, ok = sig.Body[0].(bool)
	return sleeping, ok
}

func send(ctx context.Context, out chan<- mixer.Event) bool {
	select {
	case out <- mixer.SystemResumed{}:
		return true
	case <-ctx.Done():
		return false
	}
}
