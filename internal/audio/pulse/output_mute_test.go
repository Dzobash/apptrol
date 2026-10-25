package pulse

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Dzobash/apptrol/internal/mixer"
)

// Output mute uses the same own-echo attribution as apps; a contrary outside
// value is accepted immediately (OUT-05, MUTE-07, ADR 0027).
func TestOUT05_OutputMuteAttribution(t *testing.T) {
	b := New(testLog(t), "")
	b.outMuteWant = map[string]muteWant{"test": {muted: true, until: time.Now().Add(ownMute)}}
	if b.outsideOutputMute("test", true, muteHold{}) {
		t.Fatal("own echo reported as outside")
	}
	if !b.outsideOutputMute("test", false, muteHold{}) {
		t.Fatal("outside unmute suppressed")
	}
	b.outMuteWant["test"] = muteWant{muted: true, until: time.Now().Add(ownMute)}
	if !b.outsideOutputMute("test", false, muteHold{}) {
		t.Fatal("outside unmute before own echo suppressed")
	}
	if err := b.Apply(mixer.SetOutputMute{Device: "test", Muted: true}); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("disconnected write = %v", err)
	}
}

// A mute read belongs to the owner before a description change, never to a
// newly matched control whose saved mute is applied afterwards (OUT-05, ADR 0035).
func TestOUT05_DescriptionAndMuteChangePreserveOwnersState(t *testing.T) {
	for _, tc := range []struct {
		name, before, after string
		want                bool
	}{
		{"newly matches", "Other", "Test speakers", true},
		{"still matches", "Test speakers", "Test speakers renamed", false},
		{"no longer matches", "Test speakers", "Other", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := mixer.Control{Kind: mixer.Slider, Column: 7}
			old := mixer.Device{Name: "test-output", Description: tc.before}
			m := mixer.New(mixer.Setup{
				Targets:     map[string]mixer.Target{"test": {ID: "test", Kind: mixer.Output, Match: []string{"speakers"}}},
				Assignments: map[mixer.Control]string{c: "test"},
			}, mixer.State{Muted: map[mixer.Control]bool{c: true}})
			m.Handle(mixer.AudioSnapshot{Outputs: []mixer.Device{old}})
			tr := &tracker{outputs: map[uint32]mixer.Device{1: old}, outMute: map[uint32]bool{1: true}}
			now := SinkInfo{Device: mixer.Device{Name: old.Name, Description: tc.after}, Index: 1, Muted: false}
			events, err := outputChange(tr, 1, false, func(uint32) (SinkInfo, error) { return now, nil })
			if err != nil {
				t.Fatal(err)
			}
			deviceMuted := now.Muted
			for _, e := range events {
				for _, a := range m.Handle(e) {
					if mute, ok := a.(mixer.SetOutputMute); ok {
						deviceMuted = mute.Muted
					}
				}
			}
			if saved := m.Snapshot().Muted[c]; saved != tc.want || deviceMuted != tc.want {
				t.Fatalf("saved mute=%v, device mute=%v, want both %v", saved, deviceMuted, tc.want)
			}
			if tc.name == "newly matches" {
				for _, a := range m.Handle(mixer.ButtonPressed{Button: mixer.ButtonM, Column: 7}) {
					if mute, ok := a.(mixer.SetOutputMute); ok {
						deviceMuted = mute.Muted
					}
				}
				if deviceMuted {
					t.Fatal("one M press did not unmute newly owned output")
				}
			}
		})
	}
}

// muteRig drives the adapter's output path without a server: report hands it
// a read as a change notification would, and writes and mute writes collect
// what it set (OUT-05, QA-11, #177).
type muteRig struct {
	t          *testing.T
	b          *Backend
	tr         *tracker
	d          SinkInfo
	writes     []float64
	muteWrites []bool
}

