// Package launcher finds the apps installed for the desktop and, in later
// steps, starts them (ADR 0019). Apps are found through their desktop files
// as the Desktop Entry and Base Directory specifications describe:
// https://specifications.freedesktop.org/desktop-entry-spec/latest/ and
// https://specifications.freedesktop.org/basedir-spec/latest/.
package launcher

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Sources of an app, for `apptrol list apps` (LAUNCH-09).
const (
	SourceUser    = "user"
	SourceFlatpak = "flatpak"
	SourceSnap    = "snap"
	SourceSystem  = "system"
)

// App is an installed app: one desktop file.
type App struct {
	ID              string // desktop ID, e.g. "com.obsproject.Studio" or "firefox_firefox"
	Name            string // Name= (not localized)
	Exec            string // Exec=, unparsed; ParseExec splits it
	DBusActivatable bool   // started over D-Bus when it has no Exec (LAUNCH-03)
	NoDisplay       bool   // not shown in menus, nor by `apptrol list apps`
	Source          string // SourceUser, SourceFlatpak, SourceSnap or SourceSystem
	Path            string // the desktop file
}

// Apps are the installed apps by desktop ID.
type Apps map[string]App

// DataDirs returns the folders searched for applications/, most important
// first: $XDG_DATA_HOME, then each $XDG_DATA_DIRS entry, with the
// specification's defaults.
func DataDirs() []string {
	home := os.Getenv("XDG_DATA_HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".local", "share")
		}
	}
	dirs := os.Getenv("XDG_DATA_DIRS")
	if dirs == "" {
		dirs = "/usr/local/share:/usr/share"
	}
	var out []string
	if home != "" {
		out = append(out, home)
	}
	for _, d := range strings.Split(dirs, ":") {
		if d != "" {
			out = append(out, d)
		}
	}
	return out
}

// Find reads the desktop files under applications/ in the given folders
// (LAUNCH-02). For each desktop ID the file in the first folder wins, as
// the specification says; a file with Hidden=true there means the app
// counts as not installed, even if a later folder has it.
func Find(dataDirs []string) Apps {
	apps := Apps{}
	seen := map[string]bool{}
	for i, dir := range dataDirs {
		root := filepath.Join(dir, "applications")
		source := sourceOf(dir, i == 0 && isDataHome(dir))
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".desktop") {
				return nil
			}
			id := desktopID(root, path)
			if seen[id] {
				return nil
			}
			seen[id] = true
			app, hidden, ok := readDesktopFile(path)
			if !ok || hidden {
				return nil
			}
			app.ID, app.Source, app.Path = id, source, path
			apps[id] = app
			return nil
		})
	}
	return apps
}

// Installed finds the installed apps in the usual folders.
func Installed() Apps { return Find(DataDirs()) }

// desktopID is the file's path below applications/, with "/" turned into
// "-" and without ".desktop": applications/kde/org.kde.foo.desktop is
// "kde-org.kde.foo".
func desktopID(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = filepath.Base(path)
	}
	return strings.ReplaceAll(strings.TrimSuffix(rel, ".desktop"), string(filepath.Separator), "-")
}

func isDataHome(dir string) bool {
	if h := os.Getenv("XDG_DATA_HOME"); h != "" {
		return filepath.Clean(h) == filepath.Clean(dir)
	}
	home, err := os.UserHomeDir()
	return err == nil && filepath.Clean(dir) == filepath.Join(home, ".local", "share")
}

// sourceOf names where a data folder's apps come from.
func sourceOf(dir string, dataHome bool) string {
	switch {
	case strings.Contains(dir, "/flatpak/exports/share"):
		return SourceFlatpak // before user: Flatpak's user installs live under the data home
	case dataHome:
		return SourceUser
	case strings.Contains(dir, "/snapd/desktop"):
		return SourceSnap
	}
	return SourceSystem
}

// readDesktopFile reads the keys Apptrol needs from the [Desktop Entry]
// group. ok is false for files that are not applications.
func readDesktopFile(path string) (app App, hidden, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return App{}, false, false
	}
	defer f.Close()
	inEntry := false
	typ := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inEntry = line == "[Desktop Entry]"
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !inEntry || !found {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch key {
		case "Type":
			typ = value
		case "Name":
			app.Name = unescape(value)
		case "Exec":
			app.Exec = value
		case "Hidden":
			hidden = value == "true"
		case "NoDisplay":
			app.NoDisplay = value == "true"
		case "DBusActivatable":
			app.DBusActivatable = value == "true"
		}
	}
	return app, hidden, typ == "Application"
}

// Search returns the apps shown by `apptrol list apps [search]`: without
// NoDisplay ones, matching search case-insensitively in ID or name, sorted
// by ID, ignoring case (LAUNCH-09).
func (a Apps) Search(search string) []App {
	search = strings.ToLower(search)
	var out []App
	for _, app := range a {
		if app.NoDisplay {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(app.ID), search) && !strings.Contains(strings.ToLower(app.Name), search) {
			continue
		}
		out = append(out, app)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].ID), strings.ToLower(out[j].ID)
		if a != b {
			return a < b
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// NotInstalled is a launcher whose desktop ID is not installed.
type NotInstalled struct {
	Layout, Button, DesktopID string
}

// Missing returns the launchers of every layout whose desktop ID is not
// installed, sorted. They are only warned about: an app may be installed
// later (LAUNCH-10).
func (a Apps) Missing(layouts map[string]map[string]string) []NotInstalled {
	var out []NotInstalled
	for layout, buttons := range layouts {
		for button, id := range buttons {
			if _, ok := a[id]; !ok {
				out = append(out, NotInstalled{Layout: layout, Button: button, DesktopID: id})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Layout != out[j].Layout {
			return out[i].Layout < out[j].Layout
		}
		return out[i].Button < out[j].Button
	})
	return out
}
