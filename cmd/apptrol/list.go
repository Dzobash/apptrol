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

// listSection is one section of `apptrol list` that can also be asked for
// alone with `apptrol list <word>` (LAUNCH-16).
type listSection int

const (
	sectionAll     listSection = iota // every section, as `apptrol list` prints them
	sectionPlaying                    // `apptrol list playing`
	sectionInputs                     // `apptrol list inputs`
	sectionOutputs                    // `apptrol list outputs`
)

// listSectionOf maps the word after `list` to the section it names (LAUNCH-16).
func listSectionOf(word string) (listSection, bool) {
	switch word {
	case "playing":
		return sectionPlaying, true
	case "inputs":
		return sectionInputs, true
	case "outputs":
		return sectionOutputs, true
	}
	return sectionAll, false
}

// cmdList shows the playing apps, input devices and output devices with the
// names used for matching, and which control each app, input and output is on
// (CFG-10, OUT-12, ADR 0027).
func cmdList(configPath string, stdout io.Writer, list func() (*pulse.Listing, error)) error {
	return cmdListSection(configPath, stdout, list, sectionAll)
}

// cmdListSection shows one section of the listing alone: `apptrol list outputs`
// prints only the output devices, `list inputs` only the input devices and
// `list playing` only the playing apps, so a name for a match list can be
// found without scrolling through everything (LAUNCH-16). Every section is
// written by the same code as in `apptrol list`, so the two cannot drift apart.
func cmdListSection(configPath string, stdout io.Writer, list func() (*pulse.Listing, error), section listSection) error {
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
	return printListing(stdout, l, setup, note, section)
}

// printListing writes the listing. setup may be nil when there is no valid
// configuration; the CONTROL column then shows "-". section is one section to
// write alone, or all of them (LAUNCH-16).
func printListing(w io.Writer, l *pulse.Listing, setup *mixer.Setup, note string, section listSection) error {
	streamControl := func(uint32) string { return "-" }
	inputControl := map[string]string{}
	outputControl := map[string]string{} // an output device on two controls lists both (OUT-04)
	if setup != nil {
		m := mixer.New(*setup, mixer.State{})
		m.Handle(pulse.Snapshot(l.Streams, l.Sources, l.Sinks, l.DefaultSink))
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
		// In control order, so a device on two controls lists them as the log does.
		for _, k := range []mixer.ControlKind{mixer.Slider, mixer.Knob} {
			for col := 1; col <= mixer.NumColumns; col++ {
				id, ok := setup.Assignments[mixer.Control{Kind: k, Column: col}]
				dev := m.OutputDevice(id)
				if !ok || dev == "" {
					continue
				}
				if outputControl[dev] != "" {
					outputControl[dev] += ", "
				}
				outputControl[dev] += label(id)
			}
		}
	}

	// The server line belongs to the whole listing; a section asked for alone
	// starts with its own header (LAUNCH-16).
	if section == sectionAll {
		fmt.Fprintf(w, "Audio server: %s\n\n", l.Server)
	}

	wrote := false
	if section == sectionAll || section == sectionPlaying {
		heading := "Playing apps. Put part of NAME or BINARY into an app's match list:"
		if section == sectionPlaying {
			// The standalone command also points to the launcher command,
			// because the user is looking for names there; `apptrol list`
			// itself keeps its shorter heading, so its output does not
			// change (LAUNCH-16).
			heading = "Playing apps. Put part of NAME or BINARY into an app's match list (`apptrol list apps` shows the desktop IDs a launcher uses):"
		}
		if err := printPlayingSection(w, l, streamControl, heading); err != nil {
			return err
		}
		wrote = true
	}
	if section == sectionAll || section == sectionInputs {
		if wrote {
			fmt.Fprintln(w)
		}
		if err := printInputSection(w, l, inputControl); err != nil {
			return err
		}
		wrote = true
	}
	if section == sectionAll || section == sectionOutputs {
		if wrote {
			fmt.Fprintln(w)
		}
		if err := printOutputSection(w, l, outputControl); err != nil {
			return err
		}
	}

	if note != "" {
		fmt.Fprintf(w, "\n%s\n", note)
	}
	return nil
}

// printPlayingSection writes the playing apps section under heading. Only the
// heading differs between `apptrol list` and `apptrol list playing`; the table
// is the same, from this one place (CFG-10, LAUNCH-16).
func printPlayingSection(w io.Writer, l *pulse.Listing, streamControl func(uint32) string, heading string) error {
	fmt.Fprintln(w, heading)
	if len(l.Streams) == 0 {
		fmt.Fprintln(w, "  Nothing is playing. Start the app, play something, and run `apptrol list` again.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  NAME\tBINARY\tVOLUME\tOUTPUT\tCONTROL")
	for _, s := range l.Streams {
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", dash(s.AppName), dash(s.Binary),
			volume(s.Volume, s.Muted), dash(l.OutputDescription(s.Sink)), streamControl(s.ID))
	}
	return tw.Flush()
}

// printInputSection writes the input devices section (CFG-10, LAUNCH-16).
func printInputSection(w io.Writer, l *pulse.Listing, inputControl map[string]string) error {
	fmt.Fprintln(w, "Input devices. Put part of DESCRIPTION or NAME into an input's match list:")
	if len(l.Sources) == 0 {
		fmt.Fprintln(w, "  No input devices found.")
		return nil
	}
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
	return tw.Flush()
}

// printOutputSection writes the output devices section, with * on the one the
// system uses right now: an output with device = "default" follows it (OUT-12,
// ADR 0027, LAUNCH-16).
func printOutputSection(w io.Writer, l *pulse.Listing, outputControl map[string]string) error {
	fmt.Fprintln(w, "Output devices (* = the system's default). Put part of DESCRIPTION or NAME into an output's match list:")
	if len(l.Sinks) == 0 {
		fmt.Fprintln(w, "  No output devices found.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "    DESCRIPTION\tNAME\tVOLUME\tCONTROL")
	for _, d := range l.Sinks {
		mark := " "
		if d.Name == l.DefaultSink {
			mark = "*"
		}
		fmt.Fprintf(tw, "  %s %s\t%s\t%s\t%s\n", mark, d.Description, d.Name, volume(d.Volume, d.Muted), dash(outputControl[d.Name]))
	}
	return tw.Flush()
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
