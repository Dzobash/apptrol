package pulse

import (
	"bytes"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jfreymuth/pulse/proto"

	"github.com/Dzobash/apptrol/internal/mixer"
)

// testFlatSink supplies explicit synthetic positions for older scalar guard
// tests; production reads always carry the server's vector (OUT-15, ADR 0039).
func testFlatSink(d SinkInfo) SinkInfo {
	d.Positions = make(proto.ChannelMap, d.Channels)
	for i := range d.Positions {
		d.Positions[i] = byte(i + 1)
	}
	d.Volumes = channelVolumes(d.Channels, d.Volume)
	return d
}

// balanceRig drives real notification, action and read-back paths with
// synthetic channel vectors and deterministic event order (OUT-14 to OUT-16,
// QA-11, ADR 0039).
type balanceRig struct {
	t      *testing.T
	b      *Backend
	tr     *tracker
	d      SinkInfo
	writes []proto.ChannelVolumes
}

// newBalanceRig starts a newly appeared, owned stereo output (OUT-16, ADR 0039).
func newBalanceRig(t *testing.T) *balanceRig {
	r := &balanceRig{t: t, b: New(quietLog(), ""), tr: &tracker{outputs: map[uint32]mixer.Device{}, outMute: map[uint32]bool{}},
		d: SinkInfo{Device: mixer.Device{Name: "balance-test", Description: "Balance test"}, Index: 7,
			Positions: proto.ChannelMap{proto.ChannelLeft, proto.ChannelRight}, Channels: 2}}
	r.b.cur = &conn{}
	r.b.outChans, r.b.outGuard = map[string]int{}, map[string]*guard{}
	r.b.outputWrite = func(_ string, cv proto.ChannelVolumes) error {
		r.writes = append(r.writes, slices.Clone(cv))
		return nil
	}
	r.b.outputMuteWrite = func(string, bool) error { return nil }
	r.read(1, 1)
	r.apply(mixer.KeepOutput{Device: r.d.Name, Generation: r.d.Generation})
	return r
}

// read reports a server vector through the subscription path (OUT-16, ADR 0039).
func (r *balanceRig) read(levels ...float64) {
	r.t.Helper()
	r.d.Volumes = nil
	for _, v := range levels {
		r.d.Volumes = append(r.d.Volumes, proto.NormVolume(v))
	}
	r.d.Volume = outputLevel(r.d.Volumes)
	r.d.Channels = len(levels)
	r.b.wire.Lock()
	evs, err := r.b.sinkEvent(r.b.cur, r.tr, r.d.Index, false, func(uint32) (SinkInfo, error) { return r.d, nil })
	r.b.wire.Unlock()
	if err != nil {
		r.t.Fatal(err)
	}
	for _, ev := range evs {
		if added, ok := ev.(mixer.OutputAdded); ok {
			r.d.Generation = added.Device.Generation
		}
	}
}

// apply reports action failures immediately (OUT-14 to OUT-16, ADR 0039).
func (r *balanceRig) apply(a mixer.Action) {
	r.t.Helper()
	if err := r.b.Apply(a); err != nil {
		r.t.Fatal(err)
	}
}

// move sets the scalar level through the unchanged service boundary
// (OUT-14, ADR 0039).
func (r *balanceRig) move(level float64) {
	r.t.Helper()
	r.apply(mixer.SetOutputVolume{Device: r.d.Name, Volume: level})
}

// wantWrite compares actual protocol vectors, allowing one unit for the
// initial fixture's percentage quantization (OUT-14, ADR 0039).
func (r *balanceRig) wantWrite(levels ...float64) {
	r.t.Helper()
	if len(r.writes) == 0 {
		r.t.Fatal("no output write")
	}
	got := r.writes[len(r.writes)-1]
	if len(got) != len(levels) {
		r.t.Fatalf("channels = %v, want %v", got, levels)
	}
	for i, level := range levels {
		want := proto.NormVolume(level)
		if delta := int64(got[i]) - int64(want); delta < -1 || delta > 1 {
			r.t.Fatalf("channel %d = %d, want %d; vector %v", i, got[i], want, got)
		}
	}
}

func TestOUT16_LateRestoreSuppliesBalanceOnANewOutput(t *testing.T) {
	r := newBalanceRig(t)
	r.move(0.5)
	r.wantWrite(0.5, 0.5)
	r.read(0.8, 0.6)
	r.wantWrite(0.5, 0.375)
	before := len(r.writes)
	r.read(0.5, 0.25)
	if len(r.writes) != before {
		t.Fatal("balance-only change caused a correction")
	}
	r.move(0.4)
	r.wantWrite(0.4, 0.2)
}

func TestOUT15_ZeroAndRoundedEchoesNeverDrift(t *testing.T) {
	r := newBalanceRig(t)
	r.read(1, 12345.0/65536)
	want := slices.Clone(r.b.outBalance[r.d.Name].ratios)
	for range 50 {
		for _, level := range []float64{0.00002, 0.017, 0, 0.6} {
			r.move(level)
			written := r.writes[len(r.writes)-1]
			r.read(written[0].Norm(), written[1].Norm())
			if !slices.Equal(r.b.outBalance[r.d.Name].ratios, want) {
				t.Fatal("own echo changed ratios")
			}
		}
	}
	r.move(0.5)
	r.wantWrite(0.5, 12345.0/131072)
}

