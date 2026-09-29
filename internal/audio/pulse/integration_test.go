package pulse

// Integration tests against a real audio server (QA-06). They run only when
// APPTROL_PULSE_TEST=1 and need pactl and pacat (pulseaudio-utils). CI starts
// a headless PipeWire for them (QA-09). Locally they run against your own
// PipeWire with `make test-audio`; they add a silent test output and test
// inputs, and remove them again when done.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Dzobash/apptrol/internal/mixer"
)

func needServer(t *testing.T) {
	t.Helper()
	if os.Getenv("APPTROL_PULSE_TEST") != "1" {
		t.Skip("set APPTROL_PULSE_TEST=1 to run against a real audio server")
	}
	for _, tool := range []string{"pactl", "pacat"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("%s is needed: %v", tool, err)
		}
	}
}

// unique makes names that cannot clash with real apps or earlier runs.
func unique(base string) string {
	return fmt.Sprintf("%s-%d", base, time.Now().UnixNano()%1_000_000_000)
}

// play starts a silent playback stream with the given app name and binary.
func play(t *testing.T, name, binary string) *exec.Cmd {
	t.Helper()
	args := []string{"--playback", "--property=application.name=" + name}
	if binary != "" {
		args = append(args, "--property=application.process.binary="+binary)
	}
	cmd := exec.Command("pacat", append(args, "/dev/zero")...)
	cmd.Stderr = os.Stderr // pacat's errors help when a stream never appears
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return cmd
}

// loadModule loads a server module and returns a function that unloads it.
func loadModule(t *testing.T, args ...string) func() {
	t.Helper()
	out, err := exec.Command("pactl", append([]string{"load-module"}, args...)...).Output()
	if err != nil {
		t.Fatalf("pactl load-module %v: %v", args, err)
	}
	id := strings.TrimSpace(string(out))
	done := false
	remove := func() {
		if !done {
			done = true
			_ = exec.Command("pactl", "unload-module", id).Run()
		}
	}
	t.Cleanup(remove)
	return remove
}

// addSource creates a capture device and returns a function that removes it.
// A remapped monitor of a silent test output is a real capture device on
// PipeWire and PulseAudio alike.
func addSource(t *testing.T, name string) func() {
	t.Helper()
	sink := unique("apptrol_test_out")
	loadModule(t, "module-null-sink", "sink_name="+sink)
	return loadModule(t, "module-remap-source", "master="+sink+".monitor", "source_name="+name)
}

// next waits for an event that satisfies ok, skipping others.
func next[T mixer.Event](t *testing.T, events <-chan mixer.Event, ok func(T) bool) T {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev := <-events:
			if e, is := ev.(T); is && ok(e) {
				return e
			}
		case <-timeout:
			var zero T
			t.Fatalf("timed out waiting for %T", zero)
			return zero
		}
	}
}

// waitStream polls until the stream called name satisfies ok. PipeWire
// confirms a change before every client can read it back, so a check right
// after Apply may still see the old value.
func waitStream(t *testing.T, name string, ok func(StreamInfo) bool) {
	t.Helper()
	var last StreamInfo
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		l, err := List("")
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range l.Streams {
			if s.AppName == name {
				if last = s; ok(s) {
					return
				}
			}
		}
	}
	t.Fatalf("stream %q: last seen %+v", name, last)
}

