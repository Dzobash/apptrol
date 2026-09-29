package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestLOG07_RotatesBySizeAndKeepsMaxFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apptrol.log")
	r, err := openRotatingFile(path, 100, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	line := strings.Repeat("x", 39) + "\n" // 40 bytes: two lines fit in 100, three do not
	for i := 0; i < 10; i++ {
		if _, err := r.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{path, path + ".1", path + ".2"} {
		if !exists(p) {
			t.Errorf("%s missing", filepath.Base(p))
		}
		if st, err := os.Stat(p); err == nil && st.Size() > 100 {
			t.Errorf("%s is %d bytes, over the 100-byte limit", filepath.Base(p), st.Size())
		}
	}
	if exists(path + ".3") {
		t.Error("more rotated files kept than max_files")
	}
}

func TestLOG07_MaxFilesZeroTruncates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apptrol.log")
	r, err := openRotatingFile(path, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	_, _ = r.Write([]byte(strings.Repeat("a", 40)))
	_, _ = r.Write([]byte(strings.Repeat("b", 40)))
	if exists(path + ".1") {
		t.Error("rotated file kept although max_files = 0")
	}
	if got, _ := os.ReadFile(path); string(got) != strings.Repeat("b", 40) {
		t.Errorf("file = %q, want only the newest write", got)
	}
}

func TestLOG07_LoweredMaxFilesRemovesOldFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apptrol.log")
	for _, n := range []string{".1", ".2", ".3", ".4"} {
		if err := os.WriteFile(path+n, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r, err := openRotatingFile(path, 10, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	_, _ = r.Write([]byte("0123456789"))
	_, _ = r.Write([]byte("next")) // triggers rotation
	if !exists(path+".1") || exists(path+".2") || exists(path+".4") {
		t.Error("rotation did not trim files beyond the new max_files")
	}
}

func TestLOG07_AppendsToExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apptrol.log")
	if err := os.WriteFile(path, []byte("earlier\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := openRotatingFile(path, 1<<20, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = r.Write([]byte("later\n"))
	_ = r.Close()
	if got, _ := os.ReadFile(path); string(got) != "earlier\nlater\n" {
		t.Errorf("file = %q", got)
	}
	if _, err := r.Write([]byte("x")); err == nil {
		t.Error("write after Close must fail")
	}
}

func TestLOG07_OversizedWriteIsKeptWhole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apptrol.log")
	r, err := openRotatingFile(path, 10, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	big := strings.Repeat("z", 25)
	if n, err := r.Write([]byte(big)); err != nil || n != 25 {
		t.Fatalf("Write = %d, %v", n, err)
	}
	if got, _ := os.ReadFile(path); string(got) != big {
		t.Errorf("oversized record was split or lost: %q", got)
	}
}

func TestOpenRotatingFileNeedsPath(t *testing.T) {
	if _, err := openRotatingFile("", 10, 1); err == nil {
		t.Error("want an error for an empty path")
	}
}
