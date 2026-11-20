package launcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	sd "github.com/coreos/go-systemd/v22/dbus"
	"github.com/godbus/dbus/v5"

	"github.com/Dzobash/apptrol/internal/desktop"
	"github.com/Dzobash/apptrol/internal/logattr"
)

// systemd is the user manager connection the Starter shares between presses
// (LAUNCH-04, LAUNCH-11, ADR 0019). A fake replaces it in tests (QA-06).
type systemd interface {
	StartTransientUnitContext(ctx context.Context, name, mode string, properties []sd.Property, ch chan<- string) (int, error)
	ListUnitsByPatternsContext(ctx context.Context, states, patterns []string) ([]sd.UnitStatus, error)
	Connected() bool
	Close()
}

// conn shares one connection attempt while giving each waiting press its own
// time limit (LAUNCH-04, LAUNCH-11, ADR 0019).
func (s *Starter) conn(ctx context.Context) (systemd, error) {
	for ctx.Err() == nil {
		s.mu.Lock()
		if sys := s.systemd; sys != nil {
			s.mu.Unlock()
			if sys.Connected() {
				return sys, nil
			}
			s.dropConn(sys, errors.New("cached systemd connection is already closed"))
			continue
		}
		if wait := s.connecting; wait != nil {
			s.mu.Unlock()
			select {
			case <-wait:
			case <-ctx.Done():
			}
			continue
		}
		wait := make(chan struct{})
		s.connecting = wait
		s.mu.Unlock()

		sys, err := s.connect(ctx)
		s.mu.Lock()
		if err == nil {
			s.systemd = sys
		}
		s.connecting = nil
		close(wait)
		s.mu.Unlock()
		if err != nil {
			return nil, fmt.Errorf("cannot reach the systemd user manager: %w", err)
		}
		s.log.Debug("connected to the systemd user manager")
		return sys, nil
	}
	return nil, fmt.Errorf("cannot reach the systemd user manager: %w", ctx.Err())
}

// dropConn closes a broken connection only if it is still cached. Another
// press may already have opened a replacement (LAUNCH-04, LAUNCH-11, ADR 0019).
func (s *Starter) dropConn(sys systemd, err error) {
	s.mu.Lock()
	if s.systemd != sys {
		s.mu.Unlock()
		return
	}
	s.systemd = nil
	s.mu.Unlock()
	sys.Close()
	s.log.Debug("dropped the systemd user manager connection", logattr.Error(logattr.ErrDesktopLost, err))
}

// systemdTransportError identifies a broken transport. A job result, a bus
// error reply or a press timing out can occur with both sockets still open
// (LAUNCH-04, LAUNCH-11, ADR 0019).
func systemdTransportError(sys systemd, err error) bool {
	var socketErr *net.OpError
	return err != nil && (!sys.Connected() || errors.Is(err, dbus.ErrClosed) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &socketErr))
}

// connectSystemd uses the existing session bus so a launcher never starts
// a bus (LAUNCH-04, ADR 0019 and ADR 0017).
func connectSystemd(ctx context.Context) (systemd, error) {
	addr, err := desktop.SessionBusAddress()
	if err != nil {
		return nil, err
	}
	return openSystemd(ctx, addr)
}

