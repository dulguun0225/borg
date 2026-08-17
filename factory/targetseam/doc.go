// Package targetseam is the named set of operations an agent reaches a deploy
// target through. [Target] declares them — [Target.Deploy], [Target.Stop],
// [Target.ReadRunning] — and [Fake] is the only implementation, which records
// what was called on it and reaches nothing.
//
// Adding an operation is an edit to [Target], which is the point: production
// access is three named methods in one interface rather than spread through
// the codebase, so there is one place for a policy to attach when there is one
// to attach. There is none today — nothing here checks an agent's scope,
// authenticates a caller, or refuses an operation.
//
// A deploy names its credential as a [secretref.Ref] and never as a value.
// That is where seam 3 and seam 4 meet: the environment record holds a name,
// the seam passes the name, and whatever eventually sits behind the seam
// resolves it at the moment it connects.
//
// Who may write what: this package writes no record. A component that deploys
// calls the seam and writes to the decision log itself.
//
// What defines it: seam 4 of "Security comes last" in
// ../../end-goal/deferred.md#security-comes-last. What the seam attaches to on
// the other side is an agent's scope, from "One pipeline" in
// ../../end-goal/how-humans-do-it/01-one-pipeline.md, declared and enforced by
// nothing until there is something behind the seam.
package targetseam
