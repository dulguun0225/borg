package buildrunner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/dulguun0225/borg/factory/build"
)

type mutantResolver struct{}

func (mutantResolver) Resolve(context.Context, Checkout, string, []string) (Resolution, error) {
	return Resolution{Coverage: []build.Coverage{{Ecosystem: "go", Digests: true, FetchWithoutRunning: true}}}, nil
}

type mutantProcess struct{ calls int }

func (p *mutantProcess) Run(_ context.Context, input ProcessInput) (ProcessOutput, error) {
	p.calls++
	if err := os.WriteFile(input.Output, []byte("artifact"), 0o755); err != nil {
		return ProcessOutput{}, err
	}
	return ProcessOutput{ArtifactDigest: "sha256:artifact"}, nil
}

func TestCompileMutantsUsesOneBoundedOperatorPerTouchedLineAndNoRecord(t *testing.T) {
	dir := t.TempDir()
	runMutationGit(t, dir, "init")
	runMutationGit(t, dir, "config", "user.email", "test@example.com")
	runMutationGit(t, dir, "config", "user.name", "test")
	initial := []byte("package main\n\nfunc value() string {\n\tif true {\n\t\treturn \"before\"\n\t}\n\treturn \"old\"\n}\n")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), initial, 0o644); err != nil {
		t.Fatal(err)
	}
	runMutationGit(t, dir, "add", "main.go")
	runMutationGit(t, dir, "commit", "-m", "initial")
	base := mutationGitOutput(t, dir, "rev-parse", "HEAD")
	changed := []byte("package main\n\nfunc value() string {\n\tif false {\n\t\treturn \"before\"\n\t}\n\treturn \"changed\"\n}\n")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), changed, 0o644); err != nil {
		t.Fatal(err)
	}
	runMutationGit(t, dir, "add", "main.go")
	runMutationGit(t, dir, "commit", "-m", "change")
	commit := mutationGitOutput(t, dir, "rev-parse", "HEAD")
	process := &mutantProcess{}
	runner := &Runner{resolver: mutantResolver{}, process: process, shippedBundle: "bundle-test"}
	artifacts, err := runner.CompileMutants(t.Context(), MutantRequest{
		Checkout: Checkout{Directory: dir, Base: base, Commit: commit}, Cap: 2,
		OutputDirectory: filepath.Join(t.TempDir(), "artifacts"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 2 || process.calls != 2 {
		t.Fatalf("artifacts = %+v, process calls = %d, want two transient compilations", artifacts, process.calls)
	}
	if artifacts[0].Operator != "negate condition" || artifacts[1].Operator != "replace literal" {
		t.Fatalf("operators = %q, %q, want condition and literal operators", artifacts[0].Operator, artifacts[1].Operator)
	}
	got, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil || string(got) != string(changed) {
		t.Fatalf("checkout changed after compilation: %q, %v", got, err)
	}
}

func TestCoverageReasonsStayWithTheirRowAndDigestlessResolutionStaysVisible(t *testing.T) {
	coverage := coverageWithReason(Resolution{
		Coverage: []build.Coverage{
			{Ecosystem: "go", FetchWithoutRunning: false, Digests: true},
			{Ecosystem: "go", FetchWithoutRunning: true, Digests: false},
		},
	})
	if coverage[0].FetchWithoutRunningReason == "" || coverage[0].MissingDigests != "" ||
		coverage[1].MissingDigests == "" || coverage[1].FetchWithoutRunningReason != "" {
		t.Fatalf("coverage reasons = %+v, want each row's own reason", coverage)
	}
	coverage = coverageWithReason(Resolution{Entries: []build.ResolvedEntry{{Package: "x"}}})
	if len(coverage) != 1 || coverage[0].MissingDigests == "" {
		t.Fatalf("digestless empty coverage = %+v, want retained reason", coverage)
	}
}

func runMutationGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func mutationGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(output[:len(output)-1])
}
