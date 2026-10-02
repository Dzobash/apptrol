package mixer

import (
	"log/slog"
	"reflect"
	"testing"
)

// Launcher buttons (LAUNCH-01): Record, the Marker buttons and R as a launcher.

func launchSetup() Setup {
	s := testSetup()
	s.Launchers = map[LED]Launch{
		{Transport: Record}:          {DesktopID: "com.obsproject.Studio"},
		{Transport: MarkerPrev}:      {Command: []string{"konsole", "-e", "htop"}},
		{Button: ButtonR, Column: 3}: {DesktopID: "discord", SkipIfRunning: true},
	}
	s.Buttons = map[LED]Button{{Button: ButtonR, Column: 3}: {Mode: ModeLauncher}}
	return s
}

func TestLAUNCH01_LauncherButtonsStartTheirApps(t *testing.T) {
	w := newWorld(t, launchSetup(), State{})
	w.key(Record)
	w.key(MarkerPrev)
	w.press(ButtonR, 3)
	want := []LaunchApp{
		{Button: LED{Transport: Record}, Launch: Launch{DesktopID: "com.obsproject.Studio"}},
		{Button: LED{Transport: MarkerPrev}, Launch: Launch{Command: []string{"konsole", "-e", "htop"}}},
		{Button: LED{Button: ButtonR, Column: 3}, Launch: Launch{DesktopID: "discord", SkipIfRunning: true}},
	}
	if !reflect.DeepEqual(w.launched, want) {
		t.Errorf("launched = %+v, want %+v", w.launched, want)
	}
}

func TestLAUNCH01_WithoutALauncherNothingStarts(t *testing.T) {
	w := started(t)
	w.key(Record)
	w.key(MarkerSet)
	w.key(MarkerNext)
	if len(w.launched) != 0 {
		t.Errorf("launched %+v without launchers", w.launched)
	}
	w.wantNotice(slog.LevelDebug, "no launcher configured")
}

func TestLAUNCH01_RAsLauncherDoesNotPlayOrPause(t *testing.T) {
	w := newWorld(t, launchSetup(), State{})
	w.do(PlayerSnapshot{Players: []Player{player("discord", "Discord", "")}})
	w.press(ButtonR, 3)
	w.wantCommands()
	if len(w.launched) != 1 {
		t.Errorf("launched = %+v", w.launched)
	}
	w.wantLEDs(3, false, false, false) // MEDIA-09: R is off as a launcher
}
