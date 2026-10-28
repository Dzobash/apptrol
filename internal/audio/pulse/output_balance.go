package pulse

import (
	"fmt"
	"math"
	"slices"

	"github.com/jfreymuth/pulse/proto"

	"github.com/Dzobash/apptrol/internal/logattr"
	"github.com/Dzobash/apptrol/internal/mixer"
)

// outputBalance separates a live layout's usable proportions from the levels
// last read and written. Zero and rounded echoes must not replace those
// proportions (OUT-14, OUT-15, ADR 0039).
type outputBalance struct {
	index     uint32
	positions proto.ChannelMap
	ratios    []float64
	read      proto.ChannelVolumes
	written   proto.ChannelVolumes
}

// observeOutputBalance accepts a server read under mu, invalidating both
// ratios and echoes when the live identity or positions change (OUT-15,
// OUT-16, ADR 0039).
func (b *Backend) observeOutputBalance(d SinkInfo, reason string) {
	if len(d.Positions) == 0 || len(d.Positions) != len(d.Volumes) {
		delete(b.outBalance, d.Name)
		return
	}
	if b.outBalance == nil {
		b.outBalance = make(map[string]*outputBalance)
	}
	s := b.outBalance[d.Name]
	if s == nil || s.index != d.Index || !slices.Equal(s.positions, d.Positions) {
		if s != nil {
			reason = "layout_changed"
		}
		s = &outputBalance{index: d.Index, positions: slices.Clone(d.Positions)}
		b.outBalance[d.Name] = s
	}
	retained := s.observe(d)
	attrs := append([]any{logattr.KeyDeviceName, d.Name, logattr.KeyDeviceIndex, d.Index}, s.attributes()...)
	if retained != "" && reason != "snapshot" && reason != "resync" && reason != "layout_changed" && reason != "new_device" && reason != "missing_cache" {
		b.log.Debug("output balance retained", append(attrs, logattr.KeyBalanceReason, retained)...)
	} else {
		b.log.Debug("output balance selected", append(attrs, logattr.KeyBalanceReason, reason)...)
	}
}

// resetOutputBalance is shared by initial snapshots and full overflow resyncs;
// even matching identities cannot retain history across missed events. The
// caller holds wire then mu (OUT-15, ADR 0039).
func (b *Backend) resetOutputBalance(sinks []SinkInfo, reason string) {
	b.outBalance = make(map[string]*outputBalance)
	for _, d := range sinks {
		b.observeOutputBalance(d, reason)
	}
}

// balanceForWrite reads missing channel information without guessing a
// layout. Normal writes use the cache under wire (OUT-15, ADR 0039).
func (b *Backend) balanceForWrite(k *conn, name string) (*outputBalance, error) {
	b.mu.Lock()
	s := b.outBalance[name]
	b.mu.Unlock()
	if s != nil {
		return s, nil
	}
	read := k.sinkByName
	if b.outputRead != nil {
		read = b.outputRead
	}
	d, err := read(name)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.observeOutputBalance(d, "missing_cache")
	s = b.outBalance[name]
	if s == nil {
		return nil, fmt.Errorf("output channel positions and volumes are missing or inconsistent")
	}
	return s, nil
}

// attributes includes positions with every ratio vector so its ordering is
// unambiguous in debug logs (OUT-13, ADR 0039).
func (s *outputBalance) attributes() []any {
	source := "server"
	ratios := s.ratios
	if len(ratios) == 0 {
		source = "equal"
		ratios = make([]float64, len(s.positions))
		for i := range ratios {
			ratios[i] = 1
		}
	} else if outputLevel(s.read) == 0 || slices.Equal(s.read, s.written) {
		source = "remembered"
	}
	return []any{logattr.KeyChannelPositions, []byte(s.positions), logattr.KeyBalanceRatios, slices.Clone(ratios), logattr.KeyBalanceSource, source}
}

// channelPercents preserves fractional channel levels in diagnostics rather
// than hiding balance differences behind integer rounding (OUT-13, ADR 0039).
func channelPercents(volumes proto.ChannelVolumes) []float64 {
	p := make([]float64, len(volumes))
	for i, v := range volumes {
		p[i] = v.Norm() * 100
	}
	return p
}

// outputLevel uses the loudest channel for outputs only; streams and inputs
// retain their average conversion (OUT-14, ADR 0039).
func outputLevel(volumes proto.ChannelVolumes) float64 {
	var loudest proto.Volume
	for _, v := range volumes {
		loudest = max(loudest, v)
	}
	return loudest.Norm()
}

// scaled rounds each channel from the retained proportions, never from the
// preceding rounded write (OUT-14, OUT-15, ADR 0039).
func (s *outputBalance) scaled(level float64) proto.ChannelVolumes {
	level = math.Max(0, math.Min(mixer.MaxBoost, level))
	volumes := make(proto.ChannelVolumes, len(s.positions))
	for i := range volumes {
		ratio := 1.0
		if len(s.ratios) != 0 {
			ratio = s.ratios[i]
		}
		volumes[i] = proto.NormVolume(level * ratio)
	}
	return volumes
}

// observe retains exact own echoes and zero, otherwise accepting the server's
// balance before level defense (OUT-15, OUT-16, ADR 0039).
func (s *outputBalance) observe(d SinkInfo) string {
	s.read = slices.Clone(d.Volumes)
	if len(s.written) != 0 && slices.Equal(s.written, d.Volumes) {
		return "own_write"
	}
	if outputLevel(d.Volumes) == 0 {
		return "zero"
	}
	s.written = nil
	s.ratios = make([]float64, len(d.Volumes))
	var loudest proto.Volume
	for _, v := range d.Volumes {
		loudest = max(loudest, v)
	}
	for i, v := range d.Volumes {
		s.ratios[i] = float64(v) / float64(loudest)
	}
	return ""
}
