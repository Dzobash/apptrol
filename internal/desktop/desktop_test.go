package desktop

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/Dzobash/apptrol/internal/mixer"
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

// The next retry is the earliest one, by name when two are due at once; none
// while no player waits (MEDIA-01, ADR 0018, #254).
func TestMEDIA01_TheEarliestWaitingPlayerIsReadFirst(t *testing.T) {
	tr := newTracker(context.Background(), New(slog.New(slog.DiscardHandler), ""), nil)
	if _, _, ok := tr.nextRetry(); ok {
		t.Error("a retry while no player waits")
	}
	now := time.Now()
	tr.pending["c"] = waiting{due: now.Add(time.Second)}
	tr.pending["b"] = waiting{due: now.Add(50 * time.Millisecond)}
	tr.pending["a"] = waiting{due: now.Add(50 * time.Millisecond)}
	if name, at, ok := tr.nextRetry(); !ok || name != "a" || !at.Equal(now.Add(50*time.Millisecond)) {
		t.Errorf("next retry %q at %v, %v; want a, the earliest", name, at, ok)
	}
	// A waiting player whose name goes is forgotten, without an event.
	if evs := tr.remove("a"); evs != nil || len(tr.pending) != 2 {
		t.Errorf("remove of a waiting player: %v, pending %v", evs, tr.pending)
	}
}

// A player that was added and then waits again, because its own queued
// acquisition read it once more after the startup scan, still gets its
// PlayerRemoved when it goes (MEDIA-01, ADR 0018, #254).
func TestMEDIA01_AnAddedPlayerThatWaitsAgainIsStillRemoved(t *testing.T) {
	const owner, name = ":1.7", mprisPrefix + "example"
	tr := newTracker(context.Background(), New(slog.New(slog.DiscardHandler), ""), nil)
	tr.players[name] = mixer.Player{BusName: name}
	tr.owners[owner] = map[string]struct{}{name: {}}
	tr.pending[name] = waiting{owner: owner, reads: 1, due: time.Now()}

	evs := tr.remove(name)
	if want := []mixer.Event{mixer.PlayerRemoved{BusName: name}}; !slices.Equal(evs, want) {
		t.Errorf("remove = %v, want %v", evs, want)
	}
	if len(tr.pending) != 0 || len(tr.players) != 0 || len(tr.owners) != 0 {
		t.Errorf("left behind: pending %v, players %v, owners %v", tr.pending, tr.players, tr.owners)
	}
}
