package logging

import (
	"errors"
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

// A log file removed by hand must not stop the rotation: there is nothing to
// keep, and the next file is simply opened (LOG-07, #250).
func TestLOG07_RotatesAfterLogFileWasRemoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apptrol.log")
	r, err := openRotatingFile(path, 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	_, _ = r.Write([]byte("0123456789"))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Write([]byte("after\n")); err != nil {
		t.Fatalf("Write after the file was removed = %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "after\n" {
		t.Errorf("new file = %q, want the record written after the removal", got)
	}
	if _, err := r.Write([]byte("more\n")); err != nil {
		t.Errorf("later Write = %v", err)
	}
}

// A rotation that fails (here: apptrol.log.1 is a folder that cannot be
// removed) keeps the record and the file open, tries again only after the file
// has grown by another max_size, and succeeds once the cause is gone
// (LOG-07, ADR 0008, #250).
func TestLOG07_FailedRotationKeepsLoggingAndRetriesLater(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apptrol.log")
	blocker := path + ".1"
	if err := os.MkdirAll(filepath.Join(blocker, "keep"), 0o750); err != nil {
		t.Fatal(err)
	}
	r, err := openRotatingFile(path, 10, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	_, _ = r.Write([]byte("0123456789"))

	n, err := r.Write([]byte("kept\n")) // rotation fails
	if err == nil || !strings.Contains(err.Error(), "rotating") {
		t.Errorf("Write error = %v, want the rotation error", err)
	}
	if n != 5 {
		t.Errorf("Write = %d bytes, want the record written anyway", n)
	}
	if got, _ := os.ReadFile(path); string(got) != "0123456789kept\n" {
		t.Errorf("file = %q", got)
	}

	// Cause gone, but the file has not grown by max_size since: no new try yet.
	if err := os.RemoveAll(blocker); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Write([]byte("x\n")); err != nil {
		t.Errorf("Write before the retry = %v", err)
	}
	if exists(blocker) {
		t.Error("rotation tried again on the next record; want it only after max_size more")
	}

	// Past the retry point: the rotation runs and succeeds.
	_, _ = r.Write([]byte("0123456789"))
	if _, err := r.Write([]byte("new\n")); err != nil {
		t.Errorf("Write at the retry = %v", err)
	}
	if got, _ := os.ReadFile(blocker); !strings.HasPrefix(string(got), "0123456789kept\n") {
		t.Errorf("%s = %q, want the old file", filepath.Base(blocker), got)
	}
	if got, _ := os.ReadFile(path); string(got) != "new\n" {
		t.Errorf("file after the retry = %q", got)
	}

	_ = r.Close()
	if _, err := r.Write([]byte("x")); !errors.Is(err, os.ErrClosed) {
		t.Errorf("Write after Close = %v, want os.ErrClosed", err)
	}
}

// When even the reopen fails (the log folder was replaced by a file), records
// fail, but the next one after the cause is gone opens the file again
// (LOG-06, LOG-07, #250).
func TestLOG07_ReopensAfterFailedReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "apptrol")
	path := filepath.Join(dir, "apptrol.log")
	r, err := openRotatingFile(path, 10, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	_, _ = r.Write([]byte("0123456789"))

	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Write([]byte("lost\n")); err == nil {
		t.Fatal("Write succeeded although the log folder is a file")
	}

	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Write([]byte("back\n")); err != nil {
		t.Fatalf("Write after the cause was removed = %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "back\n" {
		t.Errorf("file = %q", got)
	}
}
