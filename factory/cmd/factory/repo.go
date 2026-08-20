package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/service"
)

// inDir runs one command in dir and returns its combined output. On an error
// the output is part of the message, because the command's own words are what
// a human fixes the failure by.
func inDir(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("factory: %s %s in %s: %w: %s",
			name, strings.Join(args, " "), dir, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// git runs one git command in repo and returns its output trimmed, which for
// rev-parse is the value asked for.
func git(repo string, args ...string) (string, error) {
	out, err := inDir(repo, "git", args...)
	return strings.TrimSpace(out), err
}

// masterHead is the commit master is at in one service's repository, and empty
// where the service has no release: the design makes master's head the commit of
// the service's highest-numbered release, so the store is what answers it and the
// repository is where master actually is.
//
// The two are compared, and a disagreement is an error naming both. That is the
// shape the reconciler exists for and nothing else in the factory looks for this
// one — a fast-forward that landed with no release minted leaves exactly it, which
// is the window the merge queue names and does not close.
func (p *path) masterHead(ctx context.Context, svc service.Service) (string, error) {
	inGit, err := masterCommit(svc.Repository)
	if err != nil {
		return "", err
	}
	if svc.ID == "" {
		if inGit != "" {
			return "", fmt.Errorf("factory: master is at %s and the factory has no service record for %s",
				inGit, svc.Name)
		}
		return "", nil
	}
	highest, found, err := release.Highest(ctx, p.d.pool, svc.ID)
	if err != nil {
		return "", err
	}
	if !found {
		if inGit != "" {
			return "", fmt.Errorf("factory: master is at %s and %s has no release record", inGit, svc.ID)
		}
		return "", nil
	}
	bl, err := build.Get(ctx, p.d.pool, highest.BuildID)
	if err != nil {
		return "", err
	}
	if inGit != bl.CommitHash {
		return "", fmt.Errorf("factory: master is at %q and release %d of %s names commit %s",
			inGit, highest.Number, svc.ID, bl.CommitHash)
	}
	return bl.CommitHash, nil
}

// masterCommit is the commit the repository's master is at, and empty where there
// is no master. git rev-parse --verify --quiet exits 1 for a ref that is not
// there, and that one code is read as absent while every other failure is
// returned — a broken repository reported here rather than a branch quietly
// committed with no base, which is what would drop the tree the items already
// merged left.
func masterCommit(repo string) (string, error) {
	out, err := git(repo, "rev-parse", "--verify", "--quiet", "refs/heads/master")
	if err == nil {
		return out, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return "", nil
	}
	return "", err
}

// compiles checks that the build compiles, with the binary written to a directory
// that is thrown away. It is what the Implementation gate would reject a build
// for, and that gate is not built — so a build that does not compile stops the run
// here, where the build record was just written, rather than one step down where a
// candidate environment would already have been composed for it.
func compiles(repo string) error {
	dir, err := os.MkdirTemp("", "borg-compile-")
	if err != nil {
		return fmt.Errorf("factory: making a directory to compile into: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	_, err = inDir(repo, "go", "build", "-o", filepath.Join(dir, "compiled"), ".")
	return err
}

// buildInto puts the build's binary in an environment's directory, named exactly
// by the build id, which is what the local target starts. A build is what runs:
// the release is the name that build has on master, and the target has no idea
// which.
func buildInto(repo, dir, buildID string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("factory: making %s: %w", dir, err)
	}
	absolute, err := filepath.Abs(filepath.Join(dir, buildID))
	if err != nil {
		return fmt.Errorf("factory: resolving where to build %s: %w", buildID, err)
	}
	_, err = inDir(repo, "go", "build", "-o", absolute, ".")
	return err
}

// runEncodings runs the encodings once and says whether they passed, with the
// output for a message. A failure is not an error here: deciding over it is the
// gate's, and what this produces is what was observed.
func runEncodings(repo string) (bool, string) {
	out, err := inDir(repo, "go", "test", "./...")
	return err == nil, strings.TrimSpace(out)
}

// firstLines is the first few lines of a command's output, for a row a human
// reads. A whole compiler or test log in a log payload is what nobody reads.
func firstLines(output string) string {
	kept := lines(output)
	if len(kept) > 4 {
		kept = kept[:4]
	}
	return strings.Join(kept, "; ")
}

// criterionIDs is the ids of a criterion set, for a message that names which
// ones the build was required to encode.
func criterionIDs(inForce []criterion.Criterion) []string {
	ids := make([]string, 0, len(inForce))
	for _, c := range inForce {
		ids = append(ids, c.ID)
	}
	return ids
}

// repoFiles is the repository's current files, whole, for the implementer's
// brief — none on a candidate whose branch has no base, and the tree master
// points at for every candidate after the first release. The .git directory is
// the repository's bookkeeping, not part of the change, and is skipped.
func repoFiles(repo string) ([]agent.File, error) {
	var files []agent.File
	err := filepath.WalkDir(repo, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(repo, path)
		if err != nil {
			return err
		}
		files = append(files, agent.File{Path: rel, Content: string(content)})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("factory: reading the repository's files: %w", err)
	}
	return files, nil
}

// copyFile copies a built binary from one environment's directory to another's,
// executable, because a local target starts exactly dir/<build>.
func copyFile(from, to string) error {
	content, err := os.ReadFile(from)
	if err != nil {
		return fmt.Errorf("factory: reading %s: %w", from, err)
	}
	if err := os.WriteFile(to, content, 0o755); err != nil {
		return fmt.Errorf("factory: writing %s: %w", to, err)
	}
	return nil
}