func newMuteRig(t *testing.T, log *slog.Logger) *muteRig {
	r := &muteRig{t: t, b: New(log, ""), tr: &tracker{outputs: map[uint32]mixer.Device{}, outMute: map[uint32]bool{}},
		d: SinkInfo{Device: mixer.Device{Name: "spk", Description: "Speakers"}, Index: 1, Volume: 1, Channels: 2}}
	r.b.cur = &conn{} // never used: reads and writes go through the hooks
	r.b.outChans, r.b.outGuard, r.b.outMuteWant = map[string]int{}, map[string]*guard{}, map[string]muteWant{}
	r.b.outputWrite = func(_ string, _ int, v float64) error { r.writes = append(r.writes, v); return nil }
	r.b.outputMuteWrite = func(_ string, m bool) error { r.muteWrites = append(r.muteWrites, m); return nil }
	return r
}

// report hands the adapter the output as it reads now, as session does for a
// change notification, and returns the events it sends the mixer.
func (r *muteRig) report() []mixer.Event {
	r.t.Helper()
	r.b.wire.Lock()
	defer r.b.wire.Unlock()
	evs, err := r.b.sinkEvent(r.b.cur, r.tr, r.d.Index, false, func(uint32) (SinkInfo, error) { return r.d, nil })
	if err != nil {
		r.t.Fatal(err)
	}
	return evs
}

// remove hands the adapter the output's removal.
func (r *muteRig) remove() []mixer.Event {
	r.t.Helper()
	r.b.wire.Lock()
	defer r.b.wire.Unlock()
	evs, err := r.b.sinkEvent(r.b.cur, r.tr, r.d.Index, true, nil)
	if err != nil {
		r.t.Fatal(err)
	}
	return evs
}

func (r *muteRig) apply(a mixer.Action) {
	r.t.Helper()
	if err := r.b.Apply(a); err != nil {
		r.t.Fatal(err)
	}
}

// appear reports the output new and answers as the mixer does for a control
// whose saved mute is on: the mute, then KeepOutput for that read.
func (r *muteRig) appear() mixer.Device {
	r.t.Helper()
	evs := r.report()
	if len(evs) != 1 {
		r.t.Fatalf("appearing output reported %v", evs)
	}
	added := evs[0].(mixer.OutputAdded).Device
	r.apply(mixer.SetOutputMute{Device: added.Name, Muted: true})
	r.apply(mixer.KeepOutput{Device: added.Name, Generation: added.Generation})
	return added
}

func muteChanges(evs []mixer.Event) []bool {
	var got []bool
	for _, e := range evs {
		if c, ok := e.(mixer.OutputMuteChanged); ok {
			got = append(got, c.Muted)
		}
	}
	return got
}

// The regression of #177: a device appears, Apptrol writes the control's
// saved mute, the echo comes, and then WirePlumber restores the mute it
// remembered. Within the new-output guard's window, that is the server's
// restore, not an outside change: the mixer hears nothing, and the saved mute
// is set again (OUT-05, MUTE-07, QA-11, ADR 0027).
//
//	appears ─ write muted ─ echo muted ─ server: unmuted ─► muted again, mixer hears nothing
func TestOUT05_LateServerMuteRestoreIsSetAgain(t *testing.T) {
	var logs bytes.Buffer
	r := newMuteRig(t, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	r.appear()
	r.d.Muted = true // the echo of Apptrol's write
	if evs := r.report(); len(evs) != 0 {
		t.Fatalf("own echo reported %v", evs)
	}
	r.d.Muted = false // WirePlumber restores its remembered mute
	if got := muteChanges(r.report()); len(got) != 0 {
		t.Fatalf("the server's late restore reached the mixer as an outside mute change %v", got)
	}
	if fmt.Sprint(r.muteWrites) != "[true true]" {
		t.Fatalf("mute writes = %v, want the saved mute set again", r.muteWrites)
	}
	// The echo of the write that set it again is Apptrol's own as well.
	r.d.Muted = true
	if evs := r.report(); len(evs) != 0 {
		t.Fatalf("echo of the guard's write reported %v", evs)
	}
	// The log says what happened, and why the mixer heard nothing.
	for _, want := range []string{
		`msg="audio server changed the mute of a new output; setting it again" apptrol.component=audio apptrol.device.name=spk apptrol.muted=false apptrol.wanted_muted=true`,
		`msg="output mute change not taken over: the new-output guard holds the mute" apptrol.component=audio apptrol.device.name=spk apptrol.muted=false apptrol.audio.guard.state=restore_mute`,
		`apptrol.wanted_muted=true`, // on output device read
	} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, logs.String())
		}
	}
}

