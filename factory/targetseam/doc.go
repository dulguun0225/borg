// Package targetseam is the named set of operations an agent reaches a deploy
// target through. [Target] declares them — [Target.Deploy], [Target.Stop],
// [Target.ReadRunning] — as an interface.
//
// # The code
//
// seam.go holds [Target], [Op] naming each operation, [Running], what a target
// reports, and [Deployment] with [Deployment.Validate], which refuses an
// incomplete operation with [ErrIncomplete]. fake.go holds [Fake], [NewFake],
// and [Fake.Calls], recording what was called as a [Call] and reaching
// nothing; package localtarget is what the demonstrations deploy against.
//
// Adding an operation is an edit to [Target], so target access is three named
// methods in one interface rather than spread through the codebase and there
// is one place for a policy to attach. There is none here: nothing checks an
// agent's scope, authenticates a caller, or refuses an operation. A deploy
// names its credential as a [secretref.Ref] and never as a value, so whatever
// sits behind the seam resolves the name at the moment it connects.
//
// Who may write what: this package writes no record. A component that deploys
// calls the seam and writes to the decision log itself.
//
// What defines it: the seam between an agent and a deploy target is seam 4 of
// "Security comes last", ../../end-goal/deferred.md#security-comes-last. What
// it attaches to on the other side is an agent's scope, "One pipeline" in
// ../../end-goal/how-the-factory-works/01-one-pipeline.md.
package targetseam
