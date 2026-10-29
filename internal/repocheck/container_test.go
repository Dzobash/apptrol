package repocheck

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// containerFixture substitutes fixed host dependencies in a private copy, rather
// than giving the production runner an environment-controlled test backdoor
// (QA-16, ADR 0034). Real rootless/systemd enforcement is checked by test-systemd.
func containerFixture(t *testing.T) string {
	t.Helper()
	return containerFixtureAt(t, t.TempDir(), true)
}

// containerFixtureAt builds the fixture in root. With fakeGit false the runner's
// Git lookups stay real, so a test can give the checkout a hostile .git/config
// (QA-16, ADR 0034).
// fakeFullCommit is what the fixture's stand-in Git answers for HEAD; its first
// seven digits are the stand-in short commit (QA-17).
const fakeFullCommit = "abcdef0123456789abcdef0123456789abcdef01"

func containerFixtureAt(t *testing.T, root string, fakeGit bool) string {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Fatal("make is required for QA-16")
	}
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
	for _, name := range []string{"run.sh", "runner.sh", "build.sh", "build-apptrol.sh", "build-limits.sh", "build-probe.Dockerfile", "limits.sh", "proof.sh", "status.sh", "recipe-entry.sh", "valid.toml", "containers.conf", "ubuntu.Dockerfile", "fedora.Dockerfile", "pick-package.sh"} {
		data := string(readRepoFile(t, "test", "container", name))
		if name == "runner.sh" || name == "build.sh" {
			fake := "'" + strings.ReplaceAll(filepath.Join(root, "fake-podman"), "'", "'\"'\"'") + "'"
			data = strings.ReplaceAll(data, "/usr/bin/podman", fake)
			fakeScope := "'" + strings.ReplaceAll(filepath.Join(root, "fake-systemd-run"), "'", "'\"'\"'") + "'"
			data = strings.ReplaceAll(data, "/usr/bin/systemd-run", fakeScope)
			data = strings.ReplaceAll(data, "group=$(sed -n 's/^0:://p' /proc/self/cgroup)", "group=/apptrol-build-fixture.scope")
			data = strings.ReplaceAll(data, `base="/sys/fs/cgroup$group"`, `base="$repo/fake-cgroup"`)
			if name == "runner.sh" {
				data = strings.ReplaceAll(data, `base="$repo/fake-cgroup"`, `base="$repo/fake-runtime-cgroup"`)
				data = strings.ReplaceAll(data, `/proc/$pid/`, root+`/fake-proc/`)
				// The clean-up reads the host's own mounts (QA-18).
				data = strings.ReplaceAll(data, `/proc/self/mountinfo`, root+`/fake-self-mountinfo`)
				// Lets a test wait until the preflight is over and the runner is at the
				// lock, instead of guessing with a pause (QA-18).
				// Only with the stand-in Git: in a real repository the marker would be an
				// untracked file and make the audit line say dirty=yes.
				if fakeGit {
					if !strings.Contains(data, "\nflock 9\n") {
						t.Fatal("runner.sh no longer takes the shared lock on its own line; update this fixture")
					}
					data = strings.ReplaceAll(data, "\nflock 9\n", "\n: > \"$repo/reached-lock\"\nflock 9\n")
				}
			}
			data = strings.ReplaceAll(data, "$(id -u)", "1500")
			data = strings.ReplaceAll(data, `HOME=$(getent passwd "1500" | cut -d: -f6)`, "HOME=/nonexistent")
			data = strings.ReplaceAll(data, `available=$(awk '/^MemAvailable:/ {print $2}' /proc/meminfo)`, "available=16777216")
			if fakeGit {
				data = strings.ReplaceAll(data, "$(git rev-parse --show-toplevel)", "$repo")
				data = strings.ReplaceAll(data, "$(git rev-parse --path-format=absolute --git-common-dir)", `"$repo/git-common"`)
				data = strings.ReplaceAll(data, "$(git branch --show-current)", "test-branch")
				data = strings.ReplaceAll(data, "$(git rev-parse --short HEAD)", "abcdef0")
				data = strings.ReplaceAll(data, "$(git rev-parse HEAD)", fakeFullCommit)
				data = strings.ReplaceAll(data, "git status --porcelain --untracked-files=normal -- . ':!.audit/runs.log'", "printf ' M fixture'")
			}
		}
		if name == "pick-package.sh" {
			data = strings.ReplaceAll(data, "/src/dist", filepath.Join(root, "dist"))
		}
		write("test/container/"+name, data)
	}
	write("Makefile", string(readRepoFile(t, "Makefile")))
	if fakeGit {
		write("git-common/apptrol-containers.lock", "")
	}
	write("fake-cgroup/pids.max", "256\n")
	write("fake-cgroup/memory.max", "536870912\n")
	write("fake-cgroup/memory.swap.max", "0\n")
	write("fake-runtime-cgroup/pids.max", "256\n")
	write("fake-runtime-cgroup/memory.max", "536870912\n")
	write("fake-runtime-cgroup/memory.swap.max", "0\n")
	write("fake-self-mountinfo", "")
	write("fake-proc/cgroup", "0::/fixture.scope\n")
	write("fake-proc/stat", "123 (runtime init) S 0 0 0 0 0 0 0 0 0 0 0 0 0 0 30 10\n")
	write("fake-proc/net/dev", "Inter-| Receive | Transmit\n face |bytes\n lo: 0\n")
	write("fake-proc/mountinfo", "1 2 0:3 / /sys/fs/cgroup ro,nosuid - cgroup2 cgroup rw\n")
	write("fake-systemd-run", `#!/bin/sh
set -eu
printf '%s\n' "$*" >> scope-calls
while [ "$1" != /bin/bash ]; do shift; done
# Every real scope is new; a second build in one fixture must not see the first's leaf.
# The fixture has one stand-in scope, so parallel builds take turns (QA-16, #172).
exec flock "$(dirname "$0")/fake-scope.lock" sh -c 'rm -rf "$0/fake-cgroup/supervisor"; shift; exec "$@"' "$(dirname "$0")" -- "$@"
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
        env > build-environment
        while [ "$#" -gt 0 ]; do
            if [ "$1" = --iidfile ]; then shift; echo probe-image > "$1"; fi
            if [ "$1" = --authfile ]; then shift; cat "$1" > build-auth; fi
            if [ "$1" = --cert-dir ]; then shift; test -z "$(ls -A "$1")"; fi
            shift
        done ;;
    create)
        # One mount list per container, and a count of the containers alive at
        # once, so parallel runs can be checked (QA-16, #172).
        test "$2" = --name
        name=$3
        mkdir -p live
        : > "live/$name"
        ls live | wc -l >> live-counts
        : > "mounts.$name"
        while [ "$#" -gt 0 ]; do
            if [ "$1" = --mount ]; then
                shift
                spec=$1
                source=${spec#type=bind,src=}
                source=${source%%,dst=*}
                case "$spec" in
                    *,dst=/src,ro=true) printf '%s|/src|false\n' "$source" >> "mounts.$name" ;;
                    *,dst=/work,rw=true) test -z "$(ls -A "$source")"; printf '%s|/work|true\n' "$source" >> "mounts.$name" ;;
                    *) exit 1 ;;
                esac
            fi
            shift
        done ;;
    inspect)
        case "$3" in
            *State.Pid*) echo 123 ;;
            *State.ExitCode*) echo 0 ;;
            *) cat "mounts.$4" ;;
        esac ;;
    unshare) shift; "$@" ;;
    init) : ;;
    start) if [ -e start-delay ]; then sleep "$(cat start-delay)"; fi ;;
    exec) : ;;
    rm) rm -f "live/$4" ;;
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
	// RUNS belongs to test-systemd only: a recipe ignores even an invalid one (#172).
	out, err := runContainerMake(t, root, "sample", "FLAGS=--privileged", "JOBS=999", "AGENT=gpt-6-astra", "SHELL=/nonexistent", ".SHELLFLAGS=invalid", "RUNS=0")
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
	for _, forbidden := range []string{"SSH_AUTH_SOCK=", "FLAGS=", "BASH_ENV=", "CONTAINER_HOST=", "/private/untrusted.conf", "JOBS=999", "AGENT=", "RUNS="} {
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
	if !strings.Contains(string(audit), "dirty=yes ") {
		t.Error("audit lost the uncommitted-source marker")
	}
	buildEnv, err := os.ReadFile(filepath.Join(root, "build-environment"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"GIT_CONFIG_GLOBAL=/dev/null\n", "GIT_CONFIG_NOSYSTEM=1\n", "GIT_TERMINAL_PROMPT=0\n", "GIT_ASKPASS=/bin/false\n", "GIT_SSH_COMMAND=/bin/false\n", "GIT_ALLOW_PROTOCOL=\n"} {
		if !strings.Contains(string(buildEnv), want) {
			t.Errorf("missing host Git restriction %q", want)
		}
	}
	auth, err := os.ReadFile(filepath.Join(root, "build-auth"))
	if err != nil || string(auth) != "{\"auths\":{}}\n" {
		t.Fatalf("build did not use empty registry auth: %s (%v)", auth, err)
	}
}

// TestQA16_SystemdBuildIgnoresMakeVariables exercises the real Make entry point
// and Go helper, stopping at a stand-in compiler before containers (ADR 0034).
func TestQA16_SystemdBuildIgnoresMakeVariables(t *testing.T) {
	root := containerFixture(t)
	helper := filepath.Join(root, "test/container/build-apptrol.sh")
	data, err := os.ReadFile(helper)
	if err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(root, "fake-go")
	quoted := "'" + strings.ReplaceAll(fake, "'", "'\"'\"'") + "'"
	data = []byte(strings.ReplaceAll(string(data), "go build", quoted+" build"))
	data = []byte(strings.ReplaceAll(string(data), "$(git rev-parse HEAD)", "abcdef0"))
	if err := os.WriteFile(helper, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > go-args\nenv > go-environment\nexit 17\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("make", "test-systemd", "BINARY=x; touch injected", "LDFLAGS=x\"; touch injected; #", "VERSION=x\"; touch injected; #", "COMMIT=x\"; touch injected; #", "DATE=x\"; touch injected; #", "GOFLAGS=-toolexec=/private/untrusted", "FLAGS=--privileged")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "17") {
		t.Fatalf("expected stand-in compiler failure: %v\n%s", err, out)
	}
	args, err := os.ReadFile(filepath.Join(root, "go-args"))
	if err != nil || !strings.Contains(string(args), "\n-o\nbin/apptrol\n./cmd/apptrol\n") || strings.Contains(string(args), "injected") {
		t.Fatalf("unsafe compiler arguments: %s (%v)", args, err)
	}
	env, err := os.ReadFile(filepath.Join(root, "go-environment"))
	if err != nil || strings.Contains(string(env), "GOFLAGS=") || strings.Contains(string(env), "BINARY=") {
		t.Fatalf("compiler inherited Make overrides: %s (%v)", env, err)
	}
	if _, err := os.Stat(filepath.Join(root, "injected")); !os.IsNotExist(err) {
		t.Fatal("Make build variables executed shell code")
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
	for _, recipe := range []string{"", "../sample", "/tmp/sample", "sample/name", "sample name", "$(touch injected)", "--privileged", "@systemd", "@install", "@clean", "install-ubuntu", "systemd-fedora", "missing"} {
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
		{"unexpected mount", `*) cat "mounts.$4" ;;`, `*) cat "mounts.$4"; echo '/private|/private|true' ;;`, true},
		{"container failed", "*State.ExitCode*) echo 0 ;;", "*State.ExitCode*) echo 17 ;;", false},
		{"engine rejected limit", "    start) ", "    start) exit 125 ;;\n    unused) ", false},
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

// TestQA16_HostRuntimeProofFailsClosed refuses image execution even when its
// shell would exit zero without checking limits: the fake container always
// exits zero, so every failing case is that case (QA-16, ADR 0037).
func TestQA16_HostRuntimeProofFailsClosed(t *testing.T) {
	for _, tc := range []struct{ name, path, value string }{
		{"correct", "", ""},
		{"memory", "fake-runtime-cgroup/memory.max", "max\n"},
		{"swap", "fake-runtime-cgroup/memory.swap.max", "1\n"},
		{"pids", "fake-runtime-cgroup/pids.max", "max\n"},
		{"nice", "fake-proc/stat", "123 (init) S 0 0 0 0 0 0 0 0 0 0 0 0 0 0 30 0\n"},
		{"network", "fake-proc/net/dev", "header\nheader\n lo: 0\n eth0: 0\n"},
		{"unreadable", "fake-runtime-cgroup/memory.max", ""},
		{"writable cgroup", "fake-proc/mountinfo", "1 2 0:3 / /sys/fs/cgroup rw - cgroup2 cgroup rw\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := containerFixture(t)
			if tc.path != "" {
				path := filepath.Join(root, tc.path)
				if tc.name == "unreadable" {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(path, []byte(tc.value), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			out, err := runContainerMake(t, root, "sample")
			calls, _ := os.ReadFile(filepath.Join(root, "calls"))
			audit, _ := os.ReadFile(filepath.Join(root, ".audit/runs.log"))
			if tc.name == "correct" {
				if err != nil || !strings.Contains(string(out), "PASS: host-read runtime") || !strings.Contains(string(audit), "result=PASS") {
					t.Fatalf("valid proof rejected: %v\n%s", err, out)
				}
				return
			}
			if err == nil || strings.Contains(string(calls), "\nstart ") || !strings.Contains(string(audit), "result=FAIL") || !strings.Contains(string(calls), "\nrm --force --ignore ") {
				t.Fatalf("failed host proof did not stop and clean up: %v\n%s\n%s\n%s", err, out, calls, audit)
			}
		})
	}
}

// gitIn runs Git for a test repository with no identity or config from the host.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=test", "-c", "user.email=", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestQA16_RepositoryGitConfigCannotRunHostCode checks that the audit lookups
// ignore a checkout's own fsmonitor setting, and that the audit line still tells
// the truth about the source (QA-16, ADR 0034).
func TestQA16_RepositoryGitConfigCannotRunHostCode(t *testing.T) {
	root := containerFixtureAt(t, t.TempDir(), false)
	marker := filepath.Join(t.TempDir(), "fsmonitor-ran")
	hook := filepath.Join(t.TempDir(), "fsmonitor")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nps -o args= -p \"$PPID\" >> '"+marker+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, root, "init", "--quiet", "--initial-branch=fixture")
	gitIn(t, root, "add", "--all")
	gitIn(t, root, "commit", "--quiet", "--message=fixture")
	gitIn(t, root, "config", "core.fsmonitor", hook)
	if out, err := runContainerMake(t, root, "sample"); err != nil {
		t.Fatalf("make: %v\n%s", err, out)
	}
	if caller, err := os.ReadFile(marker); !os.IsNotExist(err) {
		t.Fatalf("the checkout's core.fsmonitor ran on the host, called by %s", caller)
	}
	audit, err := os.ReadFile(filepath.Join(root, ".audit", "runs.log"))
	if err != nil || !strings.Contains(string(audit), "branch=fixture ") || !strings.Contains(string(audit), "dirty=no ") || !strings.Contains(string(audit), "result=PASS") {
		t.Fatalf("audit: %s (%v)", audit, err)
	}
}

// TestQA16_ForeignRepositoryRefused stops a checkout without its own Git data
// from recording another repository's commit in the audit line (QA-16, ADR 0034).
func TestQA16_ForeignRepositoryRefused(t *testing.T) {
	outer := t.TempDir()
	gitIn(t, outer, "init", "--quiet")
	gitIn(t, outer, "commit", "--quiet", "--allow-empty", "--message=outer")
	root := filepath.Join(outer, "checkout")
	containerFixtureAt(t, root, false)
	gitIn(t, outer, "add", "--all")
	gitIn(t, outer, "commit", "--quiet", "--message=checkout")
	if out, err := runContainerMake(t, root, "sample"); err == nil {
		t.Fatalf("accepted another repository: %s", out)
	}
	if _, err := os.Stat(filepath.Join(root, "calls")); !os.IsNotExist(err) {
		t.Fatal("foreign repository reached Podman")
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

// runSystemdMake runs the real `make test-systemd` target against a fixture
// prepared by runSystemdStub (QA-16, ADR 0034, #172).
func runSystemdMake(t *testing.T, root string, jobs int, extra ...string) ([]byte, error) {
	t.Helper()
	runSystemdStub(t, root, jobs)
	cmd := exec.Command("make", append([]string{"test-systemd"}, extra...)...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "SSH_AUTH_SOCK=/private/test-socket", "CONTAINERS_CONF=/private/untrusted.conf")
	return cmd.CombinedOutput()
}

// runSystemdStub replaces the Go build with a stand-in that only counts itself,
// and sets the memory reading so the runner gets jobs slots (QA-16, #172).
func runSystemdStub(t *testing.T, root string, jobs int) {
	t.Helper()
	stub := "#!/bin/bash\necho built >> binary-builds\n"
	if err := os.WriteFile(filepath.Join(root, "test/container/build-apptrol.sh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := filepath.Join(root, "test/container/runner.sh")
	data, err := os.ReadFile(runner)
	if err != nil {
		t.Fatal(err)
	}
	// jobs = (available - 4 GiB) / 512 MiB, in KiB as /proc/meminfo has it.
	available := 4194304 + jobs*524288
	patched := strings.ReplaceAll(string(data), "available=16777216", "available="+strconv.Itoa(available))
	if err := os.WriteFile(runner, []byte(patched), 0o755); err != nil {
		t.Fatal(err)
	}
}

// auditRuns returns the run ids in runs.log per recipe and result, e.g.
// "systemd-ubuntu PASS" → ids (QA-16, #172).
func auditRuns(t *testing.T, root string) map[string][]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".audit/runs.log"))
	if err != nil {
		t.Fatal(err)
	}
	runs := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := map[string]string{}
		for _, field := range strings.Fields(line) {
			if key, value, ok := strings.Cut(field, "="); ok {
				fields[key] = value
			}
		}
		key := fields["recipe"] + " " + fields["result"]
		runs[key] = append(runs[key], fields["run"])
	}
	return runs
}

// countLines counts the lines of a fixture file; a missing file has none.
func countLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "\n")
}

// TestQA16_RunsDefaultIsOneRunPerDistro keeps `make test-systemd` without RUNS as
// it was: one run per distro, each with its own build, and no summary (#172).
func TestQA16_RunsDefaultIsOneRunPerDistro(t *testing.T) {
	root := containerFixture(t)
	out, err := runSystemdMake(t, root, 8, "AGENT=claude-opus-5.5")
	if err != nil {
		t.Fatalf("make: %v\n%s", err, out)
	}
	runs := auditRuns(t, root)
	if len(runs) != 2 || len(runs["systemd-ubuntu PASS"]) != 1 || len(runs["systemd-fedora PASS"]) != 1 {
		t.Fatalf("want one passed run per distro: %v", runs)
	}
	if n := countLines(t, filepath.Join(root, "scope-calls")); n != 2 {
		t.Errorf("want one image build per distro, got %d", n)
	}
	if strings.Contains(string(out), "wall time") {
		t.Errorf("a single run printed the repeated-run summary:\n%s", out)
	}
}

// TestQA16_RunsRepeatsEachDistro checks RUNS=5 with three slots: five audit lines
// per distro with their own run ids, one binary and one image build per distro,
// never more than three containers at once, and RUNS kept away from Podman
// (QA-16, ADR 0034, #172).
func TestQA16_RunsRepeatsEachDistro(t *testing.T) {
	root := containerFixture(t)
	// A short pause at start lets the runs overlap, so the slot limit is tested.
	if err := os.WriteFile(filepath.Join(root, "start-delay"), []byte("0.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runSystemdMake(t, root, 3, "RUNS=5", "AGENT=claude-opus-5.5")
	if err != nil {
		t.Fatalf("make: %v\n%s", err, out)
	}
	runs := auditRuns(t, root)
	if len(runs) != 2 || len(runs["systemd-ubuntu PASS"]) != 5 || len(runs["systemd-fedora PASS"]) != 5 {
		t.Fatalf("want five passed runs per distro: %v", runs)
	}
	seen := map[string]bool{}
	for _, ids := range runs {
		for _, id := range ids {
			if seen[id] {
				t.Errorf("run id %s used twice", id)
			}
			seen[id] = true
		}
	}
	if n := countLines(t, filepath.Join(root, "binary-builds")); n != 1 {
		t.Errorf("want one binary build per call, got %d", n)
	}
	if n := countLines(t, filepath.Join(root, "scope-calls")); n != 2 {
		t.Errorf("want one image build per distro, got %d", n)
	}
	counts, err := os.ReadFile(filepath.Join(root, "live-counts"))
	if err != nil {
		t.Fatal(err)
	}
	most := 0
	for _, field := range strings.Fields(string(counts)) {
		n, err := strconv.Atoi(field)
		if err != nil {
			t.Fatal(err)
		}
		most = max(most, n)
	}
	if most > 3 || most < 2 {
		t.Errorf("want up to 3 containers at once and some overlap, saw at most %d", most)
	}
	for _, want := range []string{"systemd-ubuntu: 5 passed, 0 failed, 0 not started", "systemd-fedora: 5 passed, 0 failed, 0 not started", "up to 3 at once, wall time "} {
		if !strings.Contains(string(out), want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
	}
	for _, name := range []string{"calls", "received-environment"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "RUNS") {
			t.Errorf("RUNS reached Podman through %s", name)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(root, ".tmp", "run-*")); len(left) != 0 {
		t.Errorf("passed runs left folders: %v", left)
	}
}

// TestQA16_RunsCeilingIsEight gives the runner memory for 24 containers: per-user
// inotify instances, shared with the owner's desktop, still allow only 8 at once,
// and the audit lines say jobs=8 (QA-16, ADR 0034, #172).
func TestQA16_RunsCeilingIsEight(t *testing.T) {
	root := containerFixture(t)
	if err := os.WriteFile(filepath.Join(root, "start-delay"), []byte("0.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runSystemdMake(t, root, 24, "RUNS=10")
	if err != nil {
		t.Fatalf("make: %v\n%s", err, out)
	}
	counts, err := os.ReadFile(filepath.Join(root, "live-counts"))
	if err != nil {
		t.Fatal(err)
	}
	most := 0
	for _, field := range strings.Fields(string(counts)) {
		n, err := strconv.Atoi(field)
		if err != nil {
			t.Fatal(err)
		}
		most = max(most, n)
	}
	if most > 8 {
		t.Errorf("want at most 8 containers at once, saw %d", most)
	}
	if !strings.Contains(string(out), "up to 8 at once") {
		t.Errorf("summary lacks the ceiling:\n%s", out)
	}
	audit, err := os.ReadFile(filepath.Join(root, ".audit/runs.log"))
	if err != nil || strings.Count(string(audit), " jobs=8 ") != 20 {
		t.Fatalf("want 20 audit lines with jobs=8: %s (%v)", audit, err)
	}
}

// replaceIn applies old → new pairs to a fixture file, failing when one no
// longer matches, so a changed fixture cannot silently disable a test.
func replaceIn(t *testing.T, path string, pairs ...string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for i := 0; i+1 < len(pairs); i += 2 {
		if !strings.Contains(text, pairs[i]) {
			t.Fatalf("%s no longer contains %q; update this test", path, pairs[i])
		}
		text = strings.ReplaceAll(text, pairs[i], pairs[i+1])
	}
	if err := os.WriteFile(path, []byte(text), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestQA16_RunsStopWhenAFailureIsKnownWithFreeSlots: with 8 slots and only three
// runs started, nothing waits on the failed run, so the dispatcher must see it
// before starting the next. A gate holds every later start until the failure is in
// the audit log; no timing luck is involved. The run fails two ways: its check,
// and its cleanup after a passed check (QA-16, #172).
func TestQA16_RunsStopWhenAFailureIsKnownWithFreeSlots(t *testing.T) {
	const fastStart = `    start) if [ -e start-delay ]; then sleep "$(cat start-delay)"; fi ;;`
	// The third start or rm belongs to the first reused run; the others pass.
	const failThirdStart = `    start) n=$(flock starts.lock sh -c 'echo x >> starts; wc -l < starts'); [ "$n" -ne 3 ] ;;`
	const fastRm = `    rm) rm -f "live/$4" ;;`
	const failThirdRm = `    rm) rm -f "live/$4"; n=$(flock rms.lock sh -c 'echo x >> rms; wc -l < rms'); [ "$n" -ne 3 ] ;;`
	for _, tc := range []struct {
		name   string
		podman []string // old → new in the fake Podman
	}{
		{"check fails", []string{fastStart, failThirdStart}},
		{"cleanup fails after a passed check", []string{fastRm, failThirdRm}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := containerFixture(t)
			replaceIn(t, filepath.Join(root, "fake-podman"), tc.podman...)
			runGatedAfterFailure(t, root)
		})
	}
}

// runGatedAfterFailure runs RUNS=3 with 8 slots, the gate in place, and checks
// that only the two first runs and the failed one ran (QA-16, #172).
func runGatedAfterFailure(t *testing.T, root string) {
	t.Helper()
	// The first reused start goes ahead; every later one waits until the failed
	// run's audit line is written, which comes after it publishes the failure.
	gate := `#!/bin/sh
