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
// later. Only Close stops it.
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
}

// maxKept is the highest rotated file number ever cleaned up (config allows 100).
const maxKept = 100

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
// (LOG-06), so logging also comes back after the folder was removed (#250).
func (r *rotatingFile) open() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0o750); err != nil {
		return err
	}
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
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
// (LOG-08): it takes the new limits, reopens the file if a reopen failed
// earlier and makes a pending failed rotation be tried on the next write. It
// fails when the file was closed or cannot be reopened, so the reload opens a
// new one or reports the problem (ADR 0008, #250).
func (r *rotatingFile) reuse(maxSize int64, maxFiles int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return os.ErrClosed
	}
	if r.f == nil {
		if err := r.open(); err != nil {
			return err
		}
	}
	r.maxSize, r.maxFiles = maxSize, maxFiles
	r.retryAt = 0
	return nil
}

// Write appends p, rotating first if p would take the file past its limit.
// A single write larger than the limit is still written whole. If the
// rotation fails, p is still written to the reopened file and the error is
// returned with it; the rotation is tried again once the file has grown by
// another maxSize (LOG-07, #250).
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
		if err := r.rotate(); err != nil {
			rotErr = fmt.Errorf("rotating %s: %w", r.path, err)
			r.retryAt = r.size + r.maxSize
		} else {
			r.retryAt = 0
		}
		if r.f == nil {
			return 0, rotErr
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, errors.Join(rotErr, err)
}

// rotate shifts the files and opens a new one. Whatever step fails, it ends
// with the current path open again (r.f nil only if that open failed too), so
// one failure does not end file logging (#250).
func (r *rotatingFile) rotate() error {
	// Close releases the file even when it reports an error, so the rotation
	// goes on; the error is still returned.
	closeErr := r.f.Close()
	r.f = nil
	shiftErr := r.shift()
	return errors.Join(closeErr, shiftErr, r.open())
}

// shift removes the files beyond the limit and renames the others up by one.
func (r *rotatingFile) shift() error {
	// Remove files beyond the limit (also after max_files was lowered).
	for i := maxKept; i >= r.maxFiles && i >= 1; i-- {
		if err := os.Remove(r.numbered(i)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	// Shift: .n-1 → .n, …, .1 → .2, current → .1
	for i := r.maxFiles - 1; i >= 1; i-- {
		if err := os.Rename(r.numbered(i), r.numbered(i+1)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	// The current file may be gone (removed by hand): then there is nothing to
	// keep, and open below starts a new one.
	if r.maxFiles > 0 {
		if err := os.Rename(r.path, r.numbered(1)); err != nil && !os.IsNotExist(err) {
			return err
		}
	} else if err := os.Remove(r.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (r *rotatingFile) numbered(i int) string { return fmt.Sprintf("%s.%d", r.path, i) }

// Close closes the file; every later Write fails with os.ErrClosed.
func (r *rotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}