// openSystemd bounds setup of both sockets by the press's context, including
// go-systemd's registration for job signals. Once setup succeeds, the sockets
// stay open for later presses (LAUNCH-04, LAUNCH-11, ADR 0019).
func openSystemd(ctx context.Context, addr string) (systemd, error) {
	var stopClosing []func() bool
	sys, err := sd.NewConnection(func() (*dbus.Conn, error) {
		socket, stop, err := dialSystemdBus(ctx, addr)
		if err != nil {
			return nil, err
		}
		stopClosing = append(stopClosing, stop)
		return greetSystemdBus(socket)
	})
	for _, stop := range stopClosing {
		stop()
	}
	if ctx.Err() != nil {
		if sys != nil {
			sys.Close()
		}
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	if !sys.Connected() {
		sys.Close()
		return nil, errors.New("systemd connection closed during setup")
	}
	return sys, nil
}

// greetSystemdBus lets godbus perform authentication and Hello while keeping
// its Unix transport for Unix sockets (LAUNCH-04, ADR 0019).
func greetSystemdBus(socket net.Conn) (*dbus.Conn, error) {
	var conn *dbus.Conn
	var err error
	if unix, ok := socket.(*net.UnixConn); ok {
		conn, err = dbus.DialUnix(unix)
	} else {
		conn, err = dbus.NewConn(socket)
	}
	if err != nil {
		_ = socket.Close()
		return nil, err
	}
	err = conn.Auth(nil)
	if err == nil {
		err = conn.Hello()
	}
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// dialSystemdBus tries addresses in bus order. godbus.Dial has no context
// for the socket dial, so the launcher dials each address itself (LAUNCH-04, ADR 0019).
func dialSystemdBus(ctx context.Context, addr string) (net.Conn, func() bool, error) {
	var err error
	for address := range strings.SplitSeq(addr, ";") {
		var socket net.Conn
		var stop func() bool
		socket, stop, err = dialSystemdAddress(ctx, address)
		if err == nil {
			return socket, stop, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, nil, err
}

// dialSystemdAddress keeps a cancellation hook on the raw socket through
// setup. Closing it interrupts reads and writes in Auth, Hello and AddMatch
// (LAUNCH-04, LAUNCH-11, ADR 0019).
func dialSystemdAddress(ctx context.Context, address string) (net.Conn, func() bool, error) {
	network, endpoint, nonceFile, err := systemdBusEndpoint(address)
	if err != nil {
		return nil, nil, err
	}
	var nonce []byte
	if nonceFile != "" {
		nonce, err = os.ReadFile(nonceFile)
		if err != nil {
			return nil, nil, err
		}
	}
	socket, err := (&net.Dialer{}).DialContext(ctx, network, endpoint)
	if err != nil {
		return nil, nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = socket.Close() })
	if len(nonce) != 0 {
		if _, err := socket.Write(nonce); err != nil {
			stop()
			_ = socket.Close()
			return nil, nil, err
		}
	}
	return socket, stop, nil
}

// systemdBusEndpoint decodes the Linux session bus transports used by godbus
// so the socket can be opened with a deadline (LAUNCH-04, ADR 0019).
func systemdBusEndpoint(address string) (string, string, string, error) {
	transport, fields, ok := strings.Cut(address, ":")
	if !ok {
		return "", "", "", errors.New("D-Bus address has no transport")
	}
	values := make(map[string]string)
	for field := range strings.SplitSeq(fields, ",") {
		key, value, ok := strings.Cut(field, "=")
		if _, exists := values[key]; ok && !exists {
			values[key], _ = dbus.UnescapeBusAddressValue(value)
		}
	}
	switch transport {
	case "unix":
		path, abstract := values["path"], values["abstract"]
		if (path == "") == (abstract == "") {
			return "", "", "", errors.New("D-Bus address with unix transport needs either path or abstract")
		}
		if path == "" {
			path = "@" + abstract
		}
		return "unix", path, "", nil
	case "tcp", "nonce-tcp":
		host, port := values["host"], values["port"]
		nonceFile := values["noncefile"]
		if host == "" || port == "" || (transport == "nonce-tcp" && nonceFile == "") {
			return "", "", "", errors.New("TCP D-Bus address is missing host, port or required noncefile")
		}
		if transport == "tcp" {
			nonceFile = ""
		}
		var network string
		switch values["family"] {
		case "":
			network = "tcp"
		case "ipv4":
			network = "tcp4"
		case "ipv6":
			network = "tcp6"
		default:
			return "", "", "", errors.New("TCP D-Bus address has an invalid family")
		}
		return network, net.JoinHostPort(host, port), nonceFile, nil
	default:
		return "", "", "", errors.New("unsupported D-Bus transport")
	}
}
