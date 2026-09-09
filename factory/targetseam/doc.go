// Package targetseam is the named set of operations the deployer reaches a
// deploy target through. [Target] declares them — [Target.Deploy],
// [Target.Stop], [Target.ReadRunning], [Target.ShiftTraffic],
// [Target.SetInstanceCount], [Target.ApplySchemaChange], [Target.Snapshot] and
// [Target.DeleteSnapshot] — as an interface. No agent reaches a deploy target at all.
//
// # The code
//
// seam.go holds [Target], [Op] naming each operation with [Ops] listing them
// and [Mitigation] naming the two a mitigation is, [Deployment] with the
// [ValueSet] it carries, [DeployIDName] and the [Placement] it returns,
// [Replacement] with [Replacements], [Shift], [InstanceCount], [SchemaChange],
// [SnapshotRequest] and [Snapshot], what a target reports in [Running] with
// its [SchemaChangeApplied] rows, each type's Validate, [CheckPrincipal], and
// the errors [ErrIncomplete], [ErrNoPrincipal], [ErrShareNotAFraction],
// [ErrCountNegative], [ErrNoSnapshotBeforeIt] and [ErrCannotDrain]. fake.go
// holds [Fake], [NewFake], and [Fake.Calls], recording what was called as a
// [Call] and reaching nothing, with [Fake.RefuseShift] and [Fake.RefuseDrain]
// for what a platform refuses; package localtarget is what the demonstrations
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
// The way-in token and the deploy record's own identity are the two values
// that cross this seam inside [Deployment.Configuration] rather than in a
// field of their own: the deployer mints the token at every deploy and hands
// it to the service in its configuration, beside the service's own
// credentials, the deploy id under [DeployIDName], and the address of the
// entrance the service presents the token at. The deploy record holds a
// digest of the token and never the token itself, and the [Fake] records
// neither.
//
// A replacement drops no request: [Replacements] holds the drain alone, and a
// platform that cannot hold a request open across a replacement refuses with
// [ErrCannotDrain] rather than reporting one — [Fake.RefuseDrain] is the test's
// own way to ask for that refusal. A change that destroys stored data names
// the copy taken and verified before it, which [SchemaChange.Validate] refuses
// a change without, and every row of a store's schema history names the build
// the change was applied under and the release that shipped it wherever one
// exists — a deploy naming neither is refused.
//
// Who may write what: this package writes no record. The component that
// deploys calls the seam and writes the deploy record itself.
//
// What defines it: the seam between the deployer and a deploy target, and the
// mitigation's operations at it, are seam 4 of "Security comes last",
// ../../end-goal/deferred.md#security-comes-last (C0096, C0098, C0103, C0105,
// C0110, C0112, C0119); the principal on every call is seam 5 of the same file.
// The replacement that drains, the refusal where a platform cannot, and the
// share of traffic a control's schedule shifts, are
// ../../end-goal/how-the-factory-works/03-gates/02-the-rollout-strategy.md
// (C0914).
//
// The schema change applied before a build takes traffic, the snapshot taken
// and verified before a change that destroys stored data, the deletion of that
// copy at the end of the service's retention or at an owner's call, and the row
// a deploy naming no release writes, are
// ../../end-goal/how-the-factory-works/06-releases/05-the-deploy-record/01-a-schema-change.md
// (C1664, C1670, C1675, C1678, C2996).
//
// The schema history's row — the release that shipped the change, the change's
// identity, a checksum of its text, and the mark that says the store arrived
// carrying it — is
// ../../end-goal/how-the-factory-works/07-contracts/09-the-store-is-a-contract-too.md
// (C1861, C1862, C1867, C1868, C1869).
//
// The named operations the deployer reaches a target through are
// ../../end-goal/how-the-factory-works/08-operations/09-the-deployer.md
// (C2182).
//
// The instance told the deploy record's identity at placement, beside the
// way-in token, which [DeployIDName] names in [Deployment.Configuration] and
// [Deployment.Validate] refuses a deployment naming none of, is
// ../../end-goal/how-the-factory-works/08-operations/01-the-health-monitor.md
// (C1953).
package targetseam
