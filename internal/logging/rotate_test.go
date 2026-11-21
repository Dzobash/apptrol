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
// keep, so the archive stays, and the next file is simply opened
// (LOG-07, ADR 0008, #250).
func TestLOG07_RotatesAfterLogFileWasRemoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apptrol.log")
	if err := os.WriteFile(path+".1", []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := openRotatingFile(path, 10, 1)
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
	if got, _ := os.ReadFile(path + ".1"); string(got) != "archive" {
		t.Errorf(".1 = %q, want the archive kept: the removed file had nothing to keep", got)
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
// (LOG-06, LOG-07, ADR 0008, #250).
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

// failOn replaces a file system call for one test, to make one step of a
// rotation fail (LOG-07, ADR 0008, #250).
func failOn[F any](t *testing.T, call *F, replacement F) {
	t.Helper()
	orig := *call
	*call = replacement
	t.Cleanup(func() { *call = orig })
}

// When the full file was moved away but the new one could not be opened, the
// next file rotates at max_size as usual: the back-off is only for a full
// file that stayed in place (LOG-07, ADR 0008, #250).
func TestLOG07_ReopenedFileAfterMoveRotatesAtMaxSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apptrol.log")
	r, err := openRotatingFile(path, 10, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	_, _ = r.Write([]byte("0123456789"))

	broken := true
	failOn(t, &openFile, func(name string, flag int, perm os.FileMode) (*os.File, error) {
		if broken {
			return nil, errors.New("open failed")
		}
		return os.OpenFile(name, flag, perm)
	})
	if _, err := r.Write([]byte("lost\n")); err == nil {
		t.Fatal("Write succeeded although the new file could not be opened")
	}
	broken = false
	_, _ = r.Write([]byte("ab"))
	_, _ = r.Write([]byte("cdefghijk")) // 2 + 9 > 10: rotates
	if got, _ := os.ReadFile(path); string(got) != "cdefghijk" {
		t.Errorf("file = %q, want a rotation at max_size", got)
	}
	if got, _ := os.ReadFile(path + ".1"); string(got) != "ab" {
		t.Errorf(".1 = %q", got)
	}
}

// A rotation that keeps failing after moving .1 to .2 does not remove .2 on
// every retry: the retry only fills the free number (LOG-07, ADR 0008, #250).
func TestLOG07_FailedRetriesKeepTheArchives(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apptrol.log")
	for n, text := range map[string]string{".1": "one", ".2": "two"} {
		if err := os.WriteFile(path+n, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r, err := openRotatingFile(path, 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	_, _ = r.Write([]byte("0123456789"))

	broken := true
	failOn(t, &rename, func(from, to string) error {
		if broken && from == path {
			return errors.New("rename failed")
		}
		return os.Rename(from, to)
	})
	// First try: .2 ("two") is the oldest and goes, .1 moves to .2, then the
	// current file cannot be moved.
	if _, err := r.Write([]byte("a")); err == nil {
		t.Fatal("want the rotation error")
	}
	// Retries after another max_size each, still failing.
	for i := 0; i < 3; i++ {
		_, _ = r.Write([]byte("0123456789"))
	}
	if got, _ := os.ReadFile(path + ".2"); string(got) != "one" {
		t.Errorf(".2 = %q, want the archive kept across failed retries", got)
	}

	broken = false
	_, _ = r.Write([]byte("new")) // the file has reached the next retry point
	if got, _ := os.ReadFile(path + ".2"); string(got) != "one" {
		t.Errorf(".2 after the rotation = %q, want %q", got, "one")
	}
	if got, _ := os.ReadFile(path + ".1"); !strings.HasPrefix(string(got), "0123456789a") {
		t.Errorf(".1 after the rotation = %q, want the full file", got)
	}
	if got, _ := os.ReadFile(path); string(got) != "new" {
		t.Errorf("file = %q", got)
	}
}

// When the files cannot be renamed and no new file can be opened (e.g. the
// folder lost its permissions), the open file keeps the records: the rotation
// gives up the open file only after it moved it away (LOG-07, ADR 0008, #250).
func TestLOG07_FailedShiftKeepsTheOpenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apptrol.log")
	r, err := openRotatingFile(path, 10, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	_, _ = r.Write([]byte("0123456789"))

	failOn(t, &rename, func(string, string) error { return errors.New("rename failed") })
	failOn(t, &openFile, func(string, int, os.FileMode) (*os.File, error) {
		return nil, errors.New("open failed")
	})
	n, err := r.Write([]byte("kept\n"))
	if err == nil {
		t.Error("want the rotation error")
	}
	if n != 5 {
		t.Errorf("Write = %d bytes, want the record written to the open file", n)
	}
	if got, _ := os.ReadFile(path); string(got) != "0123456789kept\n" {
		t.Errorf("file = %q", got)
	}
}

// A log file deleted and created again by hand before the rotation is not
// archived over the real .1: the rotation takes the new file as the current
// one and keeps the archive (LOG-07, ADR 0008, #250).
func TestLOG07_ReplacedLogFileKeepsTheArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apptrol.log")
	if err := os.WriteFile(path+".1", []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := openRotatingFile(path, 10, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	_, _ = r.Write([]byte("0123456789"))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Write([]byte("x")); err != nil {
		t.Fatalf("Write = %v", err)
	}
	if got, _ := os.ReadFile(path + ".1"); string(got) != "archive" {
		t.Errorf(".1 = %q, want the archive kept", got)
	}
	if got, _ := os.ReadFile(path); string(got) != "new\nx" {
		t.Errorf("file = %q, want the record appended to the new file", got)
	}
}

// A current file that disappears during the shift, after the check that it is
// there, costs no archive: the oldest one is replaced only by a rename onto it
// (LOG-07, ADR 0008, #250).
func TestLOG07_FileGoneDuringShiftKeepsTheArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apptrol.log")
	if err := os.WriteFile(path+".1", []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := openRotatingFile(path, 10, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	_, _ = r.Write([]byte("0123456789"))

	failOn(t, &rename, func(from, to string) error {
		if from == path {
			_ = os.Remove(path) // removed by hand at this moment
		}
		return os.Rename(from, to)
	})
	if _, err := r.Write([]byte("new\n")); err != nil {
		t.Fatalf("Write = %v", err)
	}
	if got, _ := os.ReadFile(path + ".1"); string(got) != "archive" {
		t.Errorf(".1 = %q, want the archive kept", got)
	}
	if got, _ := os.ReadFile(path); string(got) != "new\n" {
		t.Errorf("file = %q", got)
	}
}
