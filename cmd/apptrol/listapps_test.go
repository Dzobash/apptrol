package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dzobash/apptrol/internal/launcher"
)

func TestLAUNCH09_ListApps(t *testing.T) {
	apps := launcher.Apps{
		"com.obsproject.Studio": {ID: "com.obsproject.Studio", Name: "OBS Studio", Source: launcher.SourceFlatpak},
		"vlc":                   {ID: "vlc", Name: "VLC media player", Source: launcher.SourceSystem},
	}
	for _, tc := range []struct {
		search string
		want   []string
		absent []string
	}{
		{"", []string{
			"DESKTOP ID",
			"com.obsproject.Studio  OBS Studio",
			"flatpak",
			"vlc",
			`Installed apps, for launchers (app = "<desktop ID>").`,
			"For match = […] in [apps], the name can differ: run `apptrol list` while the app plays.",
			`record = { app = "<desktop ID>" }`,
		}, []string{`record = { app = "com.obsproject.Studio" }`}},
		{"vlc", []string{"VLC media player", `record = { app = "vlc" }`, "Installed apps, for launchers"}, []string{"OBS", `record = { app = "<desktop ID>" }`}},
		{"nothing", []string{`No installed app matches "nothing".`}, []string{"DESKTOP ID", "Installed apps, for launchers"}},
	} {
		var out bytes.Buffer
		if err := cmdListApps(&out, tc.search, apps); err != nil {
			t.Fatal(err)
		}
		for _, w := range tc.want {
			if !strings.Contains(out.String(), w) {
				t.Errorf("search %q: output lacks %q:\n%s", tc.search, w, out.String())
			}
		}
		for _, a := range tc.absent {
			if strings.Contains(out.String(), a) {
				t.Errorf("search %q: output has %q:\n%s", tc.search, a, out.String())
			}
		}
	}
}

func TestLAUNCH09_ListAppsHintExample(t *testing.T) {
	// The first ID contains '='. Without a search word the closing example
	// must not use it: that line read like a syntax error (LAUNCH-09).
	const odd = "org.example.app=1"
	apps := launcher.Apps{
		odd:                  {ID: odd, Name: "Odd", Source: launcher.SourceUser},
		"org.example.player": {ID: "org.example.player", Name: "Player", Source: launcher.SourceSystem},
	}
	header := "Installed apps, for launchers (app = \"<desktop ID>\").\n" +
		"For match = […] in [apps], the name can differ: run `apptrol list` while the app plays.\n\n" +
		"DESKTOP ID"

	var out bytes.Buffer
	if err := cmdListApps(&out, "", apps); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.HasPrefix(got, header) {
		t.Errorf("header lines are not above the table:\n%s", got)
	}
	oddAt, playerAt := strings.Index(got, odd), strings.Index(got, "org.example.player")
	if oddAt < 0 || playerAt < 0 || oddAt > playerAt {
		t.Fatalf("test setup: the odd ID is not first:\n%s", got)
	}
	if !strings.Contains(got, `record = { app = "<desktop ID>" }`) {
		t.Errorf("unsought list lacks the placeholder example:\n%s", got)
	}
	if strings.Contains(got, `record = { app = "`+odd+`" }`) {
		t.Errorf("unsought list used the odd ID as the example:\n%s", got)
	}

	out.Reset()
	if err := cmdListApps(&out, "example", apps); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `record = { app = "`+odd+`" }`) {
		t.Errorf("a search should use the first match, even when it contains '=':\n%s", out.String())
	}
	if !strings.HasPrefix(out.String(), header) {
		t.Errorf("a search result lacks the header lines:\n%s", out.String())
	}

	out.Reset()
	if err := cmdListApps(&out, "player", apps); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `record = { app = "org.example.player" }`) {
		t.Errorf("search %q should use its first match:\n%s", "player", out.String())
	}
	if strings.Contains(out.String(), odd) {
		t.Errorf("search %q showed the other app:\n%s", "player", out.String())
	}
}

func TestLAUNCH09_ListAppsCommand(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_DATA_DIRS", t.TempDir())
	dir := filepath.Join(data, "applications")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	desktop := "[Desktop Entry]\nType=Application\nName=Konsole\nExec=konsole\n"
	if err := os.WriteFile(filepath.Join(dir, "org.kde.konsole.desktop"), []byte(desktop), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"list", "apps", "kons"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "org.kde.konsole") {
		t.Errorf("list apps kons: code %d\n%s%s", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"list", "apps", "a", "b"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "unexpected arguments") {
		t.Errorf("list apps a b: code %d, stderr %q", code, stderr.String())
	}
}
