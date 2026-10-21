package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestNFR11_MetadataFallback checks saves without using Linux file identity
// (CFG-06, NFR-11, ADR 0013).
func TestNFR11_MetadataFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	baseTime := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	write := func(data string, modTime time.Time) stamp {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatal(err)
		}
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return metadataStamp(fi)
	}
	initial := write("a", baseTime)
	if !initial.exists || initial.ino != 0 {
		t.Fatalf("fallback must track existence without an inode: %+v", initial)
	}
	if got := write("a", baseTime); got != initial {
		t.Fatal("unchanged metadata reported a change")
	}
	if got := write("bb", baseTime); got == initial {
		t.Fatal("size change with the same modification time was missed")
	}
	if got := write("b", baseTime.Add(time.Second)); got == initial {
		t.Fatal("modification time change with the same size was missed")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := statFile(path); got == initial || got.exists {
		t.Fatal("removal was missed")
	}
	if got := write("b", baseTime); got != initial {
		t.Fatal("fallback unexpectedly distinguished replacement with equal size and time")
	}
}
