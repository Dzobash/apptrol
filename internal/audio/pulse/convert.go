package pulse

import (
	"math"
	"strings"

	"github.com/jfreymuth/pulse/proto"

	"github.com/Dzobash/apptrol/internal/mixer"
)

// StreamInfo describes a playback stream.
type StreamInfo struct {
	mixer.Stream
	Media    string  // media.name, e.g. a song or tab title
	Volume   float64 // 0–1 as shown by desktop mixers; above 1 when boosted
	Muted    bool
	Channels int
	Sink     uint32 // index of the output it plays on
}

// SourceInfo describes a capture device.
type SourceInfo struct {
	mixer.Device
	Index    uint32 // the server's index, used in change notifications
	Volume   float64
	Muted    bool
	Channels int
}

// SinkInfo describes an output device (a PulseAudio "sink"): speakers,
// headphones, or one output of an audio interface (OUT-12, ADR 0027).
type SinkInfo struct {
	mixer.Device
	Index    uint32 // the server's index, used in change notifications and by streams
	Volume   float64
	Muted    bool
	Channels int
}

func streamFromReply(r *proto.GetSinkInputInfoReply) StreamInfo {
	return StreamInfo{
		Stream: mixer.Stream{
			ID:      r.SinkInputIndex,
			AppName: prop(r.Properties, "application.name", ""),
			Binary:  prop(r.Properties, "application.process.binary", ""),
			Corked:  r.Corked, // MEDIA-10
		},
		Media:    prop(r.Properties, "media.name", r.MediaName),
		Volume:   fromChannelVolumes(r.ChannelVolumes),
		Muted:    r.Muted,
		Channels: len(r.ChannelVolumes),
		Sink:     r.SinkIndex,
	}
}

// sourceFromReply converts a source; ok is false for the monitor of an output,
// which is not a capture device and must not be matched by inputs (a GoXLR
// output's monitor would otherwise match "GoXLR").
func sourceFromReply(r *proto.GetSourceInfoReply) (SourceInfo, bool) {
	// For a source, this field holds the index of the sink it monitors.
	if r.MonitorSourceIndex != proto.Undefined || prop(r.Properties, "device.class", "") == "monitor" {
		return SourceInfo{}, false
	}
	return SourceInfo{
		Device: mixer.Device{
			Name:        r.SourceName,
			Description: prop(r.Properties, "device.description", r.SourceName),
		},
		Index:    r.SourceIndex,
		Volume:   fromChannelVolumes(r.ChannelVolumes),
		Muted:    r.Mute,
		Channels: len(r.ChannelVolumes),
	}, true
}

// sinkFromReply converts an output device. A sink is never a monitor: the
// monitor of an output is a source, which sourceFromReply leaves out, so
// monitors are listed neither as inputs nor as outputs (OUT-12).
func sinkFromReply(r *proto.GetSinkInfoReply) SinkInfo {
	return SinkInfo{
		Device: mixer.Device{
			Name:        r.SinkName,
			Description: prop(r.Properties, "device.description", r.SinkName),
		},
		Index:    r.SinkIndex,
		Volume:   fromChannelVolumes(r.ChannelVolumes),
		Muted:    r.Mute,
		Channels: len(r.ChannelVolumes),
	}
}

// prop returns a property as a string, or def if it is missing or empty.
func prop(p proto.PropList, key, def string) string {
	if e, ok := p[key]; ok {
		if s := strings.TrimRight(e.String(), "\x00"); s != "" {
			return s
		}
	}
	return def
}

// channelVolumes sets every channel to v (0–1, up to mixer.MaxBoost for apps
// with a raised max_volume).
//
// The value is the one desktop mixers (KDE, GNOME, pavucontrol) show as a
// percentage, so a slider at 50 % shows 50 % there too (CTRL-02). The audio
// server applies its own perceptual curve on top, as it does for those mixers.
// Channel balance is not kept: all channels get the same volume.
func channelVolumes(channels int, v float64) proto.ChannelVolumes {
	if channels < 1 {
		channels = 1
	}
	vol := proto.NormVolume(math.Max(0, math.Min(mixer.MaxBoost, v))) // CTRL-03
	cv := make(proto.ChannelVolumes, channels)
	for i := range cv {
		cv[i] = vol
	}
	return cv
}

// fromChannelVolumes returns the average volume of all channels as 0–1 (more
// than 1 when boosted), as desktop mixers show it.
func fromChannelVolumes(cv proto.ChannelVolumes) float64 {
	if len(cv) == 0 {
		return 0
	}
	return cv.Avg().Norm()
}
