package version

import (
	"strings"
	"testing"
)

func TestStringUsesLinkTimeValues(t *testing.T) {
	oldV, oldC, oldD := Version, Commit, Date
	t.Cleanup(func() { Version, Commit, Date = oldV, oldC, oldD })

	Version, Commit, Date = "v1.2.3", "0123456789abcdef", "2026-10-01T12:00:00Z"
	got := String()

	for _, want := range []string{"apptrol v1.2.3", "commit 0123456", "built 2026-10-01T12:00:00Z"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, want it to contain %q", got, want)
		}
	}
}

func TestStringDefaults(t *testing.T) {
	got := String()
	if !strings.HasPrefix(got, "apptrol ") {
		t.Errorf("String() = %q, want prefix %q", got, "apptrol ")
	}
}
