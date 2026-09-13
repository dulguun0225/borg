package buildrunner_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/dulguun0225/borg/factory/buildrunner"
)

func TestGitCloneUsesTheSuppliedBaseAfterAPreviousDetachedBuild(t *testing.T) {
	dir, master := repository(t)
	clone := buildrunner.GitClone{}
	if _, err := clone.Select(t.Context(), buildrunner.SelectionRequest{
		Directory: dir, Branch: "old-candidate", Base: master, Mode: buildrunner.CandidateBranch, Credential: "token",
	}); err != nil {
		t.Fatalf("old candidate Select: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "old.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "old.go"}, {"-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "-m", "old candidate"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if _, err := clone.Select(t.Context(), buildrunner.SelectionRequest{
		Directory: dir, Commit: master, Mode: buildrunner.DetachedCommit, Credential: "token",
	}); err != nil {
		t.Fatalf("detached build Select: %v", err)
	}
	checkout, err := clone.Select(t.Context(), buildrunner.SelectionRequest{
		Directory: dir, Branch: "new-candidate", Base: master, Mode: buildrunner.CandidateBranch, Credential: "token",
	})
	if err != nil {
		t.Fatalf("new candidate Select: %v", err)
	}
	if checkout.Commit != master {
		t.Fatalf("new candidate commit = %s, want supplied base %s", checkout.Commit, master)
	}
}
