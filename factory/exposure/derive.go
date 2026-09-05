package exposure

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
)

// Toolchain is the toolchain this extractor reads. It is on the coverage
// because a build in another toolchain has no extractor here, and the factor is
// then unavailable rather than nothing.
const Toolchain = "go"

// ErrCheckoutIncomplete is returned by [Derive] for a checkout naming no
// directory or a call naming no head commit. Both are the caller's, so a blank
// is a defect and not a reading to report as unavailable.
var ErrCheckoutIncomplete = errors.New("exposure: the derivation needs a directory and a head commit")

// Package is one entry of the build's own resolved set, which is where a
// dependency change's licence comes from. Licence is empty where the resolver
// produced none, which is that entry's own coverage and not an error.
type Package struct {
	Package string
	Version string
	Licence string
}

// Checkout is what the derivation runs over: the repository on disk, and the
// resolved set the build performed there produced. The set is here because a
// dependency change names the package, its version and its declared licence, and
// the licence is the build's reading and not the manifest's.
type Checkout struct {
	Dir      string
	Resolved []Package
}

// Evidence is the exposure list: what the change reaches that the service did
// not reach before, in the four kinds the design names, each entry naming the
// file and the line.
//
// The four lists are spelled here and again on the score's own input, which is
// the repetition this module pays for locality: the score is handed a value and
// reads no repository, and this reads a repository and decides nothing. A caller
// that has both hands one to the other, which is one function and one place.
//
// Unavailable is why nothing could be read, and empty where the extractor ran. A
// diff adding none of this reads as nothing and a diff nobody read reads as
// unavailable, and the two call for opposite responses: the first lowers the
// number and the second resolves the factor.
type Evidence struct {
	OutboundCalls       []string
	Credentials         []string
	AuthorizationChecks []string
	DependencyChanges   []string
	Unavailable         string
}

// List is every entry in one list, in the order the four kinds are named.
func (e Evidence) List() []string {
	return slices.Concat(e.OutboundCalls, e.Credentials, e.AuthorizationChecks, e.DependencyChanges)
}

// Coverage is what the extractor read: which toolchain it ran for, how many
// changed lines it read, and why it read none. It is the same shape a build's
// resolved set records for the same reason — what was read is part of what was
// found.
type Coverage struct {
	Toolchain   string
	Changes     int
	Unavailable string
}

// Derive is the exposure list for the diff between base and head in the
// checkout, and the coverage of the read that produced it.
//
// It runs one git diff with no context, because every line it reads is one side
// or the other and a context line is indistinguishable from an unchanged one. A
// git that will not answer — no repository, no such commit — leaves the evidence
// unavailable with git's own words on it, which the published formula turns into
// a human at the gate: the gate a failure would remove is the gate that failure
// is evidence for needing.
//
// The base of a service's first build is git's empty tree, which is every line
// of it added — the reading the design gives a first release, where every call
// in it is one the service did not make before.
func Derive(ctx context.Context, c Checkout, base, head string) (Evidence, Coverage, error) {
	if c.Dir == "" || head == "" || base == "" {
		return Evidence{}, Coverage{Toolchain: Toolchain}, ErrCheckoutIncomplete
	}

	cmd := exec.CommandContext(ctx, "git", "diff", "--unified=0", "--no-color", base, head)
	cmd.Dir = c.Dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		why := fmt.Sprintf("the diff between %s and %s could not be taken in %s: %v: %s",
			base, head, c.Dir, err, strings.TrimSpace(string(out)))
		return Evidence{Unavailable: why}, Coverage{Toolchain: Toolchain, Unavailable: why}, nil
	}

	changes := parse(string(out))
	evidence := Evidence{}
	dependencies := map[string]bool{}
	for _, change := range changes {
		if found, is := outboundCall(change); is {
			evidence.OutboundCalls = append(evidence.OutboundCalls, found)
		}
		if found, is := credential(change); is {
			evidence.Credentials = append(evidence.Credentials, found)
		}
		if found, is := authorizationCheck(change); is {
			evidence.AuthorizationChecks = append(evidence.AuthorizationChecks, found)
		}
		if key, found, is := dependencyChange(change, c.Resolved); is && !dependencies[key] {
			dependencies[key] = true
			evidence.DependencyChanges = append(evidence.DependencyChanges, found)
		}
	}
	return evidence, Coverage{Toolchain: Toolchain, Changes: len(changes)}, nil
}
