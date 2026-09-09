// Package gate is the gate component: it fires a gate row, reads what applies
// before it appends anything, and writes that firing's decision into the
// decision log through [decisionlog.Writer].
//
// # The code
//
// row.go is the vocabulary of a row: [Kind] with [Kinds], [Row] with [Of],
// [DeployTo], [Row.String], [RowFrom] and [Row.Validate], the eight rows of the
// default path and the five outside every item as values, [Row.ArtifactGate],
// [Row.OffersEditInPlace], [Row.DecidesAnItem], [Row.DecidesARecord],
// [Row.ReadsAThreshold], [Row.Deploys], [FactorSetAt],
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
// rejection made from what the transition check and the drivers derived over
// the build, [AutoRejectedByCompile], the third check, over a build the
// build runner refused outright — computed by the caller from that refusal
// directly, the way the caller computes the other two from what it derived —
// and [CriterionRejection], the fourth, over a criterion the build's own
// process decided against.
// above.go is what an event gate's firing waits on: [RowAbove],
// [Gate.ApprovalStands] — the read "nothing below the target stands as
// approved" comes to — and the two refusals [ErrRowAboveNotApproved] and
// [ErrCandidateRunNotEnded]. cycle.go is the Decomposition row's third
// mechanical rejection: [SetCycleRejection] over the set's own waits-on graph,
// which names [AutoRejectedByACycle].
// strategy.go is [Strategy], [Schedule], [Whys]
// and [Pick] with [Pick.Validate], the shape the pick is stored in; the score
// picks it. waits.go is [Waits],
// [RoutedTo], and the three duties the design names for a row.
//
// gate.go is [Gate], [Composition] and [New], the [Score], [Policy],
// [DriftDetector], [Holds], [IntentState], [RaisedByTheHealthMonitor],
// [SafeguardRouting] and
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
// [Gate.Abandon] with the three reasons a decision is ended, and the two
// enforcements of the attempt limit with [Escalated]:
// [Gate.EnforceAttemptLimit] over an item's own per-stage count and
// [Gate.EnforceDecompositionRounds] over the intent's own count of
// re-decompositions. reevaluate.go is [Gate.Reevaluate]
// and [Gate.ReevaluatePending], which re-test the holds on a pending row.
// approval.go is [ApprovalTimes], the read of when each item's merge approval
// closed.
//
// # Who may write what
//
// This package owns no table. It appends into the decision log through
// [decisionlog.Writer], which owns that table. The two records it writes
// outside the log are the escalation onto an item, written through
// [item.Dispatch], which owns the item's stage, and the escalation onto an
// intent whose re-decompositions exceeded the limit, written through
// [intent.Intake], which owns the intent's state: a gate decides an event and
// edits nothing else. The safeguard the production deploy row's fourth action places
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
// [SafeguardRouting] is the composition's for the reason [Holds] is: a
// safeguard is package policy's record at Factory and this package writes none
// of its own, so where a safeguarded row routes is handed over rather than
// read. A gate composed without it routes nothing, and a safeguarded row that
// names no duty widens to the owner, which is the default the routing field
// exists to replace.
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
// row, and cmd/factory hands one over for a security predicate the factory's
// own list could not decide. The five rows outside
// every item — a role prompt or a skill, the three withdrawals, and the
// shortening of decision-log retention, the last four of them the rows that
// decide a record —
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
// # What defines it The two-row decision, what the open event names, the
// abandonment, the acknowledgement, refer, the five refusals, the marks, the
// review sample, and the read of the intent's state are
// ../../end-goal/how-the-factory-works/03-gates/01-where-a-gate-is-and-what-decides-it.md
// (C0826, C0827, C0829, C0830, C0831, C0833, C0834, C0835, C0836, C0837, C0838,
// C0839, C0842, C0843, C0844, C0845, C0846, C0847, C0848, C0849, C0850, C0851,
// C0852, C0853, C0854, C0855, C0856, C0857, C0858, C0859, C0860, C0861, C0863,
// C0867, C0868, C0869, C0870, C0871, C0872, C0875, C0876, C0877, C0878, C0879,
// C0881, C0882, C0883, C0885, C0886, C0887, C0888, C0889, C0890, C0891, C0893,
// C0897, C0898, C0899, C0900, C0901).
//
// The actions per row and the row a further environment gets are
// ../../end-goal/how-the-factory-works/03-gates/03-actions-at-each-gate.md
// (C0940, C0941, C0942, C0943, C0944, C0945, C0946, C0947, C0949, C0950, C0951,
// C0952). The three kinds of hold, the approve that names the set, and the
// re-evaluation of a pending row are
// ../../end-goal/how-the-factory-works/03-gates/04-what-a-gate-may-change.md
// (C0953, C0954, C0955, C0956, C0957, C0958, C0959, C0960, C0961, C0962, C0963,
// C0964, C0965, C0966, C0967, C0968, C0969, C0970, C0971, C0972, C0973, C0982,
// C0983, C0984, C0985, C0986, C0987, C0988, C0989, C0990, C0991, C0992, C0993).
//
// The attempt limit and the escalation are
// ../../end-goal/how-the-factory-works/03-gates/05-the-attempt-limit.md (C0994,
// C0995, C0996), and what a reject may name is
// ../../end-goal/how-the-factory-works/03-gates/06-going-back-up.md (C1004,
// C1005, C1006, C1007, C1008, C1009, C1010, C1011, C1012, C1013, C1031,
// C1032). The strategy and its schedules are
// ../../end-goal/how-the-factory-works/03-gates/02-the-rollout-strategy.md
// (C0910, C0911, C0912, C0918, C0922, C0936).
//
// The rows themselves are
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/README.md,
// each with a file of its own there: the Spec row's rejection in both
// directions over the requirement a criterion names is
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/03-the-six-patterns.md
// (C1069, C1071), the Implementation row is made when the stage finishes and
// offers no Edit in place, in
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/05-implementation/README.md
// (C1139, C1140), its rejection over the screens is
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/05-implementation/01-the-transition-check.md
// (C1110, C1111, C1113) and, over a criterion the build decided, is
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/05-implementation/02-the-encoding-and-the-emission.md
// (C1125, C1128), the candidate deploy row's holds are
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/06-deploy-to-candidate-environment.md
// (C1143, C1144, C1145, C1146, C1148, C1149, C1150, C1151, C1152, C1153), the
// merge row's mechanical rejections and its derivations are
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/07-merge-to-master.md
// (C1155, C1156, C1157, C1158, C1160, C1161, C1162), the production deploy
// row's holds, the budget hold, the change freeze, current meaning complete
// everywhere, and the four fields a service must have to auto-pass are
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/08-deploy-to-production.md
// (C1163, C1165, C1166, C1167, C1168, C1169, C1170, C1171, C1172, C1173, C1174,
// C1175, C1176, C1177, C1179, C1180, C1181, C1182, C1183, C1184), and the
// three rows outside every item that file names are
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/09-a-role-prompt-or-a-skill.md
// (C1186, C1188, C1189, C1190, C1194, C1195),
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/10-a-safeguards-withdrawal.md
// (C1196, C1198, C1199, C1200, C1201, C1202, C1203, C1204) and
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/11-a-halts-withdrawal.md
// (C1206, C1207, C1208, C1209, C1210, C1211, C1212, C1213).
//
// Two more rows belong to no item: a legal hold's withdrawal, which is
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/03-a-legal-hold.md
// (C2325), and the shortening of decision-log retention, which is
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/02-retention.md
// (C2308, C2309, C2310, C2313), and the halt no approve passes, with the two
// exceptions it takes, is
// ../../end-goal/how-the-factory-works/09-gate-policy/04-stopping-the-factory.md
// (C2335, C2337, C2338, C2339, C2340, C2341, C2344, C2346, C2347, C2349, C2354,
// C2355, C2356).
//
// The vector, the resolution that puts a human at a row whatever the number,
// and the factor set per row are
// ../../end-goal/how-the-factory-works/04-risk-score/01-factors-at-least.md
// (C1264, C1279, C1284, C1296, C1297, C1301, C1302, C1303, C1304, C1305, C1306,
// C1310, C1311, C1316, C1317); the held-out sample and the rate it is drawn
// against are
// ../../end-goal/how-the-factory-works/04-risk-score/02-how-it-learns.md
// (C1339, C1360, C1361, C1368, C1369, C1370, C1371, C1372, C1373, C1374,
// C1376). The threshold and the safeguard are
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md
// (C2214, C2215, C2219, C2240, C2255, C2260).
//
// Who holds a row's duty is ../../end-goal/what-humans-do.md (C2853, C2854,
// C2873, C2874), read from the declaration
// ../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md
// (C2569, C2584, C2585, C2586, C2587, C2588, C2589, C2652, C2653, C2668)
// describes. What Decomposition decides is
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md
// (C0727, C0751, C0756, C0761, C0762, C0766, C0768, C0769, C0773, C0774, C0775,
// C0778, C0781, C0789, C0791, C0796), and the states an intent may be in are
// ../../end-goal/how-the-factory-works/02-intent-into-items/02-the-interview.md
// (C0576).
//
// The open payload naming the artifact version and its digest is seam 2 of
// ../../end-goal/deferred.md (C0052);
//
// [Gate.FireSet] deciding one row over a set of items, and refusing a set of
// fewer than two members, are
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/01-decomposition.md
// (C1038, C1039);
//
// a row per further deploy environment as one decision, and merge and deploy
// as separate rows of the path, are
// ../../end-goal/how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md
// (C1418, C1425, C1438);
//
// every row being score-gated with a safeguard adding a human, the held-out
// sample auto-passing with the payload saying why, and a safeguard or a
// resolved factor marking the row regardless, are
// ../../end-goal/how-the-factory-works/05-environments/04-what-the-candidate-environment-decides/02-the-verdict.md
// (C1579, C1591, C1592); a failed criterion rejecting at Merge to master is
// ../../end-goal/how-the-factory-works/05-environments/04-what-the-candidate-environment-decides/README.md
// (C1600);
//
// [HoldDependencyNotCurrent] holding until the fourth target lands is
// ../../end-goal/how-the-factory-works/06-releases/05-the-deploy-record/README.md
// (C1715);
//
// [HoldRollbackAwaitingRevert] standing at every production deploy row, the
// hold's own rule with the revert exempt from it, the revert deploying
// unheld before what is held, and the one hold routed to duty 10 with its
// reason, are
// ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md
// (C2063, C2064, C2075);
//
// [Gate.Acknowledge] appending the decision row and calling the notifier is
// ../../end-goal/how-the-factory-works/08-operations/07-pages.md (C2116);
//
// the mismatch hold paging nobody, with the revert decision paging, is
// ../../end-goal/how-the-factory-works/08-operations/08-drift-detection.md
// (C2179).
package gate
