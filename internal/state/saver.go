package state

import (
	"bytes"
	"sync"
	"time"

	"github.com/Dzobash/apptrol/internal/mixer"
)

// DefaultDelay is how long the Saver waits after a change before writing. It
// keeps writes to at most one per second while a slider is moved (STATE-02).
const DefaultDelay = time.Second

// Saver writes the state in the background, at most once per delay.
//
// The first change after a write starts a timer; changes that arrive before
// it fires only replace the pending state. When the timer fires the latest
// state is written. While a slider moves continuously the file is therefore
// written once per delay, and never later than delay after a change.
//
// A write whose contents equal the last write is skipped. Saver is safe for
// concurrent use.
type Saver struct {
	path    string
	delay   time.Duration
	onError func(error) // called from the timer goroutine

	mu      sync.Mutex
	pending []byte      // encoded state waiting to be written, nil if none
	timer   *time.Timer // non-nil while a write is scheduled

	writeMu sync.Mutex // serializes writes
	last    []byte     // contents of the last successful write

	write func(path string, data []byte) error // replaced in tests
}

// NewSaver returns a Saver for the file at path. onError is called when a
// background write fails; it may be nil.
func NewSaver(path string, delay time.Duration, onError func(error)) *Saver {
	if onError == nil {
		onError = func(error) {}
	}
	return &Saver{path: path, delay: delay, onError: onError, write: writeAtomic}
}

// Path returns the state file path.
func (s *Saver) Path() string { return s.path }

// Loaded tells the Saver what the file currently holds, so an unchanged
// state is not written again right after start.
func (s *Saver) Loaded(st mixer.State) {
	s.writeMu.Lock()
	s.last = Encode(st)
	s.writeMu.Unlock()
}

// Request schedules st to be written. It returns immediately.
func (s *Saver) Request(st mixer.State) {
	data := Encode(st)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = data
	if s.timer == nil {
		s.timer = time.AfterFunc(s.delay, s.fire)
	}
}

func (s *Saver) fire() {
	if err := s.flush(false); err != nil {
		s.onError(err)
	}
}

// Flush writes any pending state now and cancels the timer. The service
// calls it on shutdown (SVC-05). It returns the write error, if any.
func (s *Saver) Flush() error { return s.flush(true) }

func (s *Saver) flush(stopTimer bool) error {
	// Hold writeMu across taking the pending data and writing it, so a
	// Flush that races with the timer never writes an older state last.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	s.mu.Lock()
	data := s.pending
	s.pending = nil
	if s.timer != nil {
		if stopTimer {
			s.timer.Stop()
		}
		s.timer = nil
	}
	s.mu.Unlock()

	if data == nil || bytes.Equal(data, s.last) {
		return nil
	}
	if err := s.write(s.path, data); err != nil {
		// Keep the data so the next change or Flush tries again.
		s.mu.Lock()
		if s.pending == nil {
			s.pending = data
		}
		s.mu.Unlock()
		return err
	}
	s.last = data
	return nil
}
