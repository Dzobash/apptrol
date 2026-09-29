package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// rotatingFile is an io.Writer that rotates the file by size (LOG-07):
// apptrol.log → apptrol.log.1 → … → apptrol.log.<maxFiles>; older files are
// removed. With maxFiles 0 the file is simply truncated when full.
type rotatingFile struct {
	path string

	mu       sync.Mutex
	f        *os.File
	size     int64
	maxSize  int64
	maxFiles int
}

// maxKept is the highest rotated file number ever cleaned up (config allows 100).
const maxKept = 100

func openRotatingFile(path string, maxSize int64, maxFiles int) (*rotatingFile, error) {
	if path == "" {
		return nil, fmt.Errorf("no log file path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	r := &rotatingFile{path: path, maxSize: maxSize, maxFiles: maxFiles}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *rotatingFile) open() error {
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

func (r *rotatingFile) setLimits(maxSize int64, maxFiles int) {
	r.mu.Lock()
	r.maxSize, r.maxFiles = maxSize, maxFiles
	r.mu.Unlock()
}

// Write appends p, rotating first if p would take the file past its limit.
// A single write larger than the limit is still written whole.
func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return 0, os.ErrClosed
	}
	if r.maxSize > 0 && r.size > 0 && r.size+int64(len(p)) > r.maxSize {
		if err := r.rotate(); err != nil {
			return 0, fmt.Errorf("rotating %s: %w", r.path, err)
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *rotatingFile) rotate() error {
	if err := r.f.Close(); err != nil {
		return err
	}
	r.f = nil
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
	if r.maxFiles > 0 {
		if err := os.Rename(r.path, r.numbered(1)); err != nil {
			return err
		}
	} else if err := os.Remove(r.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return r.open()
}

func (r *rotatingFile) numbered(i int) string { return fmt.Sprintf("%s.%d", r.path, i) }

func (r *rotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}
