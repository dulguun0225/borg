// Command driftdetector is the drift detector's own process: one pass reading what each
// production target runs and comparing it against that service's current release.
//
// Three subcommands:
//
//	driftdetector pass -secrets <file>
//	driftdetector show
//	driftdetector clear <mismatch-id> -human <name>
//
// pass is the comparison. show prints every mismatch and the last check per
// target, which is what makes a stopped drift detector visible rather than
// silent — no mismatches is not health if the last check is a week old. clear
// clears one mismatch on a named human's say-so.
//
// main.go is the whole of it: the switch on the subcommand name, the two
// stores opened together, passCommand around pass — the comparison itself,
// with the recorded deploy and the builds a window excuses read beside it —
// showCommand, and clearCommand. The tests are pass_test.go, which opens both
// stores and exercises pass directly.
//
// What it reaches a target through is the same seam an agent does, and the read
// operation is the one that changes nothing.
//
// Who may write what: it is read-only on the factory's store — the production
// environment record, the services, the current production deploy of each, and the
// open analysis windows — and it writes only into its own store, which it opens
// through a URL of its own and applies its own schema to. Nothing here builds,
// deploys, scores, approves, or measures anything, and the one thing a mismatch it
// writes can do is stop a deploy.
//
// What defines it:
// ../../../end-goal/how-the-factory-works/08-operations/08-drift-detection.md — the one process
// outside the pipeline, the owner installing it beside the factory they already host,
// the comparison, the last check per target, and clearing a mismatch as the human act
// placed here and refused at Ops. What a mismatch then holds is
// ../../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/08-deploy-to-production.md, and package
// driftdetector's own doc.go is what defines the two records this command writes.
package main