func TestOUT15_OverflowResyncDiscardsCachedBalance(t *testing.T) {
	for _, zero := range []bool{true, false} {
		r := newBalanceRig(t)
		r.read(0.8, 0.6)
		if zero {
			r.read(0, 0)
		}
		generation := r.b.outputGeneration
		r.b.wire.Lock()
		r.b.mu.Lock()
		r.b.resetOutputBalance([]SinkInfo{r.d}, "resync")
		r.b.mu.Unlock()
		r.b.wire.Unlock()
		if r.b.outputGeneration != generation {
			t.Fatal("balance reset altered ownership generation")
		}
		r.move(0.5)
		if zero {
			r.wantWrite(0.5, 0.5)
		} else {
			r.wantWrite(0.5, 0.375)
		}
	}
}

func TestOUT15_RemovalReconnectAndLayoutForgetBalance(t *testing.T) {
	for _, change := range []string{"removal", "reconnect", "layout"} {
		t.Run(change, func(t *testing.T) {
			r := newBalanceRig(t)
			r.read(0.8, 0.6)
			switch change {
			case "removal":
				old := r.d.Generation
				if _, err := r.b.sinkEvent(r.b.cur, r.tr, r.d.Index, true, nil); err != nil {
					t.Fatal(err)
				}
				r.read(0, 0)
				if r.d.Generation <= old {
					t.Fatal("reused ownership generation")
				}
			case "reconnect":
				r.b.setConn(nil)
				if len(r.b.outBalance) != 0 {
					t.Fatal("disconnect kept balance")
				}
				r.b.setConn(&conn{})
				r.read(0, 0)
			case "layout":
				r.d.Positions = proto.ChannelMap{proto.ChannelRight, proto.ChannelLeft}
				r.read(0, 0)
			}
			r.move(0.5)
			r.wantWrite(0.5, 0.5)
		})
	}
}

func TestOUT16_KeepOutputUsesHeldBalance(t *testing.T) {
	for _, answer := range []string{"keep", "stale", "release", "expired"} {
		t.Run(answer, func(t *testing.T) {
			r := newBalanceRig(t)
			r.move(0.5)
			old := r.d.Generation
			r.d.Description = "Renamed test output"
			r.read(0.8, 0.6)
			r.read(0.8, 0.4) // latest held read wins
			r.move(0.6)      // latest requested level wins too
			r.read(0.8, 0.4)
			n := len(r.writes)
			switch answer {
			case "keep":
				r.apply(mixer.KeepOutput{Device: r.d.Name, Generation: r.d.Generation})
			case "stale":
				r.apply(mixer.KeepOutput{Device: r.d.Name, Generation: old})
			case "release":
				r.apply(mixer.ReleaseOutput{Device: r.d.Name, Generation: r.d.Generation})
			case "expired":
				r.b.outGuard[r.d.Name].until = time.Now().Add(-time.Second)
				r.apply(mixer.KeepOutput{Device: r.d.Name, Generation: r.d.Generation})
			}
			if answer == "keep" {
				if len(r.writes) != n+1 {
					t.Fatal("keep did not defend held read")
				}
				r.wantWrite(0.6, 0.3)
			} else if len(r.writes) != n {
				t.Fatal("unauthorized defense")
			}
		})
	}
}

func TestOUT16_ReadBackRecoversLostBalancedWrite(t *testing.T) {
	r := newBalanceRig(t)
	r.read(0.8, 0.6)
	r.move(0.5) // accepted by the hook, but server still has 80/60
	r.b.outputRead = func(string) (SinkInfo, error) { return r.d, nil }
	n := len(r.writes)
	if _, err := r.b.recheckOutput(r.b.cur, r.tr, r.d.Name); err != nil {
		t.Fatal(err)
	}
	if len(r.writes) != n+1 {
		t.Fatal("lost write not retried")
	}
	r.wantWrite(0.5, 0.375)
}

func TestOUT15_CacheMissAndWriteFailures(t *testing.T) {
	r := newBalanceRig(t)
	r.read(0.8, 0.6)
	r.b.outBalance = nil
	reads := 0
	r.b.outputRead = func(string) (SinkInfo, error) { reads++; return r.d, nil }
	r.move(0.5)
	r.wantWrite(0.5, 0.375)
	r.move(0.4)
	if reads != 1 {
		t.Fatal("normal move added a server read")
	}
	previous := slices.Clone(r.b.outBalance[r.d.Name].written)
	r.b.outputWrite = func(string, proto.ChannelVolumes) error { return errors.New("synthetic refusal") }
	if err := r.b.Apply(mixer.SetOutputVolume{Device: r.d.Name, Volume: 0.2}); err == nil {
		t.Fatal("write failure hidden")
	}
	if !slices.Equal(previous, r.b.outBalance[r.d.Name].written) {
		t.Fatal("failed write recorded as own echo")
	}
	r.b.outBalance = nil
	r.b.outputRead = func(string) (SinkInfo, error) { return SinkInfo{}, errors.New("synthetic read failure") }
	called := false
	r.b.outputWrite = func(string, proto.ChannelVolumes) error { called = true; return nil }
	if err := r.b.Apply(mixer.SetOutputVolume{Device: r.d.Name, Volume: 0.2}); err == nil || called {
		t.Fatal("failed cache read guessed a layout")
	}
}

