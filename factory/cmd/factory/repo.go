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
	"github.com/dulguun0225/borg/factory/buildrunner"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/secretref"
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
// shape the drift detector exists for and nothing else in the factory
// looks for this one — a fast-forward that landed with no release minted leaves
// exactly it, which is the window the merge queue names and does not close.
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
	// A repository the factory has not created yet reads as no master, which
	// is what a service before its first item's implementation is: the
	// directory is made by the stage that commits the first candidate branch,
	// and every reading before that — a start's own master read among them —
	// asks about a service that has merged nothing.
	if _, err := os.Stat(repo); errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
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

var ErrDoesNotCompile = buildrunner.ErrDoesNotCompile

type buildRecordWriter struct {
	writer *build.Writer
	actor  record.Actor
}

type factorySecrets struct{ resolver *secretref.Resolver }

func (s factorySecrets) Resolve(ctx context.Context, name string) (string, error) {
	if s.resolver == nil {
		return "", fmt.Errorf("factory: no secrets resolver for repository credential %q", name)
	}
	ref, err := secretref.New(name)
	if err != nil {
		return "", err
	}
	return s.resolver.Resolve(principal.OfComponent("buildrunner"), ref)
}

func (w buildRecordWriter) Create(ctx context.Context, draft build.Draft) (build.Build, error) {
	return w.writer.Create(ctx, w.actor, draft)
}

func (w buildRecordWriter) Complete(ctx context.Context, id string, completion build.Completion) (build.Build, error) {
	completion.BuildID = id
	completion.Actor = w.actor
	return w.writer.Complete(ctx, completion)
}

func criterionIDs(inForce []criterion.Criterion) []string {
	ids := make([]string, 0, len(inForce))
	for _, c := range inForce {
		ids = append(ids, c.ID)
	}
	return ids
}

func (p *path) buildInto(ctx context.Context, repo, dir, buildID, serviceID string) (build.Build, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return build.Build{}, fmt.Errorf("factory: making %s: %w", dir, err)
	}
	absolute, err := filepath.Abs(filepath.Join(dir, ".borg-artifact"))
	if err != nil {
		return build.Build{}, fmt.Errorf("factory: resolving where to build %s: %w", buildID, err)
	}
	commit, err := git(repo, "rev-parse", "HEAD")
	if err != nil {
		return build.Build{}, err
	}
	prior, err := build.Get(ctx, p.d.pool, buildID)
	if err != nil {
		return build.Build{}, err
	}
	result, err := p.createBuildResult(ctx, repo, "", prior.ItemID, serviceID, commit, absolute)
	if err != nil {
		return build.Build{}, err
	}
	if err := os.Rename(result.ArtifactPath, filepath.Join(dir, result.Build.ID)); err != nil {
		return build.Build{}, fmt.Errorf("factory: naming artifact %s: %w", result.Build.ID, err)
	}
	return result.Build, nil
}

func (p *path) createBuild(ctx context.Context, repo, branch, itemID, serviceID, commit string) (build.Build, error) {
	result, err := p.createBuildResult(ctx, repo, branch, itemID, serviceID, commit, "")
	if err != nil {
		return build.Build{}, err
	}
	return result.Build, nil
}

func (p *path) createBuildResult(ctx context.Context, repo, branch, itemID, serviceID, commit, output string) (buildrunner.Result, error) {
	base, err := masterCommit(repo)
	if err != nil {
		return buildrunner.Result{}, err
	}
	mode := buildrunner.CandidateBranch
	if branch == "" {
		mode = buildrunner.DetachedCommit
	}
	request := buildrunner.Request{
		Selection: buildrunner.SelectionRequest{
			Directory: repo, Branch: branch, Commit: commit, Base: base, Mode: mode,
			CredentialName: serviceCredentialName(p, serviceID),
		},
		ItemID: itemID, ServiceID: serviceID,
		CurrentRelease: p.currentReleaseResolved(ctx, serviceID),
		Output:         output,
	}
	if itemID == "" {
		if origin, found := p.currentReleaseBuild(ctx, serviceID); found {
			request.SearchOrigin = &buildrunner.SearchOrigin{BuildID: origin.ID, DesignSystemConstraintID: origin.DesignSystemConstraintID}
		}
	}
	result, err := p.runner.Build(ctx, request)
	if err != nil {
		return buildrunner.Result{}, err
	}
	return result, nil
}

func serviceCredentialName(p *path, serviceID string) string {
	for _, svc := range p.serviceByID {
		if svc.ID == serviceID {
			return svc.Provisioned.BranchCredential.Name()
		}
	}
	return ""
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

// repoFiles is the repository's current files, whole, for the implementer's
// role prompt — none on a candidate whose branch has no base, and the tree master
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
