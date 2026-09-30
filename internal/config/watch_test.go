package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func startWatch(t *testing.T, path string, current []byte) chan Change {
	t.Helper()
	out := make(chan Change, 10)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { Watch(ctx, path, current, 5*time.Millisecond, 10*time.Millisecond, out); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return out
}

func nextChange(t *testing.T, out chan Change) Change {
	t.Helper()
	select {
	case c := <-out:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("no change reported")
		return Change{}
	}
}

func noChange(t *testing.T, out chan Change) {
	t.Helper()
	select {
	case c := <-out:
		t.Fatalf("unexpected change %+v", c)
	case <-time.After(80 * time.Millisecond):
	}
}

func TestCFG06_WatchReportsChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(path, []byte("a"), 0o600))
	out := startWatch(t, path, []byte("a"))
	noChange(t, out)

	// Plain write.
	must(os.WriteFile(path, []byte("bb"), 0o600))
	if c := nextChange(t, out); string(c.Data) != "bb" || c.Removed {
		t.Errorf("got %+v", c)
	}

	// Saved without changes: nothing to report.
	later := time.Now().Add(time.Second)
	must(os.Chtimes(path, later, later))
	noChange(t, out)

	// Editor-style save: write a new file and rename it over the old one.
	tmp := path + ".tmp"
	must(os.WriteFile(tmp, []byte("cc"), 0o600))
	must(os.Rename(tmp, path))
	if c := nextChange(t, out); string(c.Data) != "cc" {
		t.Errorf("got %+v", c)
	}

	// Removed, then created again with the same contents.
	must(os.Remove(path))
	if c := nextChange(t, out); !c.Removed {
		t.Errorf("got %+v, want Removed", c)
	}
	must(os.WriteFile(path, []byte("cc"), 0o600))
	if c := nextChange(t, out); string(c.Data) != "cc" {
		t.Errorf("got %+v", c)
	}
}

func TestCFG06_WatchFileCreatedLater(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	out := startWatch(t, path, nil)
	noChange(t, out)
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if c := nextChange(t, out); string(c.Data) != "x" {
		t.Errorf("got %+v", c)
	}
}

func TestCFG06_WatchUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read every file")
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := startWatch(t, path, []byte("x"))
	// Give the watcher time to look at the file first; otherwise, on a busy
	// machine, it may only start after the replacement and see nothing change.
	noChange(t, out)
	// Replace it in one step with a file nobody may read.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte("yy"), 0o000); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	if c := nextChange(t, out); c.Err == nil {
		t.Errorf("got %+v, want an error", c)
	}
}
