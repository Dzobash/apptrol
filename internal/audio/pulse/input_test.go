package pulse

import (
	"bytes"
	"errors"
	"log/slog"
	"slices"
	"strings"
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
	keep := mixer.KeepDevice{Device: r.d.Name, Generation: ev.Device.Generation}
	apply(keep)
	if !slices.Equal(r.volumes, []float64{0.4}) || !slices.Equal(r.mutes, []bool{true}) {
		t.Fatalf("initial writes: volumes %v, mutes %v; want one of each", r.volumes, r.mutes)
	}
	r.d.Volume, r.d.Muted = 0.4, true
	r.report(t)
	r.d.Volume, r.d.Muted = 0.7, false
	if evs := r.report(t); len(evs) != 0 {
		t.Fatalf("restore reported as outside: %v", evs)
	}
	if !slices.Equal(r.volumes, []float64{0.4, 0.4}) || !slices.Equal(r.mutes, []bool{true, true}) {
		t.Fatalf("writes after distinct restore: volumes %v, mutes %v; want one more of each", r.volumes, r.mutes)
	}
	apply(keep)
	if len(r.volumes) != 2 || len(r.mutes) != 2 {
		t.Fatalf("KeepDevice repeated the corrections: volumes %v, mutes %v", r.volumes, r.mutes)
	}
	r.d.Muted = true
	apply(mixer.ForgetDeviceVolume{Device: r.d.Name})
	v := len(r.volumes)
	r.report(t)
	if len(r.volumes) != v {
		t.Fatal("old owner's volume still defended")
	}
	apply(mixer.ReleaseDevice{Device: r.d.Name, Generation: ev.Device.Generation})
	v, m := len(r.volumes), len(r.mutes)
	r.d.Muted = false
	if evs := r.report(t); len(evs) != 0 {
		t.Fatal("released restore leaked to future owner")
	}
	if len(r.volumes) != v || len(r.mutes) != m {
		t.Fatal("released guard wrote")
	}
	r.b.devGuard[r.d.Name].until = time.Now().Add(-time.Second)
	r.d.Muted = true
	if evs := r.report(t); len(evs) != 1 {
		t.Fatal("outside change suppressed after window")
	}
}

func TestMUTE07_HeldInputMuteConsumesEchoAndLogsOnlyChanges(t *testing.T) {
	r := newInputRig()
	var logs bytes.Buffer
	r.b.log = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	const (
		heldLog = "input mute change not taken over: the new-input guard holds the mute"
		echoLog = "input mute echo ignored: matches Apptrol's write"
	)
	generation := r.report(t)[0].(mixer.DeviceAdded).Device.Generation
	if err := r.b.Apply(mixer.SetDeviceMute{Device: r.d.Name, Muted: true}); err != nil {
		t.Fatal(err)
	}
	r.d.Volume = 0.8
	if evs := r.report(t); len(evs) != 0 || strings.Contains(logs.String(), heldLog) || strings.Contains(logs.String(), echoLog) {
		t.Fatalf("volume-only notification produced events %v or a mute log:\n%s", evs, &logs)
	}
	if want, ok := r.b.devMuteWant[r.d.Name]; !ok || !want.muted {
		t.Fatal("volume-only notification used up the expected mute echo")
	}
	r.d.Muted = true
	if evs := r.report(t); len(evs) != 0 || strings.Contains(logs.String(), heldLog) {
		t.Fatalf("own echo produced events %v or a held-mute log:\n%s", evs, &logs)
	}
	if strings.Count(logs.String(), echoLog) != 1 {
		t.Fatalf("want exactly one mute echo log:\n%s", &logs)
	}
	if _, ok := r.b.devMuteWant[r.d.Name]; ok {
		t.Fatal("own echo did not consume the expected mute")
	}
	if err := r.b.Apply(mixer.KeepDevice{Device: r.d.Name, Generation: generation}); err != nil {
		t.Fatal(err)
	}
	r.d.Muted = false
	if evs := r.report(t); len(evs) != 0 || strings.Count(logs.String(), heldLog) != 1 {
		t.Fatalf("restore produced events %v or did not log exactly one held mute:\n%s", evs, &logs)
	}
	if strings.Count(logs.String(), echoLog) != 1 {
		t.Fatalf("restore logged a mute echo:\n%s", &logs)
	}
	if want, ok := r.b.devMuteWant[r.d.Name]; !ok || !want.muted {
		t.Fatal("restore used up the expected echo of the correction")
	}
}

func TestINPUT09_FailedWritesPreserveAReportedRestore(t *testing.T) {
	r := newInputRig()
	r.report(t)
	volume := mixer.SetDeviceVolume{Device: r.d.Name, Volume: 0.4}
	mute := mixer.SetDeviceMute{Device: r.d.Name, Muted: true}
	for _, a := range []mixer.Action{volume, mute} {
		if err := r.b.Apply(a); err != nil {
			t.Fatal(err)
		}
	}
	r.d.Volume, r.d.Muted = 0.4, true
	r.report(t)
	r.d.Description, r.d.Volume, r.d.Muted = "Renamed test microphone", 0.7, false
	latest := r.report(t)[0].(mixer.DeviceAdded).Device.Generation

	failed := errors.New("test write failure")
	writeVolume, writeMute := r.b.sourceWrite, r.b.sourceMuteWrite
	r.b.sourceWrite = func(string, int, float64) error { return failed }
	r.b.sourceMuteWrite = func(string, bool) error { return failed }
	for _, a := range []mixer.Action{volume, mute} {
		if err := r.b.Apply(a); !errors.Is(err, failed) {
			t.Fatalf("Apply(%T) = %v, want write failure", a, err)
		}
	}
	g := r.b.devGuard[r.d.Name]
	if !g.owedMute || *g.readMuted || *g.read != 0.7 {
		t.Fatalf("failed writes replaced the restore read: volume %v, muted %v, mute owed %v", *g.read, *g.readMuted, g.owedMute)
	}
	r.b.sourceWrite, r.b.sourceMuteWrite = writeVolume, writeMute
	if err := r.b.Apply(mixer.KeepDevice{Device: r.d.Name, Generation: latest}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.volumes, []float64{0.4, 0.4}) || !slices.Equal(r.mutes, []bool{true, true}) {
		t.Fatalf("writes after retry: volumes %v, mutes %v; want initial writes and corrections", r.volumes, r.mutes)
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
