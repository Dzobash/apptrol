package pulse

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jfreymuth/pulse/proto"

	"github.com/Dzobash/apptrol/internal/mixer"
)

func props(kv ...string) proto.PropList {
	p := proto.PropList{}
	for i := 0; i < len(kv); i += 2 {
		p[kv[i]] = proto.PropListString(kv[i+1])
	}
	return p
}

func TestStreamFromReply(t *testing.T) {
	r := &proto.GetSinkInputInfoReply{
		SinkInputIndex: 57,
		MediaName:      "fallback",
		SinkIndex:      3,
		ChannelVolumes: proto.ChannelVolumes{proto.VolumeNorm / 2, proto.VolumeNorm / 2},
		Muted:          true,
		Properties:     props("application.name", "Firefox", "application.process.binary", "firefox", "media.name", "Tab"),
	}
	got := streamFromReply(r)
	want := StreamInfo{Stream: mixer.Stream{ID: 57, AppName: "Firefox", Binary: "firefox"},
		Media: "Tab", Volume: 0.5, Muted: true, Channels: 2, Sink: 3}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}

	// Spotify reports only a name; the media name falls back to the field.
	r = &proto.GetSinkInputInfoReply{SinkInputIndex: 1, MediaName: "Spotify", Properties: props("application.name", "Spotify")}
	got = streamFromReply(r)
	if got.Binary != "" || got.Media != "Spotify" || got.Volume != 0 || got.Channels != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestMEDIA10_StreamCorkedFromReply(t *testing.T) {
	for _, corked := range []bool{true, false} {
		r := &proto.GetSinkInputInfoReply{SinkInputIndex: 7, Corked: corked, Properties: props("application.name", "Firefox")}
		if got := streamFromReply(r); got.Corked != corked {
			t.Errorf("Corked = %v, want %v", got.Corked, corked)
		}
	}
}

