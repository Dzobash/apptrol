// Package packaging has no code; it holds the files that go into the .deb and .rpm
// (ADR 0013) and the tests that keep them consistent with the rest of the repository.
package packaging

import (
	"os"
	"strings"
	"testing"
)

// mitTextFromCopyright returns the license text of the standalone "License: MIT"
// paragraph of a DEP-5 copyright file, the way DEP-5 defines it: every line of the
// text starts with one space, and a line with only " ." stands for an empty line.
func mitTextFromCopyright(t *testing.T, copyright string) string {
	t.Helper()
	// A DEP-5 file is paragraphs separated by empty lines. The "Files: *" paragraph
	// also contains the line "License: MIT" (its short name), so the standalone
	// stanza is the paragraph that *starts* with it.
	for _, paragraph := range strings.Split(strings.TrimSpace(copyright), "\n\n") {
		lines := strings.Split(paragraph, "\n")
		if lines[0] != "License: MIT" {
			continue
		}
		text := make([]string, 0, len(lines)-1)
		for _, line := range lines[1:] {
			switch {
			case line == " .":
				text = append(text, "")
			case strings.HasPrefix(line, " "):
				text = append(text, line[1:])
			default:
				t.Fatalf("copyright: line %q in the MIT paragraph does not start with a space (DEP-5 continuation line)", line)
			}
		}
		return strings.Join(text, "\n")
	}
	t.Fatal(`copyright: no standalone "License: MIT" paragraph found`)
	return ""
}

// TestNFR09_DebianCopyrightHasTheLicenseText keeps the MIT text in the .deb's
// DEP-5 copyright file equal to LICENSE (NFR-09, #146). The two are separate files
// because DEP-5 needs its own line format; without this test, changing one of them
// (a new year, a new holder) would leave the package carrying a different license text
// than the repository, and nothing would notice until someone read the installed file.
func TestNFR09_DebianCopyrightHasTheLicenseText(t *testing.T) {
	copyright, err := os.ReadFile("debian/copyright")
	if err != nil {
		t.Fatal(err)
	}
	license, err := os.ReadFile("../LICENSE")
	if err != nil {
		t.Fatal(err)
	}

	got := mitTextFromCopyright(t, string(copyright))
	// Only a trailing newline may differ: DEP-5 text cannot end with an empty line.
	want := strings.TrimRight(string(license), "\n")
	if got != want {
		t.Errorf("the MIT text in packaging/debian/copyright differs from LICENSE (NFR-09);\n--- copyright (DEP-5 markup removed):\n%s\n--- LICENSE:\n%s", got, want)
	}
}

// TestNFR09_MITTextParsing checks the DEP-5 reading itself, so a mistake in it cannot
// make the comparison above pass by accident (NFR-09).
func TestNFR09_MITTextParsing(t *testing.T) {
	const file = "Files: *\nLicense: MIT\n\nLicense: MIT\n Title\n .\n Body line\n"
	if got, want := mitTextFromCopyright(t, file), "Title\n\nBody line"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
