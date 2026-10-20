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
	"log/slog"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jfreymuth/pulse/proto"

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

// addSink creates a silent test output and returns a function that removes it.
func addSink(t *testing.T, name, description string) func() {
	t.Helper()
	return loadModule(t, "module-null-sink", "sink_name="+name,
		"sink_properties=device.description="+description)
}

func TestIntegration_List(t *testing.T) {
	needServer(t)
	name := unique("ListApp")
	play(t, name, "listbin")
	src := unique("list_mic")
	addSource(t, src)
	out := unique("apptrol_test_list_out")
	addSink(t, out, "Apptrol-Test-Output")

	deadline := time.Now().Add(5 * time.Second)
	for {
		l, err := List("")
		if err != nil {
			t.Fatal(err)
		}
		var sawStream, sawSource, sawSink bool
		for _, s := range l.Streams {
			sawStream = sawStream || (s.AppName == name && s.Binary == "listbin" && s.Channels > 0)
		}
		for _, s := range l.Sources {
			sawSource = sawSource || s.Name == src
			if strings.HasSuffix(s.Name, ".monitor") {
				t.Errorf("monitor %q listed as an input", s.Name)
			}
		}
		// The test output is listed with its description; no monitor is
		// listed as an output (OUT-12).
		for _, s := range l.Sinks {
			sawSink = sawSink || (s.Name == out && s.Description == "Apptrol-Test-Output" && s.Channels > 0)
			if strings.HasSuffix(s.Name, ".monitor") {
				t.Errorf("monitor %q listed as an output", s.Name)
			}
		}
		if l.Server == "" {
			t.Errorf("listing lacks the server: %+v", l)
		}
		if sawStream && sawSource && sawSink {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("stream seen %v, source seen %v, output seen %v", sawStream, sawSource, sawSink)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestIntegration_OUT12_OutputDevices adds a silent test output and removes
// it again, and expects the backend to report both. Its monitor, a source,
// is never reported as an input.
func TestIntegration_OUT12_OutputDevices(t *testing.T) {
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

	out := unique("apptrol_test_out")
	// Every event on the way is looked at, so a monitor reported as an input
	// is not skipped unseen.
	noMonitor := func(ev mixer.Event) {
		if e, ok := ev.(mixer.DeviceAdded); ok && strings.HasPrefix(e.Device.Name, out) {
			t.Errorf("the output's monitor was reported as an input: %+v", e)
		}
	}
	remove := addSink(t, out, "Apptrol-Test-Output")
	added := next(t, events, func(e mixer.Event) bool {
		noMonitor(e)
		a, ok := e.(mixer.OutputAdded)
		return ok && a.Device.Name == out
	}).(mixer.OutputAdded)
	if added.Device.Description != "Apptrol-Test-Output" {
		t.Errorf("output description = %q", added.Device.Description)
	}
	remove()
	next(t, events, func(e mixer.Event) bool {
		noMonitor(e)
		r, ok := e.(mixer.OutputRemoved)
		return ok && r.Name == out
	})
}

// waitSink is waitStream for output devices. On failure it also lists every
// volume it saw, in order, to tell a volume never set from one set back.
func waitSink(t *testing.T, name string, ok func(SinkInfo) bool) {
	t.Helper()
	var last SinkInfo
	var seen []int
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		l, err := List("")
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range l.Sinks {
			if s.Name == name {
				if p := percent(s.Volume); len(seen) == 0 || seen[len(seen)-1] != p {
					seen = append(seen, p)
				}
				if last = s; ok(s) {
					return
				}
			}
		}
	}
	t.Fatalf("output %q: last seen %+v; volumes seen (%%): %v", name, last, seen)
}

// testLog sends the backend's log, debug included, to the test's output, so a
// failure on CI shows what the backend saw and did (QA-09).
func testLog(t *testing.T) *slog.Logger {
	return slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimSuffix(string(p), "\n"))
	return len(p), nil
}

// newOutput runs a backend with its log in the test's output, adds a silent
// test output and waits until the backend reports it. Afterwards it acts as
// the mixer would for a control that holds the output: each later
// OutputAdded for it (a new description) is logged and answered with
// KeepOutput (OUT-05, ADR 0027).
func newOutput(t *testing.T, base string) (*Backend, string) {
	t.Helper()
	b := New(testLog(t), "")
	events := make(chan mixer.Event, 256)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.Run(ctx, events) }()
	next(t, events, func(mixer.AudioSnapshot) bool { return true })

	out := unique(base)
	addSink(t, out, "Apptrol-Test-Output")
	first := next(t, events, func(e mixer.OutputAdded) bool { return e.Device.Name == out })
	t.Logf("output added: %q, description %q", first.Device.Name, first.Device.Description)

	mixerDone := make(chan struct{})
	go func() {
		defer close(mixerDone)
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-events:
				if e, ok := ev.(mixer.OutputAdded); ok && e.Device.Name == out {
					t.Logf("output changed: description %q; keeping it", e.Device.Description)
					if err := b.Apply(mixer.KeepOutput{Device: out}); err != nil {
						t.Errorf("Apply(KeepOutput) = %v", err)
					}
				}
			}
		}
	}()
	t.Cleanup(func() {
		cancel()
		<-mixerDone
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("Run = %v", err)
		}
	})
	return b, out
}