func TestMEDIA10_StreamChanges(t *testing.T) {
	playing := mixer.Stream{ID: 7, AppName: "Firefox"}
	paused := mixer.Stream{ID: 7, AppName: "Firefox", Corked: true}
	outside := func(uint32, bool) bool { return true }
	ours := func(uint32, bool) bool { return false } // Apptrol muted it itself
	tests := []struct {
		name     string
		old      mixer.Stream
		wasMuted bool
		now      StreamInfo
		outside  func(uint32, bool) bool
		want     []mixer.Event
	}{
		{"nothing changed", playing, false, StreamInfo{Stream: playing}, outside, nil},
		{"paused", playing, false, StreamInfo{Stream: paused}, outside,
			[]mixer.Event{mixer.StreamCorkChanged{ID: 7, Corked: true}}},
		{"resumed", paused, false, StreamInfo{Stream: playing}, outside,
			[]mixer.Event{mixer.StreamCorkChanged{ID: 7, Corked: false}}},
		{"muted outside", playing, false, StreamInfo{Stream: playing, Muted: true}, outside,
			[]mixer.Event{mixer.StreamMuteChanged{ID: 7, Muted: true}}},
		{"muted by Apptrol", playing, false, StreamInfo{Stream: playing, Muted: true}, ours, nil},
		// One notification, two changes: neither may be lost.
		{"muted outside and paused", playing, false, StreamInfo{Stream: paused, Muted: true}, outside,
			[]mixer.Event{mixer.StreamMuteChanged{ID: 7, Muted: true}, mixer.StreamCorkChanged{ID: 7, Corked: true}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := streamChanges(tt.old, tt.wasMuted, tt.now, tt.outside)
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMEDIA10_CorkingIsNotANewStream(t *testing.T) {
	if !mixer.SameStream(mixer.Stream{ID: 7, AppName: "Firefox"}, mixer.Stream{ID: 7, AppName: "Firefox", Corked: true}) {
		t.Error("a paused stream counts as a different stream; it would be matched and set again")
	}
	if mixer.SameStream(mixer.Stream{ID: 7, AppName: "Firefox"}, mixer.Stream{ID: 7, AppName: "Vivaldi"}) {
		t.Error("a stream with a new application.name counts as the same stream")
	}
}

func TestSourceFromReply(t *testing.T) {
	mic := &proto.GetSourceInfoReply{
		SourceIndex: 4, SourceName: "alsa_input.goxlr", MonitorSourceIndex: proto.Undefined,
		ChannelVolumes: proto.ChannelVolumes{proto.VolumeNorm},
		Properties:     props("device.description", "GoXLR Mini Mic"),
	}
	got, ok := sourceFromReply(mic)
	want := SourceInfo{Device: mixer.Device{Name: "alsa_input.goxlr", Description: "GoXLR Mini Mic"}, Index: 4, Volume: 1, Channels: 1}
	if !ok || got != want {
		t.Errorf("got %+v %v, want %+v", got, ok, want)
	}

	noDesc := &proto.GetSourceInfoReply{SourceName: "raw", MonitorSourceIndex: proto.Undefined}
	if got, _ := sourceFromReply(noDesc); got.Description != "raw" {
		t.Errorf("description falls back to the name, got %q", got.Description)
	}

	// Monitors of outputs are not capture devices (CFG-05).
	monitor := &proto.GetSourceInfoReply{SourceName: "alsa_output.goxlr.monitor", MonitorSourceIndex: 2,
		Properties: props("device.description", "Monitor of GoXLR")}
	if _, ok := sourceFromReply(monitor); ok {
		t.Error("monitor accepted")
	}
	classMonitor := &proto.GetSourceInfoReply{SourceName: "m", MonitorSourceIndex: proto.Undefined,
		Properties: props("device.class", "monitor")}
	if _, ok := sourceFromReply(classMonitor); ok {
		t.Error("device.class=monitor accepted")
	}
}

func TestCTRL02_ChannelVolumes(t *testing.T) {
	cv := channelVolumes(2, 0.5)
	if len(cv) != 2 || cv[0] != proto.VolumeNorm/2 || cv[1] != cv[0] {
		t.Errorf("channelVolumes(2, 0.5) = %v", cv)
	}
	if v := fromChannelVolumes(cv); math.Abs(v-0.5) > 1e-9 {
		t.Errorf("round trip = %v", v)
	}
	// CTRL-03: never above 150 %, never below 0.
	if cv := channelVolumes(1, 1.7); cv[0] != proto.NormVolume(1.5) {
		t.Errorf("1.7 -> %v, want 150 %%", cv[0])
	}
	if v := fromChannelVolumes(channelVolumes(2, 1.5)); math.Abs(v-1.5) > 1e-9 {
		t.Errorf("150 %% round trip = %v", v)
	}
	if cv := channelVolumes(1, -1); cv[0] != proto.VolumeMuted {
		t.Errorf("-1 -> %v, want 0", cv[0])
	}
	if cv := channelVolumes(0, 1); len(cv) != 1 {
		t.Errorf("0 channels -> %d volumes, want 1", len(cv))
	}
	if v := fromChannelVolumes(nil); v != 0 {
		t.Errorf("no channels -> %v", v)
	}
	// A slider at the top gives exactly 100 %.
	if cv := channelVolumes(2, mixer.Volume(mixer.MaxValue)); cv[0] != proto.VolumeNorm {
		t.Errorf("slider at 127 -> %v", cv[0])
	}
}

func TestProp(t *testing.T) {
	p := props("a", "x", "empty", "")
	if prop(p, "a", "d") != "x" || prop(p, "empty", "d") != "d" || prop(p, "missing", "d") != "d" {
		t.Error("prop")
	}
}

func TestErrorKinds(t *testing.T) {
	if !IsGone(proto.ErrNoSuchEntity) || IsGone(proto.ErrAccessDenied) || IsGone(io.EOF) {
		t.Error("IsGone")
	}
	if !isServerError(proto.ErrAccessDenied) || isServerError(io.EOF) || isServerError(context.DeadlineExceeded) {
		t.Error("isServerError")
	}
}

func TestSnapshot(t *testing.T) {
	ev := Snapshot(
		[]StreamInfo{{Stream: mixer.Stream{ID: 1, AppName: "a"}}},
		[]SourceInfo{{Device: mixer.Device{Name: "n", Description: "d"}}},
		[]SinkInfo{{Device: mixer.Device{Name: "out", Description: "Out"}}})
	if len(ev.Streams) != 1 || ev.Streams[0].ID != 1 || len(ev.Devices) != 1 || ev.Devices[0].Name != "n" ||
		len(ev.Outputs) != 1 || ev.Outputs[0].Name != "out" {
		t.Errorf("Snapshot = %+v", ev)
	}
}

func TestOUT12_SinkFromReply(t *testing.T) {
	r := &proto.GetSinkInfoReply{
		SinkIndex:      4,
		SinkName:       "alsa_output.test.analog-stereo",
		ChannelVolumes: proto.ChannelVolumes{proto.VolumeNorm / 4, proto.VolumeNorm / 4},
		Mute:           true,
		Properties:     props("device.description", "Test Speakers"),
	}
	want := SinkInfo{Device: mixer.Device{Name: "alsa_output.test.analog-stereo", Description: "Test Speakers"},
		Index: 4, Volume: 0.25, Muted: true, Channels: 2}
	if got := sinkFromReply(r); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	// Without a description, the name stands in for it.
	if got := sinkFromReply(&proto.GetSinkInfoReply{SinkName: "null"}); got.Description != "null" {
		t.Errorf("description = %q", got.Description)
	}
}

// An output device's life as the server reports it, one notification after
// another: appearing, renaming and going away each become one event; a change
// of only its volume or mute, and a repeated notification, become none. The
// tracker holds the device as last reported, so a rename is not reported
// twice (OUT-12, ADR 0027).
func TestOUT12_OutputChanges(t *testing.T) {
	speakers := mixer.Device{Name: "alsa_output.test.analog-stereo", Description: "Test Speakers"}
	renamed := mixer.Device{Name: speakers.Name, Description: "Renamed Speakers"}
	sink := func(d mixer.Device, volume float64, muted bool) SinkInfo {
		return SinkInfo{Device: d, Index: 3, Volume: volume, Muted: muted, Channels: 2}
	}
	steps := []struct {
		name    string
		removed bool
		now     SinkInfo // what the server reports when asked
		want    []mixer.Event
		tracked *mixer.Device // the tracker's entry afterwards; nil = none
	}{
		{"appeared", false, sink(speakers, 0.8, false), []mixer.Event{mixer.OutputAdded{Device: speakers}}, &speakers},
		{"turned down and muted", false, sink(speakers, 0.3, true), nil, &speakers},
		{"turned up past 100 % and unmuted", false, sink(speakers, 1.2, false), nil, &speakers},
		{"renamed", false, sink(renamed, 1.2, false), []mixer.Event{mixer.OutputAdded{Device: renamed}}, &renamed},
		{"renamed, the same notification again", false, sink(renamed, 1.2, false), nil, &renamed},
		{"renamed, then muted", false, sink(renamed, 1.2, true), nil, &renamed},
		{"removed", true, SinkInfo{}, []mixer.Event{mixer.OutputRemoved{Name: speakers.Name}}, nil},
		{"removed again", true, SinkInfo{}, nil, nil},
	}
	tr := &tracker{outputs: map[uint32]mixer.Device{}}
	for _, st := range steps {
		got, err := outputChange(tr, 3, st.removed, func(uint32) (SinkInfo, error) { return st.now, nil })
		if err != nil {
			t.Fatalf("%s: err = %v", st.name, err)
		}
		if fmt.Sprint(got) != fmt.Sprint(st.want) {
			t.Errorf("%s: events = %v, want %v", st.name, got, st.want)
		}
		d, ok := tr.outputs[3]
		switch {
		case st.tracked == nil && ok:
			t.Errorf("%s: still tracked as %+v", st.name, d)
		case st.tracked != nil && (!ok || d != *st.tracked):
			t.Errorf("%s: tracked %+v (%v), want %+v", st.name, d, ok, *st.tracked)
		}
	}
}

// A notification about an output that cannot be read: gone in the meantime
// counts as removed; another error ends the session and changes nothing
// (OUT-12, ADR 0027).
func TestOUT12_OutputChangeReadErrors(t *testing.T) {
	speakers := mixer.Device{Name: "alsa_output.test.analog-stereo", Description: "Test Speakers"}
	fails := func(err error) func(uint32) (SinkInfo, error) {
		return func(uint32) (SinkInfo, error) { return SinkInfo{}, err }
	}
	tests := []struct {
		name    string
		known   bool // the tracker knows speakers at index 3
		read    func(uint32) (SinkInfo, error)
		want    []mixer.Event
		wantErr bool
		kept    bool
	}{
		{"gone before it was read", true, fails(proto.ErrNoSuchEntity),
			[]mixer.Event{mixer.OutputRemoved{Name: speakers.Name}}, false, false},
		{"unknown and gone", false, fails(proto.ErrNoSuchEntity), nil, false, false},
		{"read failed", true, fails(proto.ErrAccessDenied), nil, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &tracker{outputs: map[uint32]mixer.Device{}}
			if tt.known {
				tr.outputs[3] = speakers
			}
			got, err := outputChange(tr, 3, false, tt.read)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v", err)
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("events = %v, want %v", got, tt.want)
			}
			if d, kept := tr.outputs[3]; kept != tt.kept || (kept && d != speakers) {
				t.Errorf("tracked %+v (%v), want kept = %v", d, kept, tt.kept)
			}
		})
	}
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestSVC04_ApplyWithoutConnection(t *testing.T) {
	b := New(quietLog(), "")
	for _, a := range []mixer.Action{
		mixer.SetStreamVolume{StreamID: 1, Volume: 1}, mixer.SetStreamMute{StreamID: 1},
		mixer.SetDeviceVolume{Device: "x"}, mixer.SetDeviceMute{Device: "x"},
		mixer.SetOutputVolume{Device: "x", Volume: 0.5},
	} {
		if err := b.Apply(a); !errors.Is(err, ErrNotConnected) {
			t.Errorf("Apply(%T) = %v, want ErrNotConnected", a, err)
		}
	}
	// Actions that are not about audio are ignored.
	if err := b.Apply(mixer.SetLED{}); err != nil {
		t.Errorf("Apply(SetLED) = %v", err)
	}
}

// The volume Apptrol sets on a new output is what its guard defends against
// WirePlumber's remembered volume; an output that is not new has no guard
// (OUT-05, PRIO-03, ADR 0027).
func TestOUT05_GuardKeepsTheVolumeSetOnANewOutput(t *testing.T) {
	b := New(quietLog(), "")
	b.outChans, b.outGuard = map[string]int{}, map[string]*guard{}
	b.outGuard["new"] = newGuardNow()
	_ = b.Apply(mixer.SetOutputVolume{Device: "new", Volume: 0.3})
	_ = b.Apply(mixer.SetOutputVolume{Device: "old", Volume: 0.6})
	g := b.outGuard["new"]
	if g.volume == nil || *g.volume != 0.3 || g.muted != nil {
		t.Fatalf("guard = %+v, want volume 0.3 and no mute", g)
	}
	if v, _, _ := g.check(1, false); v == nil || *v != 0.3 {
		t.Errorf("the server restored 100 %%; guard returned %v", v)
	}
	if _, guarded := b.outGuard["old"]; guarded {
		t.Error("an output that is not new got a guard")
	}
}

// A slider moved while the new-output guard is writing the volume it had set
// before wins: its write waits for the guard's, instead of being overwritten
// by it, and the guard defends the new value from then on (OUT-01, OUT-05,
// ADR 0027).
func TestOUT01_LatestSliderPositionWinsOverAGuardWrite(t *testing.T) {
	b := New(quietLog(), "")
	b.cur = &conn{} // never used: writes go through outputWrite
	b.outChans, b.outGuard = map[string]int{"spk": 2}, map[string]*guard{"spk": newGuardNow()}
	v := 1.0
	b.outGuard["spk"].volume = &v // the slider was at 100 % when the output appeared

	var mu sync.Mutex
	var writes []float64 // in the order they finished
	guardWriting, sliderWaits := make(chan struct{}), make(chan struct{})
	b.beforeWire = func() { close(sliderWaits) }
	b.outputWrite = func(_ string, _ int, v float64) error {
		if v == 1.0 {
			// The guard's write lasts until the slider's Apply is at wire:
			// if Apply did not wait there, its write would finish first.
			close(guardWriting)
			<-sliderWaits
		}
		mu.Lock()
		writes = append(writes, v)
		mu.Unlock()
		return nil
	}

	// The server restored 50 %; the guard sets 100 % again, holding wire as
	// session does around handle.
	guardDone := make(chan error, 1)
	go func() {
		b.wire.Lock()
		defer b.wire.Unlock()
		guardDone <- b.defendOutput(b.cur, SinkInfo{Device: mixer.Device{Name: "spk"}, Volume: 0.5, Channels: 2})
	}()
	<-guardWriting
	if err := b.Apply(mixer.SetOutputVolume{Device: "spk", Volume: 0.1}); err != nil {
		t.Fatal(err)
	}
	if err := <-guardDone; err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(writes) != 2 || writes[len(writes)-1] != 0.1 {
		t.Fatalf("writes in order = %v, want the slider's 0.1 last", writes)
	}
	if g := b.outGuard["spk"]; g.volume == nil || *g.volume != 0.1 {
		t.Errorf("guard defends %v, want 0.1", g.volume)
	}
}

// An output a reload left without a control is released: within the guard's
// window, a volume changed elsewhere then stays, instead of being set back to
// what Apptrol set; a control taking it back is defended again (OUT-01,
// CTRL-06, ADR 0027). Releasing waits for a guard write in progress.
func TestCTRL06_ReleasedOutputIsNoLongerDefended(t *testing.T) {
	b := New(quietLog(), "")
	b.cur = &conn{} // never used: writes go through outputWrite
	b.outChans, b.outGuard = map[string]int{"spk": 2}, map[string]*guard{"spk": newGuardNow()}
	var writes []float64
	b.outputWrite = func(_ string, _ int, v float64) error { writes = append(writes, v); return nil }
	if err := b.Apply(mixer.SetOutputVolume{Device: "spk", Volume: 1}); err != nil {
		t.Fatal(err)
	}

	// The reload releases it while an event is being handled: it waits.
	b.wire.Lock()
	waits := make(chan struct{})
	b.beforeWire = func() { close(waits) }
	released := make(chan error, 1)
	go func() { released <- b.Apply(mixer.ReleaseOutput{Device: "spk"}) }()
	<-waits
	b.mu.Lock()
	early := b.outGuard["spk"].volume == nil
	b.mu.Unlock()
	if early {
		t.Error("released while an event held wire")
	}
	b.wire.Unlock()
	if err := <-released; err != nil {
		t.Fatal(err)
	}
	b.beforeWire = nil

	writes = nil
	lowered := SinkInfo{Device: mixer.Device{Name: "spk"}, Volume: 0.2, Channels: 2} // set elsewhere
	if err := b.defendOutput(b.cur, lowered); err != nil {
		t.Fatal(err)
	}
	if len(writes) != 0 {
		t.Errorf("a released output was set back: %v", writes)
	}

	// A control takes it back within the window: defended again.
	if err := b.Apply(mixer.SetOutputVolume{Device: "spk", Volume: 0.6}); err != nil {
		t.Fatal(err)
	}
	writes = nil
	if err := b.defendOutput(b.cur, lowered); err != nil {
		t.Fatal(err)
	}
	if len(writes) != 1 || writes[0] != 0.6 {
		t.Errorf("writes = %v, want 0.6 set again", writes)
	}
	// Releasing needs no connection, and an output with no guard is fine.
	b.cur = nil
	if err := b.Apply(mixer.ReleaseOutput{Device: "other"}); err != nil {
		t.Errorf("Apply(ReleaseOutput) without a connection = %v", err)
	}
}

// A read that sees an output's description change together with a volume the
// server restored is not defended at once: the mixer decides first. When a
// control still holds the output (KeepOutput), the volume read is set back
// without waiting for another change; when the change released it
// (ReleaseOutput), the read volume stays (OUT-05, CTRL-06, ADR 0027).
//
//	Apply 30 % ─► read: "Speakers Pro", 100 % ─► nothing yet ─► KeepOutput    ─► 30 % again
//	                                                         └► ReleaseOutput ─► 100 % stays
func TestOUT05_RenamedOutputIsDefendedOnceTheMixerKeepsIt(t *testing.T) {
	for _, keep := range []bool{true, false} {
		b := New(quietLog(), "")
		b.cur = &conn{} // never used: writes go through outputWrite
		b.outChans, b.outGuard = map[string]int{}, map[string]*guard{}
		var writes []float64
		b.outputWrite = func(_ string, _ int, v float64) error { writes = append(writes, v); return nil }

		spk := SinkInfo{Device: mixer.Device{Name: "spk", Description: "Speakers"}, Volume: 1, Channels: 2}
		if err := b.sinkRead(b.cur, &spk, false); err != nil { // it appears
			t.Fatal(err)
		}
		if err := b.Apply(mixer.SetOutputVolume{Device: "spk", Volume: 0.3}); err != nil {
			t.Fatal(err)
		}
		writes = nil

		restored := spk
		restored.Description, restored.Volume = "Speakers Pro", 1
		if err := b.sinkRead(b.cur, &restored, false); err != nil {
			t.Fatal(err)
		}
		if len(writes) != 0 {
			t.Fatalf("keep=%v: defended before the mixer decided: %v", keep, writes)
		}

		var a mixer.Action = mixer.ReleaseOutput{Device: "spk", Generation: restored.Generation}
		if keep {
			a = mixer.KeepOutput{Device: "spk", Generation: restored.Generation}
		}
		if err := b.Apply(a); err != nil {
			t.Fatal(err)
		}
		switch {
		case keep && (len(writes) != 1 || writes[0] != 0.3):
			t.Errorf("kept: writes = %v, want 0.3 set again", writes)
		case !keep && len(writes) != 0:
			t.Errorf("released: writes = %v, want none", writes)
		}
	}

	// Kept without a connection, or with nothing to defend: nothing to do.
	b := New(quietLog(), "")
	if err := b.Apply(mixer.KeepOutput{Device: "spk"}); err != nil {
		t.Errorf("Apply(KeepOutput) without a connection = %v", err)
	}
}

// A second notification must not defend an output while the mixer has yet to
// release its renamed identity (CTRL-06, QA-11, ADR 0027). Direct calls force
// the buffered-event order without sleeps or a running audio server.
//
// rename -> restored volume notification -> no write -> late ReleaseOutput
func TestCTRL06_RenamedOutputWaitsForReleaseAcrossNotifications(t *testing.T) {
	var logs bytes.Buffer
	b := New(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})), "")
	b.cur = &conn{}
	b.outChans, b.outGuard = map[string]int{}, map[string]*guard{}
	var writes []float64
	b.outputWrite = func(_ string, _ int, v float64) error { writes = append(writes, v); return nil }
	d := SinkInfo{Device: mixer.Device{Name: "spk", Description: "Speakers"}, Volume: 1, Channels: 2}
	read := func(same bool) {
		t.Helper()
		b.wire.Lock()
		err := b.sinkRead(b.cur, &d, same)
		b.wire.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	}
	read(false)
	if err := b.Apply(mixer.KeepOutput{Device: d.Name, Generation: d.Generation}); err != nil {
		t.Fatal(err)
	}
	if err := b.Apply(mixer.SetOutputVolume{Device: d.Name, Volume: 0.3}); err != nil {
		t.Fatal(err)
	}
	writes = nil
	d.Description, d.Volume = "Unmatched", 0.3
	read(false)
	d.Volume = 1
	read(true)
	if len(writes) != 0 {
		t.Errorf("guard wrote before the pending release: %v", writes)
	}
	if !strings.Contains(logs.String(), "apptrol.audio.guard.state=ownership_pending") {
		t.Error("the skipped guard write did not log its pending ownership reason")
	}
	if err := b.Apply(mixer.ReleaseOutput{Device: d.Name, Generation: d.Generation}); err != nil {
		t.Fatal(err)
	}
	read(true)
	if len(writes) != 0 {
		t.Errorf("released output was defended: %v", writes)
	}
}

