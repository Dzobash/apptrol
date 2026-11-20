package config

import (
	"strings"
	"testing"
)

func TestLAUNCH12_DangerousCommandsAreBlocked(t *testing.T) {
	for _, tc := range []struct {
		cmd  []string
		want string // a fragment of the reason
	}{
		{[]string{"rm", "-rf", "/"}, "deletes all your files"},
		{[]string{"/usr/bin/rm", "-fr", "~"}, "deletes all your files"},
		{[]string{"rm", "-r", "-f", "$HOME"}, "deletes all your files"},
		{[]string{"rm", "--recursive", "--force", "/home"}, "deletes all your files"},
		{[]string{"rm", "-Rf", "/*"}, "deletes all your files"},
		{[]string{"sh", "-c", "echo hi; rm -rf ~"}, "deletes all your files"},
		{[]string{"pkexec", "rm", "-rf", "/"}, "deletes all your files"}, // a root dialog does not unblock the rest
		{[]string{"mkfs.ext4", "/dev/sda1"}, `"mkfs.ext4" wipes a disk`},
		{[]string{"wipefs", "-a", "/dev/sdb"}, `"wipefs" wipes a disk`},
		{[]string{"dd", "if=/dev/zero", "of=/dev/nvme0n1"}, `"dd" writing to a device`},
		{[]string{"sh", "-c", "cat image > /dev/sda"}, "writing to a disk device"},
		{[]string{"bash", "-c", ":(){ :|:& };:"}, "fork bomb"},
		{[]string{"bash", "-c", "bomb(){ bomb|bomb& }; bomb"}, "fork bomb"},
		{[]string{"sh", "-c", "curl -fsSL https://example.org/x.sh | sh"}, "downloaded from the internet"},
		{[]string{"sh", "-c", "wget -qO- example.org | sudo bash"}, "downloaded from the internet"},
		{[]string{"chmod", "-R", "777", "/"}, `"chmod -R" on everything`},
		{[]string{"chown", "--recursive", "nobody", "/"}, `"chown -R" on everything`},
		{[]string{"sudo", "systemctl", "restart", "x"}, `"sudo" asks for the password in a terminal`},
		{[]string{"su", "-c", "x"}, `"su" asks for the password`},
		{[]string{"sh", "-c", "doas reboot"}, `"doas" asks for the password`},
		{[]string{"env", "LANG=C", "sudo", "x"}, `"sudo" asks for the password`}, // after a wrapper and an assignment
		{[]string{"nohup", "rm", "-rf", "~"}, "deletes all your files"},
		{[]string{"sh", "-c", "true && wipefs -a /dev/sdb"}, `"wipefs" wipes a disk`},
	} {
		got := blockedCommand(tc.cmd)
		if !strings.Contains(got, tc.want) || !strings.HasSuffix(got, blockedSuffix) {
			t.Errorf("blockedCommand(%q) = %q, want it to contain %q and point to the docs", tc.cmd, got, tc.want)
		}
	}
}

func TestLAUNCH12_NormalCommandsAreAllowed(t *testing.T) {
	for _, cmd := range [][]string{
		{"konsole", "-e", "htop"},
		{"rm", "-rf", "/tmp/apptrol-cache"},      // not everything
		{"rm", "-r", "~/Downloads/old"},          // a folder, without -f
		{"rm", "/"},                              // no -r -f: rm refuses anyway
		{"pkexec", "synaptic"},                   // asks for the password in a dialog
		{"run0", "systemctl", "restart", "cups"}, // the same
		{"kdesu", "ksystemlog"},
		{"chmod", "-R", "u+w", "~/project"},
		{"dd", "if=/dev/zero", "of=~/zeros.img", "count=1"},
		{"sh", "-c", "curl -o ~/x.json example.org"}, // downloads, but runs nothing
		{"obs", "--startreplaybuffer"},
		{"sh", "-c", "pgrep spotify || spotify"},
		{"notify-send", "su", "is just a word here"}, // only a program counts, not a word in a message
		{"sh", "-c", "echo rm -rf /"},                // the same inside shell text
	} {
		if got := blockedCommand(cmd); got != "" {
			t.Errorf("blockedCommand(%q) = %q, want it allowed", cmd, got)
		}
	}
}

