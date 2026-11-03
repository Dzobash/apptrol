package pulse

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/Dzobash/apptrol/internal/mixer"
)

// setDefaultSource waits for the server's observed default, rather than
// treating pactl's successful request as applied state (INPUT-09, QA-09,
// ADR 0038). Cleanup uses the same check to verify restoration.
func setDefaultSource(t *testing.T, name string) {
	t.Helper()
	if out, err := exec.Command("pactl", "set-default-source", name).CombinedOutput(); err != nil {
		t.Fatalf("set default input: %v %s", err, out)
	}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		l, err := List("")
		if err != nil {
			t.Fatal(err)
		}
		if l.DefaultSource == name {
			return
		}
	}
	t.Fatal("default input did not settle at the requested device")
}

// This switches the real default only between silent test microphones and
// restores the original even on failure (INPUT-09, QA-06, ADR 0038).
func TestIntegration_INPUT09_DefaultInputFollowsASwitch(t *testing.T) {
	needServer(t)
	before, err := List("")
	if err != nil {
		t.Fatal(err)
	}
	a, z := unique("apptrol_test_input_a"), unique("apptrol_test_input_b")
	addSource(t, a)
	addSource(t, z)
	t.Cleanup(func() {
		if before.DefaultSource != "" {
			setDefaultSource(t, before.DefaultSource)
		}
	})
	setDefaultSource(t, a)
	b := New(testLog(t), "")
	events := make(chan mixer.Event, 256)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.Run(ctx, events) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("Run: %v", err)
		}
	})
	full := next(t, events, func(mixer.AudioSnapshot) bool { return true })
	if full.DefaultInput != a {
		t.Fatal("snapshot missed default input")
	}
	var devices []mixer.Device
	for _, d := range full.Devices {
		if d.Name == a || d.Name == z {
			devices = append(devices, d)
		}
	}
	c := mixer.Control{Kind: mixer.Slider, Column: 8}
	s := mixer.Setup{Targets: map[string]mixer.Target{"mic": {ID: "mic", Kind: mixer.Input, FollowsDefault: true}}, Assignments: map[mixer.Control]string{c: "mic"}}
	m := mixer.New(s, mixer.State{})
	apply := func(e mixer.Event) {
		t.Helper()
		for _, action := range m.Handle(e) {
			if err := b.Apply(action); err != nil {
				t.Fatalf("Apply(%T): %v", action, err)
			}
		}
	}
	apply(mixer.AudioSnapshot{Devices: devices, DefaultInput: a})
	apply(mixer.ControlMoved{Control: c, Value: 38})
	apply(mixer.ButtonPressed{Button: mixer.ButtonM, Column: 8})
	waitSource(t, a, func(d SourceInfo) bool { return d.Muted && near(d.Volume, 38.0/127) })
	setDefaultSource(t, z)
	apply(next(t, events, func(e mixer.DefaultInputChanged) bool { return e.Name == z }))
	waitSource(t, z, func(d SourceInfo) bool { return d.Muted })
	waitSource(t, a, func(d SourceInfo) bool { return !d.Muted && near(d.Volume, 38.0/127) })
	s.Buttons = map[mixer.LED]mixer.Button{{Button: mixer.ButtonM, Column: 8}: {Mode: mixer.ModeHoldToTalk}}
	apply(mixer.ConfigChanged{Setup: s})
	apply(mixer.ButtonPressed{Button: mixer.ButtonM, Column: 8})
	waitSource(t, z, func(d SourceInfo) bool { return !d.Muted })
	setDefaultSource(t, a)
	apply(next(t, events, func(e mixer.DefaultInputChanged) bool { return e.Name == a }))
	waitSource(t, a, func(d SourceInfo) bool { return d.Muted })
	waitSource(t, z, func(d SourceInfo) bool { return !d.Muted })
	apply(mixer.ButtonReleased{Button: mixer.ButtonM, Column: 8})
	apply(mixer.ButtonPressed{Button: mixer.ButtonM, Column: 8})
	waitSource(t, a, func(d SourceInfo) bool { return !d.Muted })
}

// A fresh default must defend reported volume/mute restores, and releasing
// it must stop those writes (INPUT-09, PRIO-03, ADR 0038).
func TestIntegration_INPUT09_NewDefaultDefendsReportedRestores(t *testing.T) {
	needServer(t)
	before, err := List("")
	if err != nil {
		t.Fatal(err)
	}
	b := New(testLog(t), "")
	events := make(chan mixer.Event, 256)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.Run(ctx, events) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("Run: %v", err)
		}
	})
	next(t, events, func(mixer.AudioSnapshot) bool { return true })
	name := unique("apptrol_test_input_new")
	addSource(t, name)
	t.Cleanup(func() {
		if before.DefaultSource != "" {
			setDefaultSource(t, before.DefaultSource)
		}
	})
	d := next(t, events, func(e mixer.DeviceAdded) bool { return e.Device.Name == name }).Device
	setDefaultSource(t, name)
	// The server may already have made a new source the default before its
	// DeviceAdded. The switch path is tested above; here the guard must work
	// in either notification order (INPUT-09, ADR 0038).
	c := mixer.Control{Kind: mixer.Slider, Column: 8}
	m := mixer.New(mixer.Setup{Targets: map[string]mixer.Target{"mic": {ID: "mic", Kind: mixer.Input, FollowsDefault: true}}, Assignments: map[mixer.Control]string{c: "mic"}}, mixer.State{Muted: map[mixer.Control]bool{c: true}})
	apply := func(e mixer.Event) {
		t.Helper()
		for _, a := range m.Handle(e) {
			if err := b.Apply(a); err != nil {
				t.Fatalf("Apply(%T): %v", a, err)
			}
		}
	}
	apply(mixer.AudioSnapshot{Devices: []mixer.Device{d}, DefaultInput: name})
	apply(mixer.ControlMoved{Control: c, Value: 38})
	waitSource(t, name, func(d SourceInfo) bool { return d.Muted && near(d.Volume, 38.0/127) })
	b.mu.Lock()
	g := b.devGuard[name]
	inWindow := g != nil && time.Until(g.until) > time.Second
	b.mu.Unlock()
	if !inWindow {
		t.Skip("server took too long to act within the new-input guard window")
	}
	if err := exec.Command("pactl", "set-source-volume", name, "90%").Run(); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("pactl", "set-source-mute", name, "0").Run(); err != nil {
		t.Fatal(err)
	}
	waitSource(t, name, func(d SourceInfo) bool { return d.Muted && near(d.Volume, 38.0/127) })
	apply(mixer.DefaultInputChanged{})
	if err := exec.Command("pactl", "set-source-volume", name, "70%").Run(); err != nil {
		t.Fatal(err)
	}
	waitSource(t, name, func(d SourceInfo) bool { return !d.Muted && near(d.Volume, 0.7) })
}