// The guard's window ends while it sets the mute again. The read it answered
// was within the window, so it stays the server's restore: the mixer hears
// nothing, and the echo of that write is still Apptrol's own (OUT-05,
// MUTE-07, QA-11, #177 review).
//
//	read unmuted (window open) ─ write muted ─ window ends ─► not outside; echo muted ─► own
func TestOUT05_GuardEndingDuringItsWriteStillHoldsTheRead(t *testing.T) {
	r := newMuteRig(t, quietLog())
	r.appear()
	r.d.Muted = true
	r.report()
	r.b.outputMuteWrite = func(_ string, m bool) error {
		r.muteWrites = append(r.muteWrites, m)
		r.b.mu.Lock()
		r.b.outGuard["spk"].until = time.Now().Add(-time.Millisecond)
		r.b.mu.Unlock()
		return nil
	}
	r.d.Muted = false
	if got := muteChanges(r.report()); len(got) != 0 {
		t.Fatalf("restore read within the window reported %v after the window ended", got)
	}
	r.d.Muted = true
	if got := muteChanges(r.report()); len(got) != 0 {
		t.Fatalf("echo of the guard's write reported %v", got)
	}
	if fmt.Sprint(r.muteWrites) != "[true true]" {
		t.Fatalf("mute writes = %v, want one write again", r.muteWrites)
	}
}

// The window ends after the guard decided to hold a read but before it
// defends it: the change it hides from the mixer must still be set again,
// or the device keeps the restored mute while the saved mute and M's LED
// say otherwise (OUT-05, QA-11, #177 review).
//
//	read unmuted: held ─ window ends ─ defense ─► muted again, mixer hears nothing
func TestOUT05_GuardEndingBeforeItsDefenseStillSetsTheMute(t *testing.T) {
	r := newMuteRig(t, quietLog())
	r.appear()
	r.d.Muted = true
	r.report()
	r.b.afterHold = func() {
		r.b.mu.Lock()
		r.b.outGuard["spk"].until = time.Now().Add(-time.Millisecond)
		r.b.mu.Unlock()
	}
	r.d.Muted = false
	if got := muteChanges(r.report()); len(got) != 0 {
		t.Fatalf("held change reported %v", got)
	}
	if fmt.Sprint(r.muteWrites) != "[true true]" {
		t.Fatalf("mute writes = %v, want the held change set again", r.muteWrites)
	}
	r.b.afterHold = nil
	r.d.Muted = true // the echo, after the window
	if got := muteChanges(r.report()); len(got) != 0 {
		t.Fatalf("echo reported %v", got)
	}
	r.d.Muted = false // the user's change, after the window
	if got := muteChanges(r.report()); fmt.Sprint(got) != "[false]" {
		t.Fatalf("change after the window = %v, want it reported", got)
	}
	if len(r.muteWrites) != 2 {
		t.Fatalf("mute writes = %v, want nothing set after the window", r.muteWrites)
	}
}

