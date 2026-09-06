// Command driftdetector is the drift detector's own process: one pass over
// every production target's build and digest, the log's chain, and the
// factory's own last checks, each compared against what the factory
// recorded.
//
// Four subcommands:
//
//	driftdetector pass -secrets <file>
//	driftdetector show
//	driftdetector clear <mismatch-id> -human <name>
//	driftdetector install -address <address>
//
// pass runs three of the six comparisons: the first, over what each target
// runs and the digest it reports; the second, over the log's chain against the
// head this store recorded; and the third, over the factory's own last check
// records, which is what makes a stopped factory component reach a human. The
// fourth (the instances a rollback would need against the count the deploy
// record keeps), the fifth (the schema history in each service's store) and the
// sixth (the configuration digest running on a target) are not built here. show
// prints every mismatch and the last check per target, which is what makes
// a stopped drift detector visible rather than silent — no mismatches is
// not health if the last check is a week old. clear clears one mismatch on
// a named human's say-so. install writes the one address, mail or chat, the
// detector delivers its own page to — done once, installing the detector
// beside the factory.
//
// main.go is the switch on the subcommand name, the two stores opened
// together, and each subcommand's own flags. pass.go is the first
// comparison: [pass] itself, [runsOn] — which of a production environment's
// targets one service runs on, which is the set this pass reads and no other
// for that service — [recordedFor], the release the deploy record marks for
// one target, read per target and not once for the service — [excusedBuilds],
// which is the rollout exemption per target, bounded by the window's own cap,
// by the targets the deploy record marks complete, and by
// [deployerLastCheckStale], and [report].
// checks.go is the second and third comparisons: [chainCheck], [staleCheck]
// with [raiseStale] and [holdsWhat], which is what a stopped component's
// mismatch holds. The tests are pass_test.go, which opens both stores and
// exercises pass directly; pertarget_test.go, the first comparison read per
// target; and checks_test.go, the third comparison's own.
//
// What it reaches a target through is the same seam an agent does, and the read
// operation is the one that changes nothing. It calls as itself, a component,
// the principal every operation of that seam now takes and decides nothing
// on.
//
// Who may write what: it is read-only on the factory's store — the production
// environment record, the services, the builds and releases, the current
// production deploy of each, the open analysis windows, the log's own
// table, read directly rather than through decisionlog.Reader, and the
// factory's last check records — and it writes only into its own store,
// which it opens through a URL of its own and applies its own schema to.
// Nothing here builds, deploys, scores, approves, or measures anything, and
// the one thing a mismatch it writes can do is stop a deploy.
//
// What defines it:
// ../../../end-goal/how-the-factory-works/08-operations/08-drift-detection.md — the one process
// outside the pipeline, the owner installing it beside the factory they already host,
// the six comparisons, the detector's own delivery, the last check per target, and clearing a
// mismatch as the human act placed here and refused at Ops. What a mismatch then holds is
// ../../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/08-deploy-to-production.md, and package
// driftdetector's own doc.go is what defines the records this command writes.
package main
