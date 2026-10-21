package systemd

import (
	"bytes"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// newNotifier returns a Notifier whose records go to the returned buffer.
func newNotifier() (*Notifier, *syncBuf) {
	out := &syncBuf{}
	return New(slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelDebug}))), out
}

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
	n, out := newNotifier()

	n.SetStatus("v0.3.0 · controller connected · 6 controls assigned")
	n.Flush(2 * time.Second)

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	got, _, err := conn.ReadFromUnix(buf)
	if err != nil {
		t.Fatal(err)
	}
	if want := "STATUS=v0.3.0 · controller connected · 6 controls assigned"; string(buf[:got]) != want {
		t.Errorf("message = %q, want %q", buf[:got], want)
	}
	if log := out.String(); !strings.Contains(log, "status line sent") || !strings.Contains(log, "apptrol.component=systemd") {
		t.Errorf("no debug record about the sent line\nlog:\n%s", log)
	}
}

func TestSVC13_NothingIsSentWithoutNotifySocketAndItIsSaidOnce(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")
	n, out := newNotifier()

	n.SetStatus("v0.3.0 · controller not found")
	n.Flush(2 * time.Second)
	n.SetStatus("v0.3.0 · stopping")
	n.Flush(2 * time.Second)

	log := out.String()
	if c := strings.Count(log, "status line not sent; Apptrol was not started by systemd"); c != 1 {
		t.Errorf("the record appeared %d times, want 1\nlog:\n%s", c, log)
	}
	if strings.Contains(log, "status line sent") || strings.Contains(log, "could not send") {
		t.Errorf("unexpected record\nlog:\n%s", log)
	}
}

func TestSVC13_AFailureToSendIsAWarning(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", filepath.Join(t.TempDir(), "gone"))
	n, out := newNotifier()

	n.SetStatus("v0.3.0 · controller not found")
	n.Flush(2 * time.Second)

	log := out.String()
	if !strings.Contains(log, "could not send the status line") || !strings.Contains(log, "error.type=status_not_sent") {
		t.Errorf("no warning with error.type\nlog:\n%s", log)
	}
}

// A systemd that has stopped reading must not hold up the caller: SetStatus
// only hands the text over, the newest text wins, and Flush gives up after its
// time (SVC-13, ADR 0015).
func TestSVC13_ASendThatBlocksNeverHoldsUpTheCaller(t *testing.T) {
	n, _ := newNotifier()
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	var mu sync.Mutex
	var sent []string
	n.send = func(state string) (bool, error) {
		mu.Lock()
		sent = append(sent, state)
		mu.Unlock()
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		return true, nil
	}

	n.SetStatus("a")
	<-started // "a" is being written and stays blocked
	begin := time.Now()
	n.SetStatus("b")
	n.SetStatus("c")
	if d := time.Since(begin); d > time.Second {
		t.Fatalf("SetStatus took %v while a send was blocked", d)
	}

	begin = time.Now()
	n.Flush(50 * time.Millisecond)
	if d := time.Since(begin); d < 40*time.Millisecond || d > time.Second {
		t.Errorf("Flush took %v, want about its 50 ms", d)
	}

	close(release)
	n.Flush(2 * time.Second)
	mu.Lock()
	defer mu.Unlock()
	if want := []string{"STATUS=a", "STATUS=c"}; !reflect.DeepEqual(sent, want) {
		t.Errorf("sent %q, want %q (the newest text wins)", sent, want)
	}
}

// The same with a real socket whose queue is full because nobody reads it. The
// test fills the queue itself first, until its own writes stop making progress,
// so the Notifier's write really would block.
func TestSVC13_AFullUnreadSocketNeverHoldsUpTheCaller(t *testing.T) {
	conn := listen(t) // closed at the end of the test, which ends the blocked writes
	path := conn.LocalAddr().String()

	// A sender is held back by its own send buffer as well as by the receiver's
	// queue, so several senders are needed to fill the receiver's queue.
	var written atomic.Int64
	for i := 0; i < 50; i++ {
		go func() {
			w, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: path, Net: "unixgram"})
			if err != nil {
				return
			}
			defer w.Close()
			for {
				if _, err := w.Write([]byte("STATUS=x")); err != nil {
					return
				}
				written.Add(1)
			}
		}()
	}
	for last, stable := int64(-1), 0; stable < 4; time.Sleep(50 * time.Millisecond) {
		if now := written.Load(); now == last && now > 0 {
			stable++
		} else {
			last, stable = now, 0
		}
	}

	n, _ := newNotifier()
	begin := time.Now()
	for i := 0; i < 100; i++ {
		n.SetStatus("v0.3.0 · controller connected · 6 controls assigned")
	}
	if d := time.Since(begin); d > time.Second {
		t.Errorf("100 calls of SetStatus took %v with the socket full", d)
	}
	begin = time.Now()
	n.Flush(100 * time.Millisecond)
	if d := time.Since(begin); d < 80*time.Millisecond || d > time.Second {
		t.Errorf("Flush took %v, want about its 100 ms (the write must still be blocked)", d)
	}
}