// A guard kept past its window never undoes a change made after it: a read
// that matched the control's mute owes nothing, a read-back after the window
// does not read, and a newer M press is what a late answer settles to
// (MUTE-07, OUT-05, CTRL-06, QA-11, #177 review).
//
//	rename, still muted ─ window ends ─ user unmutes ─ (read-back) ─ KeepOutput ─► stays unmuted
func TestMUTE07_GuardPastItsWindowNeverRevertsANewerChange(t *testing.T) {
	expire := func(r *muteRig) {
		r.b.mu.Lock()
		r.b.outGuard["spk"].until = time.Now().Add(-time.Millisecond)
		r.b.mu.Unlock()
	}
	recheck := func(r *muteRig) {
		r.t.Helper()
		r.b.wire.Lock()
		_, err := r.b.recheckOutput(r.b.cur, r.tr, "spk")
		r.b.wire.Unlock()
		if err != nil {
			r.t.Fatal(err)
		}
	}
	rename := func(r *muteRig, muted bool) mixer.Device {
		r.t.Helper()
		r.d.Description, r.d.Muted = "Speakers Pro", muted
		for _, e := range r.report() {
			if added, ok := e.(mixer.OutputAdded); ok {
				return added.Device
			}
		}
		r.t.Fatal("rename not reported")
		return mixer.Device{}
	}
	t.Run("rename without a mute change, read back after the window", func(t *testing.T) {
		r := newMuteRig(t, quietLog())
		r.appear()
		r.d.Muted = true
		r.report() // echo
		renamed := rename(r, true)
		expire(r)
		r.d.Muted = false // the user unmutes after the window
		r.b.outputRead = func(string) (SinkInfo, error) { return r.d, nil }
		recheck(r) // a read-back still queued reads it before its notification
		r.apply(mixer.KeepOutput{Device: "spk", Generation: renamed.Generation})
		if len(r.muteWrites) != 1 {
			t.Fatalf("mute writes = %v, want the user's unmute left alone", r.muteWrites)
		}
		if got := muteChanges(r.report()); fmt.Sprint(got) != "[false]" {
			t.Fatalf("user's unmute = %v, want it reported", got)
		}
	})
	t.Run("held change, read back after the window", func(t *testing.T) {
		r := newMuteRig(t, quietLog())
		r.appear()
		r.d.Muted = true
		r.report()
		renamed := rename(r, false) // held while pending: owed
		expire(r)
		r.b.outputRead = func(string) (SinkInfo, error) { t.Fatal("read back after the window"); return r.d, nil }
		recheck(r)
		r.apply(mixer.KeepOutput{Device: "spk", Generation: renamed.Generation})
		if fmt.Sprint(r.muteWrites) != "[true true]" {
			t.Fatalf("mute writes = %v, want the owed mute set once", r.muteWrites)
		}
	})
	t.Run("newer M press before the late answer", func(t *testing.T) {
		r := newMuteRig(t, quietLog())
		r.appear()
		r.d.Muted = true
		r.report()
		renamed := rename(r, false) // owed: muted
		expire(r)
		r.apply(mixer.SetOutputMute{Device: "spk", Muted: false}) // M pressed
		r.apply(mixer.KeepOutput{Device: "spk", Generation: renamed.Generation})
		if fmt.Sprint(r.muteWrites) != "[true false]" {
			t.Fatalf("mute writes = %v, want the press to win", r.muteWrites)
		}
	})
	t.Run("held change settled by a matching read-back", func(t *testing.T) {
		r := newMuteRig(t, quietLog())
		r.appear()
		r.d.Muted = true
		r.report()
		renamed := rename(r, false) // owed: muted; ownership pending
		r.d.Muted = true            // a read-back within the window sees it muted again
		r.b.outputRead = func(string) (SinkInfo, error) { return r.d, nil }
		r.b.wire.Lock()
		evs, err := r.b.recheckOutput(r.b.cur, r.tr, "spk")
		r.b.wire.Unlock()
		if err != nil || len(muteChanges(evs)) != 0 {
			t.Fatalf("read-back within the window = %v, %v; want nothing reported", evs, err)
		}
		expire(r)
		r.d.Muted = false // the user unmutes after the window
		if got := muteChanges(r.report()); fmt.Sprint(got) != "[false]" {
			t.Fatalf("user's unmute = %v, want it reported", got)
		}
		r.apply(mixer.KeepOutput{Device: "spk", Generation: renamed.Generation})
		if len(r.muteWrites) != 1 {
			t.Fatalf("mute writes = %v, want the user's unmute left alone", r.muteWrites)
		}
	})
	t.Run("held change settled by a matching read", func(t *testing.T) {
		r := newMuteRig(t, quietLog())
		r.appear()
		r.d.Muted = true
		r.report()
		renamed := rename(r, false) // owed: muted
		r.d.Muted = true            // the device reads muted again within the window
		r.report()
		expire(r)
		r.d.Muted = false // the user unmutes after the window
		if got := muteChanges(r.report()); fmt.Sprint(got) != "[false]" {
			t.Fatalf("user's unmute = %v, want it reported", got)
		}
		r.apply(mixer.KeepOutput{Device: "spk", Generation: renamed.Generation})
		if len(r.muteWrites) != 1 {
			t.Fatalf("mute writes = %v, want nothing owed", r.muteWrites)
		}
	})
}

