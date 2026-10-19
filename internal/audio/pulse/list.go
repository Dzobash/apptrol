package pulse

import (
	"fmt"
	"sort"
)

// Listing is everything `apptrol list` shows.
type Listing struct {
	Server        string // e.g. "PulseAudio (on PipeWire 1.6.0) 15.0.0"
	DefaultSource string // name of the default capture device
	DefaultSink   string // name of the default output device (OUT-12)
	Streams       []StreamInfo
	Sources       []SourceInfo
	Sinks         []SinkInfo
}

// OutputDescription returns the description of the output (sink) with the
// server's index, "" if there is none; a stream names its output this way.
func (l *Listing) OutputDescription(index uint32) string {
	for _, s := range l.Sinks {
		if s.Index == index {
			return s.Description
		}
	}
	return ""
}

// List connects once and reads the playback streams, capture devices and
// output devices (CFG-10, OUT-12). Streams are sorted by app name, sources
// and sinks by description.
func List(server string) (*Listing, error) {
	k, err := dial(server)
	if err != nil {
		return nil, err
	}
	defer k.shutdown()

	info, err := k.serverInfo()
	if err != nil {
		return nil, fmt.Errorf("reading server information: %w", err)
	}
	l := &Listing{Server: info.PackageName + " " + info.PackageVersion,
		DefaultSource: info.DefaultSourceName, DefaultSink: info.DefaultSinkName}
	if l.Streams, err = k.streams(); err != nil {
		return nil, fmt.Errorf("listing playback streams: %w", err)
	}
	if l.Sources, err = k.sources(); err != nil {
		return nil, fmt.Errorf("listing capture devices: %w", err)
	}
	if l.Sinks, err = k.sinks(); err != nil {
		return nil, fmt.Errorf("listing outputs: %w", err)
	}
	sort.SliceStable(l.Streams, func(i, j int) bool {
		a, b := l.Streams[i], l.Streams[j]
		if a.AppName != b.AppName {
			return a.AppName < b.AppName
		}
		return a.ID < b.ID
	})
	sort.SliceStable(l.Sources, func(i, j int) bool { return l.Sources[i].Description < l.Sources[j].Description })
	sort.SliceStable(l.Sinks, func(i, j int) bool { return l.Sinks[i].Description < l.Sinks[j].Description })
	return l, nil
}
