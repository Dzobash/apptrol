package logging

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// rotatingFile is an io.Writer that rotates the file by size (LOG-07):
// apptrol.log → apptrol.log.1 → … → apptrol.log.<maxFiles>; older files are
// removed. With maxFiles 0 the file is simply truncated when full.
//
// A failed rotation never stops file logging for good (LOG-07, ADR 0008, #250):
// the file is reopened and the record written, and the rotation is tried again
// later. Only Close, or retire once no record is being written, stops it.
type rotatingFile struct {
	path string

	mu       sync.Mutex
	f        *os.File // nil after a failed reopen; the next Write opens it again
	closed   bool     // set by Close only: writes fail from then on
	size     int64
	maxSize  int64
	maxFiles int
	// retryAt is the size the file must reach before a failed rotation is tried
	// again, so a rotation that keeps failing is not repeated on every record
	// (0: no failed rotation pending).
	retryAt int64

	// refMu guards the reference count apart from mu, which is held during
	// file I/O, so a slow write never blocks a reload (LOG-08, ADR 0008, #250).
	// Lock order: mu before refMu; mu is never taken while refMu is held.
	refMu sync.Mutex
	// refs counts records being written right now (acquire, release); retired
	// marks a file a reload replaced, closed when the last of them is done;
	// done marks that close as decided (or made), after which revive and
	// reuse fail.
	refs    int
	retired bool
	done    bool
	// pending holds limits from a reload that the next write takes, so a
	// reload never waits for a write in progress (LOG-08, ADR 0008, #250).
	pending      bool
	pendingSize  int64
	pendingFiles int
}

// maxKept is the highest rotated file number ever cleaned up (config allows 100).
const maxKept = 100

// File system calls, replaced in tests to make one step of a rotation fail.
var (
	openFile = os.OpenFile
	rename   = os.Rename
)

func openRotatingFile(path string, maxSize int64, maxFiles int) (*rotatingFile, error) {
	if path == "" {
		return nil, fmt.Errorf("no log file path")
	}
	r := &rotatingFile{path: path, maxSize: maxSize, maxFiles: maxFiles}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

// open opens r.path for appending, creating its directory if it is missing
// (LOG-06), so logging also comes back after the folder was removed
// (LOG-07, ADR 0008, #250).
func (r *rotatingFile) open() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0o750); err != nil {
		return err
	}
	f, err := openFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	r.f, r.size = f, st.Size()
	return nil
}

// reuse prepares the file for another configuration with the same path
// (LOG-08): it hands over the new limits, makes a pending failed rotation be
// tried on the next write, and reopens the file if a reopen failed earlier. It
// returns os.ErrClosed for a closed file, which the reload then replaces, and
// the open error for a file that still cannot be opened, which the reload
// reports while keeping the file for the next write to reopen. While a write
// is in progress (it may be stuck in the file system), it only hands over the
// limits and does not wait (ADR 0008, #250).
func (r *rotatingFile) reuse(maxSize int64, maxFiles int) error {
	r.refMu.Lock()
	if r.done {
		r.refMu.Unlock()
		return os.ErrClosed
	}
	r.pending, r.pendingSize, r.pendingFiles = true, maxSize, maxFiles
	r.refMu.Unlock()
	if !r.mu.TryLock() {
		return nil
	}
	defer r.mu.Unlock()
	r.takePending()
	if r.closed {
		return os.ErrClosed
	}
	if r.f == nil {
		return r.open()
	}
	return nil
}

// takePending applies limits handed over by reuse; r.mu must be held
// (LOG-08, ADR 0008, #250).
func (r *rotatingFile) takePending() {
	r.refMu.Lock()
	defer r.refMu.Unlock()
	if r.pending {
		r.maxSize, r.maxFiles = r.pendingSize, r.pendingFiles
		r.retryAt = 0
		r.pending = false
	}
}