set -eu
cd "$(dirname "$0")"
[ "$1" = reuse ] || exit 0
echo x >> dispatches
[ "$(wc -l < dispatches)" -gt 1 ] || exit 0
i=0
until grep -q result=FAIL .audit/runs.log 2>/dev/null; do
    i=$((i + 1)); [ "$i" -lt 400 ] || exit 1; sleep 0.025
done
`
	if err := os.WriteFile(filepath.Join(root, "fake-dispatch"), []byte(gate), 0o755); err != nil {
		t.Fatal(err)
	}
	runSystemdStub(t, root, 8)
	runner := filepath.Join(root, "test/container/runner.sh")
	slotWait := "        while ((${#running[@]} >= slots)); do reap; done\n"
	data, err := os.ReadFile(runner)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), slotWait) != 1 {
		t.Fatal("runner.sh no longer waits for a slot on its own line; update this test")
	}
	replaceIn(t, runner, slotWait, slotWait+"        \"$repo/fake-dispatch\" \"$how\"\n")
	cmd := exec.Command("make", "test-systemd", "RUNS=3")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("a failed run was reported as success:\n%s", out)
	}
	runs := auditRuns(t, root)
	fails := runs["systemd-ubuntu FAIL"]
	if len(runs["systemd-ubuntu PASS"]) != 1 || len(runs["systemd-fedora PASS"]) != 1 || len(fails) != 1 || len(runs) != 3 {
		t.Fatalf("want the two first runs and the failed one, nothing after it: %v\n%s", runs, out)
	}
	if want := "First failed run: " + fails[0]; !strings.Contains(string(out), want) {
		t.Errorf("output lacks %q:\n%s", want, out)
	}
	for _, want := range []string{"systemd-ubuntu: 1 passed, 1 failed, 1 not started", "systemd-fedora: 1 passed, 0 failed, 2 not started"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(root, ".tmp", "stop-*")); len(left) != 0 {
		t.Errorf("the stop marker was left behind: %v", left)
	}
}

// fileText reads a command's output file when printed.
type fileText struct{ path string }

func (f fileText) String() string {
	data, _ := os.ReadFile(f.path)
	return string(data)
}

// TestQA16_SignalDuringLaunchStopsTheNewRun sends TERM to the runner after a run
// is launched (`&`) but before it is registered, on the RUNS dispatch path and on
// run_distros. The runner pauses at that point on a gate and the run's start is
// held until the test releases it, so without the guard the call ends while the
// run still waits there, and the run's audit line cannot exist yet: no timing luck
// decides the result (QA-16, ADR 0034, #172, review round 4).
func TestQA16_SignalDuringLaunchStopsTheNewRun(t *testing.T) {
	for _, tc := range []struct {
		name       string
		launchLine string // the runner's launch line; the gate goes right after it
		pauseAt    int    // which pass over that line pauses
		blockStart int    // which fake `start` waits for the test
		args       []string
		wantLines  int // audit lines when the call ends: the runs before, and the stopped one
		wantCreate int // most containers created: no run starts after the signal
	}{
		{"RUNS dispatch", "        run_one \"$folder\" \"$next\" systemd \"$how\" 6<&- &\n", 3, 3, []string{"RUNS=2"}, 3, 3},
		{"run_distros", "\n        run_one \"$run\" \"$mode-ubuntu\" \"$mode\" &\n", 1, 1, nil, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := containerFixture(t)
			replaceIn(t, filepath.Join(root, "fake-podman"),
				`    start) if [ -e start-delay ]; then sleep "$(cat start-delay)"; fi ;;`,
				`    start) n=$(flock starts.lock sh -c 'echo x >> starts; wc -l < starts'); if [ "$n" -eq `+strconv.Itoa(tc.blockStart)+` ]; then i=0; until [ -e release-start ]; do i=$((i + 1)); [ "$i" -lt 800 ] || exit 1; sleep 0.025; done; fi ;;`)
			gate := `#!/bin/sh
