package launcher

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Dzobash/apptrol/internal/logattr"
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

	apps := Find(nil, []string{home, flatpak, snap, system})
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
	apps := Find(nil, []string{home, system})
	if got := apps["discord"]; got.Name != "My Discord" || got.Source != SourceUser {
		t.Errorf("discord = %+v, want the user's own file", got)
	}
}

func TestLAUNCH02_HiddenMeansNotInstalled(t *testing.T) {
	home, _, _, system := dataDirs(t)
	writeDesktop(t, home, "steam.desktop", entry("Steam", "steam", "Hidden=true")) // the user "deleted" it
	writeDesktop(t, system, "steam.desktop", entry("Steam", "steam"))
	if _, ok := Find(nil, []string{home, system})["steam"]; ok {
		t.Error("an app hidden in the first folder counts as installed")
	}
}

func TestLAUNCH09_SearchListsVisibleAppsByIDOrName(t *testing.T) {
	_, _, _, system := dataDirs(t)
	writeDesktop(t, system, "com.obsproject.Studio.desktop", entry("OBS Studio", "obs"))
	writeDesktop(t, system, "vlc.desktop", entry("VLC media player", "vlc %U"))
	writeDesktop(t, system, "obs-helper.desktop", entry("Helper", "helper", "NoDisplay=true"))
	apps := Find(nil, []string{system})
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

// failReader serves its data and then fails, like a read error in the middle
// of a file (#260).
type failReader struct {
	data []byte
}

func (r *failReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, errors.New("read error")
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

// TestLAUNCH02_LongLinesAreStillParsed: a line over bufio.Scanner's 64 KiB
// limit (a long MimeType=) does not truncate the file; the keys before and
// after it are read (LAUNCH-02, LAUNCH-03, ADR 0019, #260).
func TestLAUNCH02_LongLinesAreStillParsed(t *testing.T) {
	home, _, _, system := dataDirs(t)
	long := "MimeType=" + strings.Repeat("application/x-apptrol-test;", 3000)
	if len(long) <= 64*1024 {
		t.Fatalf("test line is only %d bytes", len(long))
	}
	writeDesktop(t, system, "big.desktop",
		"[Desktop Entry]\nType=Application\nName=Big\n"+long+"\nExec=big %U\nDBusActivatable=true\nHidden=false\n")
	app, ok := Find(nil, []string{home, system})["big"]
	if !ok {
		t.Fatal("an app whose file has a line over 64 KiB is missing")
	}
	if app.Name != "Big" || app.Exec != "big %U" || !app.DBusActivatable {
		t.Errorf("big = %+v, want the keys before and after the long line", app)
	}
}

// TestLAUNCH02_AReadErrorRejectsTheFile: keys after a failed read were never
// seen, so the file is rejected even when Type=Application came before the
// error — a partial entry must not count as valid (LAUNCH-02, LAUNCH-03,
// ADR 0019, #260).
func TestLAUNCH02_AReadErrorRejectsTheFile(t *testing.T) {
	app, hidden, ok, err := parseDesktopEntry(&failReader{data: []byte("[Desktop Entry]\nType=Application\nName=Ghost\nExec=ghost\n")})
	if err == nil || ok || hidden || app != (App{}) {
		t.Errorf("after a read error: app = %+v, hidden = %v, ok = %v, err = %v; want a rejected file", app, hidden, ok, err)
	}
	_, _, ok, err = parseDesktopEntry(&failReader{data: []byte("[Desktop Entry]\nType=Appli")})
	if err == nil || ok {
		t.Errorf("early read error: ok = %v, err = %v; want a rejected file", ok, err)
	}
}

// TestLAUNCH02_ARejectedFileIsLogged: a desktop file that cannot be read is
// rejected and logged, so a missing app can be explained from the logs
// (LAUNCH-02, ADR 0019, #260).
func TestLAUNCH02_ARejectedFileIsLogged(t *testing.T) {
	home, _, _, system := dataDirs(t)
	if err := os.MkdirAll(filepath.Join(system, "applications"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A dangling symlink: the walk finds it, but it cannot be opened.
	link := filepath.Join(system, "applications", "ghost.desktop")
	if err := os.Symlink(filepath.Join(system, "applications", "gone.desktop"), link); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil)).With(logattr.Component(logattr.Launcher))
	if _, ok := Find(log, []string{home, system})["ghost"]; ok {
		t.Error("an unreadable desktop file counts as installed")
	}
	out := buf.String()
	for _, want := range []string{"desktop file could not be read", "ghost.desktop", "desktop_file_unreadable", "apptrol.component=launcher"} {
		if !strings.Contains(out, want) {
			t.Errorf("log does not contain %q: %s", want, out)
		}
	}
}

// TestLAUNCH02_ARejectedFileKeepsFirstFolderWins: an unreadable file in the
// first folder still wins over a good copy in a later folder, as LAUNCH-02
// says for Hidden=true (LAUNCH-02, ADR 0019, #260).
func TestLAUNCH02_ARejectedFileKeepsFirstFolderWins(t *testing.T) {
	home, _, _, system := dataDirs(t)
	if err := os.MkdirAll(filepath.Join(home, "applications"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "applications", "gone.desktop"), filepath.Join(home, "applications", "discord.desktop")); err != nil {
		t.Fatal(err)
	}
	writeDesktop(t, system, "discord.desktop", entry("Discord", "discord"))
	if _, ok := Find(nil, []string{home, system})["discord"]; ok {
		t.Error("a later folder's file resurrected an app whose first-folder file was rejected")
	}
}

// endlessReader never ends and always has more, like a .desktop symlink to
// /dev/zero (#260).
type endlessReader struct{}

func (endlessReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}

// TestLAUNCH02_AnOversizedFileIsRejected: the read is capped at
// maxDesktopFileSize, so an endless or huge file is rejected as unreadable
// instead of being read until memory runs out (LAUNCH-02, ADR 0019, #260).
func TestLAUNCH02_AnOversizedFileIsRejected(t *testing.T) {
	app, hidden, ok, err := parseDesktopEntry(endlessReader{})
	if !errors.Is(err, errDesktopFileTooLarge) || ok || hidden || app != (App{}) {
		t.Errorf("endless reader: app = %+v, hidden = %v, ok = %v, err = %v; want a rejected file with errDesktopFileTooLarge", app, hidden, ok, err)
	}
}

// TestLAUNCH02_TheSizeCapBoundary: a file of exactly maxDesktopFileSize is
// still read; one byte more is rejected (LAUNCH-02, #260).
func TestLAUNCH02_TheSizeCapBoundary(t *testing.T) {
	header := "[Desktop Entry]\nType=Application\nExec=x\n"
	atCap := header + strings.Repeat("#", maxDesktopFileSize-len(header))
	if len(atCap) != maxDesktopFileSize {
		t.Fatalf("test setup: %d bytes, want %d", len(atCap), maxDesktopFileSize)
	}
	if _, _, ok, err := parseDesktopEntry(strings.NewReader(atCap)); err != nil || !ok {
		t.Errorf("a file of exactly the cap: ok = %v, err = %v; want accepted", ok, err)
	}
	if _, _, ok, err := parseDesktopEntry(strings.NewReader(atCap + "a")); !errors.Is(err, errDesktopFileTooLarge) || ok {
		t.Errorf("a file one byte over the cap: ok = %v, err = %v; want rejected with errDesktopFileTooLarge", ok, err)
	}
}

// TestLAUNCH02_LastLineWithoutNewlineIsRead: a file whose last line has no
// trailing newline is still parsed completely (LAUNCH-02, LAUNCH-03,
// ADR 0019, #260).
func TestLAUNCH02_LastLineWithoutNewlineIsRead(t *testing.T) {
	app, _, ok, err := parseDesktopEntry(strings.NewReader("[Desktop Entry]\nType=Application\nExec=x"))
	if err != nil || !ok || app.Exec != "x" {
		t.Errorf("parseDesktopEntry without a trailing newline = %+v, ok = %v, err = %v; want Exec=x and ok", app, ok, err)
	}
}
