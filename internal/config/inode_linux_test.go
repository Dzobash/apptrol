package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestNFR11_LinuxDetectsReplacementWithEqualMetadata preserves Linux's inode
// check when the portable fallback is added (CFG-06, NFR-11, ADR 0013).
func TestNFR11_LinuxDetectsReplacementWithEqualMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	replacement := path + ".tmp"
	modTime := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	for _, file := range []string{path, replacement} {
		if err := os.WriteFile(file, []byte("a"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(file, modTime, modTime); err != nil {
			t.Fatal(err)
		}
	}
	before := statFile(path)
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	after := statFile(path)
	if before == after || before.ino == after.ino {
		t.Fatal("replacement with equal size and time was missed")
	}
	before.ino, after.ino = 0, 0
	if before != after {
		t.Fatal("test replacement must preserve all metadata except the inode")
	}
}
