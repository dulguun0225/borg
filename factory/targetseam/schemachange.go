package targetseam

import (
	"errors"
	"fmt"

	"github.com/dulguun0225/borg/factory/secretref"
)

// The two operations that touch the service's store rather than what runs it:
// the schema change applied before a build takes traffic, and the snapshot
// taken and verified before one that destroys stored data.

// SchemaChange is one change applied to the service's store before the build
// takes traffic: the change's identity, which is what the store's schema
// history holds, the text that performs it, and whether it destroys stored
// data — which is what makes a snapshot owed before it.
type SchemaChange struct {
	Service string
	// Change is the change's identity, the one the build declares and the
	// history is read against.
	Change string
	// Release is the release that ships the change, which the history row names
	// beside it, and is empty on a deploy that names none — a candidate's own
	// environment, and a build the search called for.
	Release string
	// Build is the build the change is applied under, which every history row
	// names. It is what a row a deploy naming no release writes stands on, so a
	// change naming neither is refused.
	Build string
	// Text is what performs the change.
	Text string
	// Destroys is whether the change destroys stored data, which the store rule
	// forbids without a snapshot before it.
	Destroys bool
	// FoundApplied is the adoption's word: the store arrived with the change
	// already in it, so the row goes into the history and the change is not
	// applied. It is the deploy of the adoption item's release that asks for it,
	// on every environment, and no other deploy does.
	FoundApplied bool
	// Snapshot is the copy of the service's store taken and verified before a
	// change that destroys stored data, which is required before one is applied
	// and empty on every change that destroys none. A change found applied
	// applies nothing and needs none.
	Snapshot   Snapshot
	Credential secretref.Ref
}

// SchemaChangeApplied is one row of the store's schema history, which is what
// says which changes a store carries: the build the change was applied under,
// the release that shipped it wherever one exists, the change's identity, a
// checksum of its text, whether it widened the store or removed something from
// it, and whether the deployer applied it or took it on the adoption's word.
type SchemaChangeApplied struct {
	// Release is the release that shipped the change, and is empty where the
	// deploy that wrote the row named none — a candidate's, and the search's.
	Release string
	// Build is the build the change was applied under, which every row names:
	// it is what a row a deploy naming no release wrote stands on.
	Build    string
	Change   string
	Checksum string
	Widened  bool
	// FoundApplied is a row written at an adopted service's first release
	// without the change being applied: the store arrived carrying it. A later
	// reader tells a change the factory applied from one it took on the
	// adoption's word by this field.
	FoundApplied bool
}

// SnapshotRequest is a whole copy of the service's store, asked for before a
// change that destroys stored data, and the same copy named again when the
// deployer deletes it.
type SnapshotRequest struct {
	Service string
	// Name is what the copy is to be called, so the deploy record can name where
	// what the change destroyed can still be read.
	Name       string
	Credential secretref.Ref
}

// Snapshot is a copy taken and verified: what it is called, and the digest the
// verification read. A copy the target could not take or could not verify is an
// error from [Target.Snapshot] and never one of these.
type Snapshot struct {
	Name   string
	Digest string
}

// ErrNoSnapshotBeforeIt is returned by [SchemaChange.Validate] for a change
// that destroys stored data and names no copy taken before it. The
// requirement is here rather than at the deployer alone, so a caller that
// forgot the copy reaches no store.
var ErrNoSnapshotBeforeIt = errors.New("targetseam: a change that destroys stored data names the snapshot taken and verified before it")

// Validate reports whether the schema change may be applied.
func (c SchemaChange) Validate() error {
	if err := check(c.Service, c.Credential); err != nil {
		return err
	}
	if c.Change == "" {
		return fmt.Errorf("%w: service %q names no change", ErrIncomplete, c.Service)
	}
	if c.Release == "" && c.Build == "" {
		return fmt.Errorf("%w: %s of service %q names neither a release nor a build",
			ErrIncomplete, c.Change, c.Service)
	}
	if c.Destroys && !c.FoundApplied && (c.Snapshot.Name == "" || c.Snapshot.Digest == "") {
		return fmt.Errorf("%w: %s of service %q names %q with digest %q",
			ErrNoSnapshotBeforeIt, c.Change, c.Service, c.Snapshot.Name, c.Snapshot.Digest)
	}
	return nil
}

// Validate reports whether the snapshot may be taken.
func (s SnapshotRequest) Validate() error {
	if err := check(s.Service, s.Credential); err != nil {
		return err
	}
	if s.Name == "" {
		return fmt.Errorf("%w: service %q names no snapshot", ErrIncomplete, s.Service)
	}
	return nil
}
