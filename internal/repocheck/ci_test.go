package repocheck

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestQA16_SystemdSummaryAudit exercises the shipped helper without a container;
// old evidence and package-install records must never claim a Status pass
// (QA-16, ADR 0034).
func TestQA16_SystemdSummaryAudit(t *testing.T) {
	const commit = "abcdef0123456789abcdef0123456789abcdef0123"
	const ubuntu = "recipe=systemd-ubuntu commit=abcdef0 result=PASS seconds=6\n"
	const fedora = "seconds=7 result=PASS commit=abcdef0 recipe=systemd-fedora\n"
	const missing = "no result | —"
	for _, tc := range []struct {
		name, audit, initial, source, ubuntu, fedora string
		absent                                       bool
	}{
		{name: "both pass", audit: ubuntu + fedora, initial: "0", ubuntu: "✅ pass | 6 s", fedora: "✅ pass | 7 s"},
		{name: "completion order", audit: fedora + ubuntu, initial: "0", ubuntu: "✅ pass | 6 s", fedora: "✅ pass | 7 s"},
		{name: "ubuntu fails", audit: strings.ReplaceAll(ubuntu, "PASS", "FAIL") + fedora, initial: "0", ubuntu: "❌ fail | 6 s", fedora: "✅ pass | 7 s"},
		{name: "fedora fails", audit: ubuntu + strings.ReplaceAll(fedora, "PASS", "FAIL"), initial: "0", ubuntu: "✅ pass | 6 s", fedora: "❌ fail | 7 s"},
		{name: "one missing", audit: ubuntu, initial: "0", ubuntu: "✅ pass | 6 s", fedora: missing},
		{name: "absent file", initial: "0", absent: true, ubuntu: missing, fedora: missing},
		{name: "empty file", initial: "0", ubuntu: missing, fedora: missing},
		{name: "same commit history", audit: ubuntu + fedora + strings.ReplaceAll(ubuntu, "PASS", "FAIL"), initial: "2", ubuntu: "❌ fail | 6 s", fedora: missing},
		{name: "historical commit", audit: "recipe=systemd-fedora commit=1234567 result=PASS seconds=9\n" + ubuntu, initial: "1", ubuntu: "✅ pass | 6 s", fedora: missing},
		{name: "no new records", audit: ubuntu + fedora, initial: "2", ubuntu: missing, fedora: missing},
		{name: "shortened log", audit: ubuntu, initial: "2", ubuntu: missing, fedora: missing},
		{name: "missing boundary", audit: ubuntu + fedora, ubuntu: missing, fedora: missing},
		{name: "invalid boundary", audit: ubuntu + fedora, initial: "-1", ubuntu: missing, fedora: missing},
		{name: "wrong commit", audit: strings.ReplaceAll(ubuntu+fedora, "abcdef0", "1234567"), initial: "0", ubuntu: missing, fedora: missing},
		{name: "longer abbreviation", audit: strings.ReplaceAll(ubuntu+fedora, "abcdef0", "abcdef012345"), initial: "0", ubuntu: "✅ pass | 6 s", fedora: "✅ pass | 7 s"},
		{name: "install records", audit: strings.ReplaceAll(ubuntu+fedora, "systemd-", "install-") + ubuntu, initial: "0", ubuntu: "✅ pass | 6 s", fedora: missing},
		{name: "unrelated recipe", audit: "recipe=sample commit=abcdef0 result=PASS seconds=1\n", initial: "0", ubuntu: missing, fedora: missing},
		{name: "duplicate records", audit: ubuntu + strings.ReplaceAll(ubuntu, "PASS", "FAIL") + fedora, initial: "0", ubuntu: missing, fedora: "✅ pass | 7 s"},
		{name: "unknown result", audit: strings.ReplaceAll(ubuntu, "PASS", "CANCELED"), initial: "0", ubuntu: missing, fedora: missing},
		{name: "missing commit", audit: strings.ReplaceAll(ubuntu, " commit=abcdef0", ""), initial: "0", ubuntu: missing, fedora: missing},
		{name: "invalid commit", audit: strings.ReplaceAll(ubuntu, "abcdef0", "<commit>"), initial: "0", ubuntu: missing, fedora: missing},
		{name: "duplicate commit", audit: strings.TrimSpace(ubuntu) + " commit=abcdef0\n", initial: "0", ubuntu: missing, fedora: missing},
		{name: "duplicate recipe", audit: strings.TrimSpace(ubuntu) + " recipe=systemd-ubuntu\n", initial: "0", ubuntu: missing, fedora: missing},
		{name: "missing result", audit: "recipe=systemd-ubuntu commit=abcdef0 seconds=6\n", initial: "0", ubuntu: missing, fedora: missing},
		{name: "duplicate result", audit: strings.TrimSpace(ubuntu) + " result=FAIL\n", initial: "0", ubuntu: missing, fedora: missing},
		{name: "missing time", audit: strings.ReplaceAll(ubuntu, " seconds=6", ""), initial: "0", ubuntu: "✅ pass | —", fedora: missing},
		{name: "invalid time", audit: strings.ReplaceAll(ubuntu, "seconds=6", "seconds=-6"), initial: "0", ubuntu: "✅ pass | —", fedora: missing},
		{name: "duplicate time", audit: strings.TrimSpace(ubuntu) + " seconds=8\n", initial: "0", ubuntu: "✅ pass | —", fedora: missing},
		{name: "zero time", audit: strings.ReplaceAll(ubuntu, "seconds=6", "seconds=0"), initial: "0", ubuntu: "✅ pass | 0 s", fedora: missing},
		{name: "invalid source", audit: ubuntu + fedora, initial: "0", source: "<script>", ubuntu: missing, fedora: missing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			helper := filepath.Join(root, "summary.sh")
			if err := os.WriteFile(helper, readRepoFile(t, ".github", "scripts", "systemd-summary.sh"), 0o600); err != nil {
				t.Fatal(err)
			}
			audit := filepath.Join(root, "runs.log")
			if !tc.absent {
				if err := os.WriteFile(audit, []byte(tc.audit), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			source, heading := commit, commit[:7]
			if tc.source != "" {
				source, heading = tc.source, "unknown"
			}
			out, err := exec.Command("bash", helper, audit, tc.initial, source).CombinedOutput()
			if err != nil {
				t.Fatalf("summary: %v\n%s", err, out)
			}
			want := fmt.Sprintf("### Systemd in containers — commit %s\n\n| Distro | Result | Time |\n|---|---|---|\n| Ubuntu 26.04 | %s |\n| Fedora 44 | %s |\n", heading, tc.ubuntu, tc.fedora)
			if string(out) != want {
				t.Fatalf("summary:\n%s\nwant:\n%s", out, want)
			}
			if !tc.absent {
				data, err := os.ReadFile(audit)
				if err != nil || string(data) != tc.audit {
					t.Fatalf("audit changed: %v\n%s", err, data)
				}
			}
		})
	}
}

// TestQA16_SystemdSummaryWorkflow keeps the boundary before setup and the summary
// before diagnostics/upload, including failed or canceled attempts (QA-16,
// ADR 0034). Helper tests alone cannot prove the workflow calls it correctly.
func TestQA16_SystemdSummaryWorkflow(t *testing.T) {
	workflow := string(readRepoFile(t, ".github", "workflows", "ci.yml"))
	_, systemd, ok := strings.Cut(workflow, "\n  systemd:\n")
	if !ok {
		t.Fatal("missing systemd job")
	}
	systemd, _, _ = strings.Cut(systemd, "\n  lint:\n")
	steps := strings.Split(systemd, "\n      - ")
	index := 0
	for _, marker := range []string{
		"uses: actions/checkout@",
		"name: Record the audit boundary",
		"uses: actions/setup-go@",
		"name: Prepare rootless Podman",
		"run: make test-systemd AGENT=ci",
		"name: Summarize each distro",
		"name: Host confinement diagnostics",
		"name: Upload container audit",
	} {
		index++
		if index >= len(steps) || !strings.Contains(steps[index], marker) {
			t.Fatalf("step %d must contain %q", index, marker)
		}
	}
	for _, want := range []string{"id: systemd-audit", `echo "initial=$initial" >> "$GITHUB_OUTPUT"`, `echo "commit=$(git rev-parse HEAD)" >> "$GITHUB_OUTPUT"`} {
		if !strings.Contains(steps[2], want) {
			t.Errorf("boundary step missing %q", want)
		}
	}
	for _, want := range []string{
		"if: always()",
		"AUDIT_INITIAL: ${{ steps.systemd-audit.outputs.initial }}",
		"AUDIT_COMMIT: ${{ steps.systemd-audit.outputs.commit }}",
		`run: bash .github/scripts/systemd-summary.sh .audit/runs.log "$AUDIT_INITIAL" "$AUDIT_COMMIT" >> "$GITHUB_STEP_SUMMARY"`,
	} {
		if !strings.Contains(steps[6], want) {
			t.Errorf("summary step missing %q", want)
		}
	}
	for _, want := range []string{"if: always()", "path: .audit/", "include-hidden-files: true", "retention-days: 14", "name: systemd-container-audit-${{ github.run_attempt }}"} {
		if !strings.Contains(steps[8], want) {
			t.Errorf("audit upload missing %q", want)
		}
	}
	if strings.Contains(steps[5], "continue-on-error") || strings.Contains(steps[5], "|| true") {
		t.Error("test failure must remain a job failure")
	}

	// Execute the workflow boundary itself against fixtures, without Git or any
	// runner setup. A fake Git returns only this synthetic commit (QA-16, ADR 0034).
	_, capture, ok := strings.Cut(steps[2], "run: |\n")
	if !ok {
		t.Fatal("missing boundary shell")
	}
	for _, tc := range []struct {
		name, audit, want string
	}{
		{"absent", "", "initial=0"},
		{"history", "old\nold\n", "initial=2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, ".audit"), 0o700); err != nil {
				t.Fatal(err)
			}
			if tc.audit != "" {
				if err := os.WriteFile(filepath.Join(root, ".audit", "runs.log"), []byte(tc.audit), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(root, "git"), []byte("#!/bin/sh\nprintf 'abcdef0123456789abcdef0123456789abcdef0123\\n'\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(root, "outputs")
			cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", capture)
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "PATH="+root+":"+os.Getenv("PATH"), "GITHUB_OUTPUT="+output)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("boundary: %v\n%s", err, out)
			}
			data, err := os.ReadFile(output)
			if err != nil || string(data) != tc.want+"\ncommit=abcdef0123456789abcdef0123456789abcdef0123\n" {
				t.Fatalf("boundary outputs: %v\n%s", err, data)
			}
		})
	}
}
