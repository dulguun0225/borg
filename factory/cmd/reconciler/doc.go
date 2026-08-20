// Command reconciler is the reconciler's own process: one pass reading what each
// production target runs and comparing it against that service's current release.
//
// It is a second binary because a reconciler the factory deployed would be inside
// the trust domain it exists to check. The owner installs it beside the factory they
// already host, which is substrate outside the twelve duties and is done once.
//
// It is read-only on the factory's store — the production environment record, the
// services, the current production deploy of each, and the open watch windows — and
// it writes only into its own, which it opens through a URL of its own and applies
// its own schema to. Nothing here builds, deploys, scores, approves, or measures
// anything, and the one thing a mismatch it writes can do is stop a deploy.
//
// Three subcommands:
//
//	reconciler pass -secrets <file>
//	reconciler show
//	reconciler clear <mismatch-id> -human <name>
//
// pass is the comparison. show prints every mismatch and the last comparison per
// target, which is what makes a stopped reconciler visible rather than silent — no
// mismatches is not health if the last comparison is a week old. clear is the human
// act the design puts here and refuses in the factory: clearing a mismatch from Ops
// would make the factory a writer of the record that says the factory is wrong.
//
// What it reaches a target through is the same seam an agent does, and the read
// operation is the one that changes nothing. That the read works from a process which
// did not perform the deploy is what package localtarget had to change to allow.
//
// What defines it:
// ../../../end-goal/how-humans-do-it/08-operations.md#the-reconciler — the one process
// outside the pipeline, which the owner installs beside the factory they already host,
// and where clearing a mismatch is the human act placed at the reconciler and refused
// at Ops. The three subcommands are that section's comparison, its last comparison per
// target read so a stopped reconciler is visible rather than silent, and that
// clearing. What a mismatch then holds is
// ../../../end-goal/how-humans-do-it/03-gates.md#deploy-to-production, and package
// reconciler's own doc.go is what defines the two records this command writes.
package main
