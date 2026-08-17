package main

import (
	"os"
	"slices"
	"strings"
	"testing"
)

const sample = `# a comment
record
decisionlog -> record   # a trailing comment

postgres -> decisionlog record
test decisionlog -> postgres
`

func parse(t *testing.T, text string) *Rules {
	t.Helper()
	rules, err := ParseRules(strings.NewReader(text))
	if err != nil {
		t.Fatalf("ParseRules: %v", err)
	}
	return rules
}

func TestCheckAcceptsATreeThatAgrees(t *testing.T) {
	found := Check(parse(t, sample), []Package{
		{Path: "record"},
		{Path: "decisionlog", Imports: []string{"record"}, TestImports: []string{"record", "postgres"}},
		{Path: "postgres", Imports: []string{"decisionlog"}},
	})
	if len(found) != 0 {
		t.Fatalf("Check found %v, want nothing", found)
	}
}

func TestCheckFindsTheEdgesTheFileDoesNotAllow(t *testing.T) {
	cases := map[string]struct {
		packages []Package
		want     string
	}{
		"an import that is not allowed": {
			packages: []Package{{Path: "record", Imports: []string{"decisionlog"}}},
			want:     "record imports decisionlog, which deps.txt does not allow",
		},
		"a test import that is not allowed": {
			packages: []Package{{Path: "record", TestImports: []string{"postgres"}}},
			want:     "the tests of record import postgres, which deps.txt does not allow",
		},
		"a test edge used outside a test": {
			packages: []Package{{Path: "decisionlog", Imports: []string{"postgres"}}},
			want:     "decisionlog imports postgres, which deps.txt does not allow",
		},
		"a package the file does not list": {
			packages: []Package{{Path: "targetseam"}},
			want:     "targetseam is not in deps.txt",
		},
		"a line naming no package": {
			packages: []Package{{Path: "record"}},
			want:     "deps.txt lists decisionlog, which is not a package of this module",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			found := Check(parse(t, sample), c.packages)
			if !slices.Contains(found, c.want) {
				t.Fatalf("Check found %v, want it to contain %q", found, c.want)
			}
		})
	}
}

func TestParseRulesRefusesAMalformedFile(t *testing.T) {
	cases := map[string]string{
		"no arrow":            "record decisionlog\n",
		"arrow and nothing":   "record ->\n",
		"listed twice":        "record\nrecord\n",
		"test with no line":   "test record -> postgres\n",
		"test naming nothing": "record\ntest\n",
		"test allowing none":  "record\ntest record\n",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseRules(strings.NewReader(text)); err == nil {
				t.Fatal("ParseRules accepted it, want an error")
			}
		})
	}
}

// TestTheRepositoryFileParses reads the file the build actually checks
// against, so a typo in it fails here rather than only where depscheck runs.
func TestTheRepositoryFileParses(t *testing.T) {
	file, err := os.Open("../../" + rulesFile)
	if err != nil {
		t.Fatalf("opening the rules: %v", err)
	}
	defer file.Close()
	if _, err := ParseRules(file); err != nil {
		t.Fatalf("ParseRules: %v", err)
	}
}