// Buffered answers for an older identity cannot resume or cancel defense of
// the newest one; removing and recreating a device also changes its token
// (CTRL-06, OUT-05, QA-11, ADR 0027).
func TestCTRL06_OutputOwnershipRejectsOlderGenerations(t *testing.T) {
	for _, removed := range []bool{false, true} {
		t.Run(fmt.Sprintf("removed=%v", removed), func(t *testing.T) {
			b := New(quietLog(), "")
			b.cur = &conn{}
			b.outChans, b.outGuard = map[string]int{}, map[string]*guard{}
			var writes []float64
			b.outputWrite = func(_ string, _ int, v float64) error { writes = append(writes, v); return nil }
			tracker := tracker{outputs: map[uint32]mixer.Device{}}
			d := SinkInfo{Device: mixer.Device{Name: "spk", Description: "Speakers"}, Volume: 1, Channels: 2}
			read := func() mixer.Device {
				t.Helper()
				b.wire.Lock()
				defer b.wire.Unlock()
				old, known := tracker.outputs[d.Index]
				evs, err := outputChange(&tracker, d.Index, false, func(uint32) (SinkInfo, error) {
					err := b.sinkRead(b.cur, &d, known && sameOutputIdentity(old, d.Device))
					return d, err
				})
				if err != nil || len(evs) != 1 {
					t.Fatalf("reported read = %v, %v", evs, err)
				}
				return evs[0].(mixer.OutputAdded).Device
			}
			apply := func(a mixer.Action) {
				t.Helper()
				if err := b.Apply(a); err != nil {
					t.Fatal(err)
				}
			}
			first := read()
			if removed {
				delete(tracker.outputs, d.Index)
				delete(b.outChans, d.Name)
				delete(b.outGuard, d.Name)
			}
			d.Description = "Speakers Pro"
			latest := read()
			if latest.Generation <= first.Generation {
				t.Fatal("new reported identity reused an older generation")
			}
			apply(mixer.SetOutputVolume{Device: d.Name, Volume: 0.3})
			writes = nil
			apply(mixer.KeepOutput{Device: d.Name, Generation: first.Generation})
			apply(mixer.ReleaseOutput{Device: d.Name, Generation: first.Generation})
			b.wire.Lock()
			err := b.sinkRead(b.cur, &d, true)
			b.wire.Unlock()
			if err != nil || len(writes) != 0 {
				t.Fatalf("old answers or volume write acknowledged the pending read: %v, %v", writes, err)
			}
			apply(mixer.KeepOutput{Device: d.Name, Generation: latest.Generation})
			if len(writes) != 1 || writes[0] != 0.3 {
				t.Fatalf("latest keep did not defend: %v", writes)
			}
			apply(mixer.ReleaseOutput{Device: d.Name, Generation: first.Generation})
			if b.outGuard[d.Name].volume == nil {
				t.Fatal("stale release cleared the newest ownership")
			}
		})
	}
}