// setSinkVolume changes an output's volume from outside Apptrol, as
// WirePlumber restoring a remembered volume or the desktop's applet would.
func setSinkVolume(t *testing.T, name, percent string) {
	t.Helper()
	if out, err := exec.Command("pactl", "set-sink-volume", name, percent).CombinedOutput(); err != nil {
		t.Fatalf("pactl set-sink-volume: %v %s", err, out)
	}
}

// TestIntegration_OUT01_OutputVolume sets the volume of a silent test output
// right after it appeared, and only of it (OUT-01): Apply(SetOutputVolume 0.3)
// makes it read 30 %, as `pactl get-sink-volume` would show. The backend's
// log in the output shows each read of the output and the server's answer.
func TestIntegration_OUT01_OutputVolume(t *testing.T) {
	needServer(t)
	b, out := newOutput(t, "apptrol_test_out")
	if err := b.Apply(mixer.SetOutputVolume{Device: out, Volume: 0.3}); err != nil {
		t.Fatal(err)
	}
	waitSink(t, out, func(s SinkInfo) bool { return near(s.Volume, 0.3) })
	if err := b.Apply(mixer.SetOutputVolume{Device: out + "_missing", Volume: 0.3}); err == nil || !IsGone(err) {
		t.Errorf("Apply on a missing output = %v, want IsGone", err)
	}
}

// TestIntegration_OUT05_GuardSetsTheOutputVolumeAgain: a volume set from
// outside within the new-output guard's window, as WirePlumber restores a
// remembered one, is set back to Apptrol's 30 % (OUT-05); after the window an
// outside change stays (PRIO-02, ADR 0027).
//
//	appears ─ Apply 30 % ─ outside 80 % ─► 30 % again ─┤ 3 s ├─ outside 70 % ─► stays
func TestIntegration_OUT05_GuardSetsTheOutputVolumeAgain(t *testing.T) {
	needServer(t)
	b, out := newOutput(t, "apptrol_test_guard_out")
	// Count the writes of the output's volume, which still go to the server.
	// The server may also restore a volume on its own right after Apptrol's
	// first write (PipeWire sets a new output to 100 %): the guard answers
	// that too, so only the outcome is checked within the window.
	var mu sync.Mutex
	var writes []float64
	b.outputWrite = func(name string, channels int, v float64) error {
		b.mu.Lock()
		k := b.cur
		b.mu.Unlock()
		if k == nil {
			return ErrNotConnected
		}
		mu.Lock()
		writes = append(writes, v)
		mu.Unlock()
		return k.setSinkVolume(name, channels, v)
	}
	written := func() int { mu.Lock(); defer mu.Unlock(); return len(writes) }
	if err := b.Apply(mixer.SetOutputVolume{Device: out, Volume: 0.3}); err != nil {
		t.Fatal(err)
	}
	waitSink(t, out, func(s SinkInfo) bool { return near(s.Volume, 0.3) })

	b.mu.Lock()
	until := b.outGuard[out].until
	b.mu.Unlock()
	if left := time.Until(until); left < time.Second {
		t.Fatalf("only %v of the guard left before the outside change; the server was too slow to test it", left)
	}
	// pactl returns once the server has set 80 %: reading 30 % afterwards
	// means the guard set it again.
	setSinkVolume(t, out, "80%")
	waitSink(t, out, func(s SinkInfo) bool { return near(s.Volume, 0.3) })

	// After the window, a change from outside stays. The read that sees it
	// finds the guard expired and drops it; no write may follow.
	time.Sleep(time.Until(until))
	before := written()
	setSinkVolume(t, out, "70%")
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		b.mu.Lock()
		_, guarded := b.outGuard[out]
		b.mu.Unlock()
		if !guarded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the guard was not dropped after its window")
		}
	}
	if n := written(); n != before {
		mu.Lock()
		t.Errorf("set %v after the guard's window; an outside change must stay", writes[before:])
		mu.Unlock()
	}
	waitSink(t, out, func(s SinkInfo) bool { return near(s.Volume, 0.7) })
}

