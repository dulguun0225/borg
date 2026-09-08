package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDesignPhrasesHoldsUpToThreeWords(t *testing.T) {
	root := t.TempDir()
	// One sentence, with no '.', '!', '?', ';', ':', or '\n' inside it, so
	// the runs below hold regardless of the segment break at its closing
	// period; a run that would otherwise cross a '.', '!', '?', ';', ':',
	// or '\n' is covered by TestDesignPhrasesBreaksAtSentenceBoundaries
	// instead, one break at a time.
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("The merge queue holds a screen state machine."), 0o644); err != nil {
		t.Fatal(err)
	}
	phrases, err := DesignPhrases(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"merge", "merge queue", "screen state machine", "state machine"} {
		if !phrases[p] {
			t.Errorf("phrases lack %q", p)
		}
	}
	if phrases["merge queue holds a"] {
		t.Error("phrases hold a four-word run")
	}
}

// TestDesignPhrasesBreaksAtSentenceBoundaries covers all six of
// segmentBreak's alternatives — '.', '!', '?', ';', ':', and a bare
// newline — each isolated from the others by a word on both sides, so no
// two breaks can mask each other: the fixture reads "The gate holds. A
// score is read; the window closes: a hold ends\nthe item waits! the run
// stops? the log grows", and each break sits between a pair of adjacent
// words that is adjacent nowhere else in the fixture.
func TestDesignPhrasesBreaksAtSentenceBoundaries(t *testing.T) {
	root := t.TempDir()
	content := "The gate holds. A score is read; the window closes: a hold ends\n" +
		"the item waits! the run stops? the log grows"
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	phrases, err := DesignPhrases(root)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		phrase string
		break_ string
		want   bool
	}{
		{"holds a score", "'.'", false},
		{"read the window", "';'", false},
		{"closes a hold", "':'", false},
		{"ends the item", "newline", false},
		{"waits the run", "'!'", false},
		{"stops the log", "'?'", false},
		{"gate holds", "", true},
		{"score is read", "", true},
		{"window closes", "", true},
		{"hold ends", "", true},
		{"item waits", "", true},
		{"run stops", "", true},
		{"log grows", "", true},
	}
	for _, c := range cases {
		if phrases[c.phrase] != c.want {
			if c.want {
				t.Errorf("phrases lack %q", c.phrase)
			} else {
				t.Errorf("phrases hold %q, a run crossing the %s segment break", c.phrase, c.break_)
			}
		}
	}
}

func TestCheckPackageNamesSplitsAName(t *testing.T) {
	phrases := map[string]bool{"merge queue": true, "gate": true, "screen state machine": true}
	findings := CheckPackageNames([]string{"mergequeue", "gate", "screenstatemachine", "widget"}, phrases)
	if len(findings) != 1 || !slices.ContainsFunc(findings, func(f string) bool {
		return f == "widget: a package named for no phrase the design uses, and not in namedOtherwise"
	}) {
		t.Errorf("findings %v, want widget alone", findings)
	}
}

func TestCheckPackageNamesRejectsFunctionWordSplits(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("the item is a score\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	phrases, err := DesignPhrases(root)
	if err != nil {
		t.Fatal(err)
	}
	findings := CheckPackageNames([]string{"theitem", "ascore", "isa", "item", "score"}, phrases)
	for _, want := range []string{"theitem", "ascore", "isa"} {
		if !slices.ContainsFunc(findings, func(f string) bool { return strings.HasPrefix(f, want+":") }) {
			t.Errorf("findings %v lack %s", findings, want)
		}
	}
	for _, accepted := range []string{"item", "score"} {
		if slices.ContainsFunc(findings, func(f string) bool { return strings.HasPrefix(f, accepted+":") }) {
			t.Errorf("findings %v wrongly reject %s", findings, accepted)
		}
	}
}

func TestCheckPackageNamesAcceptsTheAllowance(t *testing.T) {
	if got := CheckPackageNames([]string{"postgres"}, map[string]bool{}); len(got) != 0 {
		t.Errorf("postgres is in namedOtherwise and was reported: %v", got)
	}
}

func TestPackageDirsSkipsCmdAndClient(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"gate", "cmd/factory", "client/src", "clientdist", "empty"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"gate/gate.go", "cmd/factory/main.go", "clientdist/embed.go", "client/src/x.ts"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := PackageDirs(root)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"clientdist", "gate"}) {
		t.Errorf("PackageDirs = %v, want clientdist gate", got)
	}
}
