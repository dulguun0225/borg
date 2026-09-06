// Package gate is the gate component: it fires a gate row, reads what applies
// before it appends anything, and writes that firing's decision into the
// decision log through [decisionlog.Writer].
//
// # The code
//
// row.go is the vocabulary of a row: [Kind] with [Kinds], [Row] with [Of],
// [DeployTo], [Row.String], [RowFrom] and [Row.Validate], the eight rows of the
// default path and the five outside every item as values, [Row.ArtifactGate],
// [Row.DecidesAnItem], [Row.DecidesARecord], [Row.ReadsAThreshold],
// [Row.Deploys], [FactorSetAt],
// [Verdict] with its four values, [Actions], [ReturnsTo] with
// [ReturnsToTargets] and [DefaultReturnsTo], and [ErrEditInPlaceRefused].
//
// hold.go is the fourteen holds, [HoldsAt] per row, [Subjects] a hold is
// computed against, the [Holds] interface and [NoHolds], and the three refusals
// an approve takes: [ErrApproveNamesAHoldNotStanding], [ErrApproveLeavesAHoldOut]
// and [ErrApproveThroughAHalt]. mark.go is [Mark] with [Marks], what put a
// human at a row. merge.go is the Merge to master row's own vocabulary:
// [MechanicalChecks] and [Derivations]. spec.go is the Spec row's:
// [SpecChecks] — the uncontrolled hazard and the two directions over the
// requirement a criterion names — [SpecRejection] over the second and third of
// them, and [ChecksAt], the checks a row rejects on. implementation.go is the
// Implementation row's: [ImplementationChecks] and [ScreenRejection], the
// rejection made from what the transition check and the drivers derived over the
// build. strategy.go is [Strategy], [Schedule], [Whys]
// and [Pick] with [Pick.Validate], the shape the pick is stored in; the score
// picks it. waits.go is [Waits],
// [RoutedTo], and the three duties the design names for a row.
//
// gate.go is [Gate], [Composition] and [New], the [Score], [Policy],
// [DriftDetector], [Holds], [IntentState], [RaisedByTheHealthMonitor] and
// [Notifier] a gate is composed from with [NoDriftDetector] and [NoNotifier],
// plus the errors every call shares and [Component], the actor a row's open
// event is written as.
// sample.go is the two samples: the score's held-out one, and the review sample
// with its [Draw], [RandomDraw] and [NeverDraw].
//
// The decision is two rows, and may carry two more. fire.go appends the open
// event: [Gate.Fire] takes a [Firing] — whose [Firing.PriorsRestarted] is the
// one field only the row that decides a shortening of decision-log retention
// carries — and returns [Opened], and [Gate.EditInPlace]
// is the one firing that supersedes a pending row. checks.go is everything a
// firing reads first — the intent's state, the rows already pending
// ([Gate.Pending]), the version under decision, the drift detector's store, the
// holds standing, what the deployer found on the service, and the strategy.
// payload.go is [OpeningPayload], [CriterionResult] and the read back into
// [Opened] with [Opened.Pages]. set.go is the Decomposition row's:
// [DecompositionChecks] and [Gate.FireSet], which decides over a [SetFiring] of
// [SetMember]s — each naming how many of the intent's requirements its item
// answers, which is what the change group is computed from at that row — and
// [SetOpeningPayload], with [Gate.EditSetInPlace] beside it, the Decomposition
// row's own Edit in place, which takes the human who edited the set and bars
// them from closing the row it fires. strategysafeguard.go is the production deploy row's
// fourth action: [StrategySafeguard], which places the safeguard
// and answers whether one stands, [Gate.SafeguardTheStrategy], and its three
// refusals [ErrStrategyNotPickedHere], [ErrPlatformServesNoShare] and
// [ErrStrategySafeguardNotComposed].
//
// verdict.go appends the close event: [Gate.Decide] takes a [Given],
// [Gate.AutoPass] is the factory approving, [Gate.AutoReject] is the factory
// rejecting on one of [ChecksAt], and [ClosingPayload] is that event's
// shape. refer.go is [Gate.Refer], the one verdict that closes a row and re-fires
// it. refuse.go is the three refusals the log's writer cannot evaluate on its
// own, supplied to it per close and compared as per-person keys: the People
// declaration holds a duty by key, an artifact version records the actor that
// wrote it by key, and a record's own routing names the human it bars. acknowledge.go is
// [Gate.Acknowledge]. abandon.go is
// [Gate.Abandon] with the three reasons a decision is ended, and
// [Gate.EnforceAttemptLimit] with [Escalated]. reevaluate.go is [Gate.Reevaluate]
// and [Gate.ReevaluatePending], which re-test the holds on a pending row.
// approval.go is [ApprovalTimes], the read of when each item's merge approval
// closed.
//
// # Who may write what
//
// This package owns no table. It appends into the decision log through
// [decisionlog.Writer], which owns that table. The one record it writes outside
// the log is the escalation onto an item, and it writes that through
// [item.Dispatch], which owns the item's stage: a gate decides an event and edits
// nothing else. The safeguard the production deploy row's fourth action places
// is written through [StrategySafeguard], which the composition supplies, so the
// writer of that record is still Factory.
//
// # Which callers are not built
//
// [IntentState] is a function the composition supplies, and the component that
// reads an item's intent is not built. [RaisedByTheHealthMonitor] is the second
// read of that intent, which is what a halt's two exceptions come to; a gate
// composed without it excepts nothing, so every item holds while a halt stands.
// [Holds] is the composition's,
// and three of the holds this package names read records that do not exist yet:
// a service's maximum concurrent kept fleets, an advisory match, and the
// producing release of a contract migration. [Notifier] reaches
// a human, and the one call made on it is the page's acknowledged event.
// [SpecRejection] is computed here and read by the caller: what the two lists
// it compares are read from is the requirement record and the criterion
// record, and the Spec row's firing path is what hands them over. The third
// check that row rejects on, [AutoRejectedByUncontrolledHazard], is the same
// arrangement: the query is package criterion's and the caller makes it. So is
// [DecompositionChecks] at the Decomposition row, over the intent's
// requirements and what each item of the set answers.
// [Firing.RevertWhileRollbackHolds] is the caller's too: the hold a rollback
// leaves stands on every item but the revert, and what says which item is the
// revert is a walk from a deploy record this package does not make.
// [ScreenRejection] is the same arrangement at the Implementation row, over the
// machines in force and the two derivations from the build.
//
// [Firing.Exposure] is what the component that built hands the gate, derived by
// package exposure and read off the build record at the three firing sites
// below a build. [Firing.CouldNotDerive] is the same arrangement at the merge
// row, and no firing site hands one over, so a derivation that produced no
// result puts no human there yet. The four rows outside
// every item and the row that decides a shortening of decision-log retention
// fire like any other, and what fires them — the artifact store's fleet
// versions, a safeguard's withdrawal, a halt's withdrawal, a legal hold's
// withdrawal, and a shortening of decision-log retention written pending —
// reaches them from the composition.
//
// [StrategySafeguard] is the writer and the reader of the safeguard the
// production deploy row's fourth action places, and it is the composition's: a
// safeguard is package policy's write at Factory and this package writes no
// record of its own. A gate composed without one refuses that action with
// [ErrStrategySafeguardNotComposed] — a second refusal beside the platform's,
// which is the one the design allows — and reads no such safeguard when it picks.
//
// [Firing.Screens] is what the transition check derived over the build, and the
// drivers' derivation reaches [ScreenRejection] the same way: both run where the
// checkout is, and the component that builds is what hands them over. Neither
// derivation is composed at the Implementation row yet.
//
// # What defines it
//
// The two-row decision, what the open event names, the abandonment, the
// acknowledgement, refer, the five refusals, the marks, the review sample, and
// the read of the intent's state are
// ../../end-goal/how-the-factory-works/03-gates/01-where-a-gate-is-and-what-decides-it.md.
// The actions per row and the row a further environment gets are
// ../../end-goal/how-the-factory-works/03-gates/03-actions-at-each-gate.md. The
// three kinds of hold, the approve that names the set, and the re-evaluation of
// a pending row are
// ../../end-goal/how-the-factory-works/03-gates/04-what-a-gate-may-change.md.
// The attempt limit and the escalation are
// ../../end-goal/how-the-factory-works/03-gates/05-the-attempt-limit.md, and
// what a reject may name is
// ../../end-goal/how-the-factory-works/03-gates/06-going-back-up.md. The
// strategy and its schedules are
// ../../end-goal/how-the-factory-works/03-gates/02-the-rollout-strategy.md.
//
// The rows themselves are
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/README.md,
// each with a file of its own there: the Spec row's rejection in both
// directions over the requirement a criterion names is
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/03-the-six-patterns.md,
// the Implementation row's rejection over the screens is
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/05-implementation/01-the-transition-check.md
// and
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/05-implementation/02-the-encoding-and-the-emission.md,
// the candidate deploy row's holds are
// 06-deploy-to-candidate-environment.md, the merge row's mechanical rejections
// and its derivations are 07-merge-to-master.md, the production deploy row's
// holds and the four fields a service must have to auto-pass are
// 08-deploy-to-production.md, and the three rows outside every item that file
// names are 09-a-role-prompt-or-a-skill.md, 10-a-safeguards-withdrawal.md and
// 11-a-halts-withdrawal.md. Two more rows belong to no item: a legal hold's
// withdrawal, which is
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/03-a-legal-hold.md,
// and the shortening of decision-log retention, which is
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/02-retention.md,
// and the halt no approve passes, with the two exceptions it takes, is
// ../../end-goal/how-the-factory-works/09-gate-policy/04-stopping-the-factory.md.
//
// The vector, the resolution that puts a human at a row whatever the number, and
// the factor set per row are
// ../../end-goal/how-the-factory-works/04-risk-score/01-factors-at-least.md; the
// held-out sample and the rate it is drawn against are
// ../../end-goal/how-the-factory-works/04-risk-score/02-how-it-learns.md. The
// threshold and the safeguard are
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md.
// Who holds a row's duty is ../../end-goal/what-humans-do.md, read from the
// declaration
// ../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md
// describes. What Decomposition decides is
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md,
// and the states an intent may be in are
// ../../end-goal/how-the-factory-works/02-intent-into-items/02-the-interview.md.
package gate