// Write appends p, rotating first if p would take the file past its limit.
// A single write larger than the limit is still written whole. If the
// rotation fails, p is still written to the reopened file and the error is
// returned with it. While the full file stays in place, the rotation is tried
// again only once it has grown by another maxSize (LOG-07, ADR 0008, #250).
func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, os.ErrClosed
	}
	r.takePending()
	if r.f == nil {
		if err := r.open(); err != nil {
			return 0, fmt.Errorf("reopening %s: %w", r.path, err)
		}
	}
	var rotErr error
	if r.maxSize > 0 && r.size > 0 && r.size+int64(len(p)) > r.maxSize && r.size >= r.retryAt {
		moved, err := r.rotate()
		r.retryAt = 0
		if err != nil {
			rotErr = fmt.Errorf("rotating %s: %w", r.path, err)
			if !moved {
				// r.size is still the full file's size, even if the
				// reopen failed: it is the same file.
				r.retryAt = r.size + r.maxSize
			}
		}
		if r.f == nil {
			return 0, rotErr
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, errors.Join(rotErr, err)
}

// rotate shifts the files and opens a new one. moved tells whether the full
// file was moved out of the way. If the shift fails, the open file stays in
// use, so records keep going to it even when its folder cannot be changed;
// after a move, r.f is nil only if the new file cannot be opened. One failure
// does not end file logging (LOG-07, ADR 0008, #250).
func (r *rotatingFile) rotate() (moved bool, err error) {
	if moved, err := r.shift(); !moved {
		return false, err
	}
	// Close releases the file even when it reports an error, so the rotation
	// goes on; the error is still returned.
	closeErr := r.f.Close()
	r.f = nil
	return true, errors.Join(closeErr, r.open())
}

// shift removes the files beyond the limit and moves the others up by one,
// only as far as the first free number: a rotation that failed after moving
// .1 to .2 left .1 free, so the retry does not remove another file. When the
// path no longer names the open file (removed by hand, with its folder, or
// replaced by another file), there is nothing to keep, so no archive is
// removed for it and the open after the shift takes whatever is at the path
// (LOG-07, ADR 0008, #250). moved reports whether the open file is out of the
// way: moved, removed or already gone from the path.
func (r *rotatingFile) shift() (moved bool, err error) {
	if !r.atPath() {
		return true, nil
	}
	if r.maxFiles <= 0 {
		for i := maxKept; i >= 1; i-- {
			if err := os.Remove(r.numbered(i)); err != nil && !os.IsNotExist(err) {
				return false, err
			}
		}
		if err := os.Remove(r.path); err != nil && !os.IsNotExist(err) {
			return false, err
		}
		return true, nil
	}
	// Remove files beyond the limit (also after max_files was lowered).
	for i := maxKept; i > r.maxFiles; i-- {
		if err := os.Remove(r.numbered(i)); err != nil && !os.IsNotExist(err) {
			return false, err
		}
	}
	// The first free number; with none free, the oldest file goes.
	free := r.maxFiles
	for i := 1; i <= r.maxFiles; i++ {
		if _, err := os.Lstat(r.numbered(i)); os.IsNotExist(err) {
			free = i
			break
		}
	}
	if free == r.maxFiles {
		if err := os.Remove(r.numbered(free)); err != nil && !os.IsNotExist(err) {
			return false, err
		}
	}
	// Shift: .free-1 → .free, …, .1 → .2, current → .1
	for i := free - 1; i >= 1; i-- {
		if err := rename(r.numbered(i), r.numbered(i+1)); err != nil && !os.IsNotExist(err) {
			return false, err
		}
	}
	// The current file may have gone since the check above: then there is
	// nothing to keep, and the open after the shift starts a new one.
	if err := rename(r.path, r.numbered(1)); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return true, nil
}

// atPath reports whether r.path still names the open file. When that cannot
// be told (e.g. no permission to look), it assumes so, and the shift reports
// the problem (LOG-07, ADR 0008, #250).
func (r *rotatingFile) atPath() bool {
	st, err := os.Stat(r.path)
	if os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR) {
		return false
	}
	if err != nil {
		return true
	}
	open, err := r.f.Stat()
	if err != nil {
		return true
	}
	return os.SameFile(st, open)
}

func (r *rotatingFile) numbered(i int) string { return fmt.Sprintf("%s.%d", r.path, i) }

// acquire marks a record as being written to the file, so that a reload
// replacing the file closes it only after that record (LOG-08, ADR 0008, #250).
func (r *rotatingFile) acquire() {
	r.refMu.Lock()
	r.refs++
	r.refMu.Unlock()
}

// release ends acquire, and closes a retired file after its last record
// (LOG-08, ADR 0008, #250).
func (r *rotatingFile) release() {
	r.refMu.Lock()
	r.refs--
	last := r.refs == 0 && r.retired
	if last {
		r.done = true
	}
	r.refMu.Unlock()
	if last {
		_ = r.Close()
	}
}

// retire closes the file once no record is being written to it any more: at
// once, with the error, or after the last one. It does not wait for a write in
// progress (LOG-08, ADR 0008, #250).
func (r *rotatingFile) retire() error {
	r.refMu.Lock()
	r.retired = true
	if r.refs > 0 {
		r.refMu.Unlock()
		return nil
	}
	r.done = true
	r.refMu.Unlock()
	return r.Close()
}

// revive takes back a retired file whose records are still being written,
// when a reload returns to its path, so that one file at a time rotates a
// path (LOG-07, LOG-08, ADR 0008, #250). It fails once the file is closed or
// about to be.
func (r *rotatingFile) revive() bool {
	r.refMu.Lock()
	defer r.refMu.Unlock()
	if r.done {
		return false
	}
	r.retired = false
	return true
}

// draining reports whether the file is retired but not closed yet.
func (r *rotatingFile) draining() bool {
	r.refMu.Lock()
	defer r.refMu.Unlock()
	return r.retired && !r.done
}

// Close closes the file; every later Write fails with os.ErrClosed
// (LOG-07, ADR 0008).
func (r *rotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closeLocked()
}

func (r *rotatingFile) closeLocked() error {
	r.closed = true
	r.refMu.Lock()
	r.done = true
	r.refMu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}
