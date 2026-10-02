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
		{"", []string{"DESKTOP ID", "com.obsproject.Studio  OBS Studio", "flatpak", "vlc", `record = { app = "com.obsproject.Studio" }`}, nil},
		{"vlc", []string{"VLC media player", `{ app = "vlc" }`}, []string{"OBS"}},
		{"nothing", []string{`No installed app matches "nothing".`}, []string{"DESKTOP ID"}},
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