// A change the guard held back while ownership was pending is still set
// right when the mixer's answer comes after the window: it was read within
// it, and nothing else would correct it (OUT-05, CTRL-06, QA-11, #177 review).
//
//	rename + unmuted (window open, held) ─ window ends ─ KeepOutput ─► muted again
func TestOUT05_HeldMuteIsSetWhenTheAnswerComesAfterTheWindow(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer func(r *muteRig, renamed mixer.Device)
		want   string
	}{
		{"kept", func(r *muteRig, d mixer.Device) {
			r.apply(mixer.KeepOutput{Device: "spk", Generation: d.Generation})
		}, "[true true]"},
		{"released", func(r *muteRig, d mixer.Device) {
			r.apply(mixer.SetOutputMute{Device: "spk", Muted: false})
			r.apply(mixer.ReleaseOutput{Device: "spk", Generation: d.Generation})
		}, "[true false]"},
		{"stale answer, then the newest", func(r *muteRig, d mixer.Device) {
			r.apply(mixer.KeepOutput{Device: "spk", Generation: d.Generation - 1})
			if len(r.muteWrites) != 1 {
				t.Fatalf("stale answer settled the owed mute: %v", r.muteWrites)
			}
			r.apply(mixer.KeepOutput{Device: "spk", Generation: d.Generation})
		}, "[true true]"},
		{"muted elsewhere after the window, then kept", func(r *muteRig, d mixer.Device) {
			r.d.Muted = true // the user's change, after the window
			if got := muteChanges(r.report()); fmt.Sprint(got) != "[true]" {
				t.Fatalf("change after the window = %v, want it reported", got)
			}
			r.d.Muted = false // and unmuted again, before the answer
			if got := muteChanges(r.report()); fmt.Sprint(got) != "[false]" {
				t.Fatalf("change after the window = %v, want it reported", got)
			}
			r.apply(mixer.KeepOutput{Device: "spk", Generation: d.Generation})
		}, "[true]"},
		{"removed before the answer", func(r *muteRig, d mixer.Device) {
			r.remove()
			r.apply(mixer.KeepOutput{Device: "spk", Generation: d.Generation})
		}, "[true]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newMuteRig(t, quietLog())
			r.appear()
			r.d.Muted = true
			r.report()
			r.d.Description, r.d.Muted = "Speakers Pro", false // in one notification
			var renamed mixer.Device
			for _, e := range r.report() {
				switch e := e.(type) {
				case mixer.OutputMuteChanged:
					t.Fatalf("held change reported: %v", e)
				case mixer.OutputAdded:
					renamed = e.Device
				}
			}
			r.b.mu.Lock()
			r.b.outGuard["spk"].until = time.Now().Add(-time.Millisecond)
			r.b.mu.Unlock()
			tc.answer(r, renamed)
			if fmt.Sprint(r.muteWrites) != tc.want {
				t.Fatalf("mute writes = %v, want %s", r.muteWrites, tc.want)
			}
			// Once answered, an outside change is the user's again.
			r.b.wire.Lock()
			hold := r.b.heldMute("spk", true)
			r.b.wire.Unlock()
			if hold.state != "" {
				t.Errorf("guard still holds after the answer: %+v", hold)
			}
		})
	}
}

