package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Dzobash/apptrol/internal/mixer"
)

// fakeDevice replays events and records LED changes.
type fakeDevice struct {
	events []mixer.Event

	mu   sync.Mutex
	leds map[mixer.LED]bool
	sets int
}

func (f *fakeDevice) Run(ctx context.Context, out chan<- mixer.Event) error {
	for _, ev := range f.events {
		out <- ev
	}
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeDevice) SetLED(l mixer.LED, on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.leds[l] = on
	f.sets++
	return nil
}

func (f *fakeDevice) HasLED(l mixer.LED) bool { return l.Transport != mixer.TrackPrev }

func (f *fakeDevice) lit() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, on := range f.leds {
		if on {
			n++
		}
	}
	return n
}

// syncBuf is a bytes.Buffer safe for the test to read while cmdTest writes.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestHW07_Test(t *testing.T) {
	dev := &fakeDevice{leds: map[mixer.LED]bool{}, events: []mixer.Event{
		mixer.ControllerConnected{},
		mixer.ControlMoved{Control: mixer.Control{Kind: mixer.Slider, Column: 3}, Value: 127},
		mixer.ButtonPressed{Button: mixer.ButtonM, Column: 2},
		mixer.ButtonPressed{Button: mixer.ButtonS, Column: 1},
		mixer.ButtonPressed{Button: mixer.ButtonM, Column: 2},
		mixer.TransportPressed{Button: mixer.TrackPrev},
		mixer.TransportPressed{Button: mixer.Play},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	var out syncBuf
	done := make(chan error, 1)
	go func() {
		done <- cmdTest(ctx, "nanoKONTROL2", &out, slog.New(slog.NewTextHandler(io.Discard, nil)),
			func(*slog.Logger, string) device { return dev })
	}()

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "▶") {
		if time.Now().After(deadline) {
			t.Fatalf("output so far:\n%s", out.String())
		}
		time.Sleep(time.Millisecond)
	}
	if n := dev.lit(); n != 2 { // S1 and Play; M2 was toggled twice
		t.Errorf("%d LEDs lit, want 2", n)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if n := dev.lit(); n != 0 {
		t.Errorf("%d LEDs still lit after stopping", n)
	}
	got := out.String()
	for _, want := range []string{
		`sound card id "nanoKONTROL2"`,
		"Controller connected",
		"slider3      127  (100 %)",
		"M2           pressed  LED on",
		"S1           pressed  LED on",
		"M2           pressed  LED off",
		"track◀       pressed  (this button has no LED)",
		"LED mode is Internal",
		"Stopped.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "LED mode is Internal") != 1 {
		t.Error("LED mode hint shown more than once")
	}
}

func TestTestPort(t *testing.T) {
	path := writeConfig(t, "[controller]\nport = \"MyKontrol\"\n\n[layouts.default]\n")
	if got := testPort(path); got != "MyKontrol" {
		t.Errorf("testPort = %q", got)
	}
	if got := testPort(path + ".missing"); got != "nanoKONTROL2" {
		t.Errorf("testPort without config = %q", got)
	}
	if n := len(allLEDs()); n != 3*mixer.NumColumns+len(mixer.AllTransport) {
		t.Errorf("allLEDs has %d", n)
	}
}