// TestIntegration_MEDIA10_Cork pauses and resumes a real stream, as a browser
// tab or a player does, and expects the backend to report it as a cork
// change, not as a new stream.
func TestIntegration_MEDIA10_Cork(t *testing.T) {
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

	// pacat cannot pause its stream, so the test creates one over its own
	// connection and corks it with plain requests, as a player does. (The
	// library's high-level PlaybackStream is not used: it has data races of
	// its own, which -race reports.)
	name := unique("TestCork")
	k, err := dial("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(k.shutdown)
	var created proto.CreatePlaybackStreamReply
	if err := k.request(&proto.CreatePlaybackStream{
		SinkIndex:             proto.Undefined,
		ChannelMap:            proto.ChannelMap{proto.ChannelMono},
		SampleSpec:            proto.SampleSpec{Format: proto.FormatFloat32LE, Channels: 1, Rate: 44100},
		ChannelVolumes:        proto.ChannelVolumes{proto.VolumeNorm},
		BufferMaxLength:       proto.Undefined,
		BufferTargetLength:    proto.Undefined,
		BufferPrebufferLength: proto.Undefined,
		BufferMinimumRequest:  proto.Undefined,
		Properties:            proto.PropList{"application.name": proto.PropListString(name)},
	}, &created); err != nil {
		t.Fatal(err)
	}
	// Like a real player, send sound (silence): some PipeWire versions report
	// a stream that has no data yet as corked.
	n := int(created.Missing)
	if n == 0 {
		n = 44100 * 4 / 2 // half a second of mono float32
	}
	if err := k.c.Send(created.StreamIndex, make([]byte, n)); err != nil {
		t.Fatal(err)
	}
	cork := func(corked bool) {
		t.Helper()
		if err := k.request(&proto.CorkPlaybackStream{StreamIndex: created.StreamIndex, Corked: corked}, nil); err != nil {
			t.Fatal(err)
		}
	}

	added := next(t, events, func(e mixer.StreamAdded) bool { return e.Stream.AppName == name })
	id := added.Stream.ID
	if id != created.SinkInputIndex {
		t.Fatalf("stream %d appeared, created %d", id, created.SinkInputIndex)
	}
	// Follow the stream's state through the reported changes, whatever it
	// started as.
	corked := added.Stream.Corked
	wait := func(want bool) {
		t.Helper()
		for corked != want {
			corked = next(t, events, func(e mixer.StreamCorkChanged) bool { return e.ID == id }).Corked
		}
	}

	wait(false) // playing
	cork(true)
	wait(true)
	cork(false)
	wait(false)
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
	// Boost above 100 % (CTRL-03, max_volume).
	if err := b.Apply(mixer.SetStreamVolume{StreamID: id, Volume: 1.5}); err != nil {
		t.Fatal(err)
	}
	waitStream(t, name, func(s StreamInfo) bool { return near(s.Volume, 1.5) })
	// Our own changes do not come back as StreamAdded or StreamMuteChanged.
	time.Sleep(200 * time.Millisecond)
	for len(events) > 0 {
		switch e := (<-events).(type) {
		case mixer.StreamAdded:
			if e.Stream.ID == id {
				t.Errorf("volume change reported as %+v", e)
			}
		case mixer.StreamMuteChanged:
			t.Errorf("Apptrol's own mute reported as %+v", e)
		}
	}

	// A mute change made by someone else is reported (MUTE-07), once the
	// new-stream guard and Apptrol's own change are over.
	time.Sleep(newGuard + ownMute)
	if out, err := exec.Command("pactl", "set-sink-input-mute", fmt.Sprint(id), "0").CombinedOutput(); err != nil {
		t.Fatalf("pactl set-sink-input-mute: %v %s", err, out)
	}
	next(t, events, func(e mixer.StreamMuteChanged) bool { return e.ID == id && !e.Muted })

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
	if len(snap.Outputs) == 0 {
		t.Error("snapshot after reconnect lacks the output devices") // addSource's test output is still there
	}
	if err := b.Apply(mixer.SetStreamVolume{StreamID: id2, Volume: 0.3}); err != nil {
		t.Fatalf("Apply after reconnect: %v", err)
	}
	waitStream(t, name2, func(s StreamInfo) bool { return near(s.Volume, 0.3) })
}
