// Command apptrol controls per-application volume on Linux with a MIDI controller.
//
// Usage:
//
//	apptrol [flags] [command]
//
// Commands:
//
//	run      run the service (default)
//	list     show playing apps and input devices with the names used for matching
//	check    validate the configuration file and exit
//	test     show what the controller sends and light its buttons, to check it
//	version  print version information
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/Dzobash/apptrol/internal/audio/pulse"
	"github.com/Dzobash/apptrol/internal/config"
	"github.com/Dzobash/apptrol/internal/controller/rawmidi"
	"github.com/Dzobash/apptrol/internal/desktop"
	"github.com/Dzobash/apptrol/internal/launcher"
	"github.com/Dzobash/apptrol/internal/logging"
	"github.com/Dzobash/apptrol/internal/power"
	"github.com/Dzobash/apptrol/internal/service"
	"github.com/Dzobash/apptrol/internal/state"
	"github.com/Dzobash/apptrol/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run parses arguments and dispatches to a command. It returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("apptrol", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "path to the configuration file (default: ~/.config/apptrol/config.toml)")
	showVersion := fs.Bool("version", false, "print version information and exit")
	logLevel := fs.String("log-level", "", "log level for this run only: debug, info, warn or error\n(default: [log] level in the configuration, which stays unchanged)")
	fs.Usage = func() {
		fmt.Fprint(stderr, `Usage: apptrol [flags] [command]

Commands:
  run      run the service (default)
  list     show playing apps and input devices with the names used for matching
  list apps [search]
           show installed apps with the desktop IDs used by launchers
  check    validate the configuration file and exit
  test     show what the controller sends and light its buttons, to check it
  version  print version information

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if *showVersion {
		fmt.Fprintln(stdout, version.String())
		return 0
	}

	// --log-level is checked before anything starts (LOG-16).
	var level *slog.Level
	if *logLevel != "" {
		l, err := logging.ParseLevel(*logLevel)
		if err != nil {
			fmt.Fprintf(stderr, "apptrol: --log-level %q is not valid (use debug, info, warn or error)\n", *logLevel)
			return 2
		}
		level = &l
	}

	cmd := "run"
	if fs.NArg() > 0 {
		cmd = fs.Arg(0)
	}
	// `list apps [search]` is the only command with arguments (LAUNCH-09).
	listApps := cmd == "list" && fs.NArg() > 1 && fs.Arg(1) == "apps"
	if fs.NArg() > 1 && (!listApps || fs.NArg() > 3) {
		fmt.Fprintf(stderr, "apptrol: unexpected arguments after %q: %v\n", cmd, fs.Args()[1:])
		return 2
	}

	var err error
	switch cmd {
	case "run":
		err = cmdRun(*configPath, level, *logLevel)
	case "list":
		if listApps {
			err = cmdListApps(stdout, fs.Arg(2), launcher.Installed())
			break
		}
		err = cmdList(*configPath, stdout, func() (*pulse.Listing, error) { return pulse.List("") })
	case "check":
		err = cmdCheck(*configPath, stdout)
	case "test":
		err = runTest(*configPath, level, stdout, stderr)
	case "version":
		fmt.Fprintln(stdout, version.String())
		return 0
	default:
		fmt.Fprintf(stderr, "apptrol: unknown command %q\n\n", cmd)
		fs.Usage()
		return 2
	}

	if err != nil {
		fmt.Fprintf(stderr, "apptrol %s: %v\n", cmd, err)
		return 1
	}
	return 0
}

// cmdRun runs the service until SIGTERM or Ctrl+C. level, if not nil, is the
// --log-level override; levelName is its name for the start record.
func cmdRun(configPath string, level *slog.Level, levelName string) error {
	path, err := resolveConfigPath(configPath)
	if err != nil {
		return err
	}
	stateDir, err := config.DefaultStateDir()
	if err != nil {
		return err
	}
	// Log to the journal (or the terminal) until the configuration says otherwise.
	logs, err := logging.New(defaultLog, logging.Options{Level: level})
	if err != nil {
		return err
	}
	defer func() { _ = logs.Close() }()
	log := logs.Logger()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return service.Run(ctx, service.Options{
		ConfigPath:   path,
		StatePath:    filepath.Join(stateDir, state.FileName),
		Log:          log,
		Logs:         logs,
		LogLevelFlag: levelName,
		Audio:        pulse.New(log, ""),
		Desktop:      desktop.New(log, ""),
		Power:        power.New(log, ""),
		Launcher:     launcher.NewStarter(log),
		NewController: func(port string) service.Controller {
			return rawmidi.New(log, port)
		},
	})
}

// defaultLog is used before a configuration is loaded, and by `apptrol test`.
var defaultLog = config.Log{Level: "info", Outputs: []string{config.OutputJournald},
	Journald: config.JournaldOutput{Format: "text"}}

// printButtons lists the buttons the layout sets (CFG-13); the others keep
// their defaults.
func printButtons(w io.Writer, l config.Layout) error {
	if len(l.Buttons) == 0 {
		return nil
	}
	names := make([]string, 0, len(l.Buttons))
	for n := range l.Buttons {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Fprintln(w, "Buttons:")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, n := range names {
		b := l.Buttons[n]
		what := b.Mode
		switch {
		case b.App != "":
			what = "starts " + b.App
		case len(b.Command) > 0:
			what = "runs " + strings.Join(b.Command, " ")
		}
		if b.Launcher() && b.IfRunning == config.IfRunningSkip {
			what += ", unless it runs"
		}
		if b.TalkOver {
			what += ", talk_over"
		}
		fmt.Fprintf(tw, "  %s\t%s\n", n, what)
	}
	return tw.Flush()
}

// cmdCheck validates the configuration file and prints what it assigns (CFG-11).
func cmdCheck(configPath string, stdout io.Writer) error {
	path, err := resolveConfigPath(configPath)
	if err != nil {
		return err
	}
	cfg, warnings, err := config.Load(path)
	if errors.Is(err, config.ErrNotFound) {
		return fmt.Errorf("no configuration file at %s\n"+
			"  To start from the example:\n"+
			"    mkdir -p %s && cp /usr/share/doc/apptrol/examples/config.toml %s",
			path, filepath.Dir(path), path)
	}
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "%s: OK\n\n", path)
	fmt.Fprintf(stdout, "Controller: %s\n", cfg.Controller.Port)
	fmt.Fprintf(stdout, "Layout %q:\n", config.DefaultLayout)
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, a := range cfg.Layout().Assignments {
		app := cfg.Apps[a.AppID]
		extra := ""
		if app.MaxVolume != 100 {
			extra = fmt.Sprintf("\tmax %d %%", app.MaxVolume)
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s: %s%s\n", a.Control, app.Name, app.Type, strings.Join(app.Match, ", "), extra)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(cfg.Layout().Assignments) == 0 {
		fmt.Fprintln(stdout, "  (no controls assigned)")
	}
	if err := printButtons(stdout, cfg.Layout()); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Logging: %s to %s\n", cfg.Log.Level, strings.Join(cfg.Log.Outputs, " and "))
	// Launchers of apps that are not installed: a warning only, as the app
	// may be installed later (LAUNCH-10).
	for _, m := range launcher.Installed().Missing(cfg.LauncherApps()) {
		warnings = append(warnings, fmt.Sprintf("layouts.%s.buttons.%s: desktop ID %q is not installed (`apptrol list apps` shows the installed ones)",
			m.Layout, m.Button, m.DesktopID))
	}
	for _, w := range warnings {
		fmt.Fprintf(stdout, "\nwarning: %s", w)
	}
	if len(warnings) > 0 {
		fmt.Fprintln(stdout)
	}
	return nil
}

// resolveConfigPath returns the --config value, or the default location.
func resolveConfigPath(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	return config.DefaultPath()
}

// runTest wires `apptrol test` to the real controller, a terminal logger and
// Ctrl+C.
func runTest(configPath string, level *slog.Level, stdout, stderr io.Writer) error {
	terminal := false
	logs, _ := logging.New(defaultLog, logging.Options{Stdout: stderr, Journal: &terminal, Level: level})
	defer func() { _ = logs.Close() }()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return cmdTest(ctx, testPort(configPath), stdout, logs.Logger(),
		func(log *slog.Logger, port string) device { return rawmidi.New(log, port) })
}