// After the guard's window, an outside mute change is the user's: the mixer
// takes it over and nothing is set again (MUTE-07, OUT-05, ADR 0027).
func TestMUTE07_OutputMuteChangeAfterTheGuardIsTakenOver(t *testing.T) {
	r := newMuteRig(t, quietLog())
	r.appear()
	r.d.Muted = true
	r.report()
	r.b.outGuard["spk"].until = time.Now().Add(-time.Millisecond)
	r.d.Muted = false // unmuted in the desktop's applet
	if got := muteChanges(r.report()); fmt.Sprint(got) != "[false]" {
		t.Fatalf("outside unmute after the window = %v, want it reported", got)
	}
	if len(r.muteWrites) != 1 {
		t.Fatalf("mute writes = %v, want only the first", r.muteWrites)
	}
	if _, guarded := r.b.outGuard["spk"]; guarded {
		t.Error("expired guard was kept")
	}
}

// An M press within the guard's window replaces the mute it defends: a
// restore of the older one is undone, never the press (OUT-06, OUT-05,
// MUTE-01).
//
//	appears ─ muted ─ M: unmuted ─ server: muted ─► unmuted again
func TestOUT06_MPressWithinTheGuardWins(t *testing.T) {
	r := newMuteRig(t, quietLog())
	r.appear()
	r.d.Muted = true
	r.report()
	r.apply(mixer.SetOutputMute{Device: "spk", Muted: false}) // M pressed
	r.d.Muted = false
	r.report() // its echo
	r.d.Muted = true
	if got := muteChanges(r.report()); len(got) != 0 {
		t.Fatalf("restore of the older mute reported %v", got)
	}
	if fmt.Sprint(r.muteWrites) != "[true false false]" {
		t.Fatalf("mute writes = %v, want the press's unmute set again", r.muteWrites)
	}
}

// A released output's mute is no longer defended, and a stale release for an
// older identity does not end the defense of the newest (CTRL-06, OUT-05,
// ADR 0035).
func TestCTRL06_ReleasedOutputMuteIsNoLongerDefended(t *testing.T) {
	t.Run("released", func(t *testing.T) {
		r := newMuteRig(t, quietLog())
		added := r.appear()
		r.d.Muted = true
		r.report()
		// The reload left it on no control: the mixer unmutes and releases it.
		r.apply(mixer.SetOutputMute{Device: "spk", Muted: false})
		r.apply(mixer.ReleaseOutput{Device: "spk", Generation: added.Generation})
		r.d.Muted = false
		r.report()
		r.d.Muted = true // muted elsewhere, or restored: no control holds it
		if got := muteChanges(r.report()); fmt.Sprint(got) != "[true]" {
			t.Fatalf("change on a released output = %v, want it reported (the mixer ignores it)", got)
		}
		if fmt.Sprint(r.muteWrites) != "[true false]" {
			t.Fatalf("mute writes = %v, want nothing set again after the release", r.muteWrites)
		}
	})
	t.Run("stale release", func(t *testing.T) {
		r := newMuteRig(t, quietLog())
		first := r.appear()
		r.d.Muted = true
		r.report()
		r.d.Description = "Speakers Pro" // a newer identity, still kept
		renamed := r.report()[0].(mixer.OutputAdded).Device
		r.apply(mixer.KeepOutput{Device: "spk", Generation: renamed.Generation})
		r.apply(mixer.ReleaseOutput{Device: "spk", Generation: first.Generation})
		r.d.Muted = false
		if got := muteChanges(r.report()); len(got) != 0 {
			t.Fatalf("stale release ended the defense: %v reported", got)
		}
		if fmt.Sprint(r.muteWrites) != "[true true]" {
			t.Fatalf("mute writes = %v, want the mute set again", r.muteWrites)
		}
	})
}

