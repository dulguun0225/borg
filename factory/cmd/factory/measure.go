package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/exposure"
	"github.com/dulguun0225/borg/factory/score"
)

// emptyTree is git's hash of the empty tree, which is the same in every
// repository. It is what a first item's diff is taken against: the candidate
// branch has no base, so the change is every line of it, and diffing against
// nothing is how that is stated to git.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// measure takes the build's diff where the repository is. The score cannot read
// a repository and must not re-take a diff later — a diff re-taken against a
// master other items have merged into is not the diff the decision was made on —
// so this runs once, at the firing, and what it produces is handed to the score
// and never stored. The vector computed from it is the record.
//
// A git command that fails leaves the measurement unavailable with the reason on
// it, which the published formula turns into a human at the gate. That is the
// direction the design fixes: the gate a failure would remove is the gate that
// failure is evidence for needing.
func measure(repo string, masterExists bool) score.Measurement {
	base := emptyTree
	if masterExists {
		base = "master"
	}

	numstat, err := git(repo, "diff", "--numstat", base, "HEAD")
	if err != nil {
		return score.Measurement{Unavailable: fmt.Sprintf("the diff against %s could not be taken: %v", base, err)}
	}
	files, err := git(repo, "ls-tree", "-r", "--name-only", "HEAD")
	if err != nil {
		return score.Measurement{Unavailable: fmt.Sprintf("the build's tree could not be listed: %v", err)}
	}

	m := score.Measurement{FilesInTree: len(lines(files))}
	for _, line := range lines(numstat) {
		added, removed, path, ok := numstatLine(line)
		if !ok {
			// A binary file's numstat line carries dashes where the counts
			// belong. It is a file changed and no lines changed, which is what
			// git says about it and all the score can read.
			m.FilesChanged++
			continue
		}
		_ = path
		m.FilesChanged++
		m.LinesChanged += added + removed
	}
	return m
}

// numstatLine reads one line of git diff --numstat: lines added, lines removed,
// and the path, tab separated. It is not ok for a line whose counts are dashes,
// which is how git reports a binary file.
func numstatLine(line string) (added, removed int, path string, ok bool) {
	parts := strings.SplitN(line, "\t", 3)
	if len(parts) != 3 {
		return 0, 0, "", false
	}
	added, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, parts[2], false
	}
	removed, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, parts[2], false
	}
	return added, removed, parts[2], true
}

// lines is the non-empty lines of a command's output.
func lines(output string) []string {
	var kept []string
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) != "" {
			kept = append(kept, line)
		}
	}
	return kept
}

// reaches is what the build runner derives about what a change reaches: the
// exposure list out of the diff between the base and this build's commit, and
// whether the checkout ships a schema change. Both are readings of the checkout,
// so both are taken here, where the repository is, and recorded on the build
// record — which is where the gate rows over a build read them off, the design
// putting the exposure factor's inputs in a diff and a build record.
//
// The base is master where the service has one and git's empty tree where it
// does not, which is the rule [measure] takes for the same reason: a candidate
// with no master is diffed against nothing, which is every line of it added and
// is the reading the design gives a first release — the widest reach and nothing
// to return to.
//
// A git that will not answer leaves the exposure unavailable with git's own
// words on it, which the published formula turns into a human at the row: the
// gate a failure would remove is the gate that failure is evidence for needing.
// The schema reading answers false there, which decides nothing on its own — the
// row it would have been read at already has a human at it.
func reaches(ctx context.Context, repo, commit string, resolved []build.ResolvedEntry) (exposure.Evidence, bool) {
	base := emptyTree
	if head, err := masterCommit(repo); err == nil && head != "" {
		base = "master"
	}
	packages := make([]exposure.Package, 0, len(resolved))
	for _, entry := range resolved {
		packages = append(packages, exposure.Package{
			Package: entry.Package, Version: entry.Version, Licence: entry.Licence,
		})
	}
	evidence, _, err := exposure.Derive(ctx, exposure.Checkout{Dir: repo, Resolved: packages}, base, commit)
	if err != nil {
		return exposure.Evidence{Unavailable: fmt.Sprintf("the exposure list could not be derived: %v", err)}, false
	}
	return evidence, declaresSchemaChange(repo, base, commit)
}

// declaresSchemaChange is the build's own reading of whether its checkout ships
// a schema change: a path under a migrations or schema directory changed between
// the base and this build's commit. It is the reading enforcement's store rule
// asks about before it requires the candidate environment to have applied the
// change twice — a store contract's form moves whenever the code deriving it
// moves, and a build can move it and ship nothing for a deploy to apply.
//
// It is a convention about where a change lives, per toolchain the way every
// other derivation from a checkout is, and a git that will not answer reads as
// no change declared.
func declaresSchemaChange(repo, base, head string) bool {
	names, err := git(repo, "diff", "--name-only", base, head)
	if err != nil {
		return false
	}
	for _, name := range lines(names) {
		for _, part := range strings.Split(strings.TrimSpace(name), "/") {
			if part == "migrations" || part == "schema" {
				return true
			}
		}
	}
	return false
}

// factorExposure is the exposure list as the score's own input. The two shapes
// are spelled apart on purpose: the extractor reads a repository and decides
// nothing, the score decides and reads no repository, and this is the one place
// that holds both — deps.txt states the reason on the edge.
//
// A build record holding no list at all is unavailable and never an empty list.
// A diff adding none of this reads as nothing and a diff nobody read reads as
// unavailable, and the two call for opposite responses: the first lowers the
// number, the second resolves the factor and puts a human at the row whatever
// the number says.
func factorExposure(e exposure.Evidence, read bool) score.ExposureEvidence {
	if !read {
		return score.ExposureEvidence{
			Unavailable: "the build record holds no exposure list: no extractor ran for this build's toolchain",
		}
	}
	return score.ExposureEvidence{
		OutboundCalls:       e.OutboundCalls,
		Credentials:         e.Credentials,
		AuthorizationChecks: e.AuthorizationChecks,
		DependencyChanges:   e.DependencyChanges,
		Unavailable:         e.Unavailable,
	}
}

// exposureOf is the exposure list one build's runner derived, read off the build
// record and handed to the score. Every row over a build reads it here rather
// than carrying it from the stage that derived it: the record is what says which
// build a vector was computed over, and the re-verification's build is one this
// run did not derive the list at.
func (p *path) exposureOf(ctx context.Context, buildID string) (score.ExposureEvidence, error) {
	if buildID == "" {
		return score.ExposureEvidence{
			Unavailable: "this firing names no build, so there is no diff to read what the change reaches from",
		}, nil
	}
	evidence, read, err := build.Exposure(ctx, p.d.pool, buildID)
	if err != nil {
		return score.ExposureEvidence{}, err
	}
	return factorExposure(evidence, read), nil
}
