package config

import (
	"strings"
	"testing"

	"github.com/Dzobash/apptrol/internal/mixer"
)

func TestCFG20_InputsAcceptEitherSelector(t *testing.T) {
	for _, selector := range []string{`device = "default"`, `match = ["Test mic"]`} {
		cfg, _ := mustParse(t, "[inputs.mic]\n"+selector+"\n[layouts.default]\nslider8 = \"mic\"\n")
		target := cfg.Setup().Targets["mic"]
		if target.Kind != mixer.Input || target.FollowsDefault != strings.HasPrefix(selector, "device") {
			t.Fatalf("input setup = %+v", target)
		}
	}
}

func TestCFG20_InputsRejectInvalidSelectors(t *testing.T) {
	for _, tc := range []struct{ selector, want string }{
		{"", "exactly one of match and device"},
		{"match = []\ndevice = \"default\"", "exactly one of match and device"},
		{"match = [\"mic\"]\ndevice = \"default\"", "exactly one of match and device"},
		{`match = []`, "match: empty"},
		{`match = [" "]`, "empty fragment"},
		{`device = ""`, "device:"},
		{`device = "Default"`, "device:"},
		{`device = " default "`, "device:"},
		{`device = "mic"`, "device:"},
		{`device = true`, "device"},
		{"device = \"default\"\nplayer = [\"test-player\"]", "inputs.mic.player: only an output has it; inputs have no media player"},
	} {
		requireProblem(t, problemsOf(t, "[inputs.mic]\n"+tc.selector+"\n[layouts.default]\nslider8 = \"mic\"\n"), tc.want)
	}
}
