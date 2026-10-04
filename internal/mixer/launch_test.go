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

// unlocked tells the mixer the screen is unlocked, as the session adapter does
// on start; without it, launchers do nothing (LAUNCH-13).
func (w *world) unlocked() *world { return w.do(ScreenChanged{State: ScreenUnlocked}) }

func TestLAUNCH01_LauncherButtonsStartTheirApps(t *testing.T) {
	w := newWorld(t, launchSetup(), State{}).unlocked()
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
	w := newWorld(t, launchSetup(), State{}).unlocked()
	w.do(PlayerSnapshot{Players: []Player{player("discord", "Discord", "")}})
	w.press(ButtonR, 3)
	w.wantCommands()
	if len(w.launched) != 1 {
		t.Errorf("launched = %+v", w.launched)
	}
	w.wantLEDs(3, false, false, false) // MEDIA-09: R is off as a launcher
}

// pressLaunchers presses every launcher of launchSetup.
func (w *world) pressLaunchers() {
	w.key(Record)
	w.key(MarkerPrev)
	w.press(ButtonR, 3)
}

// noticesWith counts the notices with level and message whose attributes
// contain key=value.
func (w *world) noticesWith(level slog.Level, msg, key, value string) int {
	n := 0
	for _, nt := range w.notices {
		if nt.Level != level || nt.Msg != msg {
			continue
		}
		for i := 0; i+1 < len(nt.Attrs); i += 2 {
			if nt.Attrs[i] == key && nt.Attrs[i+1] == value {
				n++
			}
		}
	}
	return n
}

func TestLAUNCH13_LaunchersStartNothingUnlessUnlocked(t *testing.T) {
	for _, tc := range []struct {
		state ScreenState
		level slog.Level
		msg   string
		want  string // the state in the log
	}{
		{ScreenLocked, slog.LevelWarn, "launcher pressed while the screen is locked; nothing started", "locked"},
		{ScreenInactive, slog.LevelWarn, "launcher pressed while the screen is locked; nothing started", "inactive"},
		{ScreenUnknown, slog.LevelInfo, "launcher not started: the screen is not known to be unlocked", "unknown"},
		{"", slog.LevelInfo, "launcher not started: the screen is not known to be unlocked", "unknown"},
	} {
		w := newWorld(t, launchSetup(), State{}).unlocked()
		w.do(ScreenChanged{State: tc.state})
		w.pressLaunchers()
		if len(w.launched) != 0 {
			t.Errorf("screen %q: launched %+v", tc.state, w.launched)
		}
		if n := w.noticesWith(tc.level, tc.msg, "apptrol.screen.state", tc.want); n != 3 {
			t.Errorf("screen %q: %d presses logged as %v %q, want 3: %v", tc.state, n, tc.level, tc.msg, w.notices)
		}
	}
}

func TestLAUNCH13_NothingStartsBeforeTheScreenStateIsKnown(t *testing.T) {
	w := newWorld(t, launchSetup(), State{}) // no ScreenChanged yet: fail closed
	w.pressLaunchers()
	if len(w.launched) != 0 {
		t.Errorf("launched %+v before the screen state was known", w.launched)
	}
	w.wantNotice(slog.LevelInfo, "launcher not started")
}

func TestLAUNCH13_UnlockingAllowsLaunchersAgain(t *testing.T) {
	w := newWorld(t, launchSetup(), State{}).unlocked()
	w.do(ScreenChanged{State: ScreenLocked})
	w.key(Record)
	w.unlocked()
	w.key(Record)
	if len(w.launched) != 1 {
		t.Errorf("launched %d times, want once (after unlocking)", len(w.launched))
	}
}

func TestLAUNCH13_EverythingElseWorksWhileLocked(t *testing.T) {
	w := newWorld(t, launchSetup(), State{})
	w.do(ControllerConnected{})
	w.do(AudioSnapshot{Streams: []Stream{spotify, firefox}, Devices: []Device{goxlr}})
	w.do(ScreenChanged{State: ScreenLocked})
	w.do(ControlMoved{Control: Control{Kind: Slider, Column: 1}, Value: 64})
	if _, ok := w.volume(spotify.ID); !ok {
		t.Error("slider 1 set no volume while locked")
	}
	w.press(ButtonM, 1)
	w.wantMuted(true, spotify.ID)
	w.press(ButtonM, 8)
	w.wantMic(true)
}

