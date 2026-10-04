package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Dzobash/apptrol/internal/mixer"
)

// buttonBase: Spotify on slider1, the microphone on slider8; tests append a
// [layouts.default.buttons] table.
const buttonBase = `
[apps.spotify]
match = ["spotify"]

[apps.mic]
type  = "input"
match = ["goxlr"]

[layouts.default]
slider1 = "spotify"
slider8 = "mic"
`

func withButtons(buttons string) string {
	return buttonBase + "\n[layouts.default.buttons]\n" + buttons
}

func TestCFG13_ButtonsAreParsed(t *testing.T) {
	cfg, _ := mustParse(t, withButtons(`
record      = { app = "com.obsproject.Studio" }
marker_set  = { app = "discord", if_running = "skip" }
marker_prev = { command = ["konsole", "-e", "htop"] }
r1          = { mode = "play_pause" }
r2          = { app = "discord" }
r8          = { mode = "off" }
m8          = { mode = "hold_to_talk", talk_over = true }
s8          = { mode = "talk_over" }
`))
	got := cfg.Layout().Buttons
	want := map[string]Button{
		"record":      {Name: "record", App: "com.obsproject.Studio", IfRunning: IfRunningStart},
		"marker_set":  {Name: "marker_set", App: "discord", IfRunning: IfRunningSkip},
		"marker_prev": {Name: "marker_prev", Command: []string{"konsole", "-e", "htop"}, IfRunning: IfRunningStart},
		"r1":          {Name: "r1", Mode: ModePlayPause, IfRunning: IfRunningStart},
		"r2":          {Name: "r2", App: "discord", IfRunning: IfRunningStart},
		"r8":          {Name: "r8", Mode: ModeOff, IfRunning: IfRunningStart},
		"m8":          {Name: "m8", Mode: ModeHoldToTalk, TalkOver: true, IfRunning: IfRunningStart},
		"s8":          {Name: "s8", Mode: ModeTalkOver, IfRunning: IfRunningStart},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buttons =\n%+v\nwant\n%+v", got, want)
	}
	if !got["r2"].Launcher() || got["r1"].Launcher() {
		t.Error("Launcher() is wrong")
	}
}

func TestCFG13_SetupPassesColumnButtons(t *testing.T) {
	cfg, _ := mustParse(t, withButtons(`
r1 = { mode = "off" }
r2 = { app = "discord" }
m8 = { mode = "hold_to_talk", talk_over = true }
s8 = { mode = "off" }
record = { app = "com.obsproject.Studio" }
`))
	got := cfg.Setup().Buttons
	want := map[mixer.LED]mixer.Button{
		{Button: mixer.ButtonR, Column: 1}: {Mode: mixer.ModeOff},
		{Button: mixer.ButtonR, Column: 2}: {Mode: mixer.ModeLauncher},
		{Button: mixer.ButtonM, Column: 8}: {Mode: mixer.ModeHoldToTalk, TalkOver: true},
		{Button: mixer.ButtonS, Column: 8}: {Mode: mixer.ModeOff},
	} // record is no column button
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Setup().Buttons = %v, want %v", got, want)
	}
}

func TestCFG13_ButtonsWithoutTableAreFine(t *testing.T) {
	cfg, _ := mustParse(t, buttonBase)
	if len(cfg.Layout().Buttons) != 0 {
		t.Errorf("buttons = %v", cfg.Layout().Buttons)
	}
}

func TestCFG14_ButtonProblems(t *testing.T) {
	for _, tc := range []struct{ buttons, want string }{
		{`r1 = { app = "a", command = ["b"] }`, "buttons.r1: set either app or command, not both"},
		{`r1 = { }`, "buttons.r1: set app, command or mode"},
		{`r1 = { app = "a", mode = "off" }`, "buttons.r1: set either a launcher (app or command) or a mode"},
		{`r9 = { mode = "off" }`, "buttons.r9: unknown button"},
		{`play = { app = "a" }`, "buttons.play: unknown button"},
		{`r1 = { mode = "pause" }`, `buttons.r1.mode: "pause" is not valid here`},
		{`r1 = { app = "a", if_running = "never" }`, `buttons.r1.if_running: "never" is not valid`},
		{`r1 = { mode = "off", if_running = "skip" }`, "buttons.r1.if_running: only a launcher"},
		{`r1 = { mode = "off", when_locked = true }`, "buttons.r1.when_locked: only a launcher"},
		{`r1 = { app = "a", when_locked = "yes" }`, "layouts.default.buttons.r1.when_locked: expected boolean, found text"},
		{`r1 = { command = [] }`, "buttons.r1.command: give the program"},
		{`r8 = { mode = "play_pause" }`, `buttons.r8: "play_pause" needs an app on slider8; slider8 holds the input "mic"`},
		{`r5 = { mode = "play_pause" }`, `buttons.r5: "play_pause" needs an app on slider5; nothing is assigned there`},
		{`m1 = { mode = "hold_to_talk" }`, `buttons.m1: needs an input on slider1; slider1 holds the app "spotify"`},
		{`s5 = { mode = "cough" }`, "buttons.s5: needs an input on slider5; nothing is assigned there"},
		{`m8 = { mode = "cough" }`, `buttons.m8.mode: "cough" is not valid (use "mute" or "hold_to_talk")`},
		{`s8 = { mode = "mute" }`, `buttons.s8.mode: "mute" is not valid`},
		{`s8 = { app = "a" }`, "buttons.s8: this button cannot start apps"},
		{`s8 = { mode = "talk_over", talk_over = true }`, "buttons.s8.talk_over: only an M button has it"},
		{`record = { mode = "off" }`, "buttons.record: this button only starts apps"},
		{"m8 = { mode = \"hold_to_talk\" }\ns8 = { mode = \"cough\" }", `buttons.s8: "cough" has no use with "hold_to_talk" on m8: release M8 to mute`},
		{`m8 = { mode = "mute", volume = 3 }`, "buttons.m8.volume: unknown setting"},
		{`m8 = "mute"`, "layouts.default.buttons.m8: expected a table in braces"},
		{`m8 = { mode = 3 }`, "layouts.default.buttons.m8.mode: expected text in quotes, found a number"},
	} {
		t.Run(tc.buttons, func(t *testing.T) {
			requireProblem(t, problemsOf(t, withButtons(tc.buttons)), tc.want)
		})
	}
}

