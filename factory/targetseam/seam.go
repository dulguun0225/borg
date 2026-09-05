package targetseam

import (
	"context"
	"errors"
	"fmt"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/secretref"
)

// Op is one of the operations [Target] declares. It is a name a caller can
// record and a policy can match on.
type Op string

const (
	// OpDeploy puts a build on a target, replacing the instances of the build
	// it succeeds.
	OpDeploy Op = "deploy"
	// OpStop ends every instance of a service on a target. What it does not do
	// is delete anything the service wrote.
	OpStop Op = "stop"
	// OpReadRunning asks the target what is running, which is the one
	// operation that changes nothing. Drift detection is its first caller.
	OpReadRunning Op = "read_running"
	// OpShiftTraffic decides what share of arriving traffic reaches one build.
	// It is what a rollout with a control performs and what a mitigation
	// shifting traffic off a target performs.
	OpShiftTraffic Op = "shift_traffic"
	// OpSetInstanceCount changes how many instances of a build the target runs.
	// It is the second of the mitigation's two operations.
	OpSetInstanceCount Op = "set_instance_count"
	// OpApplySchemaChange applies one change to the service's store, before the
	// build takes traffic.
	OpApplySchemaChange Op = "apply_schema_change"
	// OpSnapshot takes a whole copy of the service's store and verifies it,
	// which is what a deploy does before it applies a change that destroys
	// stored data.
	OpSnapshot Op = "snapshot"
)

// Ops is every operation [Target] declares, in the order the interface does.
// TestOpsListsEveryOperation fails if the two stop agreeing.
var Ops = []Op{
	OpDeploy, OpStop, OpReadRunning, OpShiftTraffic,
	OpSetInstanceCount, OpApplySchemaChange, OpSnapshot,
}

// Target is the whole of what the deployer may do to a deploy target. No agent
// reaches one: deploying is not a stage an agent is dispatched to, so every
// call here is the deployer's own, and an operation is added by editing this
// interface and nothing else — what production access exists is answered by
// reading one declaration.
//
// Every operation takes the [principal.Principal] making the call and records
// it beside what was asked for, deciding nothing on it.
type Target interface {
	// Deploy puts the build d names on the target, reaching it with the
	// credential d references, and reports how the instances it replaced ended.
	Deploy(ctx context.Context, p principal.Principal, d Deployment) (Placement, error)
	// Stop ends every instance of the named service on the target, reaching it
	// with the credential the reference names.
	Stop(ctx context.Context, p principal.Principal, service string, credential secretref.Ref) error
	// ReadRunning is what the target says is running for the named service, and
	// the schema history where the target holds the service's store.
	ReadRunning(ctx context.Context, p principal.Principal, service string, credential secretref.Ref) (Running, error)
	// ShiftTraffic decides what share of arriving traffic reaches one build.
	ShiftTraffic(ctx context.Context, p principal.Principal, s Shift) error
	// SetInstanceCount changes how many instances of one build the target runs.
	SetInstanceCount(ctx context.Context, p principal.Principal, c InstanceCount) error
	// ApplySchemaChange applies one change to the service's store through the
	// environment's credential, and records it in the store's schema history.
	ApplySchemaChange(ctx context.Context, p principal.Principal, c SchemaChange) error
	// Snapshot takes a whole copy of the service's store and verifies it. A
	// snapshot it cannot take and verify is an error and never a Snapshot
	// reporting itself unverified.
	Snapshot(ctx context.Context, p principal.Principal, s SnapshotRequest) (Snapshot, error)
}

// Deployment is what one deploy names: the service, the build, the credential
// to reach the target with, the resolved configuration the build runs under,
// the token the deployer minted for the way in, and the identity of the deploy
// record this deploy is. The credential is a reference and there is no field on
// this struct that could hold its value.
//
// What crosses the seam is the build and not the release. A release is the name a
// build has on master, which is a fact of the store and not of the target, and a
// candidate has no such name at all — so a field called release could not carry
// the deploy into a candidate's own environment, which happens one gate before
// the number exists.
type Deployment struct {
	Service string
	Build   string
	// Credential is the reference the target is reached with, from the
	// environment record.
	Credential secretref.Ref
	// Configuration is the resolved value set the build runs under: the
	// service's configuration file with its secrets resolved through seam 3.
	// The deployer writes a digest of it on the deploy record and a rollback
	// restores the version so named.
	Configuration ValueSet
	// WayInToken is the token the deployer minted for the way in at this deploy
	// and hands to the service in its configuration. The deploy record holds a
	// digest of it and never the token, and this is the one field of the seam
	// that carries a value rather than a reference: the token is minted here
	// and stored nowhere, so there is no name to resolve it by.
	WayInToken string
	// DeployID is the deploy record's own identity, handed to every instance
	// the deploy places, which is what the health monitor's emission names to
	// tell these instances from the ones of the same build an earlier deploy
	// placed.
	DeployID string
}

// ValueSet is one resolved configuration: the names and the values the build
// runs under, in the order the caller assembled them. It crosses the seam
// because the target is what puts the values in front of the process; the
// factory's own record of it is the digest on the deploy record.
type ValueSet struct {
	Names  []string
	Values []string
}

// Replacement is how the instances a deploy replaced ended: drained, the new
// requests stopped and the held ones finished, or cut, which is what a platform
// unable to hold a request open across the replacement performs and the factory
// records as a drain.
type Replacement string

const (
	// ReplacementDrained is the operation's contract kept: no request dropped.
	ReplacementDrained Replacement = "drained"
	// ReplacementCut is a platform that could not keep it, recorded as what
	// happened rather than as a drain.
	ReplacementCut Replacement = "cut"
)

