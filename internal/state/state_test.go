package state

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Dzobash/apptrol/internal/mixer"
)

func ctl(name string) mixer.Control {
	c, ok := mixer.ParseControl(name)
	if !ok {
		panic(name)
	}
	return c
}

func sample() mixer.State {
	return mixer.State{
		Positions: map[mixer.Control]int{ctl("slider1"): 90, ctl("slider8"): 127, ctl("knob1"): 0},
		Muted:     map[mixer.Control]bool{ctl("slider8"): true, ctl("slider3"): true},
	}
}

func TestSTATE01_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", FileName) // directory is created
	if err := Save(path, sample()); err != nil {
		t.Fatal(err)
	}
	got, warnings, err := Load(path)
	if err != nil || len(warnings) > 0 {
		t.Fatalf("Load: %v %v", err, warnings)
	}
	if !reflect.DeepEqual(got, sample()) {
		t.Errorf("got %+v, want %+v", got, sample())
	}
}

func TestSTATE01_FileFormat(t *testing.T) {
	want := `{
  "version": 1,
  "layouts": {
    "default": {
      "knob1": {
        "position": 0
      },
      "slider1": {
        "position": 90
      },
      "slider3": {
        "muted": true
      },
      "slider8": {
        "position": 127,
        "muted": true
      }
    }
  }
}
`
	if got := string(Encode(sample())); got != want {
		t.Errorf("Encode:\n%s\nwant:\n%s", got, want)
	}
}

func TestSTATE02_AtomicWriteLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	for i := 0; i < 3; i++ {
		if err := Save(path, sample()); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != FileName {
		t.Errorf("directory holds %v, want only %s", entries, FileName)
	}
	info, _ := os.Stat(path)
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode %v, want 0600", perm)
	}
}

func TestSTATE02_FailedWriteKeepsOldFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := Save(path, sample()); err != nil {
		t.Fatal(err)
	}
	// A directory in place of the file makes the rename fail.
	blocked := filepath.Join(dir, "blocked")
	if err := os.MkdirAll(filepath.Join(blocked, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Save(blocked, mixer.State{}); err == nil {
		t.Fatal("expected an error")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporary file %s left behind", e.Name())
		}
	}
	if got, _, err := Load(path); err != nil || !reflect.DeepEqual(got, sample()) {
		t.Errorf("old file changed: %v %+v", err, got)
	}
}

func TestSTATE02_UnwritableDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Save(filepath.Join(file, "sub", FileName), sample()); err == nil {
		t.Error("expected an error when the directory cannot be created")
	}
}

func TestSTATE05_MissingFileIsFirstStart(t *testing.T) {
	st, _, err := Load(filepath.Join(t.TempDir(), FileName))
	if !IsFirstStart(err) {
		t.Fatalf("err = %v, want first start", err)
	}
	if len(st.Positions) != 0 || len(st.Muted) != 0 || st.Positions == nil || st.Muted == nil {
		t.Errorf("want an empty, usable state, got %+v", st)
	}
}

