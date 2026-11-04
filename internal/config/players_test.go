package config

import (
	"slices"
	"strings"
	"testing"
)

// An output's player list (MEDIA-12, CFG-14, CFG-15, CFG-20, OUT-09,
// ADR 0045). Names are placeholders.
const playerBase = `
[apps.music]
match = ["test-music"]

[outputs.speakers]
match  = ["test-sink"]
player = [" Test-Player ", "other-player"]

[outputs.headphones]
match = ["test-headset"]

[outputs.main]
device = "default"

[inputs.mic]
match = ["test-mic"]

[layouts.default]
slider1 = "speakers"
slider2 = "headphones"
slider3 = "main"
slider4 = "music"
slider8 = "mic"
`

func TestMEDIA12_AFixedOutputMayHaveAPlayerList(t *testing.T) {
	cfg, _ := mustParse(t, playerBase)
	if got := cfg.Apps["speakers"].Player; !slices.Equal(got, []string{"Test-Player", "other-player"}) {
		t.Errorf("player = %q, want the fragments trimmed", got)
	}
	if got := cfg.Apps["headphones"].Player; got != nil {
		t.Errorf("an output without player has %q", got)
	}
	s := cfg.Setup()
	if got := s.Targets["speakers"].Player; !slices.Equal(got, []string{"Test-Player", "other-player"}) {
		t.Errorf("player in the mixer's setup = %q", got)
	}
	if got := s.Targets["music"].Player; got != nil {
		t.Errorf("an app has player %q in the mixer's setup", got)
	}
	for id, want := range map[string]bool{"music": true, "speakers": true, "headphones": false, "main": false, "mic": false} {
		if got := cfg.Apps[id].HasPlayers(); got != want {
			t.Errorf("%s.HasPlayers() = %v, want %v", id, got, want)
		}
	}
}

func TestMEDIA12_PlayerIsRejectedOnAppsInputsAndTheDefaultOutput(t *testing.T) {
	for _, tt := range []struct{ name, src, want string }{
		{"app", "[apps.target]\nmatch = [\"test\"]\nplayer = [\"test\"]\n",
			"apps.target.player: only an output has it; an app's match already finds its media player"},
		{"input", "[inputs.target]\nmatch = [\"test\"]\nplayer = [\"test\"]\n",
			"inputs.target.player: only an output has it; inputs have no media player"},
		{"old-form input", "[apps.target]\ntype = \"input\"\nmatch = [\"test\"]\nplayer = [\"test\"]\n",
			"apps.target.player: only an output has it; inputs have no media player"},
		// Owner decision 1 on #231: anything may play over the default output.
		{"default output", "[outputs.target]\ndevice = \"default\"\nplayer = [\"test\"]\n",
			"outputs.target.player: only an output with match has it; any player can play over the default output"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			requireProblem(t, problemsOf(t, tt.src+"[layouts.default]\nslider1 = \"target\"\n"), tt.want)
		})
	}

	// Neither match nor device, or a device value that is not valid: CFG-20's
	// error, and player's without the default output's reason, which would be
	// wrong here.
	for _, tt := range []struct{ selector, want string }{
		{"", "outputs.target: set exactly one of match and device"},
		{"device = \"speakers\"\n", `outputs.target.device: "speakers" is not valid`},
	} {
		problems := problemsOf(t, "[outputs.target]\n"+tt.selector+"player = [\"test\"]\n[layouts.default]\nslider1 = \"target\"\n")
		requireProblem(t, problems, tt.want)
		requireProblem(t, problems, "outputs.target.player: only an output with match has it")
		for _, p := range problems {
			if strings.Contains(p, "player:") && strings.Contains(p, "default output") {
				t.Errorf("selector %q: player got the default output's reason: %s", tt.selector, p)
			}
		}
	}
}

func TestMEDIA12_APlayerListNeedsFragments(t *testing.T) {
	for _, tt := range []struct{ name, player, want string }{
		{"empty", `[]`, "outputs.speakers.player: empty; list at least one name fragment"},
		{"blank fragment", `["test", "  "]`, "outputs.speakers.player[1]: empty fragment"},
		{"not a list", `"test"`, "player"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src := strings.Replace(playerBase, `[" Test-Player ", "other-player"]`, tt.player, 1)
			requireProblem(t, problemsOf(t, src), tt.want)
		})
	}
}

func TestCFG08_PlayerUnderALayoutGetsAHint(t *testing.T) {
	requireProblem(t, problemsOf(t, playerBase+"player = [\"test\"]\n"),
		"layouts.default.player: player is an output setting, not a control; put it under [outputs.<id>]")
}

func TestCFG14_PlayPauseOnAnOutputNeedsAPlayerList(t *testing.T) {
	// With player, R may be play_pause (also its default), off or a launcher.
	for _, setting := range []string{`mode = "play_pause"`, `mode = "off"`, `command = ["true"]`} {
		mustParse(t, playerBase+"\n[layouts.default.buttons]\nr1 = { "+setting+" }\n")
	}
	// Without player, play_pause is rejected and the error says why (OUT-09).
	requireProblem(t, problemsOf(t, playerBase+"\n[layouts.default.buttons]\nr2 = { mode = \"play_pause\" }\n"),
		`layouts.default.buttons.r2: "play_pause" needs an app, or an output with player, on slider2; slider2 holds the output "headphones", which has no player`)
	requireProblem(t, problemsOf(t, playerBase+"\n[layouts.default.buttons]\nr3 = { mode = \"play_pause\" }\n"),
		`layouts.default.buttons.r3: "play_pause" needs an app, or an output with player, on slider3; slider3 holds the output "main", which follows the default output and cannot have player`)
	requireProblem(t, problemsOf(t, playerBase+"\n[layouts.default.buttons]\nr8 = { mode = \"play_pause\" }\n"),
		`layouts.default.buttons.r8: "play_pause" needs an app, or an output with player, on slider8; slider8 holds the input "mic"`)
}

func TestCFG15_MediaPlayerMayNameAnOutputWithAPlayerList(t *testing.T) {
	cfg, _ := mustParse(t, playerBase+"\n[media]\nplayer = \"speakers\"\n")
	if cfg.Media.Player != "speakers" || cfg.Setup().MediaPlayer != "speakers" {
		t.Errorf("media.player = %q", cfg.Media.Player)
	}
	// On no control too, as for an app (MEDIA-06).
	offControl := strings.Replace(playerBase, "slider1 = \"speakers\"\n", "", 1)
	if cfg, _ := mustParse(t, offControl+"\n[media]\nplayer = \"speakers\"\n"); cfg.Media.Player != "speakers" {
		t.Errorf("media.player on no control = %q", cfg.Media.Player)
	}
	for id, want := range map[string]string{
		"headphones": `media.player: "headphones" is an output without player; add player = ["<player name>"] under [outputs.headphones], or name an app`,
		"main":       `media.player: "main" is an output that follows the default output, which cannot have player`,
		"mic":        `media.player: "mic" is an input; name an app, or an output with player`,
		"missing":    `media.player: app "missing" is not defined in [apps] or, with player, in [outputs]`,
	} {
		requireProblem(t, problemsOf(t, playerBase+"\n[media]\nplayer = \""+id+"\"\n"), want)
	}
}
