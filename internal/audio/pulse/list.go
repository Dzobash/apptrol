package pulse

import (
	"fmt"
	"sort"
)

// Listing is everything `apptrol list` shows.
type Listing struct {
	Server        string // e.g. "PulseAudio (on PipeWire 1.6.0) 15.0.0"
	DefaultSource string // name of the default capture device
	Streams       []StreamInfo
	Sources       []SourceInfo
	Outputs       map[uint32]string // output (sink) descriptions by index
}

// List connects once and reads the playback streams, capture devices and
// outputs (CFG-10). Streams are sorted by app name, sources by description.
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
	l := &Listing{Server: info.PackageName + " " + info.PackageVersion, DefaultSource: info.DefaultSourceName}
	if l.Streams, err = k.streams(); err != nil {
		return nil, fmt.Errorf("listing playback streams: %w", err)
	}
	if l.Sources, err = k.sources(); err != nil {
		return nil, fmt.Errorf("listing capture devices: %w", err)
	}
	if l.Outputs, err = k.sinkNames(); err != nil {
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
	return l, nil
}
