package pulse

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/Dzobash/apptrol/internal/mixer"
)

// Output mute uses the same own-echo attribution as apps; a contrary outside
// value is accepted immediately (OUT-05, MUTE-07, ADR 0027).
func TestOUT05_OutputMuteAttribution(t *testing.T) {
	b := New(testLog(t), "")
	b.outMuteWant = map[string]muteWant{"test": {muted: true, until: time.Now().Add(ownMute)}}
	if b.outsideOutputMute("test", true) {
		t.Fatal("own echo reported as outside")
	}
	if !b.outsideOutputMute("test", false) {
		t.Fatal("outside unmute suppressed")
	}
	b.outMuteWant["test"] = muteWant{muted: true, until: time.Now().Add(ownMute)}
	if !b.outsideOutputMute("test", false) {
		t.Fatal("outside unmute before own echo suppressed")
	}
	if err := b.Apply(mixer.SetOutputMute{Device: "test", Muted: true}); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("disconnected write = %v", err)
	}
}

// M and outside mute cross the actual server boundary. Only a silent test
// output is controlled, and checks wait for state (OUT-05/06, QA-06, ADR 0027).
func TestIntegration_OUT06_OutputMuteAndOutsideChange(t *testing.T) {
	needServer(t)
	b := New(testLog(t), "")
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
	out := unique("apptrol_test_mute_out")
	remove := addSink(t, out, "Apptrol-Test-Mute")
	added := next(t, events, func(e mixer.OutputAdded) bool { return e.Device.Name == out })
	c := mixer.Control{Kind: mixer.Slider, Column: 1}
	m := mixer.New(mixer.Setup{
		Targets:     map[string]mixer.Target{"test": {ID: "test", Kind: mixer.Output, Match: []string{out}}},
		Assignments: map[mixer.Control]string{c: "test"},
	}, mixer.State{})
	apply := func(e mixer.Event) {
		t.Helper()
		for _, a := range m.Handle(e) {
			if err := b.Apply(a); err != nil {
				t.Fatalf("Apply(%T) = %v", a, err)
			}
		}
	}
	apply(added)
	apply(mixer.ButtonPressed{Button: mixer.ButtonM, Column: 1})
	waitSink(t, out, func(s SinkInfo) bool { return s.Muted })
	if result, err := exec.Command("pactl", "set-sink-mute", out, "0").CombinedOutput(); err != nil {
		t.Fatalf("outside unmute: %v %s", err, result)
	}
	changed := next(t, events, func(e mixer.OutputMuteChanged) bool { return e.Name == out })
	if changed.Muted {
		t.Fatal("own mute echo escaped attribution")
	}
	apply(changed)
	if m.Snapshot().Muted[c] {
		t.Fatal("outside unmute did not update saved state")
	}
	apply(mixer.ButtonPressed{Button: mixer.ButtonM, Column: 1})
	waitSink(t, out, func(s SinkInfo) bool { return s.Muted })
	remove()
	apply(next(t, events, func(e mixer.OutputRemoved) bool { return e.Name == out }))
	addSink(t, out, "Apptrol-Test-Mute")
	apply(next(t, events, func(e mixer.OutputAdded) bool { return e.Device.Name == out }))
	waitSink(t, out, func(s SinkInfo) bool { return s.Muted })
	apply(mixer.ButtonPressed{Button: mixer.ButtonM, Column: 1})
	waitSink(t, out, func(s SinkInfo) bool { return !s.Muted })
	if err := b.Apply(mixer.SetOutputMute{Device: out + "_missing", Muted: true}); !IsGone(err) {
		t.Fatalf("missing output write = %v", err)
	}
}
