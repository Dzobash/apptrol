package pulse

import (
	"errors"
	"testing"
	"time"

	"github.com/Dzobash/apptrol/internal/mixer"
)

func TestINPUT09_ServerChangeReportsOnlyChangedDefaults(t *testing.T) {
	tr := &tracker{defaultSink: "test-output", defaultSource: "mic-a"}
	read := func(source string) func() (string, string, error) {
		return func() (string, string, error) { return "test-output", source, nil }
	}
	for _, name := range []string{"mic-b", ""} {
		evs, err := serverChange(tr, read(name))
		if err != nil || len(evs) != 1 || evs[0] != (mixer.DefaultInputChanged{Name: name}) {
			t.Fatalf("source change: %v %v", evs, err)
		}
		if evs, err := serverChange(tr, read(name)); err != nil || len(evs) != 0 {
			t.Fatal("duplicate default reported")
		}
	}
	failed := errors.New("test read failure")
	if _, err := serverChange(tr, func() (string, string, error) { return "other", "other", failed }); !errors.Is(err, failed) || tr.defaultSource != "" || tr.defaultSink != "test-output" {
		t.Fatal("failed read changed tracked defaults")
	}
}

// inputRig makes source notifications and writes deterministic (INPUT-09,
// QA-11, ADR 0038); no real audio server is used.
type inputRig struct {
	b       *Backend
	tr      *tracker
	d       SourceInfo
	volumes []float64
	mutes   []bool
}

func newInputRig() *inputRig {
	r := &inputRig{b: New(quietLog(), ""), tr: &tracker{devices: map[uint32]mixer.Device{}, devMute: map[uint32]bool{}},
		d: SourceInfo{Device: mixer.Device{Name: "test-mic", Description: "Test microphone"}, Index: 1, Channels: 1, Volume: 1}}
	r.b.cur = &conn{}
	r.b.devGuard, r.b.devChans, r.b.devMuteWant = map[string]*guard{}, map[string]int{}, map[string]muteWant{}
	r.b.sourceWrite = func(_ string, _ int, v float64) error { r.volumes = append(r.volumes, v); return nil }
	r.b.sourceMuteWrite = func(_ string, v bool) error { r.mutes = append(r.mutes, v); return nil }
	return r
}

