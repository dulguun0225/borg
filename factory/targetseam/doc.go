// Package targetseam is the named set of operations the deployer reaches a
// deploy target through. [Target] declares them — [Target.Deploy],
// [Target.Stop], [Target.ReadRunning], [Target.ShiftTraffic],
// [Target.SetInstanceCount], [Target.ApplySchemaChange] and [Target.Snapshot]
// — as an interface. No agent reaches a deploy target at all.
//
// # The code
//
// seam.go holds [Target], [Op] naming each operation with [Ops] listing them,
// [Deployment] with the [ValueSet] it carries and the [Placement] it returns,
// [Shift], [InstanceCount], [SchemaChange], [SnapshotRequest] and [Snapshot],
// what a target reports in [Running] with its [SchemaChangeApplied] rows, each
// type's Validate, [CheckPrincipal], and the errors [ErrIncomplete],
// [ErrNoPrincipal], [ErrShareNotAFraction] and [ErrCountNegative]. fake.go
// holds [Fake], [NewFake], and [Fake.Calls], recording what was called as a
// [Call] and reaching nothing; package localtarget is what the demonstrations
// deploy against.
//
// Adding an operation is an edit to [Target], so target access is one
// interface's methods rather than something spread through the codebase and
// there is one place for a policy to attach. There is none here: nothing checks
// a scope, authenticates a caller, or refuses an operation. A deploy names its
// credential as a [secretref.Ref] and never as a value, so whatever sits behind
// the seam resolves the name at the moment it connects.
//
// Every operation takes the principal making the call and records it beside
// what was asked for, deciding nothing on it. [CheckPrincipal] refuses a call
// carrying none and reads nothing in one that does.
//
// The way-in token is the one value that crosses this seam. The deployer mints
// it at every deploy and hands it to the service in its configuration; the
// deploy record holds a digest of it and never the token, and the [Fake]
// records neither. The service side that would send it — the way in, and the
// report store that digests it — is not built.
//
// Who may write what: this package writes no record. The component that
// deploys calls the seam and writes the deploy record itself.
//
// What defines it: the seam between the deployer and a deploy target, and the
// mitigation's operations at it, are seam 4 of "Security comes last",
// ../../end-goal/deferred.md#security-comes-last; the principal on every call
// is seam 5 of the same file. The replacement that drains and the cut recorded
// as a drain, and the share of traffic a control's schedule shifts, are
// ../../end-goal/how-the-factory-works/03-gates/02-the-rollout-strategy.md.
// The schema change applied before a build takes traffic, and the snapshot
// taken and verified before a change that destroys stored data, are
// ../../end-goal/how-the-factory-works/06-releases/05-the-deploy-record/01-a-schema-change.md.
// The schema history's row — the release that shipped the change, the change's
// identity, a checksum of its text, and the mark that says the store arrived
// carrying it — is
// ../../end-goal/how-the-factory-works/07-contracts/09-the-store-is-a-contract-too.md.
package targetseam
