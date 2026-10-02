package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	// Make `list` fail the same way everywhere, with or without a running server.
	t.Setenv("PULSE_SERVER", "unix:"+filepath.Join(t.TempDir(), "no-such-socket"))
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{"version flag", []string{"--version"}, 0, "apptrol ", ""},
		{"version command", []string{"version"}, 0, "apptrol ", ""},
		{"help", []string{"-h"}, 0, "", "Usage: apptrol"},
		{"list without audio server", []string{"list"}, 1, "", "Is PipeWire"},
		{"unknown command", []string{"frobnicate"}, 2, "", `unknown command "frobnicate"`},
		{"extra arguments", []string{"list", "extra"}, 2, "", "unexpected arguments"},
		{"unknown flag", []string{"--nope"}, 2, "", "flag provided but not defined"},
		// LOG-16: an invalid --log-level stops before anything starts.
		{"log level invalid", []string{"--log-level", "verbose", "run"}, 2, "", `--log-level "verbose" is not valid`},
		{"log level valid", []string{"--log-level", "debug", "version"}, 0, "apptrol ", ""},
		{"log level in help", []string{"-h"}, 0, "", "-log-level"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d (stderr: %q)", code, tt.wantCode, stderr.String())
			}
			if !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.wantStdout)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCFG11_Check(t *testing.T) {
	// No apps installed, whatever the machine has (LAUNCH-10).
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_DATA_DIRS", t.TempDir())
	valid := writeConfig(t, `
[apps.spotify]
name  = "Spotify"
match = ["spotify"]
max_volume = 150

[apps.mic]
type  = "input"
match = ["GoXLR"]

[apps.spare]
match = ["spare"]

[layouts.default]
slider1 = "spotify"
slider8 = "mic"

[layouts.default.buttons]
record = { app = "com.obsproject.Studio", if_running = "skip" }
m8     = { mode = "hold_to_talk", talk_over = true }
`)
	invalid := writeConfig(t, "[layouts.default]\nslider9 = \"ghost\"\n")
	missing := filepath.Join(t.TempDir(), "none.toml")

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout []string
		wantStderr []string
	}{
		{"valid", []string{"--config", valid, "check"}, 0,
			[]string{": OK", "Controller: nanoKONTROL2", "slider1  Spotify", "app: spotify  max 150 %", "slider8  mic", "input: GoXLR",
				"Buttons:", "m8      hold_to_talk, talk_over", "record  starts com.obsproject.Studio, unless it runs",
				"Logging: warn to journald", "warning: apps.spare: not assigned",
				`warning: layouts.default.buttons.record: desktop ID "com.obsproject.Studio" is not installed`}, nil},
		{"invalid", []string{"--config", invalid, "check"}, 1,
			nil, []string{"apptrol check:", "slider9: unknown control", `app "ghost" is not defined`}},
		{"missing", []string{"--config", missing, "check"}, 1,
			nil, []string{"no configuration file at " + missing, "examples/config.toml"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d\nstdout: %s\nstderr: %s", code, tt.wantCode, stdout.String(), stderr.String())
			}
			for _, want := range tt.wantStdout {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("stdout missing %q:\n%s", want, stdout.String())
				}
			}
			for _, want := range tt.wantStderr {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr missing %q:\n%s", want, stderr.String())
				}
			}
		})
	}
}

func TestCheck_DefaultPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"check"}, &stdout, &stderr); code != 1 ||
		!strings.Contains(stderr.String(), filepath.Join(dir, "apptrol", "config.toml")) {
		t.Errorf("code %d, stderr %q: want the default path in the message", code, stderr.String())
	}
}
