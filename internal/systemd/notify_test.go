package systemd

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// listen makes a unix datagram socket like the one systemd gives a service in
// NOTIFY_SOCKET. The folder is short: socket paths are limited to about 100
// characters, which t.TempDir() can exceed.
func listen(t *testing.T) *net.UnixConn {
	t.Helper()
	dir, err := os.MkdirTemp("", "apptrol")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "notify")
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	t.Setenv("NOTIFY_SOCKET", path)
	return conn
}

func TestSVC13_StatusIsSentToTheNotifySocket(t *testing.T) {
	conn := listen(t)

	sent, err := Notifier{}.SetStatus("v0.3.0 · controller connected · 6 controls assigned")
	if err != nil || !sent {
		t.Fatalf("SetStatus = %v, %v; want sent", sent, err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, _, err := conn.ReadFromUnix(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(buf[:n]), "STATUS=v0.3.0 · controller connected · 6 controls assigned"; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

func TestSVC13_NothingIsSentWithoutNotifySocket(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")

	sent, err := Notifier{}.SetStatus("v0.3.0 · controller not found")
	if sent || err != nil {
		t.Errorf("SetStatus = %v, %v; want not sent, no error", sent, err)
	}
}

func TestSVC13_ASocketThatIsGoneIsAnError(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", filepath.Join(t.TempDir(), "gone"))

	sent, err := Notifier{}.SetStatus("v0.3.0 · controller not found")
	if sent || err == nil {
		t.Errorf("SetStatus = %v, %v; want an error", sent, err)
	}
}
