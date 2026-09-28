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
//	version  print version information
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

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
		err = cmdList()
	case "check":
		err = cmdCheck(*configPath)
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

func cmdList() error {
	return errNotImplemented
}

func cmdCheck(configPath string) error {
	_ = configPath
	return errNotImplemented
}
