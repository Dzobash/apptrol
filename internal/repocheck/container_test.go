package repocheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// containerFixture substitutes fixed host dependencies in a private copy, rather
// than giving the production runner an environment-controlled test backdoor
// (QA-16, ADR 0034). Real rootless/systemd enforcement is checked by test-systemd.
func containerFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Fatal("make is required for QA-16")
	}
	root := t.TempDir()
	write := func(name, data string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"run.sh", "runner.sh", "build.sh", "build-limits.sh", "build-probe.Dockerfile", "limits.sh", "proof.sh", "status.sh", "recipe-entry.sh", "valid.toml", "containers.conf"} {
		data := string(readRepoFile(t, "test", "container", name))
		if name == "runner.sh" || name == "build.sh" {
			fake := "'" + strings.ReplaceAll(filepath.Join(root, "fake-podman"), "'", "'\"'\"'") + "'"
			data = strings.ReplaceAll(data, "/usr/bin/podman", fake)
			fakeScope := "'" + strings.ReplaceAll(filepath.Join(root, "fake-systemd-run"), "'", "'\"'\"'") + "'"
			data = strings.ReplaceAll(data, "/usr/bin/systemd-run", fakeScope)
			data = strings.ReplaceAll(data, "$(git rev-parse --path-format=absolute --git-common-dir)", `"$repo/git-common"`)
			data = strings.ReplaceAll(data, "group=$(sed -n 's/^0:://p' /proc/self/cgroup)", "group=/apptrol-build-fixture.scope")
			data = strings.ReplaceAll(data, `base="/sys/fs/cgroup$group"`, `base="$repo/fake-cgroup"`)
			data = strings.ReplaceAll(data, "$(id -u)", "1500")
			data = strings.ReplaceAll(data, `HOME=$(getent passwd "1500" | cut -d: -f6)`, "HOME=/nonexistent")
			data = strings.ReplaceAll(data, `available=$(awk '/^MemAvailable:/ {print $2}' /proc/meminfo)`, "available=16777216")
			data = strings.ReplaceAll(data, "$(git branch --show-current)", "test-branch")
			data = strings.ReplaceAll(data, "$(git rev-parse --short HEAD)", "abcdef0")
		}
		write("test/container/"+name, data)
	}
	write("Makefile", string(readRepoFile(t, "Makefile")))
	write("git-common/apptrol-containers.lock", "")
	write("fake-cgroup/pids.max", "256\n")
	write("fake-cgroup/memory.max", "536870912\n")
	write("fake-cgroup/memory.swap.max", "0\n")
	write("fake-systemd-run", `#!/bin/sh
set -eu
printf '%s\n' "$*" >> scope-calls
while [ "$1" != /bin/bash ]; do shift; done
exec "$@"
`)
	write("test/container/empty-hooks/.gitkeep", "")
	write(".audit/recipes/sample/Dockerfile", "FROM scratch\n")
	write(".audit/recipes/sample/commands.sh", "echo must-not-run-on-host > host-marker\n")
	write("fake-podman", `#!/bin/sh
set -eu
if [ "$1" = --cgroup-manager=cgroupfs ]; then shift; fi
test "$1" = --default-mounts-file=/dev/null
shift
case "$1" in --hooks-dir=*/test/container/empty-hooks) shift ;; *) exit 1 ;; esac
printf '%s\n' "$*" >> calls
env >> received-environment
case "$1" in
    info) echo true ;;
    image) if [ "$2" = exists ]; then exit 1; fi ;;
    build)
        while [ "$#" -gt 0 ]; do
            if [ "$1" = --iidfile ]; then shift; echo probe-image > "$1"; fi
            shift
        done ;;
    create)
        : > mounts
        while [ "$#" -gt 0 ]; do
            if [ "$1" = --mount ]; then
                shift
                spec=$1
                source=${spec#type=bind,src=}
                source=${source%%,dst=*}
                case "$spec" in
                    *,dst=/src,ro=true) printf '%s|/src|false\n' "$source" >> mounts ;;
                    *,dst=/work,rw=true) printf '%s|/work|true\n' "$source" >> mounts ;;
                    *) exit 1 ;;
                esac
            fi
            shift
        done ;;
    inspect)
        case "$3" in
            *State.ExitCode*) echo 0 ;;
            *) cat mounts ;;
        esac ;;
    unshare) shift; "$@" ;;
    start|exec|rm) : ;;
    *) exit 1 ;;
esac
`)
	return root
}