func TestSTATE05_DamagedFile(t *testing.T) {
	for name, content := range map[string]string{
		"not json":   "{{{",
		"truncated":  `{"version": 1, "layouts": {"default": {"slider1": {"posi`,
		"no version": `{"layouts": {}}`,
		"newer":      `{"version": 2, "layouts": {}}`,
		"wrong type": `{"version": 1, "layouts": {"default": {"slider1": {"position": "high"}}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), FileName)
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			st, _, err := Load(path)
			if err == nil || IsFirstStart(err) {
				t.Fatalf("err = %v, want a damage error", err)
			}
			if len(st.Positions) != 0 || st.Positions == nil {
				t.Errorf("want an empty, usable state, got %+v", st)
			}
		})
	}
}

func TestSTATE05_BadEntriesAreSkipped(t *testing.T) {
	st, warnings, err := Decode([]byte(`{
	  "version": 1,
	  "future_field": true,
	  "layouts": {
	    "default": {
	      "slider1": {"position": 64},
	      "slider9": {"position": 10},
	      "fader1":  {"position": 10},
	      "slider2": {"position": 200},
	      "knob2":   {"position": 5, "muted": true}
	    },
	    "gaming": {"slider1": {"position": 1}}
	  }
	}`))
	if err != nil {
		t.Fatal(err)
	}
	want := mixer.State{
		Positions: map[mixer.Control]int{ctl("slider1"): 64, ctl("knob2"): 5},
		Muted:     map[mixer.Control]bool{},
	}
	if !reflect.DeepEqual(st, want) {
		t.Errorf("got %+v, want %+v", st, want)
	}
	if len(warnings) != 4 {
		t.Errorf("want 4 warnings, got %q", warnings)
	}
}

func TestSTATE04_Solo(t *testing.T) {
	if strings.Contains(string(Encode(sample())), "solo") {
		t.Error("state without solo mentions solo")
	}
	st := sample()
	st.Solo = 3
	data := Encode(st)
	if !strings.Contains(string(data), `"solo": true`) {
		t.Errorf("solo not written:\n%s", data)
	}
	got, warnings, err := Decode(data)
	if err != nil || len(warnings) > 0 || got.Solo != 3 {
		t.Errorf("Decode = %+v %q %v", got, warnings, err)
	}

	// Solo on a knob is ignored; of several solos the lowest column is kept.
	got, warnings, err = Decode([]byte(`{"version":1,"layouts":{"default":{
		"knob1":{"solo":true},"slider5":{"solo":true},"slider2":{"solo":true}}}}`))
	if err != nil || got.Solo != 2 || len(warnings) != 2 {
		t.Errorf("Decode = %+v %q %v", got, warnings, err)
	}
}

// fakeWriter records writes made by a Saver.
type fakeWriter struct {
	mu     sync.Mutex
	writes [][]byte
	fail   error
}

func (f *fakeWriter) write(_ string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.writes = append(f.writes, data)
	return nil
}

func (f *fakeWriter) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.writes)
}

func newTestSaver(delay time.Duration, onError func(error)) (*Saver, *fakeWriter) {
	s := NewSaver("unused", delay, onError)
	w := &fakeWriter{}
	s.write = w.write
	return s, w
}

func withPosition(v int) mixer.State {
	return mixer.State{Positions: map[mixer.Control]int{ctl("slider1"): v}, Muted: map[mixer.Control]bool{}}
}

func TestSTATE02_SaverCoalescesBursts(t *testing.T) {
	s, w := newTestSaver(50*time.Millisecond, nil)
	for v := 0; v <= 127; v++ { // one slider sweep
		s.Request(withPosition(v))
	}
	if w.count() != 0 {
		t.Fatal("wrote before the delay")
	}
	waitFor(t, func() bool { return w.count() == 1 })
	time.Sleep(100 * time.Millisecond)
	if n := w.count(); n != 1 {
		t.Fatalf("%d writes, want 1", n)
	}
	st, _, _ := Decode(w.writes[0])
	if st.Positions[ctl("slider1")] != 127 {
		t.Errorf("wrote %v, want the last position", st.Positions)
	}
}

func TestSTATE02_SaverFlush(t *testing.T) {
	s, w := newTestSaver(time.Hour, nil)
	if err := s.Flush(); err != nil || w.count() != 0 {
		t.Fatal("Flush with nothing pending wrote something")
	}
	s.Request(withPosition(10))
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	if w.count() != 1 {
		t.Fatalf("%d writes, want 1", w.count())
	}
	// Unchanged state is not written again.
	s.Request(withPosition(10))
	_ = s.Flush()
	if w.count() != 1 {
		t.Errorf("unchanged state written again")
	}
}

func TestSTATE02_SaverSkipsWhatWasLoaded(t *testing.T) {
	s, w := newTestSaver(time.Hour, nil)
	s.Loaded(withPosition(42))
	s.Request(withPosition(42))
	_ = s.Flush()
	if w.count() != 0 {
		t.Error("wrote the state that was just loaded")
	}
}

func TestSTATE02_SaverRetriesAfterError(t *testing.T) {
	var mu sync.Mutex
	var errs []error
	s, w := newTestSaver(10*time.Millisecond, func(err error) {
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	})
	w.fail = errors.New("disk full")
	s.Request(withPosition(1))
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(errs) == 1 })

	w.mu.Lock()
	w.fail = nil
	w.mu.Unlock()
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	if w.count() != 1 {
		t.Errorf("failed state not retried on Flush")
	}
}

func TestSTATE02_SaverRealFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	s := NewSaver(path, time.Hour, nil)
	if s.Path() != path {
		t.Errorf("Path() = %q", s.Path())
	}
	s.Request(sample())
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	if got, _, err := Load(path); err != nil || !reflect.DeepEqual(got, sample()) {
		t.Errorf("Load after Flush: %v %+v", err, got)
	}
}

func TestSaverConcurrentUse(t *testing.T) {
	s, _ := newTestSaver(time.Millisecond, nil)
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for v := 0; v < 200; v++ {
				s.Request(withPosition((v + g) % 128))
				if v%50 == 0 {
					_ = s.Flush()
				}
			}
		}(g)
	}
	wg.Wait()
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func FuzzDecode(f *testing.F) {
	f.Add(Encode(sample()))
	f.Add([]byte(`{"version":1,"layouts":{"default":{"knob8":{"position":127}}}}`))
	f.Add([]byte(`{"version":1,"layouts":{"default":{"slider0":{"muted":true}}}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		st, _, err := Decode(data)
		if st.Positions == nil || st.Muted == nil {
			t.Fatal("Decode returned nil maps")
		}
		if err != nil {
			return
		}
		for c, p := range st.Positions {
			if !c.Valid() || p < 0 || p > mixer.MaxValue {
				t.Fatalf("invalid entry %v=%d", c, p)
			}
		}
		for c := range st.Muted {
			if !c.Valid() || c.Kind != mixer.Slider {
				t.Fatalf("invalid mute %v", c)
			}
		}
		// What was accepted must survive a round trip unchanged.
		again, warnings, err := Decode(Encode(st))
		if err != nil || len(warnings) > 0 || !reflect.DeepEqual(again, st) {
			t.Fatalf("round trip changed the state: %v %v", err, warnings)
		}
	})
}
