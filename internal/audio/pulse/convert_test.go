package pulse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"path/filepath"
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

// An output device that appears, is renamed or goes away becomes one event;
// a change of only its volume or mute becomes none (OUT-12, ADR 0027).
func TestOUT12_OutputChanges(t *testing.T) {
	speakers := mixer.Device{Name: "alsa_output.test.analog-stereo", Description: "Test Speakers"}
	renamed := mixer.Device{Name: speakers.Name, Description: "Renamed Speakers"}
	reads := func(d mixer.Device, err error) func(uint32) (SinkInfo, error) {
		return func(index uint32) (SinkInfo, error) { return SinkInfo{Device: d, Index: index}, err }
	}
	tests := []struct {
		name    string
		known   bool // the tracker knows speakers at index 3
		removed bool
		read    func(uint32) (SinkInfo, error)
		want    []mixer.Event
		wantErr bool
		kept    bool // speakers still tracked afterwards
	}{
		{"appeared", false, false, reads(speakers, nil), []mixer.Event{mixer.OutputAdded{Device: speakers}}, false, true},
		{"volume or mute changed", true, false, reads(speakers, nil), nil, false, true},
		{"renamed", true, false, reads(renamed, nil), []mixer.Event{mixer.OutputAdded{Device: renamed}}, false, true},
		{"removed", true, true, nil, []mixer.Event{mixer.OutputRemoved{Name: speakers.Name}}, false, false},
		{"gone before it was read", true, false, reads(mixer.Device{}, proto.ErrNoSuchEntity),
			[]mixer.Event{mixer.OutputRemoved{Name: speakers.Name}}, false, false},
		{"unknown removed", false, true, nil, nil, false, false},
		{"read failed", true, false, reads(mixer.Device{}, proto.ErrAccessDenied), nil, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &tracker{outputs: map[uint32]mixer.Device{}}
			if tt.known {
				tr.outputs[3] = speakers
			}
			got, err := outputChange(tr, 3, tt.removed, tt.read)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v", err)
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("events = %v, want %v", got, tt.want)
			}
			if _, kept := tr.outputs[3]; kept != tt.kept {
				t.Errorf("tracked = %v, want %v", kept, tt.kept)
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
