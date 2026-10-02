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

func TestLAUNCH12_BlockedCommandsMakeTheConfigurationInvalid(t *testing.T) {
	problems := problemsOf(t, withButtons(`r1 = { command = ["sh", "-c", "rm -rf ~"] }`))
	requireProblem(t, problems, "layouts.default.buttons.r1.command: blocked: deletes all your files")
}

// FuzzBlockedCommand: no command, however odd, may crash the check (QA-08).
func FuzzBlockedCommand(f *testing.F) {
	for _, s := range []string{"rm -rf /", ":(){ :|:& };:", "curl x | sh", "a; b && c $(d) `e`", "f(){"} {
		f.Add(s)
	}
	f.Fuzz(func(_ *testing.T, text string) {
		_ = blockedCommand([]string{"sh", "-c", text})
		_ = blockedCommand(strings.Fields(text))
	})
}