func TestLAUNCH14_WhenLocked(t *testing.T) {
	cfg, warnings := mustParse(t, withButtons(`
record     = { command = ["lights-off"], when_locked = true }
marker_set = { app = "discord", when_locked = false }
r2         = { app = "discord" }
`))
	l := cfg.Setup().Launchers
	if !l[mixer.LED{Transport: mixer.Record}].WhenLocked {
		t.Error("record: when_locked = true did not reach the mixer")
	}
	if l[mixer.LED{Transport: mixer.MarkerSet}].WhenLocked || l[mixer.LED{Button: mixer.ButtonR, Column: 2}].WhenLocked {
		t.Error("when_locked is on without being set to true")
	}
	// A warning on every load, for each launcher allowed while locked.
	var found []string
	for _, w := range warnings {
		if strings.Contains(w, "when_locked") {
			found = append(found, w)
		}
	}
	want := "layouts.default.buttons.record: when_locked = true: this button starts its app also while the screen is locked, for anyone at the controller"
	if len(found) != 1 || found[0] != want {
		t.Errorf("when_locked warnings = %q, want only %q", found, want)
	}
}

func TestCFG14_EveryButtonProblemIsReported(t *testing.T) {
	problems := problemsOf(t, withButtons(`
r9 = { mode = "off" }
m1 = { mode = "mute" }
s8 = { app = "a" }
`))
	if len(problems) != 3 {
		t.Errorf("want 3 problems, got %d:\n%s", len(problems), strings.Join(problems, "\n"))
	}
}

func TestCFG15_MediaPlayer(t *testing.T) {
	cfg, _ := mustParse(t, buttonBase+"\n[media]\nplayer = \"spotify\"\n")
	if cfg.Media.Player != "spotify" {
		t.Errorf("media.player = %q", cfg.Media.Player)
	}
	requireProblem(t, problemsOf(t, buttonBase+"\n[media]\nplayer = \"vlc\"\n"), `media.player: app "vlc" is not defined`)
	requireProblem(t, problemsOf(t, buttonBase+"\n[media]\nplayer = \"mic\"\n"), `media.player: "mic" is an input`)
}

func TestCFG16_TalkOverVolume(t *testing.T) {
	cfg, _ := mustParse(t, buttonBase)
	if v := cfg.Apps["mic"].TalkOverVolume; v != DefaultTalkOverVolume {
		t.Errorf("default talk_over_volume = %d", v)
	}
	src := strings.Replace(buttonBase, `match = ["goxlr"]`, "match = [\"goxlr\"]\ntalk_over_volume = 0", 1)
	cfg, _ = mustParse(t, src)
	if v := cfg.Apps["mic"].TalkOverVolume; v != 0 {
		t.Errorf("talk_over_volume = %d, want 0 (silence is allowed)", v)
	}
	if v := cfg.Setup().Targets["mic"].TalkOverVolume; v != 0 {
		t.Errorf("Setup talk-over volume = %d", v)
	}
	for _, tc := range []struct{ src, want string }{
		{strings.Replace(buttonBase, `match = ["goxlr"]`, "match = [\"goxlr\"]\ntalk_over_volume = 101", 1), "apps.mic.talk_over_volume: 101 is out of range"},
		{strings.Replace(buttonBase, `match = ["spotify"]`, "match = [\"spotify\"]\ntalk_over_volume = 20", 1), "apps.spotify.talk_over_volume: only an input"},
	} {
		requireProblem(t, problemsOf(t, tc.src), tc.want)
	}
}

func TestLAUNCH10_LauncherApps(t *testing.T) {
	cfg, _ := mustParse(t, withButtons(`
record      = { app = "com.obsproject.Studio" }
marker_prev = { command = ["konsole"] }
r1          = { app = "discord" }
m8          = { mode = "mute" }
`))
	want := map[string]map[string]string{"default": {"record": "com.obsproject.Studio", "r1": "discord"}}
	if got := cfg.LauncherApps(); !reflect.DeepEqual(got, want) {
		t.Errorf("LauncherApps = %v, want %v (commands are not desktop IDs)", got, want)
	}
}
