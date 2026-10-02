package launcher

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeDesktop writes a desktop file below dir/applications.
func writeDesktop(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, "applications", rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func entry(name, exec string, extra ...string) string {
	return "[Desktop Entry]\nType=Application\nName=" + name + "\nExec=" + exec + "\n" + strings.Join(extra, "\n") + "\n"
}

// dataDirs: a user data home, a Flatpak export, the Snap folder and /usr/share.
func dataDirs(t *testing.T) (home, flatpak, snap, system string) {
	root := t.TempDir()
	home = filepath.Join(root, "home", ".local", "share")
	flatpak = filepath.Join(root, "var", "lib", "flatpak", "exports", "share")
	snap = filepath.Join(root, "var", "lib", "snapd", "desktop")
	system = filepath.Join(root, "usr", "share")
	t.Setenv("XDG_DATA_HOME", home)
	return
}

func TestLAUNCH02_DesktopFilesAreFound(t *testing.T) {
	home, flatpak, snap, system := dataDirs(t)
	writeDesktop(t, flatpak, "com.obsproject.Studio.desktop", entry("OBS Studio", "/usr/bin/flatpak run com.obsproject.Studio"))
	writeDesktop(t, snap, "firefox_firefox.desktop", entry("Firefox", "/snap/bin/firefox %u"))
	writeDesktop(t, system, "discord.desktop", entry("Discord", "/usr/bin/discord"))
	writeDesktop(t, system, "kde/org.kde.konsole.desktop", entry("Konsole", "konsole"))
	writeDesktop(t, home, "myscript.desktop", entry("My script", "~/bin/script"))
	writeDesktop(t, system, "readme.txt", "not a desktop file")
	writeDesktop(t, system, "link.desktop", "[Desktop Entry]\nType=Link\nName=A link\nURL=https://example.org\n")

	apps := Find([]string{home, flatpak, snap, system})
	want := map[string]string{
		"com.obsproject.Studio": SourceFlatpak,
		"firefox_firefox":       SourceSnap,
		"discord":               SourceSystem,
		"kde-org.kde.konsole":   SourceSystem, // subfolders join the ID with "-"
		"myscript":              SourceUser,
	}
	got := map[string]string{}
	for id, app := range apps {
		got[id] = app.Source
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("apps = %v, want %v", got, want)
	}
	if apps["firefox_firefox"].Name != "Firefox" || apps["firefox_firefox"].Exec != "/snap/bin/firefox %u" {
		t.Errorf("firefox = %+v", apps["firefox_firefox"])
	}
}

func TestLAUNCH02_TheFirstFolderWins(t *testing.T) {
	home, _, _, system := dataDirs(t)
	writeDesktop(t, home, "discord.desktop", entry("My Discord", "discord --start-minimized"))
	writeDesktop(t, system, "discord.desktop", entry("Discord", "discord"))
	apps := Find([]string{home, system})
	if got := apps["discord"]; got.Name != "My Discord" || got.Source != SourceUser {
		t.Errorf("discord = %+v, want the user's own file", got)
	}
}

func TestLAUNCH02_HiddenMeansNotInstalled(t *testing.T) {
	home, _, _, system := dataDirs(t)
	writeDesktop(t, home, "steam.desktop", entry("Steam", "steam", "Hidden=true")) // the user "deleted" it
	writeDesktop(t, system, "steam.desktop", entry("Steam", "steam"))
	if _, ok := Find([]string{home, system})["steam"]; ok {
		t.Error("an app hidden in the first folder counts as installed")
	}
}

func TestLAUNCH09_SearchListsVisibleAppsByIDOrName(t *testing.T) {
	_, _, _, system := dataDirs(t)
	writeDesktop(t, system, "com.obsproject.Studio.desktop", entry("OBS Studio", "obs"))
	writeDesktop(t, system, "vlc.desktop", entry("VLC media player", "vlc %U"))
	writeDesktop(t, system, "obs-helper.desktop", entry("Helper", "helper", "NoDisplay=true"))
	apps := Find([]string{system})
	ids := func(list []App) []string {
		var out []string
		for _, a := range list {
			out = append(out, a.ID)
		}
		return out
	}
	if got := ids(apps.Search("")); !reflect.DeepEqual(got, []string{"com.obsproject.Studio", "vlc"}) {
		t.Errorf("all = %v", got)
	}
	if got := ids(apps.Search("OBS")); !reflect.DeepEqual(got, []string{"com.obsproject.Studio"}) {
		t.Errorf("search obs = %v (NoDisplay apps are not listed)", got)
	}
	if got := ids(apps.Search("media")); !reflect.DeepEqual(got, []string{"vlc"}) {
		t.Errorf("search by name = %v", got)
	}
	if _, ok := apps["obs-helper"]; !ok {
		t.Error("a NoDisplay app is still installed, only not listed")
	}
}

func TestLAUNCH02_DataDirsDefaults(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/data/home")
	t.Setenv("XDG_DATA_DIRS", "")
	if got := DataDirs(); !reflect.DeepEqual(got, []string{"/data/home", "/usr/local/share", "/usr/share"}) {
		t.Errorf("DataDirs = %v", got)
	}
	t.Setenv("XDG_DATA_DIRS", "/var/lib/flatpak/exports/share:/usr/share")
	if got := DataDirs(); !reflect.DeepEqual(got, []string{"/data/home", "/var/lib/flatpak/exports/share", "/usr/share"}) {
		t.Errorf("DataDirs = %v", got)
	}
}

func TestLAUNCH03_ExecIsParsedAsTheSpecificationSays(t *testing.T) {
	for _, tc := range []struct {
		exec string
		want []string
	}{
		{"/snap/bin/firefox %u", []string{"/snap/bin/firefox"}},
		{"vlc --started-from-file %U", []string{"vlc", "--started-from-file"}},
		{"gimp-2.10 %F", []string{"gimp-2.10"}},
		{"app --icon %i --file=%f", []string{"app", "--icon", "--file="}},
		{"app --name %c", []string{"app", "--name", "My App"}},
		{"app 100%%", []string{"app", "100%"}},
		{`"/opt/My App/run" --flag`, []string{"/opt/My App/run", "--flag"}},
		{`sh -c "echo \"hi\" \$HOME"`, []string{"sh", "-c", `echo "hi" $HOME`}},
		{`app "a\\\\b"`, []string{"app", `a\b`}}, // four backslashes in the file: one in the argument
		{`app\sname`, []string{"app", "name"}},   // escapes come before splitting: \s separates
		{"  app   two  ", []string{"app", "two"}},
		{`app ""`, []string{"app", ""}},
	} {
		got, err := ParseExec(tc.exec, "My App")
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ParseExec(%q) = %q, %v; want %q", tc.exec, got, err, tc.want)
		}
	}
}

