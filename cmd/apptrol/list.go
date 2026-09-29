package main

import (
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"text/tabwriter"

	"github.com/Dzobash/apptrol/internal/audio/pulse"
	"github.com/Dzobash/apptrol/internal/config"
	"github.com/Dzobash/apptrol/internal/mixer"
)

// cmdList shows the playing apps and input devices with the names used for
// matching, and which control each one is on (CFG-10).
func cmdList(configPath string, stdout io.Writer, list func() (*pulse.Listing, error)) error {
	l, err := list()
	if err != nil {
		return fmt.Errorf("%w\n  Is PipeWire (pipewire-pulse) or PulseAudio running for this user?", err)
	}

	var setup *mixer.Setup
	note := ""
	path, err := resolveConfigPath(configPath)
	if err == nil {
		var cfg *config.Config
		cfg, _, err = config.Load(path)
		switch {
		case err == nil:
			s := cfg.Setup()
			setup = &s
			note = fmt.Sprintf("Controls are from %s.", path)
		case errors.Is(err, config.ErrNotFound):
			note = fmt.Sprintf("There is no configuration at %s yet, so no controls are shown.", path)
		default:
			note = fmt.Sprintf("The configuration at %s has errors (see `apptrol check`), so no controls are shown.", path)
		}
	}
	return printListing(stdout, l, setup, note)
}

// printListing writes the listing. setup may be nil when there is no valid
// configuration; the CONTROL column then shows "-".
func printListing(w io.Writer, l *pulse.Listing, setup *mixer.Setup, note string) error {
	streamControl := func(uint32) string { return "-" }
	inputControl := map[string]string{}
	if setup != nil {
		m := mixer.New(*setup, mixer.State{})
		m.Handle(pulse.Snapshot(l.Streams, l.Sources))
		label := func(id string) string {
			c, _ := m.ControlOf(id)
			return fmt.Sprintf("%s (%s)", c, setup.Targets[id].Name)
		}
		streamControl = func(id uint32) string {
			if t := m.StreamTarget(id); t != "" {
				return label(t)
			}
			return "-"
		}
		ids := make([]string, 0, len(setup.Targets))
		for id := range setup.Targets {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			if dev := m.InputDevice(id); dev != "" {
				inputControl[dev] = label(id)
			}
		}
	}

	fmt.Fprintf(w, "Audio server: %s\n\n", l.Server)

	fmt.Fprintln(w, "Playing apps. Put part of NAME or BINARY into an app's match list:")
	if len(l.Streams) == 0 {
		fmt.Fprintln(w, "  Nothing is playing. Start the app, play something, and run `apptrol list` again.")
	} else {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "  NAME\tBINARY\tVOLUME\tOUTPUT\tCONTROL")
		for _, s := range l.Streams {
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", dash(s.AppName), dash(s.Binary),
				volume(s.Volume, s.Muted), dash(l.Outputs[s.Sink]), streamControl(s.ID))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}

	fmt.Fprintln(w, "\nInput devices. Put part of DESCRIPTION or NAME into an input's match list:")
	if len(l.Sources) == 0 {
		fmt.Fprintln(w, "  No input devices found.")
	} else {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "  DESCRIPTION\tNAME\tVOLUME\tCONTROL")
		for _, d := range l.Sources {
			desc := d.Description
			if d.Name == l.DefaultSource {
				desc += " (default)"
			}
			ctl := inputControl[d.Name]
			if ctl == "" {
				ctl = "-"
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", desc, d.Name, volume(d.Volume, d.Muted), ctl)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}

	if note != "" {
		fmt.Fprintf(w, "\n%s\n", note)
	}
	return nil
}

func volume(v float64, muted bool) string {
	s := fmt.Sprintf("%d%%", int(math.Round(v*100)))
	if muted {
		s += " muted"
	}
	return s
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