// whenLockedSetup is launchSetup with when_locked = true on Record.
func whenLockedSetup() Setup {
	s := launchSetup()
	l := s.Launchers[LED{Transport: Record}]
	l.WhenLocked = true
	s.Launchers[LED{Transport: Record}] = l
	return s
}

func TestLAUNCH14_WhenLockedStartsWhileLockedWithAWarning(t *testing.T) {
	for _, state := range []ScreenState{ScreenLocked, ScreenInactive} {
		w := newWorld(t, whenLockedSetup(), State{}).unlocked()
		w.do(ScreenChanged{State: state})
		w.pressLaunchers()
		if len(w.launched) != 1 || w.launched[0].Button != (LED{Transport: Record}) {
			t.Errorf("screen %q: launched %+v, want only Record", state, w.launched)
		}
		if n := w.noticesWith(slog.LevelWarn, "launcher pressed while the screen is locked; started (when_locked)",
			"apptrol.launcher.desktop_id", "com.obsproject.Studio"); n != 1 {
			t.Errorf("screen %q: %d warnings for the start, want 1: %v", state, n, w.notices)
		}
		if n := w.noticesWith(slog.LevelWarn, "launcher pressed while the screen is locked; nothing started",
			"apptrol.screen.state", string(state)); n != 2 {
			t.Errorf("screen %q: %d warnings for the others, want 2: %v", state, n, w.notices)
		}
	}
}

func TestLAUNCH14_WhenLockedNeverStartsWhileUnknown(t *testing.T) {
	w := newWorld(t, whenLockedSetup(), State{}) // unknown: fail closed, whatever the setting
	w.key(Record)
	if len(w.launched) != 0 {
		t.Errorf("launched %+v while the screen state is unknown", w.launched)
	}
}

func TestLAUNCH14_WhenLockedUnlockedIsAnOrdinaryPress(t *testing.T) {
	w := newWorld(t, whenLockedSetup(), State{}).unlocked()
	w.key(Record)
	if len(w.launched) != 1 {
		t.Errorf("launched %+v", w.launched)
	}
	for _, nt := range w.notices {
		if nt.Level >= slog.LevelWarn {
			t.Errorf("warning while unlocked: %v", nt)
		}
	}
}

func TestLAUNCH15_ButtonsPressedWhileLockedAreWarnings(t *testing.T) {
	w := newWorld(t, launchSetup(), State{})
	w.do(ControllerConnected{})
	w.do(AudioSnapshot{Streams: []Stream{spotify, firefox}, Devices: []Device{goxlr}})
	w.do(ScreenChanged{State: ScreenLocked})
	const msg = "button pressed while the screen is locked"
	w.press(ButtonM, 1)
	w.release(ButtonM, 1)
	w.press(ButtonM, 1)
	w.press(ButtonS, 2)
	w.key(Play)
	w.do(ControlMoved{Control: Control{Kind: Slider, Column: 1}, Value: 30})
	w.do(ControlMoved{Control: Control{Kind: Knob, Column: 1}, Value: 30})
	for button, n := range map[string]int{"M1": 2, "S2": 1, "▶": 1} {
		if got := w.noticesWith(slog.LevelWarn, msg, "apptrol.button", button); got != n {
			t.Errorf("%s: %d warnings, want %d (one per press, none per release)", button, got, n)
		}
	}
	warnings := 0
	for _, nt := range w.notices {
		if nt.Level == slog.LevelWarn {
			warnings++
		}
	}
	if warnings != 4 {
		t.Errorf("%d warnings, want 4: sliders and knobs are not reported: %v", warnings, w.notices)
	}
	// R3 is a launcher: it warns once, with what it did, not twice.
	w.press(ButtonR, 3)
	if got := w.noticesWith(slog.LevelWarn, msg, "apptrol.button", "R3"); got != 0 {
		t.Errorf("launcher R3 also warned as a plain button")
	}
}

func TestLAUNCH15_NoWarningsUnlessLocked(t *testing.T) {
	for _, state := range []ScreenState{ScreenUnlocked, ScreenUnknown} {
		w := newWorld(t, launchSetup(), State{})
		w.do(ControllerConnected{})
		w.do(AudioSnapshot{Streams: []Stream{spotify}, Devices: []Device{goxlr}})
		w.do(ScreenChanged{State: state})
		w.press(ButtonM, 1)
		w.key(Play)
		if n := w.noticesWith(slog.LevelWarn, "button pressed while the screen is locked", "apptrol.screen.state", string(state)); n != 0 {
			t.Errorf("screen %q: %d warnings for pressed buttons, want none", state, n)
		}
	}
}