// runContainerMake uses the real target so exported command-line Make variables
// are tested as well as the shell script (QA-16, ADR 0034).
func runContainerMake(t *testing.T, root, recipe string, extra ...string) ([]byte, error) {
	t.Helper()
	args := append([]string{"container-run", "RECIPE=" + recipe}, extra...)
	cmd := exec.Command("make", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "SSH_AUTH_SOCK=/private/test-socket", "CONTAINER_HOST=ssh://untrusted", "CONTAINERS_CONF=/private/untrusted.conf", "BASH_ENV="+filepath.Join(root, "injected.sh"))
	return cmd.CombinedOutput()
}

// TestQA16_RecipeMountsEnvironmentAndFlags checks the exact mount inventory,
// fixed limits, no environment leakage and no host execution of commands.sh.
func TestQA16_RecipeMountsEnvironmentAndFlags(t *testing.T) {
	root := containerFixture(t)
	if err := os.WriteFile(filepath.Join(root, "injected.sh"), []byte("touch env-marker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runContainerMake(t, root, "sample", "FLAGS=--privileged", "JOBS=999", "AGENT=gpt-6-astra", "SHELL=/nonexistent", ".SHELLFLAGS=invalid")
	if err != nil {
		t.Fatalf("make: %v\n%s", err, out)
	}
	calls, err := os.ReadFile(filepath.Join(root, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--memory=512m", "--memory-swap=512m", "--pids-limit=256", "--network=none", "--unsetenv-all", "--http-proxy=false", "--image-volume=ignore", "--entrypoint /bin/sh"} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("missing fixed argument %s in %s", want, calls)
		}
	}
	if strings.Contains(string(calls), "--privileged") || strings.Count(string(calls), "--mount ") != 2 {
		t.Errorf("unexpected flags or mount count: %s", calls)
	}
	for _, want := range []string{"src=" + root + ",dst=/src,ro=true", ",dst=/work,rw=true"} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("missing mount %s", want)
		}
	}
	env, err := os.ReadFile(filepath.Join(root, "received-environment"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"SSH_AUTH_SOCK=", "FLAGS=", "BASH_ENV=", "CONTAINER_HOST=", "/private/untrusted.conf", "JOBS=999", "AGENT="} {
		if strings.Contains(string(env), forbidden) {
			t.Errorf("Podman received %s", forbidden)
		}
	}
	for _, marker := range []string{"host-marker", "env-marker"} {
		if _, err := os.Stat(filepath.Join(root, marker)); !os.IsNotExist(err) {
			t.Errorf("host code ran: %s", marker)
		}
	}
	audit, err := os.ReadFile(filepath.Join(root, ".audit", "runs.log"))
	if err != nil || strings.Count(string(audit), "\n") != 1 || !strings.Contains(string(audit), "result=PASS") {
		t.Fatalf("audit: %s (%v)", audit, err)
	}
	if strings.Contains(string(audit), root) {
		t.Error("audit leaked a host path")
	}
	if !strings.Contains(string(audit), "agent=gpt-6-astra ") {
		t.Error("audit lost the caller's model label")
	}
}

// TestQA16_BuildScopeAndProbeFailClosed checks the build budget and refuses recipe
// execution when either the scope or its uncached proof fails (ADR 0037).
func TestQA16_BuildScopeAndProbeFailClosed(t *testing.T) {
	for _, failure := range []string{"", "scope", "probe", "kernel limit"} {
		t.Run(failure, func(t *testing.T) {
			root := containerFixture(t)
			switch failure {
			case "scope":
				if err := os.WriteFile(filepath.Join(root, "fake-systemd-run"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			case "probe":
				path := filepath.Join(root, "fake-podman")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(data), "build)\n", "build) exit 1 ;;\n    unused)\n")), 0o755); err != nil {
					t.Fatal(err)
				}
			case "kernel limit":
				if err := os.WriteFile(filepath.Join(root, "fake-cgroup/pids.max"), []byte("max\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			out, err := runContainerMake(t, root, "sample")
			if failure != "" {
				if err == nil {
					t.Fatalf("accepted failed build proof: %s", out)
				}
				calls, _ := os.ReadFile(filepath.Join(root, "calls"))
				if strings.Contains(string(calls), "\ncreate ") || strings.Contains(string(calls), "--tag ") {
					t.Fatalf("failed proof reached recipe: %s", calls)
				}
				audit, _ := os.ReadFile(filepath.Join(root, ".audit/runs.log"))
				if !strings.Contains(string(audit), "result=FAIL") {
					t.Fatal("missing build failure audit")
				}
				return
			}
			if err != nil {
				t.Fatalf("make: %v\n%s", err, out)
			}
			scope, _ := os.ReadFile(filepath.Join(root, "scope-calls"))
			for _, want := range []string{"--user --scope", "Delegate=yes", "TasksMax=256", "MemoryMax=512M", "MemorySwapMax=0"} {
				if !strings.Contains(string(scope), want) {
					t.Errorf("missing scope restriction %s", want)
				}
			}
			calls, _ := os.ReadFile(filepath.Join(root, "calls"))
			if strings.Count(string(calls), "--cgroup-parent=/apptrol-build-fixture.scope/build") != 2 ||
				!strings.Contains(string(calls), "--no-cache --cgroupns=host") ||
				!strings.Contains(string(calls), "--no-cache --cgroupns=private --tag") {
				t.Fatalf("probe/recipe build restrictions: %s", calls)
			}
		})
	}
}

// TestQA16_BadRecipesRefused covers path traversal, shell syntax, option syntax,
// missing folders and the private systemd selector (QA-16, ADR 0034).
func TestQA16_BadRecipesRefused(t *testing.T) {
	for _, recipe := range []string{"", "../sample", "/tmp/sample", "sample/name", "sample name", "$(touch injected)", "--privileged", "@systemd", "missing"} {
		t.Run(recipe, func(t *testing.T) {
			root := containerFixture(t)
			out, err := runContainerMake(t, root, recipe)
			if err == nil {
				t.Fatalf("accepted %q: %s", recipe, out)
			}
			if _, err := os.Stat(filepath.Join(root, "calls")); !os.IsNotExist(err) {
				t.Error("invalid recipe reached Podman")
			}
			audit, err := os.ReadFile(filepath.Join(root, ".audit", "runs.log"))
			if err != nil || strings.Count(string(audit), "\n") != 1 || !strings.Contains(string(audit), "result=FAIL") {
				t.Fatalf("missing failure record: %s (%v)", audit, err)
			}
		})
	}
}

// TestQA16_SharedLockRefusesSymlinks protects the host-only common Git directory
// lock used by every worktree (QA-16, ADR 0034).
func TestQA16_SharedLockRefusesSymlinks(t *testing.T) {
	root := containerFixture(t)
	lock := filepath.Join(root, "git-common/apptrol-containers.lock")
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "lock-target")
	if err := os.WriteFile(target, []byte("untouched"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, lock); err != nil {
		t.Fatal(err)
	}
	if out, err := runContainerMake(t, root, "sample"); err == nil {
		t.Fatalf("accepted unsafe shared lock: %s", out)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "untouched" {
		t.Fatalf("lock followed symlink: %q (%v)", data, err)
	}
	if _, err := os.Stat(filepath.Join(root, "calls")); !os.IsNotExist(err) {
		t.Fatal("unsafe lock reached Podman")
	}
}

// TestQA16_SymlinkRecipeRefused prevents an otherwise valid name from selecting
// files outside the recipe directory (QA-16, ADR 0034).
func TestQA16_SymlinkRecipeRefused(t *testing.T) {
	root := containerFixture(t)
	if err := os.Symlink(t.TempDir(), filepath.Join(root, ".audit", "recipes", "linked")); err != nil {
		t.Fatal(err)
	}
	if out, err := runContainerMake(t, root, "linked"); err == nil {
		t.Fatalf("accepted linked recipe: %s", out)
	}
}

// TestQA16_ExtraArgumentsIgnored tests the public entry point separately from
// Make's exported variables (QA-16, ADR 0034).
func TestQA16_ExtraArgumentsIgnored(t *testing.T) {
	root := containerFixture(t)
	cmd := exec.Command("/bin/sh", "test/container/run.sh", "sample", "--privileged", "--volume=/private:/private")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runner: %v\n%s", err, out)
	}
	calls, err := os.ReadFile(filepath.Join(root, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(calls), "--privileged") || strings.Contains(string(calls), "/private") {
		t.Errorf("extra arguments reached Podman: %s", calls)
	}
}

// TestQA16_UnexpectedMountAndContainerFailureAreRecorded checks that a default
// mount fails before start and runtime failures cannot be reported as green
// (QA-16, ADR 0034).
func TestQA16_UnexpectedMountAndContainerFailureAreRecorded(t *testing.T) {
	for _, tc := range []struct {
		name, old, replacement string
		mustNotStart           bool
	}{
		{"unexpected mount", "*) cat mounts ;;", "*) cat mounts; echo '/private|/private|true' ;;", true},
		{"container failed", "*State.ExitCode*) echo 0 ;;", "*State.ExitCode*) echo 17 ;;", false},
		{"engine rejected limit", "start|exec|rm) : ;;", "start) exit 125 ;;\n    exec|rm) : ;;", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := containerFixture(t)
			path := filepath.Join(root, "fake-podman")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(data), tc.old, tc.replacement)), 0o755); err != nil {
				t.Fatal(err)
			}
			if out, err := runContainerMake(t, root, "sample"); err == nil {
				t.Fatalf("reported success: %s", out)
			}
			calls, err := os.ReadFile(filepath.Join(root, "calls"))
			if err != nil {
				t.Fatal(err)
			}
			if tc.mustNotStart && strings.Contains(string(calls), "\nstart ") {
				t.Error("container with unexpected mount was started")
			}
			audit, err := os.ReadFile(filepath.Join(root, ".audit", "runs.log"))
			if err != nil || strings.Count(string(audit), "result=FAIL") != 1 || strings.Contains(string(audit), "result=PASS") {
				t.Fatalf("incorrect failure audit: %s (%v)", audit, err)
			}
			logs, err := filepath.Glob(filepath.Join(root, ".audit", "logs", "*.log"))
			if err != nil || len(logs) != 1 {
				t.Fatalf("failed run did not retain its log: %v (%v)", logs, err)
			}
		})
	}
}

// TestQA16_AuditLabelIsData prevents a caller label from forging a record or
// reaching Podman (QA-16, ADR 0034 implementation clarification).
func TestQA16_AuditLabelIsData(t *testing.T) {
	for _, label := range []string{"model\nresult=PASS", "model;touch-injected", strings.Repeat("a", 65), "model name"} {
		root := containerFixture(t)
		if out, err := runContainerMake(t, root, "sample", "AGENT="+label); err == nil {
			t.Fatalf("accepted invalid audit label: %s", out)
		}
		audit, err := os.ReadFile(filepath.Join(root, ".audit", "runs.log"))
		if err != nil || strings.Count(string(audit), "\n") != 1 || !strings.Contains(string(audit), "agent=unspecified ") || !strings.Contains(string(audit), "result=FAIL") {
			t.Fatalf("incorrect audit: %s (%v)", audit, err)
		}
		if _, err := os.Stat(filepath.Join(root, "calls")); !os.IsNotExist(err) {
			t.Error("invalid label reached Podman")
		}
	}
}