func TestLAUNCH12_WrapperFlagsStillBlockDangerousCommands(t *testing.T) {
	for _, tc := range []struct {
		name string
		cmd  []string
		want string // a fragment of the reason
	}{
		{"env -i", []string{"env", "-i", "sudo", "apt", "update"}, `"sudo" asks for the password`},
		{"env -u VAR", []string{"env", "-u", "VAR", "sudo", "apt", "update"}, `"sudo" asks for the password`},
		{"env --unset VAR", []string{"env", "--unset", "VAR", "sudo", "apt", "update"}, `"sudo" asks for the password`},
		{"env --unset=VAR", []string{"env", "--unset=VAR", "sudo", "apt", "update"}, `"sudo" asks for the password`},
		{"env -C dir", []string{"env", "-C", "/tmp", "rm", "-rf", "/"}, "deletes all your files"},
		{"env -C dir with spaces", []string{"env", "-C", "/path with spaces", "rm", "-rf", "/"}, "deletes all your files"},
		{"env -i -u VAR assignment", []string{"env", "-i", "-u", "VAR", "LANG=C", "sudo", "x"}, `"sudo" asks for the password`},
		{"env clustered -iu VAR", []string{"env", "-iu", "VAR", "sudo", "x"}, `"sudo" asks for the password`},
		{"env attached -uVAR", []string{"env", "-uVAR", "sudo", "x"}, `"sudo" asks for the password`},
		{"env legacy dash", []string{"env", "-", "sudo", "apt", "update"}, `"sudo" asks for the password`},
		{"env -S split string", []string{"env", "-S", "sudo apt update"}, `"sudo" asks for the password`},
		{"env --split-string=", []string{"env", "--split-string=rm -rf /"}, "deletes all your files"},
		{"env abbreviated --s=", []string{"env", "--s=sudo x"}, `"sudo" asks for the password`},
		{"env abbreviated --s separate arg", []string{"env", "--s", "sudo x"}, `"sudo" asks for the password`},
		{"systemd-run multi-word description then env -S", []string{"systemd-run", "--user", "--description", "my job", "env", "-S", "sudo x"}, `"sudo" asks for the password`},
		{"systemd-run multi-word description then env -S with flag", []string{"systemd-run", "--user", "--description", "my job", "env", "-S", "-i sudo x"}, `"sudo" asks for the password`},
		{"env --", []string{"env", "--", "sudo", "apt", "update"}, `"sudo" asks for the password`},
		{"env -i --", []string{"env", "-i", "--", "rm", "-rf", "/"}, "deletes all your files"},
		{"nice -n 10", []string{"nice", "-n", "10", "rm", "-rf", "/"}, "deletes all your files"},
		{"nice -n negative", []string{"nice", "-n", "-5", "rm", "-rf", "/"}, "deletes all your files"},
		{"nice attached -n10", []string{"nice", "-n10", "rm", "-rf", "/"}, "deletes all your files"},
		{"nice legacy -10", []string{"nice", "-10", "rm", "-rf", "/"}, "deletes all your files"},
		{"nice --adjustment 10", []string{"nice", "--adjustment", "10", "rm", "-rf", "/"}, "deletes all your files"},
		{"nice --adjustment=10", []string{"nice", "--adjustment=10", "rm", "-rf", "/"}, "deletes all your files"},
		{"nice -n 10 --", []string{"nice", "-n", "10", "--", "rm", "-rf", "/"}, "deletes all your files"},
		{"systemd-run --user", []string{"systemd-run", "--user", "rm", "-rf", "/"}, "deletes all your files"},
		{"systemd-run --user --", []string{"systemd-run", "--user", "--scope", "--", "rm", "-rf", "/"}, "deletes all your files"},
		{"systemd-run --unit val", []string{"systemd-run", "--user", "--unit", "job", "rm", "-rf", "/"}, "deletes all your files"},
		{"systemd-run --unit=val", []string{"systemd-run", "--user", "--unit=job", "rm", "-rf", "/"}, "deletes all your files"},
		{"systemd-run -p prop", []string{"systemd-run", "--user", "-p", "MemoryMax=1G", "rm", "-rf", "/"}, "deletes all your files"},
		{"systemd-run multi-word description", []string{"systemd-run", "--user", "--description", "my cool job", "rm", "-rf", "/"}, "deletes all your files"},
		{"systemd-run inline multi-word description", []string{"systemd-run", "--user", "--description=my cool job", "rm", "-rf", "/"}, "deletes all your files"},
		{"pkexec --user root", []string{"pkexec", "--user", "root", "rm", "-rf", "/"}, "deletes all your files"},
		{"pkexec --disable-internal-agent", []string{"pkexec", "--disable-internal-agent", "rm", "-rf", "/"}, "deletes all your files"},
		{"run0 --user root", []string{"run0", "--user", "root", "rm", "-rf", "/"}, "deletes all your files"},
		{"run0 --lightweight value", []string{"run0", "--lightweight", "yes", "rm", "-rf", "/"}, "deletes all your files"},
		{"run0 --lightweight=value", []string{"run0", "--lightweight=yes", "rm", "-rf", "/"}, "deletes all your files"},
		{"run0 --lightweight bare", []string{"run0", "--lightweight", "rm", "-rf", "/"}, "deletes all your files"},
		{"kdesu -n no-arg", []string{"kdesu", "-n", "rm", "-rf", "/"}, "deletes all your files"},
		{"kdesu -u root -c", []string{"kdesu", "-u", "root", "-c", "rm -rf /"}, "deletes all your files"},
		{"xargs -n 1", []string{"xargs", "-n", "1", "sudo", "x"}, `"sudo" asks for the password`},
		{"xargs -0 -I {}", []string{"xargs", "-0", "-I", "{}", "rm", "-rf", "/"}, "deletes all your files"},
		{"exec -c -a name", []string{"exec", "-c", "-a", "custom", "sudo", "x"}, `"sudo" asks for the password`},
		{"setsid -f -w", []string{"setsid", "-f", "-w", "rm", "-rf", "/"}, "deletes all your files"},
		{"time -o file", []string{"time", "-o", "/tmp/t.log", "rm", "-rf", "/"}, "deletes all your files"},
		{"chained wrappers", []string{"env", "-i", "nice", "-n", "10", "systemd-run", "--user", "rm", "-rf", "/"}, "deletes all your files"},
		{"wrapper inside flatpak-spawn args", []string{"flatpak-spawn", "--host", "pkexec", "wipefs", "-a", "/dev/sdb"}, `"wipefs" wipes a disk`},
		{"wrapper with flag inside flatpak-spawn args", []string{"flatpak-spawn", "--host", "pkexec", "--user", "root", "wipefs", "-a", "/dev/sdb"}, `"wipefs" wipes a disk`},
		{"wrapper inside konsole -e args", []string{"konsole", "--hold", "-e", "pkexec", "mkfs.ext4", "/dev/sdb1"}, `"mkfs.ext4" wipes a disk`},
		{"wrapper inside sh -c args", []string{"sh", "-c", "foo nice rm -rf /"}, "deletes all your files"},
		{"wrapper with flag inside sh -c args", []string{"sh", "-c", "foo nice -n 10 rm -rf /"}, "deletes all your files"},
		{"shell text with wrapper flags", []string{"sh", "-c", "env -i sudo apt update"}, `"sudo" asks for the password`},
		{"path-qualified wrapper", []string{"/usr/bin/env", "-u", "HOME", "/usr/bin/sudo", "x"}, `"sudo" asks for the password`},
		{"unknown long flag without value", []string{"systemd-run", "--unknown-bool", "rm", "-rf", "/"}, "deletes all your files"},
		{"unknown long flag with value", []string{"systemd-run", "--unknown-opt", "val", "rm", "-rf", "/"}, "deletes all your files"},
		{"unknown long flag with inline value", []string{"systemd-run", "--unknown-opt=val", "rm", "-rf", "/"}, "deletes all your files"},
		{"unknown long flag with value then known flag", []string{"systemd-run", "--unknown-opt", "val", "--user", "rm", "-rf", "/"}, "deletes all your files"},
		{"unknown long flag with flag-like value", []string{"systemd-run", "--unknown-opt", "-u", "rm", "-rf", "/"}, "deletes all your files"},
		{"unknown short flag without value", []string{"env", "-Z", "sudo", "x"}, `"sudo" asks for the password`},
		{"unknown short flag with value", []string{"env", "-Z", "val", "sudo", "x"}, `"sudo" asks for the password`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := blockedCommand(tc.cmd)
			if !strings.Contains(got, tc.want) || !strings.HasSuffix(got, blockedSuffix) {
				t.Errorf("blockedCommand(%q) = %q, want it to contain %q and point to the docs", tc.cmd, got, tc.want)
			}
		})
	}
}

