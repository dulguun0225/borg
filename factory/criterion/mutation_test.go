package criterion_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/criterion"
)

// TestDeriveMutationCouldNotDeriveWhereNoExtractorCoversTheBuild: Go is the one
// toolchain with an extractor, recognised by a go.mod at the root, and a build
// in any other reads could not derive rather than a score of zero.
func TestDeriveMutationCouldNotDeriveWhereNoExtractorCoversTheBuild(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\n"), 0o644); err != nil {
		t.Fatalf("writing Cargo.toml: %v", err)
	}

	m, err := criterion.DeriveMutation(t.Context(), dir)
	if err != nil {
		t.Fatalf("DeriveMutation: %v", err)
	}
	if m.Derived() || m.Toolchain != "" {
		t.Errorf("a build no extractor covers derived %+v, want could not derive with no toolchain", m)
	}
	if !strings.Contains(m.CouldNotDerive, "go.mod") {
		t.Errorf("the reason is %q, want it to name what the extractor looks for", m.CouldNotDerive)
	}
	if !m.Blocks(0.5) {
		t.Error("a build the factory cannot mutate passed the floor, and could not derive never passes")
	}
}

// TestDeriveMutationCouldNotDeriveWhereTheCheckoutNamesNoTool: a service the
// factory cannot mutate reads could not derive, and the coverage of the run
// the mutants would have been re-run under is read either way.
func TestDeriveMutationCouldNotDeriveWhereTheCheckoutNamesNoTool(t *testing.T) {
	dir := mutable(t, "module example\n\ngo 1.24\n")

	m, err := criterion.DeriveMutation(t.Context(), dir)
	if err != nil {
		t.Fatalf("DeriveMutation: %v", err)
	}
	if m.Derived() {
		t.Fatalf("a checkout naming no mutation tool derived a score: %+v", m)
	}
	if m.Toolchain != criterion.MutationToolchain || m.Tool != "" {
		t.Errorf("the derivation ran %q with tool %q, want the go extractor and no tool", m.Toolchain, m.Tool)
	}
	if !strings.Contains(m.CouldNotDerive, "tool directive") {
		t.Errorf("the reason is %q, want it to name what the checkout does not name", m.CouldNotDerive)
	}
	if !strings.Contains(m.Coverage, "go test -cover") || !strings.Contains(m.Coverage, "%") {
		t.Errorf("the coverage is %q, want what go test -cover reported over the checkout", m.Coverage)
	}
	if m.MutantsTested != 0 || m.MutantsDetected != 0 || m.Score() != 0 {
		t.Errorf("a derivation that could not be made counted %+v, want nothing", m)
	}
}

// TestDeriveMutationCouldNotDeriveWhereTheToolWillNotRun: the tool a checkout
// names is what mutates it, and one that does not run is could not derive with
// the tool's own words, never a score.
func TestDeriveMutationCouldNotDeriveWhereTheToolWillNotRun(t *testing.T) {
	dir := mutable(t, "module example\n\ngo 1.24\n\ntool example.test/absent/mutate\n")

	m, err := criterion.DeriveMutation(t.Context(), dir)
	if err != nil {
		t.Fatalf("DeriveMutation: %v", err)
	}
	if m.Derived() {
		t.Fatalf("a tool that does not run derived a score: %+v", m)
	}
	if m.Tool != "mutate" {
		t.Errorf("the tool is %q, want the name go tool runs it under, mutate", m.Tool)
	}
	if !strings.Contains(m.CouldNotDerive, "mutate") {
		t.Errorf("the reason is %q, want it to name the tool", m.CouldNotDerive)
	}
}

// TestTheMutationScoreIsDerivedFromTheTwoCounts: the score is the share of the
// mutants the encodings detected, derived at the read, and the floor rejects a
// score below it on the terms an undecided criterion is rejected on.
func TestTheMutationScoreIsDerivedFromTheTwoCounts(t *testing.T) {
	caught := criterion.Mutation{Toolchain: "go", Tool: "mutate", MutantsTested: 8, MutantsDetected: 6}
	if caught.Score() != 0.75 {
		t.Errorf("6 of 8 mutants detected is a score of %v, want 0.75", caught.Score())
	}
	if caught.Blocks(0.5) {
		t.Error("a score above the floor rejected")
	}
	if !caught.Blocks(0.8) {
		t.Error("a score below the floor passed")
	}

	// Caught none is a reading of zero, and it is not could not derive.
	missed := criterion.Mutation{Toolchain: "go", Tool: "mutate", MutantsTested: 8}
	if !missed.Derived() || missed.Score() != 0 || !missed.Blocks(0.1) {
		t.Errorf("a run that caught no mutant reads %+v, want a derived score of zero that blocks", missed)
	}
}

// mutable is a temp directory shaped like a checkout of a Go build with one
// package and one test in it, so go test -cover has something to report, and
// the go.mod given at its root.
func mutable(t *testing.T, goMod string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":       goMod,
		"shop.go":      "package shop\n\nfunc Charge(n int) int { return n }\n",
		"shop_test.go": "package shop\n\nimport \"testing\"\n\nfunc TestCharge(t *testing.T) {\n\tif Charge(1) != 1 {\n\t\tt.Fail()\n\t}\n}\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	return dir
}
