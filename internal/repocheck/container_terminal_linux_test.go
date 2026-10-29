package repocheck

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// containerTerminal gives make a real controlling terminal, so Ctrl-C follows
// the terminal's foreground group instead of a test-selected PID (QA-16, ADR 0034).
func containerTerminal(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = master.Close() })
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = slave.Close() })
	settings, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	settings.Lflag |= unix.ISIG | unix.TOSTOP
	settings.Cc[unix.VINTR] = 3
	if err := unix.IoctlSetTermios(int(slave.Fd()), unix.TCSETS, settings); err != nil {
		t.Fatal(err)
	}
	return master, slave
}

// TestQA16_TerminalCancellationKeepsDispatcherForeground gates the dispatcher
// before its second flock and presses actual terminal Ctrl-C. TOSTOP and a worker
// stdin read also expose accidental background terminal access (QA-16, ADR 0034).
func TestQA16_TerminalCancellationKeepsDispatcherForeground(t *testing.T) {
	root := containerFixture(t)
	runSystemdStub(t, root, 2)
	write := func(name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("foreground-gate", `#!/bin/bash
trap 'exit 130' INT
echo "$BASHPID" > foreground-pid
until [[ -f worker-ready ]]; do sleep 0.01; done
: > foreground-ready
until [[ -f release-foreground ]]; do sleep 0.01; done
`)
	write("worker-gate", `#!/bin/sh
echo "$$" > worker-pid
: > worker-ready
until [ -f release-worker ]; do sleep 0.01; done
if read -r line; then exit 1; fi
: > stdin-eof
`)
	runner := filepath.Join(root, "test/container/runner.sh")
	replaceIn(t, runner,
		"# The folder is made here", "echo \"$BASHPID\" > runner-pid\ntrap 'echo $? > runner-status' EXIT\n# The folder is made here",
		"\n    exec 6<&-\n", "\n    exec 6<&-\n    : > stopping\n",
		"        flock 6\n", "        if [[ $recipe == systemd-fedora ]]; then \"$repo/foreground-gate\"; fi\n        flock 6\n")
	replaceIn(t, filepath.Join(root, "fake-podman"), "    start) if", "    start) ./worker-gate; if")
	master, slave := containerTerminal(t)
	cmd := exec.Command("make", "test-systemd", "RUNS=3")
	cmd.Dir = root
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = slave.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	output := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, master)
		output <- buf.String()
	}()
	// A failing old runner may leave separate stopped process groups. Releasing
	// gates and killing only fixture groups keeps the negative check bounded
	// (QA-16, ADR 0034).
	t.Cleanup(func() {
		write("release-worker", "")
		write("release-foreground", "")
		for _, name := range []string{"worker-pid", "foreground-pid", "runner-pid"} {
			data, err := os.ReadFile(filepath.Join(root, name))
			if err != nil {
				continue
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				continue
			}
			group, err := syscall.Getpgid(pid)
			if err == nil && group > 0 && group != syscall.Getpgrp() {
				_ = syscall.Kill(-group, syscall.SIGKILL)
			}
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	})
	waitFile := func(name string) bool {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(filepath.Join(root, name)); err == nil {
				return true
			}
			time.Sleep(10 * time.Millisecond)
		}
		return false
	}
	if !waitFile("foreground-ready") {
		t.Fatal("foreground gate was not reached")
	}
	group, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPGRP)
	if err != nil || group != cmd.Process.Pid {
		t.Error("dispatcher gave its foreground command the terminal", err)
	}
	if _, err := master.Write([]byte{3}); err != nil {
		t.Fatal(err)
	}
	if !waitFile("stopping") {
		t.Fatal("terminal Ctrl-C did not enter draining")
	}
	if _, err := master.Write([]byte{3}); err != nil {
		t.Fatal(err)
	}
	write("release-worker", "")
	if !waitFile("runner-status") {
		t.Fatal("runner did not finish draining under TOSTOP")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Error("canceled make succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("make did not exit")
	}
	select {
	case text := <-output:
		if !strings.Contains(text, "systemd-ubuntu: 1 passed, 0 failed (canceled)") {
			t.Errorf("missing drained summary:\n%s", text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a process still holds the terminal")
	}
	status, err := os.ReadFile(filepath.Join(root, "runner-status"))
	if err != nil || strings.TrimSpace(string(status)) != "130" {
		t.Errorf("runner status %q (%v), want 130", status, err)
	}
	if _, err := os.Stat(filepath.Join(root, "stdin-eof")); err != nil {
		t.Error("worker stdin was not EOF", err)
	}
	runs := auditRuns(t, root)
	if len(runs) != 1 || len(runs["systemd-ubuntu PASS"]) != 1 {
		t.Errorf("want only the started Ubuntu run, no fallback run: %v", runs)
	}
	for _, pattern := range []string{"live/*", ".tmp/run-*", ".tmp/stop-*", ".audit/logs/*"} {
		if left, _ := filepath.Glob(filepath.Join(root, pattern)); len(left) != 0 {
			t.Errorf("left behind %s: %v", pattern, left)
		}
	}
	lock := exec.Command("flock", "-n", filepath.Join(root, "git-common/apptrol-containers.lock"), "true")
	if err := lock.Run(); err != nil {
		t.Errorf("shared lock still held: %v", err)
	}
}