func TestOUT16_ReleaseAndDefaultTakeoverKeepLiveBalance(t *testing.T) {
	for _, forget := range []bool{false, true} {
		r := newBalanceRig(t)
		r.read(0.8, 0.6)
		r.move(0.5)
		if forget {
			r.apply(mixer.ForgetOutputVolume{Device: r.d.Name})
		} else {
			r.apply(mixer.ReleaseOutput{Device: r.d.Name, Generation: r.d.Generation})
		}
		n := len(r.writes)
		r.read(0.4, 0.8)
		if len(r.writes) != n {
			t.Fatal("released volume defended")
		}
		r.move(0.5)
		r.wantWrite(0.25, 0.5)
	}
}

func TestOUT15_OutsideEditsAndMuteDoNotFlatten(t *testing.T) {
	r := newBalanceRig(t)
	r.read(0.8, 0.6)
	r.move(0.5)
	r.b.outGuard[r.d.Name].until = time.Now().Add(-time.Second)
	n := len(r.writes)
	r.read(0.4, 0.8)
	if len(r.writes) != n {
		t.Fatal("outside edit corrected after expiry")
	}
	r.apply(mixer.SetOutputMute{Device: r.d.Name, Muted: true})
	r.move(0.5)
	r.wantWrite(0.25, 0.5)
}

func TestOUT13_BalanceLogsExplainVectors(t *testing.T) {
	r := newBalanceRig(t)
	var logs bytes.Buffer
	r.b.log = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r.read(0.8, 0.6)
	r.move(0.5)
	r.read(0, 0)
	for _, text := range []string{"output balance selected", "output balance retained", "output device read", "output volume request completed",
		"apptrol.audio.channel_positions", "apptrol.audio.channel_volumes_percent", "apptrol.audio.wanted_channel_volumes_percent", "apptrol.audio.balance.ratios"} {
		if !strings.Contains(logs.String(), text) {
			t.Errorf("missing %s", text)
		}
	}
}

// testSeedBalance gives older ownership tests their explicitly balanced
// fixture without a server read (OUT-15, ADR 0039).
func testSeedBalance(b *Backend, name string) {
	d := SinkInfo{Channels: 2, Volume: 1}
	d.Name = name
	b.observeOutputBalance(testFlatSink(d), "snapshot")
}

// testSinkRead adapts scalar fixtures while retaining real guard processing
// (OUT-05, OUT-16, ADR 0039).
func testSinkRead(b *Backend, d *SinkInfo, same bool) error {
	*d = testFlatSink(*d)
	return b.sinkRead(b.cur, d, same)
}

// testDefendOutput supplies the live balance normally learned by sinkRead
// before older direct guard tests (OUT-05, OUT-16, ADR 0039).
func testDefendOutput(b *Backend, d SinkInfo) error {
	d = testFlatSink(d)
	b.observeOutputBalance(d, "guard_read")
	return b.defendOutput(b.cur, d)
}

// TestOUT14_ChannelScaling checks protocol units, including silent channels
// and boost, without relying on a desktop's summary (ADR 0039).
func TestOUT14_ChannelScaling(t *testing.T) {
	for _, tc := range []struct {
		name       string
		read, want []float64
		level      float64
	}{
		{"stereo", []float64{0.8, 0.6}, []float64{0.5, 0.375}, 0.5},
		{"mono", []float64{0.8}, []float64{0.5}, 0.5},
		{"surround", []float64{0.8, 0.6, 0.4, 0.4, 0.2, 0}, []float64{0.5, 0.375, 0.25, 0.25, 0.125, 0}, 0.5},
		{"silent", []float64{0.8, 0}, []float64{0.5, 0}, 0.5},
		{"boost", []float64{1, 0.5}, []float64{1.5, 0.75}, 1.5},
		{"zero", []float64{0, 0}, []float64{0.5, 0.5}, 0.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := SinkInfo{Positions: make(proto.ChannelMap, len(tc.read))}
			for _, v := range tc.read {
				d.Volumes = append(d.Volumes, proto.NormVolume(v))
			}
			s := &outputBalance{positions: d.Positions}
			s.observe(d)
			got := s.scaled(tc.level)
			for i, v := range got {
				want := proto.NormVolume(tc.want[i])
				if v != want {
					t.Errorf("channel %d = %d, want %d", i, v, want)
				}
			}
		})
	}
}
