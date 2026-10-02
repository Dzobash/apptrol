package desktop

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
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
