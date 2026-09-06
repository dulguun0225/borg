package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// createBuild writes the build record for one commit: the artifact digest —
// sha256 of the commit hash, this platform never producing a binary digest
// of its own, the local target running the checked-out commit directly rather
// than a built artifact of its own — and, for a Go module, its go.sum
// resolved into entries with licence "unknown", this milestone having no
// licence resolver. Criterion results of the build's own process are none:
// nothing in this command-line interface decides a criterion at that place yet.
func (p *path) createBuild(ctx context.Context, repo, itemID, serviceID, commit string) (build.Build, error) {
	sum := sha256.Sum256([]byte(commit))
	draft := build.Draft{
		ItemID:         itemID,
		ServiceID:      serviceID,
		CommitHash:     commit,
		ArtifactDigest: "sha256:" + hex.EncodeToString(sum[:]),
	}
	resolved, coverage, couldNotDerive := resolvedGoModules(repo)
	draft.Resolved = resolved
	switch {
	case couldNotDerive != "":
		draft.ResolvedSetCouldNotDerive = couldNotDerive
	case coverage != "":
		draft.ResolvedSetCoverage = map[string]string{"go": coverage}
	}
	// What the change reaches and whether it ships a schema change: two readings
	// of this checkout, taken where the repository is and recorded on the record
	// the gate rows and enforcement read them off. measure.go says why each is
	// derived here and nowhere else.
	reached, declares := reaches(ctx, repo, commit, resolved)
	draft.Exposure, draft.DeclaresSchemaChange = &reached, declares
	return p.builds.Create(ctx, buildActor, draft)
}

// resolvedGoModules reads go.sum in repo and returns one entry per module
// version it names — the ecosystem, the source, the package, the version, the
// digest go.sum itself carries, licence "unknown", and required_by the
// module go.mod names. Where go.mod cannot be read, resolution could not be
// performed at all and the reason is returned; where go.mod exists and go.sum
// does not, the module has no third-party dependency and coverage is "go"
// with no entries.
func resolvedGoModules(repo string) (entries []build.ResolvedEntry, coverage, couldNotDerive string) {
	module, err := readGoModule(repo)
	if err != nil {
		return nil, "", err.Error()
	}
	content, err := os.ReadFile(filepath.Join(repo, "go.sum"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, "go", ""
	}
	if err != nil {
		return nil, "", err.Error()
	}
	seen := map[string]bool{}
	for _, line := range lines(string(content)) {
		fields := strings.Fields(line)
		if len(fields) != 3 || strings.HasSuffix(fields[1], "/go.mod") {
			continue
		}
		key := fields[0] + "@" + fields[1]
		if seen[key] {
			continue
		}
		seen[key] = true
		entries = append(entries, build.ResolvedEntry{
			Ecosystem: "go", Source: "go.sum", Package: fields[0], Version: fields[1],
			Digest: fields[2], Licence: "unknown", RequiredBy: module,
		})
	}
	return entries, "go", ""
}

// readGoModule is the module path go.mod names, and an error where the file
// cannot be read or names none.
func readGoModule(repo string) (string, error) {
	content, err := os.ReadFile(filepath.Join(repo, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("factory: reading go.mod to resolve the build's dependencies: %w", err)
	}
	for _, line := range lines(string(content)) {
		if after, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(after), nil
		}
	}
	return "", errors.New("factory: go.mod names no module")
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
