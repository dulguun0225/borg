// Package gate is the gate component: it fires a gate row, asks the score and
// the policy what applies, and writes that firing's decision into the decision
// log through [decisionlog.Writer].
//
// row.go is the vocabulary of a row. [Row] and the four [Rows] —
// [Decomposition], [DeployToCandidateEnvironment], [MergeToMaster],
// [DeployToProduction] — [Verdict] and its three values, [Actions] for what each
// row offers, [WaitsOn] and [ReturnsTo], the five holds
// ([HoldDependencyNotLive], [HoldNoRoomForAnotherEnvironment],
// [HoldWindowLimitReached], [HoldRollbackAwaitingRevert] and
// [HoldDriftMismatch]), and the two refusals [ErrEditInPlaceRefused] and
// [ErrStrategySafeguardRefused]. The hold names are constants here, and four of
// the five are computed by the caller, so that a caller cannot report one under
// a name of its own.
//
// gate.go is [Gate] and [New], composed with [Score], [Policy] and
// [DriftDetector], plus the errors every call shares and [Component], the actor
// a row's open event is written as. The mismatch is asked of an interface
// rather than read from a table here, because that store is not the factory's
// and a gate importing the package owning it would hold a second pool;
// [NoDriftDetector] is what a factory with none installed is composed with.
//
// The decision is two rows. fire.go appends the open event: [Gate.Fire] takes a
// [Firing] carrying its [CriterionResult]s, writes [OpeningPayload], and returns
// [Opened] — the [policy.Applied] threshold, whether a human decides and
// [Opened.WhyHuman] which of the three reasons put them there, [Opened.HeldOut]
// with [Opened.WhyHeldOut], and [Opened.Mismatch]. set.go is [Gate.FireSet],
// which decides over a [SetFiring] of [SetMember]s with [SetOpeningPayload] and
// [NoBuildAtDecomposition]: Decomposition is the one row that decides over a set
// rather than over one item's build, and it applies the riskiest member's
// number. reason.go is [WhyOverThreshold], [WhySafeguard], [WhyBoth] and
// [WhyMismatch].
//
// verdict.go appends the close event, naming the row it closes. [Gate.Decide]
// takes a human's verdict; [Gate.AutoPass] is the factory approving and refuses
// a firing that put a human at the row; [Gate.AutoReject] is the factory
// rejecting on one of [AutoRejectedByContractDiff],
// [AutoRejectedByConsumerContract] and [AutoRejectedBySafeguardPredicate], and
// is allowed whatever the firing decided about a human. [ClosingPayload] is that
// event's shape. approval.go is [ApprovalTimes], the read of when each item's
// merge approval closed.
//
// Who may write what: this package owns no table. It appends into the decision
// log through [decisionlog.Writer], which owns that table, and it writes nowhere
// else — a gate decides an event and edits nothing.
//
// What defines it: the two-row decision, what the open event names, the
// item-and-build rule, and what puts a human at a row are
// ../../end-goal/how-the-factory-works/03-gates/01-where-a-gate-is-and-what-decides-it.md.
// The actions per row are
// ../../end-goal/how-the-factory-works/03-gates/03-actions-at-each-gate.md, and the
// kinds of hold are
// ../../end-goal/how-the-factory-works/03-gates/04-what-a-gate-may-change.md. The
// three rows below Decomposition are
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/06-deploy-to-candidate-environment.md,
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/07-merge-to-master.md
// — which is also where the merge row's checks reject on their own terms before a
// verdict is given — and
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/08-deploy-to-production.md.
// The vector, the number, and the held-out sample are
// ../../end-goal/how-the-factory-works/04-risk-score/README.md; the threshold and the
// safeguard are
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md.
// What Decomposition decides, one verdict covering the whole decomposition, and
// a rejection re-decomposing the set are
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md.
package gate