// Placement is what a deploy reports back: how the instances it replaced ended.
// The deployer writes it on the deploy record's row for that target.
type Placement struct {
	Replacement Replacement
}

// Shift is a share of arriving traffic decided for one build. A shift naming no
// build is every build of the service on that target, which is how traffic is
// shifted off a target altogether.
type Shift struct {
	Service    string
	Build      string
	Share      float64
	Credential secretref.Ref
}

// InstanceCount is how many instances of one build the target is to run.
type InstanceCount struct {
	Service    string
	Build      string
	Count      int
	Credential secretref.Ref
}

// SchemaChange is one change applied to the service's store before the build
// takes traffic: the change's identity, which is what the store's schema
// history holds, the text that performs it, and whether it destroys stored
// data — which is what makes a snapshot owed before it.
type SchemaChange struct {
	Service string
	// Change is the change's identity, the one the build declares and the
	// history is read against.
	Change string
	// Text is what performs the change.
	Text string
	// Destroys is whether the change destroys stored data, which the store rule
	// forbids without a snapshot before it.
	Destroys   bool
	Credential secretref.Ref
}

// SchemaChangeApplied is one row of the store's schema history, which is what
// says which changes a store carries: the change's identity, a checksum of its
// text, and whether it widened the store or removed something from it.
type SchemaChangeApplied struct {
	Change   string
	Checksum string
	Widened  bool
}

// SnapshotRequest is a whole copy of the service's store, asked for before a
// change that destroys stored data.
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

var (
	// ErrIncomplete is returned for an operation missing a service, a build, a
	// credential reference, or anything else the operation names.
	ErrIncomplete = errors.New("targetseam: the operation is incomplete")
	// ErrNoPrincipal is returned for a call carrying no principal, or one
	// [principal.Principal.Validate] refuses. The principal is populated on
	// every call and enforced on none: this refuses an absent one and reads
	// nothing in it.
	ErrNoPrincipal = errors.New("targetseam: the call carries no principal")
	// ErrShareNotAFraction is returned by [Shift.Validate] for a share outside
	// nothing to all of it.
	ErrShareNotAFraction = errors.New("targetseam: a share of traffic is between 0 and 1")
	// ErrCountNegative is returned by [InstanceCount.Validate] for fewer than
	// no instances.
	ErrCountNegative = errors.New("targetseam: an instance count is not negative")
)

// Validate reports whether the deployment may be attempted. An implementation
// calls it before it reaches anything.
func (d Deployment) Validate() error {
	if err := check(d.Service, d.Credential); err != nil {
		return err
	}
	if d.Build == "" {
		return fmt.Errorf("%w: service %q names no build", ErrIncomplete, d.Service)
	}
	if len(d.Configuration.Names) != len(d.Configuration.Values) {
		return fmt.Errorf("%w: service %q names %d configuration values for %d names",
			ErrIncomplete, d.Service, len(d.Configuration.Values), len(d.Configuration.Names))
	}
	return nil
}

// Validate reports whether the shift may be attempted.
func (s Shift) Validate() error {
	if err := check(s.Service, s.Credential); err != nil {
		return err
	}
	if s.Share < 0 || s.Share > 1 {
		return fmt.Errorf("%w: service %q asks for %v", ErrShareNotAFraction, s.Service, s.Share)
	}
	return nil
}

// Validate reports whether the instance count may be attempted.
func (c InstanceCount) Validate() error {
	if err := check(c.Service, c.Credential); err != nil {
		return err
	}
	if c.Build == "" {
		return fmt.Errorf("%w: service %q names no build", ErrIncomplete, c.Service)
	}
	if c.Count < 0 {
		return fmt.Errorf("%w: service %q asks for %d", ErrCountNegative, c.Service, c.Count)
	}
	return nil
}

// Validate reports whether the schema change may be applied.
func (c SchemaChange) Validate() error {
	if err := check(c.Service, c.Credential); err != nil {
		return err
	}
	if c.Change == "" {
		return fmt.Errorf("%w: service %q names no change", ErrIncomplete, c.Service)
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

// check is what every operation on a [Target] requires: something to act on,
// and a credential to reach the target with. Each operation's Validate is this
// plus the fields only that operation has.
func check(service string, credential secretref.Ref) error {
	switch {
	case service == "":
		return fmt.Errorf("%w: it names no service", ErrIncomplete)
	case credential.IsZero():
		return fmt.Errorf("%w: service %q references no credential", ErrIncomplete, service)
	}
	return nil
}

// CheckPrincipal is what every implementation calls first: the principal is
// populated and is one [principal.Principal.Validate] admits. It reads nothing
// in it and refuses on nothing else, the principal deciding nothing at this
// seam.
func CheckPrincipal(p principal.Principal) error {
	if p.IsZero() {
		return ErrNoPrincipal
	}
	if err := p.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrNoPrincipal, err)
	}
	return nil
}

// Running is what a target reports for one service. An empty Build means
// nothing is running there.
type Running struct {
	Service string
	// Build is what the target says it runs, which is a build and never the
	// name that build has on master.
	Build string
	// ArtifactDigest is the digest of the artifact the target is running, read
	// from the target and compared against what the build record holds.
	ArtifactDigest string
	// Instances is how many instances of that build the target runs, which is
	// the capacity a kept-instance count is computed from and what a proof test
	// reads against the deploy record.
	Instances int
	// SchemaHistory is which changes the service's store carries, in the order
	// they were applied, and is empty where the target holds no store for the
	// service. A deploy applies the changes its build declares that this lacks.
	SchemaHistory []SchemaChangeApplied
}
