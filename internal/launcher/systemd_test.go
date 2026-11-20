package launcher

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	sd "github.com/coreos/go-systemd/v22/dbus"
	"github.com/godbus/dbus/v5"
)

func TestLAUNCH04_WaitingPressHasItsOwnDeadline(t *testing.T) {
	sys := &fakeSystemd{}
	s, _, _ := newTestStarter(testApps, sys)
	started, release := make(chan struct{}), make(chan struct{})
	s.connect = func(context.Context) (systemd, error) {
		close(started)
		<-release
		return sys, nil
	}
	first := make(chan struct{})
	go func() {
		defer close(first)
		_, _ = s.conn(t.Context())
	}()
	t.Cleanup(func() {
		close(release)
		select {
		case <-first:
		case <-time.After(5 * time.Second):
			t.Error("first press did not return")
		}
	})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first press did not attempt to connect")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	second := make(chan error, 1)
	go func() {
		_, err := s.conn(ctx)
		second <- err
	}()
	select {
	case err := <-second:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("second press error = %v, want its deadline error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second press remained blocked on the first dial")
	}
}

func TestLAUNCH04_SetupCancellation(t *testing.T) {
	for _, phase := range []string{"Auth", "Hello", "AddMatch"} {
		t.Run(phase, func(t *testing.T) {
			listener, address := privateSystemdBus(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			result := startSystemdSetup(ctx, address)
			calls := acceptSystemdBus(t, listener)
			readAuthLine(t, calls.reader, "\x00AUTH\r\n")
			var signals *testBusPeer
			if phase != "Auth" {
				calls.authenticate(t)
				hello := calls.call(t, "Hello")
				if phase == "AddMatch" {
					calls.reply(t, hello, ":1.1")
					signals = acceptSystemdBus(t, listener)
					readAuthLine(t, signals.reader, "\x00AUTH\r\n")
					signals.authenticate(t)
					signals.reply(t, signals.call(t, "Hello"), ":1.2")
					signals.call(t, "AddMatch")
				}
			}
			cancel()
			got := awaitSystemdSetup(t, result)
			if got.sys != nil || !errors.Is(got.err, context.Canceled) {
				t.Errorf("setup returned a connection = %t, error = %v after cancellation", got.sys != nil, got.err)
			}
			calls.requireClosed(t)
			if signals != nil {
				signals.requireClosed(t)
			}
		})
	}
}

func TestLAUNCH04_ConnectionOutlivesSetupContext(t *testing.T) {
	listener, address := privateSystemdBus(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := startSystemdSetup(ctx, "unix:;"+address)
	calls := acceptSystemdBus(t, listener)
	readAuthLine(t, calls.reader, "\x00AUTH\r\n")
	calls.authenticate(t)
	calls.reply(t, calls.call(t, "Hello"), ":1.1")
	signals := acceptSystemdBus(t, listener)
	readAuthLine(t, signals.reader, "\x00AUTH\r\n")
	signals.authenticate(t)
	signals.reply(t, signals.call(t, "Hello"), ":1.2")
	signals.reply(t, signals.call(t, "AddMatch"))
	got := awaitSystemdSetup(t, result)
	if got.err != nil {
		t.Fatal(got.err)
	}
	cancel()
	queryCtx, done := context.WithTimeout(t.Context(), 5*time.Second)
	defer done()
	checked := make(chan error, 1)
	go func() {
		_, err := got.sys.ListUnitsByPatternsContext(queryCtx, []string{"active"}, []string{"app-example@*"})
		checked <- err
	}()
	calls.reply(t, calls.call(t, "ListUnitsByPatterns"), []sd.UnitStatus{})
	if err := <-checked; err != nil || !got.sys.Connected() {
		t.Fatalf("later query: %v, connected = %t", err, got.sys.Connected())
	}
}

func TestLAUNCH04_SystemdBusAddressTransports(t *testing.T) {
	for _, tc := range []struct {
		address   string
		network   string
		endpoint  string
		nonceFile string
	}{
		{"unix:abstract=example%2Cbus", "unix", "@example,bus", ""},
		{"tcp:host=localhost,port=1234", "tcp", "localhost:1234", ""},
		{"tcp:host=127.0.0.1,port=1234,family=ipv4", "tcp4", "127.0.0.1:1234", ""},
		{"nonce-tcp:host=::1,port=1234,family=ipv6,noncefile=/example/bus%2Cnonce", "tcp6", "[::1]:1234", "/example/bus,nonce"},
	} {
		network, endpoint, nonceFile, err := systemdBusEndpoint(tc.address)
		if network != tc.network || endpoint != tc.endpoint || nonceFile != tc.nonceFile || err != nil {
			t.Errorf("address %q: network = %q, endpoint = %q, nonceFile = %q, error = %v", tc.address, network, endpoint, nonceFile, err)
		}
	}
}

func TestLAUNCH04_InvalidSystemdBusAddresses(t *testing.T) {
	for _, address := range []string{
		"unix:path=/example/bus,abstract=example",
		"tcp:host=localhost",
		"tcp:host=localhost,port=1234,family=invalid",
		"nonce-tcp:host=localhost,port=1234",
		"invalid:host=localhost,port=1234",
	} {
		if _, _, _, err := systemdBusEndpoint(address); err == nil {
			t.Errorf("invalid D-Bus address %q was accepted", address)
		}
	}
}

type systemdSetupResult struct {
	sys systemd
	err error
}

func startSystemdSetup(ctx context.Context, address string) <-chan systemdSetupResult {
	result := make(chan systemdSetupResult, 1)
	go func() {
		sys, err := openSystemd(ctx, address)
		result <- systemdSetupResult{sys, err}
	}()
	return result
}

func awaitSystemdSetup(t *testing.T, result <-chan systemdSetupResult) systemdSetupResult {
	t.Helper()
	select {
	case got := <-result:
		if got.sys != nil {
			t.Cleanup(got.sys.Close)
		}
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("D-Bus setup did not return")
		return systemdSetupResult{}
	}
}

// testBusPeer scripts only the bus exchanges needed by the launcher tests
// (LAUNCH-04, LAUNCH-11, ADR 0019).
type testBusPeer struct {
	socket net.Conn
	reader *bufio.Reader
	sender *dbus.Conn
}

func privateSystemdBus(t *testing.T) (*net.UnixListener, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bus,socket")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if err := listener.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return listener, "unix:path=" + dbus.EscapeBusAddressValue(path)
}

func acceptSystemdBus(t *testing.T, listener *net.UnixListener) *testBusPeer {
	t.Helper()
	socket, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = socket.Close() })
	if err := socket.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	sender, err := dbus.NewConn(socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sender.Close() })
	return &testBusPeer{socket: socket, reader: bufio.NewReader(socket), sender: sender}
}