// Ownership moves to another control within the guard's window: the guard
// defends the new owner's saved mute, not the old one's (OUT-04, OUT-05,
// ADR 0035). The real mixer chooses the actions.
//
//	slider1 (muted) owns ─ reload: slider4 (unmuted) owns ─ server: muted ─► unmuted again
func TestOUT04_OwnershipTransferDefendsTheNewOwnersMute(t *testing.T) {
	s1, s4 := mixer.Control{Kind: mixer.Slider, Column: 1}, mixer.Control{Kind: mixer.Slider, Column: 4}
	setup := mixer.Setup{
		Targets: map[string]mixer.Target{
			"a": {ID: "a", Kind: mixer.Output, Match: []string{"spk"}},
			"b": {ID: "b", Kind: mixer.Output, Match: []string{"speakers"}},
		},
		Assignments: map[mixer.Control]string{s1: "a", s4: "b"},
	}
	m := mixer.New(setup, mixer.State{Muted: map[mixer.Control]bool{s1: true}})
	m.Handle(mixer.AudioSnapshot{})
	r := newMuteRig(t, quietLog())
	handle := func(evs ...mixer.Event) {
		t.Helper()
		for _, e := range evs {
			for _, a := range m.Handle(e) {
				r.apply(a)
			}
		}
	}
	handle(r.report()...) // appears: slider1 owns it and mutes it
	r.d.Muted = true
	handle(r.report()...)
	moved := mixer.Setup{Targets: setup.Targets, Assignments: map[mixer.Control]string{s4: "b"}}
	handle(mixer.ConfigChanged{Setup: moved}) // slider4 owns it now
	r.d.Muted = false
	handle(r.report()...)
	r.d.Muted = true // the server restores slider1's mute
	handle(r.report()...)
	if fmt.Sprint(r.muteWrites) != "[true false false]" {
		t.Fatalf("mute writes = %v, want slider4's unmute set again", r.muteWrites)
	}
	if m.Snapshot().Muted[s4] {
		t.Fatal("the restore of slider1's mute was taken over as slider4's")
	}
}

// A device removed and plugged in again gets a fresh guard: an answer or a
// read-back meant for the removed one sets nothing, and the new one defends
// only the mute its control sets now (OUT-05, CTRL-06, ADR 0027).
func TestOUT05_ReappearedOutputDefendsOnlyItsNewMute(t *testing.T) {
	r := newMuteRig(t, quietLog())
	old := r.appear()
	r.d.Muted = true
	r.report()
	if evs := r.remove(); len(evs) != 1 {
		t.Fatalf("removal reported %v", evs)
	}
	// A read-back still queued for the removed device finds no guard.
	r.b.outputRead = func(string) (SinkInfo, error) { t.Fatal("read a removed output"); return SinkInfo{}, nil }
	r.b.wire.Lock()
	_, err := r.b.recheckOutput(r.b.cur, r.tr, "spk")
	r.b.wire.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	r.d.Muted = false
	evs := r.report() // plugged in again, unmuted
	added := evs[0].(mixer.OutputAdded).Device
	r.apply(mixer.KeepOutput{Device: "spk", Generation: old.Generation}) // stale
	r.d.Muted = true
	if got := muteChanges(r.report()); len(got) != 0 {
		t.Fatalf("change before the mixer answered = %v, want it held", got)
	}
	if fmt.Sprint(r.muteWrites) != "[true]" {
		t.Fatalf("mute writes = %v, want nothing from the stale answer", r.muteWrites)
	}
	// The control's saved mute is now unmuted; the guard defends that.
	r.apply(mixer.SetOutputMute{Device: "spk", Muted: false})
	r.apply(mixer.KeepOutput{Device: "spk", Generation: added.Generation})
	if fmt.Sprint(r.muteWrites) != "[true false]" {
		t.Fatalf("mute writes = %v, want the new saved mute only", r.muteWrites)
	}
}