// report runs under wire just like handle (INPUT-09, ADR 0038).
func (r *inputRig) report(t *testing.T) []mixer.Event {
	t.Helper()
	r.b.wire.Lock()
	defer r.b.wire.Unlock()
	evs, err := r.b.sourceEvent(r.b.cur, r.tr, 1, false, func(uint32) (SourceInfo, bool, error) { return r.d, true, nil })
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

func TestINPUT09_GuardDefendsRestoresAndReleases(t *testing.T) {
	r := newInputRig()
	ev := r.report(t)[0].(mixer.DeviceAdded)
	apply := func(a mixer.Action) {
		t.Helper()
		if err := r.b.Apply(a); err != nil {
			t.Fatal(err)
		}
	}
	apply(mixer.SetDeviceVolume{Device: r.d.Name, Volume: 0.4})
	apply(mixer.SetDeviceMute{Device: r.d.Name, Muted: true})
	apply(mixer.KeepDevice{Device: r.d.Name, Generation: ev.Device.Generation})
	if evs := r.report(t); len(evs) != 0 {
		t.Fatalf("restore reported as outside: %v", evs)
	}
	if len(r.volumes) < 2 || r.volumes[len(r.volumes)-1] != 0.4 || !r.mutes[len(r.mutes)-1] {
		t.Fatal("restore not defended")
	}
	apply(mixer.ForgetDeviceVolume{Device: r.d.Name})
	v := len(r.volumes)
	r.report(t)
	if len(r.volumes) != v {
		t.Fatal("old owner's volume still defended")
	}
	apply(mixer.ReleaseDevice{Device: r.d.Name, Generation: ev.Device.Generation})
	v, m := len(r.volumes), len(r.mutes)
	r.d.Muted = true
	if evs := r.report(t); len(evs) != 0 {
		t.Fatal("released restore leaked to future owner")
	}
	if len(r.volumes) != v || len(r.mutes) != m {
		t.Fatal("released guard wrote")
	}
	r.b.devGuard[r.d.Name].until = time.Now().Add(-time.Second)
	r.d.Muted = false
	if evs := r.report(t); len(evs) != 1 {
		t.Fatal("outside change suppressed after window")
	}
}

func TestINPUT09_GuardWaitsForNewestIdentityAndSettlesMuteDebt(t *testing.T) {
	r := newInputRig()
	first := r.report(t)[0].(mixer.DeviceAdded).Device.Generation
	if err := r.b.Apply(mixer.SetDeviceMute{Device: r.d.Name, Muted: true}); err != nil {
		t.Fatal(err)
	}
	r.d.Description = "Renamed test microphone"
	latest := r.report(t)[0].(mixer.DeviceAdded).Device.Generation
	if latest == first {
		t.Fatal("identity generation reused")
	}
	if err := r.b.Apply(mixer.ReleaseDevice{Device: r.d.Name, Generation: first}); err != nil {
		t.Fatal(err)
	}
	if err := r.b.Apply(mixer.KeepDevice{Device: r.d.Name, Generation: first}); err != nil {
		t.Fatal(err)
	}
	if r.b.devGuard[r.d.Name].answered {
		t.Fatal("stale answer accepted")
	}
	r.b.devGuard[r.d.Name].until = time.Now().Add(-time.Second)
	if err := r.b.Apply(mixer.KeepDevice{Device: r.d.Name, Generation: latest}); err != nil {
		t.Fatal(err)
	}
	if !r.mutes[len(r.mutes)-1] || len(r.mutes) < 2 {
		t.Fatal("held restore not settled after window")
	}
}

func TestINPUT09_RestoreReadBeforeDefaultTakeoverDoesNotChangeSavedMute(t *testing.T) {
	r := newInputRig()
	c := mixer.Control{Kind: mixer.Slider, Column: 8}
	m := mixer.New(mixer.Setup{Targets: map[string]mixer.Target{"mic": {ID: "mic", Kind: mixer.Input, FollowsDefault: true}}, Assignments: map[mixer.Control]string{c: "mic"}}, mixer.State{})
	drive := func(e mixer.Event) {
		t.Helper()
		for _, a := range m.Handle(e) {
			if err := r.b.Apply(a); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, e := range r.report(t) {
		drive(e)
	}
	r.d.Muted = true
	queued := r.report(t)
	drive(mixer.DefaultInputChanged{Name: r.d.Name})
	for _, e := range queued {
		drive(e)
	}
	if m.Snapshot().Muted[c] {
		t.Fatal("late restore became saved mute")
	}
	if r.mutes[len(r.mutes)-1] {
		t.Fatal("default control's unmuted state lost")
	}
}

func TestINPUT09_InputOutsideMuteWithDescriptionChangeComesBeforeRematch(t *testing.T) {
	r := newInputRig()
	r.report(t)
	r.b.devGuard[r.d.Name].until = time.Now().Add(-time.Second)
	r.d.Description, r.d.Muted = "Different test description", true
	evs := r.report(t)
	if len(evs) != 2 || evs[0] != (mixer.DeviceMuteChanged{Name: r.d.Name, Muted: true}) {
		t.Fatalf("outside mute not reported before rematch: %v", evs)
	}
}

func TestINPUT09_PendingDebtSurvivesAnUnchangedReadAfterWindow(t *testing.T) {
	r := newInputRig()
	r.report(t)
	if err := r.b.Apply(mixer.SetDeviceMute{Device: r.d.Name, Muted: true}); err != nil {
		t.Fatal(err)
	}
	r.d.Description = "Renamed test input"
	latest := r.report(t)[0].(mixer.DeviceAdded).Device.Generation
	r.b.devGuard[r.d.Name].until = time.Now().Add(-time.Second)
	r.report(t)
	if err := r.b.Apply(mixer.KeepDevice{Device: r.d.Name, Generation: latest}); err != nil {
		t.Fatal(err)
	}
	if len(r.mutes) < 2 || !r.mutes[len(r.mutes)-1] {
		t.Fatal("unchanged read lost pending mute debt")
	}
}