set -eu
cd "$(dirname "$0")"
echo x >> launches
[ "$(wc -l < launches)" -eq "$1" ] || exit 0
echo "$PPID" > runner-pid
i=0
until [ -e release-launch ]; do
    i=$((i + 1)); [ "$i" -lt 800 ] || exit 1; sleep 0.025
done
`
			if err := os.WriteFile(filepath.Join(root, "fake-launch-gate"), []byte(gate), 0o755); err != nil {
				t.Fatal(err)
			}
			runSystemdStub(t, root, 8)
			runner := filepath.Join(root, "test/container/runner.sh")
			data, err := os.ReadFile(runner)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(data), tc.launchLine) != 1 {
				t.Fatalf("runner.sh no longer has %q once; update this test", tc.launchLine)
			}
			hook := "        \"$repo/fake-launch-gate\" " + strconv.Itoa(tc.pauseAt) + "\n"
			replaceIn(t, runner, tc.launchLine, tc.launchLine+hook)
			release := func(name string) {
				if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			// Let a run left behind finish before the folder goes, whatever happened.
			t.Cleanup(func() {
				release("release-launch")
				release("release-start")
				for i := 0; i < 200 && countLines(t, filepath.Join(root, ".audit/runs.log")) < tc.wantLines; i++ {
					time.Sleep(25 * time.Millisecond)
				}
			})

			// A file, not a buffer: Wait then returns when make ends, not when the
			// last process holding the output (a run left behind) does.
			outFile, err := os.Create(filepath.Join(t.TempDir(), "out"))
			if err != nil {
				t.Fatal(err)
			}
			defer outFile.Close()
			out := fileText{outFile.Name()}
			cmd := exec.Command("make", append([]string{"test-systemd"}, tc.args...)...)
			cmd.Dir = root
			cmd.Stdout, cmd.Stderr = outFile, outFile
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			var pid int
			for i := 0; ; i++ {
				if data, err := os.ReadFile(filepath.Join(root, "runner-pid")); err == nil && len(data) > 0 {
					if pid, err = strconv.Atoi(strings.TrimSpace(string(data))); err != nil {
						t.Fatal(err)
					}
					break
				}
				select {
				case err := <-done:
					t.Fatalf("the call ended before the launch gate (%v)\n%s", err, out.String())
				default:
				}
				if i > 800 {
					t.Fatal("the runner did not reach the launch gate")
				}
				time.Sleep(25 * time.Millisecond)
			}
			// The runner waits on the gate, so the signal is handled right after it.
			if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			release("release-launch")
			// A guarded runner cannot end before the stopped run has: hold that
			// run's start a moment, then let it go.
			lines := -1
			select {
			case err = <-done:
				lines = countLines(t, filepath.Join(root, ".audit/runs.log"))
			case <-time.After(1500 * time.Millisecond):
				release("release-start")
				select {
				case err = <-done:
					lines = countLines(t, filepath.Join(root, ".audit/runs.log"))
				case <-time.After(30 * time.Second):
					t.Fatal("the call did not end after the run was released")
				}
			}
			if lines != tc.wantLines {
				t.Fatalf("the call ended with %d audit lines, want %d: the launched run was not stopped and drained\n%s", lines, tc.wantLines, out.String())
			}
			if err == nil || !strings.Contains(out.String(), "Error 130") {
				t.Errorf("want the cancellation exit 130: %v\n%s", err, out.String())
			}
			calls, _ := os.ReadFile(filepath.Join(root, "calls"))
			// The stopped run may end before it creates its container.
			if n := strings.Count(string(calls), "\ncreate "); n > tc.wantCreate {
				t.Errorf("want at most %d containers created, got %d: a run started after the signal\n%s", tc.wantCreate, n, calls)
			}
			if live, _ := filepath.Glob(filepath.Join(root, "live", "*")); len(live) != 0 {
				t.Errorf("containers left after the call: %v", live)
			}
			if audit, _ := os.ReadFile(filepath.Join(root, ".audit/runs.log")); !strings.Contains(string(audit), "result=FAIL") {
				t.Errorf("the stopped run was not recorded as FAIL:\n%s\n%s", audit, out.String())
			}
		})
	}
}

// TestQA16_RunsStopAfterTheFirstFailure fails the third container: nothing new
// starts, the running ones finish, the call exits 1 and names the failed run, whose
// folder and log are kept (QA-16, ADR 0034, #172).
func TestQA16_RunsStopAfterTheFirstFailure(t *testing.T) {
	root := containerFixture(t)
	path := filepath.Join(root, "fake-podman")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The third start fails at once while the others pause, so the failure is
	// seen while the run beside it is still going.
	failThird := `start) n=$(flock starts.lock sh -c 'echo x >> starts; wc -l < starts'); [ "$n" -ne 3 ]; sleep 0.5 ;;`
	patched := strings.ReplaceAll(string(data), `    start) if [ -e start-delay ]; then sleep "$(cat start-delay)"; fi ;;`, "    "+failThird)
	if patched == string(data) {
		t.Fatal("fake Podman changed; update this test")
	}
	if err := os.WriteFile(path, []byte(patched), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runSystemdMake(t, root, 2, "RUNS=10")
	if err == nil {
		t.Fatalf("a failed run was reported as success:\n%s", out)
	}
	runs := auditRuns(t, root)
	var fails []string
	total := 0
	for key, ids := range runs {
		total += len(ids)
		if strings.HasSuffix(key, " FAIL") {
			fails = append(fails, ids...)
		}
	}
	if len(fails) != 1 {
		t.Fatalf("want exactly one failed run: %v", runs)
	}
	// Two slots: the failed run, the one beside it and at most one started while
	// it ran, after the two first runs.
	if total > 5 {
		t.Errorf("runs kept starting after the failure: %d runs: %v", total, runs)
	}
	if want := "First failed run: " + fails[0]; !strings.Contains(string(out), want) {
		t.Errorf("output lacks %q:\n%s", want, out)
	}
	notStarted := 0
	for _, m := range regexp.MustCompile(`(?m)^systemd-(?:ubuntu|fedora): \d+ passed, \d+ failed, (\d+) not started$`).FindAllStringSubmatch(string(out), -1) {
		n, _ := strconv.Atoi(m[1])
		notStarted += n
	}
	if notStarted != 20-total {
		t.Errorf("summary says %d not started, want %d:\n%s", notStarted, 20-total, out)
	}
	if !exists(filepath.Join(root, ".tmp", fails[0])) || !exists(filepath.Join(root, ".audit/logs", fails[0]+".log")) {
		t.Error("the failed run did not keep its folder and log")
	}
}

// TestQA16_BadRunsRefused refuses every RUNS that is not a plain 1-1000, through
// Make and through run.sh, before the binary build and before Podman (#172). A Make
// expression stays literal text: $(shell …) never runs, and "$(shell …)2" does not
// become a valid 2 (review round 1). <marker> is the fixture's "injected" file.
func TestQA16_BadRunsRefused(t *testing.T) {
	for _, value := range []string{"0", "1001", "-1", "+3", "01", "3 ", " 3", "1;x", "", "3\n", "1e3", "$(touch injected)", "a[$(touch injected)]", "$(shell touch <marker>)", "$(shell touch <marker>)2"} {
		// The subtest name becomes part of the fixture's path, so it must not carry
		// a Make or shell expression itself.
		name := strings.NewReplacer("$", "dollar", "(", "_", ")", "_").Replace(value)
		t.Run(name, func(t *testing.T) {
			root := containerFixture(t)
			runs := strings.ReplaceAll(value, "<marker>", filepath.Join(root, "injected"))
			if strings.Contains(value, "$(shell") {
				// The environment is the other way into Make's variables.
				runSystemdStub(t, root, 8)
				cmd := exec.Command("make", "test-systemd")
				cmd.Dir = root
				cmd.Env = append(os.Environ(), "RUNS="+runs)
				if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "RUNS must be") {
					t.Fatalf("make accepted RUNS=%q from the environment: %v\n%s", runs, err, out)
				}
			}
			// Make drops leading spaces of a command-line value itself, so " 3"
			// arrives as a valid 3; only run.sh sees it as written.
			if !strings.HasPrefix(runs, " ") {
				out, err := runSystemdMake(t, root, 8, "RUNS="+runs)
				if err == nil || !strings.Contains(string(out), "RUNS must be a whole number from 1 to 1000") {
					t.Fatalf("make accepted RUNS=%q: %v\n%s", runs, err, out)
				}
			} else {
				runSystemdStub(t, root, 8)
			}
			// run.sh is the public entry point: the environment keeps the value as
			// the caller wrote it.
			cmd := exec.Command("/bin/sh", "test/container/run.sh", "@systemd")
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "RUNS="+runs)
			if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "RUNS must be") {
				t.Fatalf("run.sh accepted RUNS=%q: %v\n%s", runs, err, out)
			}
			for _, name := range []string{"calls", "binary-builds", "injected", ".audit/runs.log"} {
				if exists(filepath.Join(root, name)) {
					t.Errorf("a refused RUNS left %s", name)
				}
			}
		})
	}
}

// runInstallMake runs the real `make test-install` target against a fixture.
// The fixture's memory reading allows one container at a time, so the install
// runs also cover the one-after-the-other path (QA-17, ADR 0034).
func runInstallMake(t *testing.T, root string, extra ...string) ([]byte, error) {
	t.Helper()
	runner := filepath.Join(root, "test/container/runner.sh")
	data, err := os.ReadFile(runner)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runner, []byte(strings.ReplaceAll(string(data), "available=16777216", "available=4980736")), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("make", append([]string{"test-install"}, extra...)...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "SSH_AUTH_SOCK=/private/test-socket", "CONTAINERS_CONF=/private/untrusted.conf")
	return cmd.CombinedOutput()
}

// TestQA17_InstallNeedsBothPackages refuses to start any container when dist/
// lacks the .deb or the .rpm, because GoReleaser is never run by the runner
// (QA-17, ADR 0034).
func TestQA17_InstallNeedsBothPackages(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files []string
		dirs  []string // folders named like packages
		links []string // dangling links named like packages
	}{
		{"no dist", nil, nil, nil},
		{"only a deb", []string{"dist/apptrol_1_amd64.deb"}, nil, nil},
		{"only an rpm", []string{"dist/apptrol-1.x86_64.rpm"}, nil, nil},
		{"folders named like packages", nil, []string{"dist/missing.deb", "dist/missing.rpm"}, nil},
		{"dangling links named like packages", nil, nil, []string{"dist/missing.deb", "dist/missing.rpm"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := containerFixture(t)
			for _, name := range tc.dirs {
				if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range tc.links {
				if err := os.MkdirAll(filepath.Join(root, "dist"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("nowhere", filepath.Join(root, name)); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range tc.files {
				if err := os.MkdirAll(filepath.Join(root, "dist"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			out, err := runInstallMake(t, root)
			if err == nil || !strings.Contains(string(out), "make snapshot") {
				t.Fatalf("accepted missing packages: %v\n%s", err, out)
			}
			if _, err := os.Stat(filepath.Join(root, "calls")); !os.IsNotExist(err) {
				t.Error("missing packages reached Podman")
			}
		})
	}
}

// TestQA17_InstallUsesTheSameSandboxAsStatus checks that the install runs keep
// every flag and mount of the Status runs, check the installed files through
// status.sh in package mode, and leave their own audit names (QA-17, QA-16).
func TestQA17_InstallUsesTheSameSandboxAsStatus(t *testing.T) {
	root := containerFixture(t)
	if err := os.MkdirAll(filepath.Join(root, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"dist/apptrol_1_amd64.deb", "dist/apptrol-1.x86_64.rpm"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// RUNS repeats only test-systemd; the install check ignores it (#172).
	out, err := runInstallMake(t, root, "FLAGS=--privileged", "AGENT=claude-sonnet-5-5", "RUNS=1;x")
	if err != nil {
		t.Fatalf("make: %v\n%s", err, out)
	}
	calls, err := os.ReadFile(filepath.Join(root, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(calls), "--privileged") || strings.Count(string(calls), "--mount ") != 4 {
		t.Errorf("unexpected flags or mount count: %s", calls)
	}
	for _, want := range []string{"--memory=512m", "--memory-swap=512m", "--pids-limit=256", "--network=none", "--systemd=always", "--entrypoint /sbin/init", "src=" + root + ",dst=/src,ro=true"} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("missing fixed argument %s in %s", want, calls)
		}
	}
	if strings.Count(string(calls), "/test/container/status.sh package "+fakeFullCommit) != 2 {
		t.Errorf("each distro must run the package check against this commit: %s", calls)
	}
	if strings.Contains(string(calls), "status.sh\n") {
		t.Errorf("a container ran the binary check instead of the package check: %s", calls)
	}
	audit, err := os.ReadFile(filepath.Join(root, ".audit", "runs.log"))
	if err != nil || strings.Count(string(audit), "result=PASS") != 2 || strings.Contains(string(audit), "recipe=systemd-") {
		t.Fatalf("audit: %s (%v)", audit, err)
	}
	for _, want := range []string{"recipe=install-ubuntu ", "recipe=install-fedora "} {
		if !strings.Contains(string(audit), want) {
			t.Errorf("audit lacks %s", want)
		}
	}
}

// TestQA17_PickPackage checks which package a container accepts: its own type and
// architecture, exactly one, from the checked-out commit whatever the length of the
// abbreviation in the file name, never a link (QA-17, ADR 0034).
func TestQA17_PickPackage(t *testing.T) {
	const commit = "f5da4e0aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, tc := range []struct {
		name  string
		files []string
		link  string
		want  string // file name printed, or "" when the pick must fail
	}{
		{"seven digits", []string{"apptrol_0.2.3-snapshot.f5da4e0_amd64.deb"}, "", "apptrol_0.2.3-snapshot.f5da4e0_amd64.deb"},
		{"twelve digits, as with core.abbrev=12", []string{"apptrol_0.2.3-snapshot.f5da4e0aaaaa_amd64.deb"}, "", "apptrol_0.2.3-snapshot.f5da4e0aaaaa_amd64.deb"},
		{"rpm", []string{"apptrol-0.2.3-snapshot.f5da4e0-1.x86_64.rpm"}, "", "apptrol-0.2.3-snapshot.f5da4e0-1.x86_64.rpm"},
		{"the other architecture is ignored", []string{"apptrol_0.2.3-snapshot.f5da4e0_arm64.deb", "apptrol_0.2.3-snapshot.f5da4e0_amd64.deb"}, "", "apptrol_0.2.3-snapshot.f5da4e0_amd64.deb"},
		{"an earlier commit", []string{"apptrol_0.2.3-snapshot.0123456_amd64.deb"}, "", ""},
		{"a hash that only looks like the commit", []string{"apptrol_0.2.3-snapshot.f5da4e1_amd64.deb"}, "", ""},
		{"a hash shorter than seven digits", []string{"apptrol_0.2.3-snapshot.f5da4e_amd64.deb"}, "", ""},
		{"two for one architecture", []string{"apptrol_0.2.3-snapshot.f5da4e0_amd64.deb", "apptrol_0.2.4-snapshot.f5da4e0aaaaa_amd64.deb"}, "", ""},
		{"none", nil, "", ""},
		{"a link", nil, "apptrol_0.2.3-snapshot.f5da4e0_amd64.deb", ""},
		{"a release without a snapshot hash", []string{"apptrol_0.2.3_amd64.deb"}, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := containerFixture(t)
			if err := os.MkdirAll(filepath.Join(root, "dist"), 0o755); err != nil {
				t.Fatal(err)
			}
			for _, name := range tc.files {
				if err := os.WriteFile(filepath.Join(root, "dist", name), nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tc.link != "" {
				// The link points at a real file, so only the link check can refuse it.
				if err := os.WriteFile(filepath.Join(root, "dist", "real-file"), nil, 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("real-file", filepath.Join(root, "dist", tc.link)); err != nil {
					t.Fatal(err)
				}
			}
			ext, arch := "deb", "amd64"
			if strings.HasSuffix(tc.name, "rpm") {
				ext, arch = "rpm", "x86_64"
			}
			cmd := exec.Command("/bin/bash", filepath.Join(root, "test/container/pick-package.sh"), ext, arch, commit)
			out, err := cmd.Output()
			got := strings.TrimSpace(string(out))
			if tc.want == "" {
				if err == nil {
					t.Fatalf("accepted %q", got)
				}
				return
			}
			if err != nil || got != filepath.Join(root, "dist", tc.want) {
				t.Fatalf("picked %q (%v), want %s", got, err, tc.want)
			}
		})
	}
}

// TestQA17_StatusScriptTrapsFailuresInFunctions keeps -E on status.sh: without it
// the ERR trap does not run inside install_package, and a failed apt or dnf install
// leaves no diagnostics in the log (QA-17, ADR 0034).
func TestQA17_StatusScriptTrapsFailuresInFunctions(t *testing.T) {
	data, err := os.ReadFile("../../test/container/status.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\nset -Eeuo pipefail\n") || !strings.Contains(string(data), "\ntrap diagnose ERR\n") {
		t.Fatal("status.sh must set -E and trap diagnose on ERR")
	}
}

// TestQA17_InstallPassesTheFullCommit reproduces a checkout whose own Git setting
// shortens commits differently from GoReleaser: the container must be given the
// full hash, never an abbreviation (QA-17, ADR 0034).
func TestQA17_InstallPassesTheFullCommit(t *testing.T) {
	root := containerFixtureAt(t, t.TempDir(), false)
	for _, name := range []string{"dist/apptrol_1_amd64.deb", "dist/apptrol-1.x86_64.rpm"} {
		if err := os.MkdirAll(filepath.Join(root, "dist"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, root, "init", "--quiet", "--initial-branch=fixture")
	gitIn(t, root, "config", "core.abbrev", "12")
	gitIn(t, root, "add", "--all")
	gitIn(t, root, "commit", "--quiet", "--message=fixture")
	rev := exec.Command("git", "rev-parse", "HEAD")
	rev.Dir = root
	head, err := rev.Output()
	if err != nil {
		t.Fatal(err)
	}
	out, err := runInstallMake(t, root)
	if err != nil {
		t.Fatalf("make: %v\n%s", err, out)
	}
	calls, err := os.ReadFile(filepath.Join(root, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(calls), "/test/container/status.sh package "+strings.TrimSpace(string(head))+"\n") != 2 {
		t.Errorf("the containers did not get the full commit %s: %s", head, calls)
	}
}

// runCleanMake runs the real `make test-clean` target against a fixture (QA-18).
func runCleanMake(t *testing.T, root string, extra ...string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command("make", append([]string{"test-clean"}, extra...)...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "SSH_AUTH_SOCK=/private/test-socket", "CONTAINERS_CONF=/private/untrusted.conf")
	return cmd.CombinedOutput()
}

// touch makes a file, and its folders, below root.
func touch(t *testing.T, root string, parts ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{root}, parts...)...)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// TestQA18_CleanRemovesOnlyKeptRunsAndLogs checks which names go and which stay,
// that Podman is asked only for `info` and `unshare rm` on exact paths directly
// inside this checkout's .tmp/, and that runs.log keeps its lines and gains one
// (QA-18, ADR 0034).
func TestQA18_CleanRemovesOnlyKeptRunsAndLogs(t *testing.T) {
	root := containerFixture(t)
	gone := []string{
		".tmp/run-AbCd1234/out/deep/file",
		".tmp/run-Zz99Zz99/file",
		".audit/logs/run-AbCd1234.log",
		".audit/logs/run-Zz99Zz99.log",
	}
	stay := []string{
		".tmp/keep-me/file",
		".tmp/run-short/file",
		".tmp/run-AbCd12345/file", // nine characters
		".tmp/run-AbCd.234/file",
		".tmp/.run-AbCd1234/file",
		".tmp/run-FileFile", // a plain file, not a folder
		".audit/logs/notes.log",
		".audit/logs/run-AbCd1234.log.txt",
		".audit/logs/run-short.log",
		".audit/recipes/sample/commands.sh",
	}
	for _, name := range append(append([]string{}, gone...), stay...) {
		touch(t, root, name)
	}
	if err := os.WriteFile(filepath.Join(root, ".audit/runs.log"), []byte("an earlier line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runCleanMake(t, root, "FLAGS=--privileged", "JOBS=999", "RECIPE=sample", "AGENT=claude-sonnet-5-5")
	if err != nil {
		t.Fatalf("make: %v\n%s", err, out)
	}
	for _, name := range gone {
		if exists(filepath.Join(root, name)) {
			t.Errorf("%s was not removed", name)
		}
	}
	for _, name := range stay {
		if !exists(filepath.Join(root, name)) {
			t.Errorf("%s must stay", name)
		}
	}
	calls, err := os.ReadFile(filepath.Join(root, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	unshares := 0
	for _, line := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
		if strings.HasPrefix(line, "info ") {
			continue
		}
		path, ok := strings.CutPrefix(line, "unshare rm -rf --one-file-system -- ")
		name, inside := strings.CutPrefix(path, filepath.Join(root, ".tmp")+"/")
		if !ok || !inside || strings.Contains(name, "/") || len(name) != len("run-AbCd1234") || !strings.HasPrefix(name, "run-") {
			t.Errorf("Podman was asked for more than one folder in .tmp: %q", line)
		}
		unshares++
	}
	if unshares != 2 {
		t.Errorf("want 2 folders removed through unshare, got %d: %s", unshares, calls)
	}
	for _, forbidden := range []string{"create", "start", "--mount", "--privileged", "999"} {
		if strings.Contains(string(calls), forbidden) {
			t.Errorf("cleanup reached Podman with %q: %s", forbidden, calls)
		}
	}
	audit, err := os.ReadFile(filepath.Join(root, ".audit/runs.log"))
	lines := strings.Split(strings.TrimSpace(string(audit)), "\n")
	if err != nil || len(lines) != 2 || lines[0] != "an earlier line" {
		t.Fatalf("runs.log must keep its lines and gain one: %q (%v)", audit, err)
	}
	for _, want := range []string{" run=clean-", " agent=claude-sonnet-5-5 ", " recipe=@clean ", " image=none ", " result=PASS "} {
		if !strings.Contains(lines[1], want) {
			t.Errorf("audit line lacks %q: %s", want, lines[1])
		}
	}
	if strings.Contains(lines[1], root) || strings.Contains(lines[1], "AbCd1234") {
		t.Errorf("audit line leaked a path or a run name: %s", lines[1])
	}
	for _, want := range []string{"removed: .tmp/run-AbCd1234", "left alone: .tmp/keep-me", "removed 2 run folders and 2 logs"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// TestQA18_CleanWithNothingKeptStillRecords covers a fresh checkout (QA-18).
func TestQA18_CleanWithNothingKeptStillRecords(t *testing.T) {
	root := containerFixture(t)
	if out, err := runCleanMake(t, root); err != nil {
		t.Fatalf("make: %v\n%s", err, out)
	}
	audit, err := os.ReadFile(filepath.Join(root, ".audit/runs.log"))
	if err != nil || strings.Count(string(audit), "\n") != 1 || !strings.Contains(string(audit), "agent=unspecified ") || !strings.Contains(string(audit), "result=PASS") {
		t.Fatalf("audit: %s (%v)", audit, err)
	}
}

// TestQA18_CleanNeverFollowsLinks puts links named like runs next to real files
// outside the checkout: the links go, what they point at stays (QA-18, ADR 0034).
func TestQA18_CleanNeverFollowsLinks(t *testing.T) {
	root := containerFixture(t)
	outside := t.TempDir()
	precious := touch(t, outside, "folder", "precious")
	preciousLog := touch(t, outside, "precious.log")
	if err := os.MkdirAll(filepath.Join(root, ".tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".audit/logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	folderLink := filepath.Join(root, ".tmp/run-LinkLink")
	logLink := filepath.Join(root, ".audit/logs/run-LinkLink.log")
	if err := os.Symlink(filepath.Dir(precious), folderLink); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(preciousLog, logLink); err != nil {
		t.Fatal(err)
	}
	out, err := runCleanMake(t, root)
	if err != nil {
		t.Fatalf("make: %v\n%s", err, out)
	}
	if exists(folderLink) || exists(logLink) {
		t.Error("a link named like a run was not removed")
	}
	if !exists(precious) || !exists(preciousLog) {
		t.Fatal("cleanup followed a link and deleted its target")
	}
	calls, _ := os.ReadFile(filepath.Join(root, "calls"))
	if strings.Contains(string(calls), outside) {
		t.Errorf("Podman was given a path outside the checkout: %s", calls)
	}
}

// TestQA18_CleanRefusesLinkedFolders stops before any removal when .tmp or the
// log folder is itself a link (QA-18, ADR 0034).
func TestQA18_CleanRefusesLinkedFolders(t *testing.T) {
	for _, linked := range []string{".tmp", ".audit/logs"} {
		t.Run(linked, func(t *testing.T) {
			root := containerFixture(t)
			outside := t.TempDir()
			victims := []string{
				touch(t, outside, "run-AbCd1234", "file"),
				touch(t, outside, "run-AbCd1234.log"),
			}
			if err := os.MkdirAll(filepath.Join(root, ".audit"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(root, linked)); err != nil {
				t.Fatal(err)
			}
			out, err := runCleanMake(t, root)
			if err == nil || !strings.Contains(string(out), "Refusing symlink") {
				t.Fatalf("accepted a linked folder: %v\n%s", err, out)
			}
			for _, victim := range victims {
				if !exists(victim) {
					t.Errorf("%s was removed through a link", victim)
				}
			}
			if _, err := os.Stat(filepath.Join(root, "calls")); !os.IsNotExist(err) {
				t.Error("a linked folder reached Podman")
			}
			// The preflight refuses before the script writes anything, as for runs.
			if _, err := os.Stat(filepath.Join(root, ".audit/runs.log")); !os.IsNotExist(err) {
				t.Error("a refused preflight wrote an audit line")
			}
		})
	}
}

// TestQA18_CleanOneFailureDoesNotStopTheRest keeps going, exits 1 and records
// FAIL, so a stuck folder is reported without leaving the others behind
// (QA-18, ADR 0034).
func TestQA18_CleanOneFailureDoesNotStopTheRest(t *testing.T) {
	root := containerFixture(t)
	path := filepath.Join(root, "fake-podman")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	patched := strings.ReplaceAll(string(data), `unshare) shift; "$@" ;;`, `unshare) shift; case "$*" in *run-BadBadBa) exit 1 ;; esac; "$@" ;;`)
	if patched == string(data) {
		t.Fatal("fake Podman changed; update this test")
	}
	if err := os.WriteFile(path, []byte(patched), 0o755); err != nil {
		t.Fatal(err)
	}
	stuck := touch(t, root, ".tmp/run-BadBadBa/file")
	other := touch(t, root, ".tmp/run-GoodGood/file")
	out, err := runCleanMake(t, root, "AGENT=claude-sonnet-5-5")
	if err == nil || !strings.Contains(string(out), "FAILED: .tmp/run-BadBadBa") {
		t.Fatalf("a failed removal was not reported: %v\n%s", err, out)
	}
	if !exists(stuck) || exists(other) {
		t.Error("the failure stopped the other removals, or removed the stuck folder")
	}
	audit, _ := os.ReadFile(filepath.Join(root, ".audit/runs.log"))
	if strings.Count(string(audit), "\n") != 1 || !strings.Contains(string(audit), "result=FAIL") {
		t.Fatalf("audit: %s", audit)
	}
}

// TestQA18_CleanAuditLabelIsData refuses a bad label before removing anything,
// and still records the attempt (QA-18, QA-16).
func TestQA18_CleanAuditLabelIsData(t *testing.T) {
	root := containerFixture(t)
	kept := touch(t, root, ".tmp/run-AbCd1234/file")
	if out, err := runCleanMake(t, root, "AGENT=model\nresult=PASS"); err == nil {
		t.Fatalf("accepted an invalid label: %s", out)
	}
	if !exists(kept) {
		t.Error("a refused cleanup removed a folder")
	}
	audit, err := os.ReadFile(filepath.Join(root, ".audit/runs.log"))
	if err != nil || strings.Count(string(audit), "\n") != 1 || !strings.Contains(string(audit), "agent=unspecified ") || !strings.Contains(string(audit), "result=FAIL") {
		t.Fatalf("audit: %s (%v)", audit, err)
	}
	if _, err := os.Stat(filepath.Join(root, "calls")); !os.IsNotExist(err) {
		t.Error("a refused cleanup reached Podman")
	}
}

// cleanBehindTheLock holds the shared lock the way a running container check does,
// starts `make test-clean` and shows that it is waiting. The returned function lets
// go of the lock and returns what the clean-up printed and how it ended. The test
// can change the checkout while the clean-up waits (QA-18, ADR 0034).
func cleanBehindTheLock(t *testing.T, root string) func() ([]byte, error) {
	t.Helper()
	if _, err := exec.LookPath("flock"); err != nil {
		t.Fatal("flock is required for QA-18")
	}
	lock := filepath.Join(root, "git-common/apptrol-containers.lock")
	holder := exec.Command("flock", lock, "sleep", "60")
	// flock runs sleep as a child that keeps the lock, so the whole group is killed.
	holder.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	release := func() { _ = syscall.Kill(-holder.Process.Pid, syscall.SIGKILL); _ = holder.Wait() }
	t.Cleanup(release)
	// Wait until the holder really has the lock, so the cleanup cannot get in first.
	for i := 0; ; i++ {
		if exec.Command("flock", "--nonblock", lock, "true").Run() != nil {
			break
		}
		if i > 200 {
			t.Fatal("the lock holder did not start")
		}
		time.Sleep(25 * time.Millisecond)
	}
	var out bytes.Buffer
	cmd := exec.Command("make", "test-clean", "AGENT=claude-sonnet-5-5")
	cmd.Dir = root
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	// The runner writes this marker just before it asks for the lock, so everything
	// the test changes from here on happens after the startup (preflight) checks.
	for i := 0; ; i++ {
		if exists(filepath.Join(root, "reached-lock")) {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("the cleanup ended before it reached the lock (%v)\n%s", err, out.String())
		default:
		}
		if i > 400 {
			t.Fatal("the cleanup did not reach the lock")
		}
		time.Sleep(25 * time.Millisecond)
	}
	select {
	case err := <-done:
		t.Fatalf("the cleanup did not wait for the lock (%v)\n%s", err, out.String())
	case <-time.After(1500 * time.Millisecond):
	}
	return func() ([]byte, error) {
		release()
		select {
		case err := <-done:
			return out.Bytes(), err
		case <-time.After(30 * time.Second):
			t.Fatal("the cleanup did not continue after the lock was released")
			return nil, nil
		}
	}
}

// TestQA18_CleanWaitsForARunInProgress: the cleanup must not delete a live run
// folder before the run lets go (QA-18, ADR 0034).
func TestQA18_CleanWaitsForARunInProgress(t *testing.T) {
	root := containerFixture(t)
	live := touch(t, root, ".tmp/run-LiveLive/file")
	finish := cleanBehindTheLock(t, root)
	if !exists(live) {
		t.Fatal("a run in progress lost its folder")
	}
	if out, err := finish(); err != nil {
		t.Fatalf("make: %v\n%s", err, out)
	}
	if exists(live) {
		t.Error("the folder stayed after the run let go")
	}
}

// TestQA18_CleanLooksAgainAfterTheLock swaps a link in for a folder (or the ledger)
// while the cleanup waits for the lock, after the preflight has already passed:
// nothing outside the checkout may be removed or written (QA-18, ADR 0034).
func TestQA18_CleanLooksAgainAfterTheLock(t *testing.T) {
	for _, swapped := range []string{".tmp", ".audit/logs", ".audit/runs.log"} {
		t.Run(swapped, func(t *testing.T) {
			root := containerFixture(t)
			outside := t.TempDir()
			victims := []string{
				touch(t, outside, "run-AbCd1234", "file"),
				touch(t, outside, "run-AbCd1234.log"),
			}
			ledger := touch(t, outside, "ledger")
			touch(t, root, ".tmp/run-AbCd1234/file")
			touch(t, root, ".audit/logs/run-AbCd1234.log")
			if err := os.WriteFile(filepath.Join(root, ".audit/runs.log"), []byte("an earlier line\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			finish := cleanBehindTheLock(t, root)
			target := outside
			if swapped == ".audit/runs.log" {
				target = ledger
			}
			if err := os.RemoveAll(filepath.Join(root, swapped)); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(root, swapped)); err != nil {
				t.Fatal(err)
			}
			out, err := finish()
			if err == nil {
				t.Fatalf("accepted a folder that became a link while waiting:\n%s", out)
			}
			for _, victim := range append(victims, ledger) {
				if !exists(victim) {
					t.Errorf("%s was removed through a link", victim)
				}
			}
			if data, _ := os.ReadFile(ledger); string(data) != "x" {
				t.Errorf("the audit line was written through a link: %q", data)
			}
			calls, _ := os.ReadFile(filepath.Join(root, "calls"))
			if strings.Contains(string(calls), "unshare rm") && swapped != ".audit/runs.log" {
				t.Errorf("Podman was asked to remove after the swap: %s", calls)
			}
		})
	}
}

// TestQA18_CleanLeavesFoldersWithMounts: `rm --one-file-system` does not stop at a
// bind mount of the same file system, so a run folder with a mount at or below it
// is not removed. A folder whose name only starts the same is (QA-18, ADR 0034).
func TestQA18_CleanLeavesFoldersWithMounts(t *testing.T) {
	root := containerFixture(t)
	exact := touch(t, root, ".tmp/run-Mounted1/file")
	below := touch(t, root, ".tmp/run-Below123/inner/file")
	other := touch(t, root, ".tmp/run-AbCd1234/file")
	touch(t, root, ".tmp/run-AbCd12345/file")
	mountinfo := strings.Join([]string{
		"36 35 0:30 / " + filepath.Join(root, ".tmp/run-Mounted1") + " rw - tmpfs tmpfs rw",
		"37 35 0:31 / " + filepath.Join(root, ".tmp/run-Below123/inner") + " rw - tmpfs tmpfs rw",
		"38 35 0:32 / " + filepath.Join(root, ".tmp/run-AbCd12345") + " rw - tmpfs tmpfs rw",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(root, "fake-self-mountinfo"), []byte(mountinfo), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runCleanMake(t, root)
	if err == nil || !strings.Contains(string(out), "FAILED: .tmp/run-Mounted1 has a mount in it") || !strings.Contains(string(out), "FAILED: .tmp/run-Below123 has a mount in it") {
		t.Fatalf("a folder with a mount in it was not refused: %v\n%s", err, out)
	}
	if !exists(exact) || !exists(below) {
		t.Error("a folder with a mount in it was removed")
	}
	if exists(other) {
		t.Error("a folder next to a mounted one was left behind")
	}
	calls, _ := os.ReadFile(filepath.Join(root, "calls"))
	if strings.Contains(string(calls), "run-Mounted1") || strings.Contains(string(calls), "run-Below123") {
		t.Errorf("Podman was asked to remove a mounted folder: %s", calls)
	}
	audit, _ := os.ReadFile(filepath.Join(root, ".audit/runs.log"))
	if !strings.Contains(string(audit), "result=FAIL") {
		t.Errorf("audit: %s", audit)
	}
}