func TestLAUNCH12_WrapperFlagsAllowNormalCommands(t *testing.T) {
	for _, tc := range []struct {
		name string
		cmd  []string
	}{
		{"env -i normal app", []string{"env", "-i", "konsole", "-e", "htop"}},
		{"env -u VAR with su in message", []string{"env", "-u", "VAR", "notify-send", "su"}},
		{"env -u su unsetting var named su", []string{"env", "-u", "su", "notify-send", "hello"}},
		{"env -i -- normal app", []string{"env", "-i", "--", "notify-send", "su"}},
		{"env -- program starting with dash", []string{"env", "--", "-i", "sudo"}},
		{"nice -n 10 normal app", []string{"nice", "-n", "10", "konsole", "-e", "htop"}},
		{"nice -n 10 with su in message", []string{"nice", "-n", "10", "notify-send", "su"}},
		{"nice -10 with su in message", []string{"nice", "-10", "notify-send", "su"}},
		{"systemd-run --user with su in message", []string{"systemd-run", "--user", "notify-send", "su"}},
		{"systemd-run --user -- with su in message", []string{"systemd-run", "--user", "--scope", "--", "notify-send", "su"}},
		{"systemd-run --unit named su", []string{"systemd-run", "--user", "--unit", "su", "notify-send", "hello"}},
		{"systemd-run unknown inline flag", []string{"systemd-run", "--unknown-opt=val", "notify-send", "su"}},
		{"systemd-run unknown flag with value", []string{"systemd-run", "--unknown-opt", "val", "notify-send", "su"}},
		{"systemd-run unknown flag before --", []string{"systemd-run", "--unknown-bool", "--", "notify-send", "su"}},
		{"pkexec --user root normal app", []string{"pkexec", "--user", "root", "synaptic"}},
		{"run0 --user root normal app", []string{"run0", "--user", "root", "systemctl", "restart", "cups"}},
		{"run0 --lightweight value normal app", []string{"run0", "--lightweight", "yes", "notify-send", "su"}},
		{"run0 --lightweight=value normal app", []string{"run0", "--lightweight=yes", "notify-send", "su"}},
		{"xargs -n 1 normal app", []string{"xargs", "-n", "1", "notify-send", "su"}},
		{"non-wrapper -u flag and su arg", []string{"notify-send", "-u", "critical", "su"}},
		{"non-wrapper -n flag and sudo arg", []string{"notify-send", "-n", "10", "sudo"}},
		{"non-wrapper -- and sudo arg", []string{"echo", "--", "sudo"}},
		{"non-wrapper --user and rm -rf / arg", []string{"echo", "--user", "rm", "-rf", "/"}},
		{"non-wrapper unknown flag and sudo arg", []string{"mytool", "--unknown-flag", "val", "sudo"}},
		{"shell echo -- sudo", []string{"sh", "-c", "echo -- sudo"}},
		{"shell ls path ending in mkfs.ext4", []string{"sh", "-c", "ls -l /usr/sbin/mkfs.ext4"}},
		{"shell xdg-open path ending in su", []string{"sh", "-c", "xdg-open ~/Notes/su"}},
		{"env -S normal app", []string{"env", "-S", "notify-send su"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := blockedCommand(tc.cmd); got != "" {
				t.Errorf("blockedCommand(%q) = %q, want it allowed", tc.cmd, got)
			}
		})
	}
}

func TestLAUNCH12_BlockedCommandsMakeTheConfigurationInvalid(t *testing.T) {
	problems := problemsOf(t, withButtons(`r1 = { command = ["sh", "-c", "rm -rf ~"] }`))
	requireProblem(t, problems, "layouts.default.buttons.r1.command: blocked: deletes all your files")
}

// FuzzBlockedCommand: no command, however odd, may crash the check (QA-08).
func FuzzBlockedCommand(f *testing.F) {
	for _, s := range []string{
		"rm -rf /",
		":(){ :|:& };:",
		"curl x | sh",
		"a; b && c $(d) `e`",
		"f(){",
		"env -i -u VAR -- sudo x",
		"env --split-string=-i sudo",
		"nice -n 10 rm -rf /",
		"systemd-run --user --unknown val rm -rf /",
	} {
		f.Add(s)
	}
	f.Fuzz(func(_ *testing.T, text string) {
		_ = blockedCommand([]string{"sh", "-c", text})
		_ = blockedCommand(strings.Fields(text))
	})
}