func TestLAUNCH03_BadExecIsRejected(t *testing.T) {
	for _, exec := range []string{`app "open`, "app %z", "app 50%", "", "%u"} {
		if got, err := ParseExec(exec, "x"); err == nil {
			t.Errorf("ParseExec(%q) = %q, want an error", exec, got)
		}
	}
}

func TestLAUNCH07_ProgramName(t *testing.T) {
	for exec, want := range map[string]string{
		"/snap/bin/firefox %u":     "firefox",
		"discord":                  "discord",
		`"/opt/My App/run" --flag`: "run",
		"/usr/bin/flatpak run com.obsproject.Studio": "flatpak",
		`app "open`: "",
	} {
		if got := ProgramName(exec); got != want {
			t.Errorf("ProgramName(%q) = %q, want %q", exec, got, want)
		}
	}
}

// FuzzParseExec: no Exec value, however broken, may crash the parser (QA-08).
func FuzzParseExec(f *testing.F) {
	for _, s := range []string{"/snap/bin/firefox %u", `sh -c "echo \"hi\""`, `a "b\\\\c"`, "%%", `"`, `\`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, exec string) {
		args, err := ParseExec(exec, "name")
		if err == nil && len(args) == 0 {
			t.Errorf("ParseExec(%q) returned no program and no error", exec)
		}
	})
}

func TestLAUNCH10_MissingLaunchers(t *testing.T) {
	apps := Apps{"discord": {ID: "discord"}}
	got := apps.Missing(map[string]map[string]string{
		"default": {"record": "com.obsproject.Studio", "r3": "discord"},
		"gaming":  {"marker_set": "steam"},
	})
	want := []NotInstalled{
		{Layout: "default", Button: "record", DesktopID: "com.obsproject.Studio"},
		{Layout: "gaming", Button: "marker_set", DesktopID: "steam"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Missing = %+v, want %+v", got, want)
	}
}
