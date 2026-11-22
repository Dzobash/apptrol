package desktop

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func TestDESK01_BusAddress(t *testing.T) {
	b := New(slog.New(slog.DiscardHandler), "")
	dir := t.TempDir()

	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/run/user/example/bus")
	t.Setenv("XDG_RUNTIME_DIR", dir)
	if a, err := b.busAddress(); err != nil || a != "unix:path=/run/user/example/bus" {
		t.Errorf("from the environment: %q, %v", a, err)
	}

	// Without the variable: the standard socket, if it exists.
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	if _, err := b.busAddress(); err == nil {
		t.Error("no socket: want an error, never a bus started for us")
	}
	socket := filepath.Join(dir, "bus")
	if err := os.WriteFile(socket, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if a, err := b.busAddress(); err != nil || a != "unix:path="+socket {
		t.Errorf("from XDG_RUNTIME_DIR: %q, %v", a, err)
	}

	// An address given to New wins.
	if a, _ := New(slog.New(slog.DiscardHandler), "unix:path=/x").busAddress(); a != "unix:path=/x" {
		t.Errorf("given address: %q", a)
	}
}

// Only the answers of a player that is not ready yet lead to another read; a
// timeout, no reply or a name nobody owns does not (MEDIA-01, ADR 0018, #254).
func TestMEDIA01_OnlyNotReadyAnswersAreReadAgain(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{dbus.Error{Name: "org.freedesktop.DBus.Error.NoSuchObject"}, true},
		{&dbus.Error{Name: "org.freedesktop.DBus.Error.UnknownObject"}, true},
		{fmt.Errorf("read: %w", dbus.Error{Name: "org.freedesktop.DBus.Error.InvalidArgs"}), true},
		{dbus.Error{Name: "org.freedesktop.DBus.Error.UnknownInterface"}, true},
		{dbus.Error{Name: "org.freedesktop.DBus.Error.UnknownMethod"}, true},
		{dbus.Error{Name: "org.freedesktop.DBus.Properties.Error.InterfaceNotFound"}, true},
		{dbus.Error{Name: "org.freedesktop.DBus.Error.UnknownProperty"}, true},
		{dbus.Error{Name: "org.freedesktop.DBus.Properties.Error.PropertyNotFound"}, true},
		{dbus.Error{Name: "org.freedesktop.DBus.Error.NoReply"}, false},
		{dbus.Error{Name: "org.freedesktop.DBus.Error.NameHasNoOwner"}, false},
		{context.DeadlineExceeded, false},
	} {
		if got := notReady(tc.err); got != tc.want {
			t.Errorf("notReady(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

// The next retry is the earliest one; none while no player waits (MEDIA-01,
// ADR 0018, #254).
func TestMEDIA01_TheEarliestWaitingPlayerIsReadFirst(t *testing.T) {
	tr := newTracker(context.Background(), New(slog.New(slog.DiscardHandler), ""), nil)
	if _, ok := tr.nextRetry(); ok {
		t.Error("a retry while no player waits")
	}
	now := time.Now()
	tr.pending["b"] = waiting{due: now.Add(time.Second)}
	tr.pending["a"] = waiting{due: now.Add(50 * time.Millisecond)}
	if at, ok := tr.nextRetry(); !ok || !at.Equal(now.Add(50*time.Millisecond)) {
		t.Errorf("next retry at %v, %v; want the earliest", at, ok)
	}
	// A waiting player whose name goes is forgotten, without an event.
	if evs := tr.remove("a"); evs != nil || len(tr.pending) != 1 {
		t.Errorf("remove of a waiting player: %v, pending %v", evs, tr.pending)
	}
}
