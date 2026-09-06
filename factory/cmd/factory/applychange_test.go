// applyChange on a real git repository: a revert of an addition rewrites a
// file it keeps and removes a file the failed release added, and git add -A
// stages both in one commit — the shape commitAndBuild commits under. Run
// against a plain temporary repository rather than the fixtures, because
// applyChange reaches no database and the fixtures' cost buys nothing here.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/agent"
)

// TestApplyChangeWritesAndDeletes is the revert's own shape: one file
// rewritten, one file removed, and git add -A staging the removal beside the
// rewrite in the same commit.
func TestApplyChangeWritesAndDeletes(t *testing.T) {
	repo := t.TempDir()
	if _, err := git(repo, "init"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := applyChange(repo, agent.Change{Files: []agent.File{
		{Path: "keep.go", Content: "package main\n"},
		{Path: "flaky.go", Content: "package main\n"},
	}}); err != nil {
		t.Fatalf("applyChange (initial write): %v", err)
	}
	if _, err := git(repo, "add", "-A"); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if _, err := git(repo, "-c", "user.name=t", "-c", "user.email=t@t.invalid",
		"commit", "-m", "initial"); err != nil {
		t.Fatalf("git commit: %v", err)
	}

	// The revert: rewrite keep.go and delete flaky.go, the way a reply with
	// one FILE block and one DELETE line does.
	if err := applyChange(repo, agent.Change{
		Files:   []agent.File{{Path: "keep.go", Content: "package main\n\n// reverted\n"}},
		Deleted: []string{"flaky.go"},
	}); err != nil {
		t.Fatalf("applyChange (revert): %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "flaky.go")); !os.IsNotExist(err) {
		t.Fatalf("flaky.go still exists after applyChange removed it, stat error = %v", err)
	}

	if _, err := git(repo, "add", "-A"); err != nil {
		t.Fatalf("git add: %v", err)
	}
	status, err := git(repo, "status", "--porcelain")
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	if !strings.Contains(status, "D  flaky.go") {
		t.Errorf("git status --porcelain = %q, want flaky.go staged as deleted", status)
	}
	if !strings.Contains(status, "M  keep.go") {
		t.Errorf("git status --porcelain = %q, want keep.go staged as modified", status)
	}
	if _, err := git(repo, "-c", "user.name=t", "-c", "user.email=t@t.invalid",
		"commit", "-m", "revert"); err != nil {
		t.Fatalf("git commit: %v", err)
	}
	tracked, err := git(repo, "ls-tree", "-r", "--name-only", "HEAD")
	if err != nil {
		t.Fatalf("git ls-tree: %v", err)
	}
	if strings.Contains(tracked, "flaky.go") {
		t.Errorf("HEAD still tracks flaky.go after the revert commit: %q", tracked)
	}
}

// TestApplyChangeDeletingAnAbsentPathIsNotAnError: the removal already
// holds where the path is already gone, which is the state a revert asks
// for.
func TestApplyChangeDeletingAnAbsentPathIsNotAnError(t *testing.T) {
	repo := t.TempDir()
	if err := applyChange(repo, agent.Change{Deleted: []string{"never-existed.go"}}); err != nil {
		t.Fatalf("applyChange: %v", err)
	}
}

// TestApplyChangeRefusesADeletedPathThatLeavesTheRepository: a deleted path
// is guarded the same way a written path is.
func TestApplyChangeRefusesADeletedPathThatLeavesTheRepository(t *testing.T) {
	repo := t.TempDir()
	err := applyChange(repo, agent.Change{Deleted: []string{"../outside.go"}})
	if err == nil {
		t.Fatal("applyChange accepted a deleted path leaving the repository")
	}
}