func readAuthLine(t *testing.T, reader *bufio.Reader, want string) {
	t.Helper()
	if got, err := reader.ReadString('\n'); err != nil || got != want {
		t.Fatalf("D-Bus auth read %q, %v, want %q", got, err, want)
	}
}

func (p *testBusPeer) authenticate(t *testing.T) {
	t.Helper()
	for _, exchange := range []struct{ reply, next string }{
		{"REJECTED EXTERNAL\r\n", "AUTH EXTERNAL\r\n"},
		{"OK 0123456789abcdef0123456789abcdef\r\n", "NEGOTIATE_UNIX_FD\r\n"},
		{"AGREE_UNIX_FD\r\n", "BEGIN\r\n"},
	} {
		if _, err := io.WriteString(p.socket, exchange.reply); err != nil {
			t.Fatal(err)
		}
		readAuthLine(t, p.reader, exchange.next)
	}
}

func (p *testBusPeer) call(t *testing.T, member string) *dbus.Message {
	t.Helper()
	msg, err := dbus.DecodeMessage(p.reader)
	if err != nil {
		t.Fatalf("reading %s: %v", member, err)
	}
	if got := msg.Headers[dbus.FieldMember].Value(); msg.Type != dbus.TypeMethodCall || got != member {
		t.Fatalf("D-Bus message type = %v, member = %v, want call %s", msg.Type, got, member)
	}
	return msg
}

func (p *testBusPeer) reply(t *testing.T, call *dbus.Message, body ...any) {
	t.Helper()
	msg := &dbus.Message{
		Type: dbus.TypeMethodReply,
		Headers: map[dbus.HeaderField]dbus.Variant{
			dbus.FieldReplySerial: dbus.MakeVariant(call.Serial()),
			dbus.FieldSignature:   dbus.MakeVariant(dbus.SignatureOf(body...)),
		},
		Body: body,
	}
	if err := p.sender.Send(msg, nil).Err; err != nil {
		t.Fatal(err)
	}
}

func (p *testBusPeer) requireClosed(t *testing.T) {
	t.Helper()
	if _, err := p.reader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Errorf("D-Bus socket read after canceled setup = %v, want EOF", err)
	}
}