// The server accepts a mute write that never reaches the device and reports
// no change; the read-back finds it and sets the mute again, once (OUT-05,
// QA-11, ADR 0027, #166).
//
//	write muted ─► success ─► (lost) ─► 200 ms: read unmuted ─► muted again
func TestOUT05_ReadBackSetsALostMuteAgain(t *testing.T) {
	r := newMuteRig(t, quietLog())
	r.appear() // accepted, but the device stays unmuted
	r.b.outputRead = func(string) (SinkInfo, error) { return r.d, nil }
	recheck := func() {
		t.Helper()
		r.b.wire.Lock()
		_, err := r.b.recheckOutput(r.b.cur, r.tr, "spk")
		r.b.wire.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	}
	recheck()
	if fmt.Sprint(r.muteWrites) != "[true true]" {
		t.Fatalf("mute writes = %v, want the lost mute set again", r.muteWrites)
	}
	r.d.Muted = true
	recheck()
	if len(r.muteWrites) != 2 {
		t.Fatalf("mute writes = %v, want no write once it reads muted", r.muteWrites)
	}
}

// While the mixer has not answered a new identity, a mute change is held, not
// taken over: the match may be on the description. KeepOutput then sets the
// owner's mute again on the last read (OUT-05, CTRL-06, ADR 0027, ADR 0035).
func TestOUT05_PendingOwnershipHoldsTheMuteUntilKept(t *testing.T) {
	var logs bytes.Buffer
	r := newMuteRig(t, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	r.appear()
	r.d.Muted = true
	r.report()
	r.d.Description = "Speakers Pro"
	renamed := r.report()[0].(mixer.OutputAdded).Device
	r.d.Muted = false
	if got := muteChanges(r.report()); len(got) != 0 {
		t.Fatalf("change while ownership is pending = %v, want it held", got)
	}
	if len(r.muteWrites) != 1 {
		t.Fatalf("mute writes = %v, want none before the answer", r.muteWrites)
	}
	if !strings.Contains(logs.String(), "apptrol.muted=false apptrol.audio.guard.state=ownership_pending") {
		t.Errorf("held change did not log its reason:\n%s", logs.String())
	}
	r.apply(mixer.KeepOutput{Device: "spk", Generation: renamed.Generation})
	if fmt.Sprint(r.muteWrites) != "[true true]" {
		t.Fatalf("mute writes = %v, want the owner's mute set again", r.muteWrites)
	}
}

// M and outside mute cross the actual server boundary. Only a silent test
// output is controlled, and checks wait for state (OUT-05/06, QA-06, ADR 0027).
//
//	appears ─ M: muted ─ outside unmute ─► muted again ─┤ 3 s ├─ outside unmute ─► taken over
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
	unmute := func() {
		t.Helper()
		if result, err := exec.Command("pactl", "set-sink-mute", out, "0").CombinedOutput(); err != nil {
			t.Fatalf("outside unmute: %v %s", err, result)
		}
	}

	// Within the new-output guard's window, an unmute is set back, as a
	// restore by the server would be (OUT-05, #177). pactl returns once the
	// server unmuted it: reading muted afterwards means the guard set it again.
	b.mu.Lock()
	until := b.outGuard[out].until
	b.mu.Unlock()
	if left := time.Until(until); left < time.Second {
		t.Fatalf("only %v of the guard left before the outside change; the server was too slow to test it", left)
	}
	unmute()
	waitSink(t, out, func(s SinkInfo) bool { return s.Muted })

	// After the window, an outside unmute is taken over (MUTE-07).
	time.Sleep(time.Until(until))
	unmute()
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
