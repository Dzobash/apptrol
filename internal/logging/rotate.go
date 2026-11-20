package logging

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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
	// refs counts records being written right now (acquire, release); retired
	// marks a file a reload replaced, closed when the last of them is done.
	refs    int
	retired bool
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
// (LOG-08): it takes the new limits, makes a pending failed rotation be tried
// on the next write, and reopens the file if a reopen failed earlier. It
// returns os.ErrClosed for a closed file, which the reload then replaces, and
// the open error for a file that still cannot be opened, which the reload
// reports while keeping the file for the next write to reopen
// (ADR 0008, #250).
func (r *rotatingFile) reuse(maxSize int64, maxFiles int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return os.ErrClosed
	}
	r.maxSize, r.maxFiles = maxSize, maxFiles
	r.retryAt = 0
	if r.f == nil {
		return r.open()
	}
	return nil
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
// file was moved out of the way. Whatever step fails, it ends with the current
// path open again (r.f nil only if that open failed too), so one failure does
// not end file logging (LOG-07, ADR 0008, #250).
func (r *rotatingFile) rotate() (moved bool, err error) {
	// Close releases the file even when it reports an error, so the rotation
	// goes on; the error is still returned.
	closeErr := r.f.Close()
	r.f = nil
	moved, shiftErr := r.shift()
	return moved, errors.Join(closeErr, shiftErr, r.open())
}

// shift removes the files beyond the limit and moves the others up by one,
// only as far as the first free number: a rotation that failed after moving
// .1 to .2 left .1 free, so the retry does not remove another file
// (LOG-07, ADR 0008, #250). moved reports whether the current file was moved
// or removed.
func (r *rotatingFile) shift() (moved bool, err error) {
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
	// The current file may be gone (removed by hand): then there is nothing to
	// keep, and the open after the shift starts a new one.
	if err := rename(r.path, r.numbered(1)); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return true, nil
}

func (r *rotatingFile) numbered(i int) string { return fmt.Sprintf("%s.%d", r.path, i) }

// acquire marks a record as being written to the file, so that a reload
// replacing the file closes it only after that record (LOG-08, ADR 0008, #250).
func (r *rotatingFile) acquire() {
	r.mu.Lock()
	r.refs++
	r.mu.Unlock()
}

// release ends acquire, and closes a retired file after its last record
// (LOG-08, ADR 0008, #250).
func (r *rotatingFile) release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refs--
	if r.refs == 0 && r.retired {
		_ = r.closeLocked()
	}
}

// retire closes the file once no record is being written to it any more: at
// once, with the error, or after the last one (LOG-08, ADR 0008, #250).
func (r *rotatingFile) retire() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.retired = true
	if r.refs > 0 {
		return nil
	}
	return r.closeLocked()
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
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}