func TestSVC04_RunKeepsRetryingUntilCanceled(t *testing.T) {
	b := New(quietLog(), "unix:"+filepath.Join(t.TempDir(), "no-such-socket"))
	b.retryMin, b.retryMax = time.Millisecond, 4*time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := b.Run(ctx, make(chan mixer.Event)); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Run = %v, want the context's error", err)
	}
}

func TestListWithoutServer(t *testing.T) {
	if _, err := List("unix:" + filepath.Join(t.TempDir(), "no-such-socket")); err == nil {
		t.Error("List without a server succeeded")
	}
}

// A mute change is Apptrol's own only if it has the value Apptrol set; the
// other value is an outside change even right after Apptrol's, so a
// hold-to-talk input unmuted then is muted again (MUTE-07, INPUT-03, #118).
func TestMUTE07_OutsideMuteWithinOwnWindowIsReported(t *testing.T) {
	tests := []struct {
		name  string
		set   *bool         // the mute Apptrol set; nil = none
		age   time.Duration // how long ago
		seen  []bool        // mute changes seen afterwards, in order
		want  []bool        // outside, for each
		guard bool          // the new-stream guard is active
	}{
		{"no own change", nil, 0, []bool{true}, []bool{true}, false},
		{"echo of own mute", ptr(true), 0, []bool{true}, []bool{false}, false},
		{"opposite right after own mute", ptr(true), 0, []bool{false}, []bool{true}, false},
		{"change after the echo", ptr(true), 0, []bool{true, false}, []bool{false, true}, false},
		{"own write never seen", ptr(true), 2 * ownMute, []bool{true}, []bool{true}, false},
		{"new-stream guard", nil, 0, []bool{false}, []bool{false}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New(quietLog(), "")
			b.guards, b.devGuard = map[uint32]*guard{}, map[string]*guard{}
			b.muteWant, b.devMuteWant = map[uint32]muteWant{}, map[string]muteWant{}
			if tt.set != nil {
				_ = b.Apply(mixer.SetStreamMute{StreamID: 7, Muted: *tt.set})
				_ = b.Apply(mixer.SetDeviceMute{Device: "mic", Muted: *tt.set})
				w := b.muteWant[7]
				w.until = w.until.Add(-tt.age)
				b.muteWant[7] = w
				d := b.devMuteWant["mic"]
				d.until = d.until.Add(-tt.age)
				b.devMuteWant["mic"] = d
			}
			if tt.guard {
				b.guards[7], b.devGuard["mic"] = newGuardNow(), newGuardNow()
			}
			for i, muted := range tt.seen {
				if got := b.outsideStreamMute(7, muted); got != tt.want[i] {
					t.Errorf("stream change %d to muted=%v: outside = %v, want %v", i, muted, got, tt.want[i])
				}
				if got := b.outsideDeviceMute("mic", muted); got != tt.want[i] {
					t.Errorf("device change %d to muted=%v: outside = %v, want %v", i, muted, got, tt.want[i])
				}
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

func TestPRIO03_GuardReappliesChangesToNewStreams(t *testing.T) {
	g := newGuardNow()
	if v, m, exp := g.check(1, false); v != nil || m != nil || exp {
		t.Error("guard without values asked to set something")
	}
	vol, muted := 0.5, true
	g.volume, g.muted = &vol, &muted
	if v, m, _ := g.check(0.5, true); v != nil || m != nil {
		t.Error("values unchanged, but guard asked to set them")
	}
	if v, m, _ := g.check(1, false); v == nil || *v != 0.5 || m == nil || !*m {
		t.Errorf("server reset the values; guard returned %v %v", v, m)
	}
	if v, _, _ := g.check(0.503, true); v != nil {
		t.Error("rounding difference treated as a change")
	}
	g.until = time.Now().Add(-time.Second)
	if v, m, exp := g.check(1, false); v != nil || m != nil || !exp {
		t.Error("expired guard still active")
	}
}
