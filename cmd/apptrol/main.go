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
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/Dzobash/apptrol/internal/audio/pulse"
	"github.com/Dzobash/apptrol/internal/config"
	"github.com/Dzobash/apptrol/internal/controller/rawmidi"
	"github.com/Dzobash/apptrol/internal/logging"
	"github.com/Dzobash/apptrol/internal/version"
)

// errNotImplemented is returned by commands that are planned but not built yet.
var errNotImplemented = errors.New("not implemented yet (planned for Phase 1)")

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run parses arguments and dispatches to a command. It returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("apptrol", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "path to the configuration file (default: ~/.config/apptrol/config.toml)")
	showVersion := fs.Bool("version", false, "print version information and exit")
	fs.Usage = func() {
		fmt.Fprint(stderr, `Usage: apptrol [flags] [command]

Commands:
  run      run the service (default)
  list     show playing apps and input devices with the names used for matching
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

	cmd := "run"
	if fs.NArg() > 0 {
		cmd = fs.Arg(0)
	}
	if fs.NArg() > 1 {
		fmt.Fprintf(stderr, "apptrol: unexpected arguments after %q: %v\n", cmd, fs.Args()[1:])
		return 2
	}

	var err error
	switch cmd {
	case "run":
		err = cmdRun(*configPath)
	case "list":
		err = cmdList(*configPath, stdout, func() (*pulse.Listing, error) { return pulse.List("") })
	case "check":
		err = cmdCheck(*configPath, stdout)
	case "test":
		err = runTest(*configPath, stdout, stderr)
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

func cmdRun(configPath string) error {
	_ = configPath
	return errNotImplemented
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
		fmt.Fprintf(tw, "  %s\t%s\t%s: %s\n", a.Control, app.Name, app.Type, strings.Join(app.Match, ", "))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(cfg.Layout().Assignments) == 0 {
		fmt.Fprintln(stdout, "  (no controls assigned)")
	}
	fmt.Fprintf(stdout, "Logging: %s to %s\n", cfg.Log.Level, strings.Join(cfg.Log.Outputs, " and "))
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
func runTest(configPath string, stdout, stderr io.Writer) error {
	terminal := false
	logs, _ := logging.New(config.Log{Level: "info", Outputs: []string{config.OutputJournald},
		Journald: config.JournaldOutput{Format: "text"}}, logging.Options{Stdout: stderr, Journal: &terminal})
	defer func() { _ = logs.Close() }()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return cmdTest(ctx, testPort(configPath), stdout, logs.Logger(),
		func(log *slog.Logger, port string) device { return rawmidi.New(log, port) })
}