// waitSource is waitStream for capture devices.
func waitSource(t *testing.T, name string, ok func(SourceInfo) bool) {
	t.Helper()
	var last SourceInfo
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		l, err := List("")
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range l.Sources {
			if s.Name == name {
				if last = s; ok(s) {
					return
				}
			}
		}
	}
	t.Fatalf("source %q: last seen %+v", name, last)
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestIntegration_List(t *testing.T) {
	needServer(t)
	name := unique("ListApp")
	play(t, name, "listbin")
	src := unique("list_mic")
	addSource(t, src)

	deadline := time.Now().Add(5 * time.Second)
	for {
		l, err := List("")
		if err != nil {
			t.Fatal(err)
		}
		var sawStream, sawSource bool
		for _, s := range l.Streams {
			sawStream = sawStream || (s.AppName == name && s.Binary == "listbin" && s.Channels > 0)
		}
		for _, s := range l.Sources {
			sawSource = sawSource || s.Name == src
			if strings.HasSuffix(s.Name, ".monitor") {
				t.Errorf("monitor %q listed as an input", s.Name)
			}
		}
		if l.Server == "" || l.Outputs == nil {
			t.Errorf("listing lacks server or outputs: %+v", l)
		}
		if sawStream && sawSource {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("stream seen %v, source seen %v", sawStream, sawSource)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestIntegration_Backend(t *testing.T) {
	needServer(t)
	b := New(quietLog(), "")
	events := make(chan mixer.Event, 256)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.Run(ctx, events) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("Run = %v", err)
		}
	})
	next(t, events, func(mixer.AudioSnapshot) bool { return true })

	// A new app appears (SVC-04 tracking, PRIO-03 relies on this).
	name := unique("TestApp")
	player := play(t, name, "testbin")
	added := next(t, events, func(e mixer.StreamAdded) bool { return e.Stream.AppName == name })
	if added.Stream.Binary != "testbin" {
		t.Errorf("binary = %q", added.Stream.Binary)
	}
	id := added.Stream.ID

	// Volume and mute (CTRL-02, MUTE-01).
	if err := b.Apply(mixer.SetStreamVolume{StreamID: id, Volume: 0.5}); err != nil {
		t.Fatal(err)
	}
	if err := b.Apply(mixer.SetStreamMute{StreamID: id, Muted: true}); err != nil {
		t.Fatal(err)
	}
	waitStream(t, name, func(s StreamInfo) bool { return near(s.Volume, 0.5) && s.Muted })
	// Our own changes do not come back as StreamAdded.
	time.Sleep(200 * time.Millisecond)
	for len(events) > 0 {
		if e, ok := (<-events).(mixer.StreamAdded); ok && e.Stream.ID == id {
			t.Errorf("volume change reported as %+v", e)
		}
	}

	// The app stops.
	_ = player.Process.Kill()
	next(t, events, func(e mixer.StreamRemoved) bool { return e.ID == id })
	if err := b.Apply(mixer.SetStreamMute{StreamID: id}); !IsGone(err) {
		t.Errorf("Apply on a gone stream = %v, want IsGone", err)
	}

	// A capture device appears, is controlled and disappears (CTRL-05).
	src := unique("test_mic")
	remove := addSource(t, src)
	next(t, events, func(e mixer.DeviceAdded) bool { return e.Device.Name == src })
	if err := b.Apply(mixer.SetDeviceVolume{Device: src, Volume: 0.25}); err != nil {
		t.Fatal(err)
	}
	if err := b.Apply(mixer.SetDeviceMute{Device: src, Muted: true}); err != nil {
		t.Fatal(err)
	}
	waitSource(t, src, func(s SourceInfo) bool { return near(s.Volume, 0.25) && s.Muted })
	remove()
	next(t, events, func(e mixer.DeviceRemoved) bool { return e.Name == src })

	// The connection breaks: the backend reconnects and sends everything again (SVC-04).
	name2 := unique("AfterReconnect")
	play(t, name2, "")
	next(t, events, func(e mixer.StreamAdded) bool { return e.Stream.AppName == name2 })
	b.mu.Lock()
	b.cur.shutdown()
	b.mu.Unlock()
	snap := next(t, events, func(mixer.AudioSnapshot) bool { return true })
	var id2 uint32
	for _, s := range snap.Streams {
		if s.AppName == name2 {
			id2 = s.ID
		}
	}
	if id2 == 0 {
		t.Fatalf("snapshot after reconnect lacks %q", name2)
	}
	if err := b.Apply(mixer.SetStreamVolume{StreamID: id2, Volume: 0.3}); err != nil {
		t.Fatalf("Apply after reconnect: %v", err)
	}
	waitStream(t, name2, func(s StreamInfo) bool { return near(s.Volume, 0.3) })
}
