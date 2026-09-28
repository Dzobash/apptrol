package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
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
		{"default is run", nil, 1, "", "not implemented yet"},
		{"list", []string{"list"}, 1, "", "apptrol list: not implemented yet"},
		{"check", []string{"check"}, 1, "", "apptrol check: not implemented yet"},
		{"unknown command", []string{"frobnicate"}, 2, "", `unknown command "frobnicate"`},
		{"extra arguments", []string{"list", "extra"}, 2, "", "unexpected arguments"},
		{"unknown flag", []string{"--nope"}, 2, "", "flag provided but not defined"},
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
