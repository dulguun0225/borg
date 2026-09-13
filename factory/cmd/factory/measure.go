package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/deploy"
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
	m.DestroysStoredData, m.DestroysStoredDataUnavailable = destroysStoredData(repo, base, "HEAD")
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

// DestructiveStatements are what an added line has to carry for the diff to
// read as destroying stored data. They are the statements that remove stored
// rows or the place they are stored, and they are a list rather than a rule
// about SQL because the list is what a human argues with: a statement outside it
// is one this reading does not find, and adding one is a line here.
//
// It is a convention per toolchain, the way every other derivation from a
// checkout is: what a diff destroys is a property of the language the schema
// change is written in, and this one is written for SQL in a Go service's
// repository. The design names the reading and not how to make it — measure.go's
// own package documentation says so.
var DestructiveStatements = []string{
	"drop table", "drop column", "drop schema", "drop database", "drop index",
	"truncate table", "delete from",
}

// destroysStoredData is whether the diff between base and head destroys stored
// data, and why nothing could say where the reading could not be made. A git
// that will not answer leaves it unavailable with git's own words on it, which
// resolves the reversibility factor at Implementation: the gate a failure would
// remove is the gate that failure is evidence for needing, and a diff nobody
// could read is never read as a diff that destroys nothing.
//
// It reads the added side alone. A statement removed from the repository
// destroys nothing when the build runs, and one added is what will run.
func destroysStoredData(repo, base, head string) (destroys bool, unavailable string) {
	diff, err := git(repo, "diff", "--unified=0", "--no-color", base, head)
	if err != nil {
		return false, fmt.Sprintf("whether the diff against %s destroys stored data could not be read: %v", base, err)
	}
	for _, line := range lines(diff) {
		if !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "+++") {
			continue
		}
		lowered := strings.ToLower(line)
		for _, statement := range DestructiveStatements {
			if strings.Contains(lowered, statement) {
				return true, ""
			}
		}
	}
	return false, ""
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

// currentReleaseResolved is the resolved set of the build the service's current
// release put on production, which is the set a dependency change is diffed
// against. A service running nothing has none, and every package this build
// resolved is then one the service did not reach before — the reading a first
// release already gets from the diff against git's empty tree.
//
// A read that fails answers none for the same reason: it is the caller's own
// store and a failure here is a factory that cannot read its own deploy record,
// which the rows over the build read as a wider exposure and never as a
// narrower one.
func (p *path) currentReleaseResolved(ctx context.Context, serviceID string) []build.ResolvedEntry {
	current, found := p.currentReleaseBuild(ctx, serviceID)
	if !found {
		return nil
	}
	entries, err := build.Resolved(ctx, p.d.pool, current.ID)
	if err != nil {
		return nil
	}
	return entries
}

func (p *path) currentReleaseBuild(ctx context.Context, serviceID string) (build.Build, bool) {
	addresses, err := p.addressesOf(ctx, serviceID)
	if err != nil {
		return build.Build{}, false
	}
	current, running, err := deploy.Current(ctx, p.d.pool, serviceID, p.production.ID, addresses)
	if err != nil || !running || current.BuildID == "" {
		return build.Build{}, false
	}
	result, err := build.Get(ctx, p.d.pool, current.BuildID)
	if err != nil {
		return build.Build{}, false
	}
	return result, true
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
	// Derived says a list was read for this build, which is what tells an
	// empty list from no list: a build that reaches nothing new is the lowest
	// reading there is, and a build nobody derived a list for is not read as
	// one.
	return score.ExposureEvidence{
		Derived:             true,
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
