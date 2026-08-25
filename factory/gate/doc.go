// Package gate is the gate component: it fires a gate row, asks the score and
// the policy what applies, and writes that firing's decision into the decision
// log through [decisionlog.Writer].
//
// # The four rows
//
// [Decomposition], [DeployToCandidateEnvironment], [MergeToMaster], and
// [DeployToProduction]. Decomposition is the stage's own row and the one that
// decides over a set rather than over one item's build, which is why it is fired
// through [Gate.FireSet] and why set.go is a file of its own. The
// candidate deploy row is where the candidate's own environment is created and
// its build is put on it, and what the deploy provides is the criteria — nothing
// else attaches to that row. The merge row is where a candidate becomes a
// numbered release and where the verdict on the candidate is given. The production
// deploy row is where the merge has happened and the number is already assigned,
// so hold is the only way to stop it.
//
// Three of the four authoring rows of the default path are still not built: each
// needs a stage that fires one, and an item reaches the deploy and merge rows
// having passed through stages that fire nothing. Decomposition is the first of
// them, and it arrives because a decomposition that yields more than one item arrived — the
// row fires where there is a set to ratify and nowhere else.
//
// All three read their threshold from production's environment record, which is
// the rule the design gives for all eight rows — and a candidate's own environment
// could not hold it anyway, being created at the approval of the row that decides
// its deploy.
//
// [Actions] is what may be done at each, and it differs by row: reject is available up
// to the merge to master and nowhere after it, and hold is offered by the deploy rows
// alone. Two third actions are refused with their reasons. Safeguard the strategy, the
// production deploy row's, is refused because a target that runs a release as a local
// process moves a process rather than traffic, so the strategy that keeps a control is
// unavailable on this substrate and every deploy goes without a control. Edit in place,
// Decomposition's, is refused because editing a set in place is a human re-decomposing
// by hand and re-decomposing is not built — [ErrEditInPlaceRefused] says what that
// leaves.
//
// # The decision is two rows
//
// [Gate.Fire] appends the open event when the gate fires, and [Gate.Decide] or
// [Gate.AutoPass] appends the close event when the verdict is given, naming the
// row it closes. What forces the write at the firing is the factor vector: a
// human is meant to argue with the score's number before deciding, so the vector
// has to exist while they are deciding, and it cannot be computed at the verdict
// because the score version moves as outcomes arrive. The open event is written
// as the component gate.<row>; the close event is written as the deciding human
// where a human decides and as the gate component where the factory decides for
// itself. Both rows are written against the item and the build and never against
// the release — one rule for both rows, so no reader has to know which side of
// the merge a gate is on.
//
// The open event names the values actually applied: the threshold in force,
// whether an owner authored it or the score supplied it, and the safeguards that
// reached the firing. That is what makes a decision readable against the policy
// it was taken under rather than against today's, which the policy version alone
// cannot give.
//
// # What decides whether a human decides
//
// Three things, and any of them is enough. The number the score reduced the
// vector to is at or above the threshold in force; a safeguard adds a human at
// the row; or, at the production deploy row alone, the drift detector found
// a record disagreeing with what runs. None of them removes a human another put
// there — a safeguard can only add, and clearing a mismatch would not lift a
// human the number put at the row.
//
// One thing removes a human, and it removes exactly one: the score's own
// held-out sample, asked through [Score.HoldOut] after the policy has answered.
// Where the score selected this item, the human the number would have put at
// the row is not there, the close event says the sample and not the threshold
// passed it, and [Opened.HeldOut] is on the open event from that firing
// onward. A safeguard's human and a mismatch's human stand: the sample is the
// score holding itself out of its own gate, and it is asked with the
// safeguard's answer so that it cannot pass one. That is the one mechanism in
// the design that takes a human off a row, and what makes it legitimate is
// whose human it is. [Opened.WhyHuman] says which, because a firing that reads
// the same to an owner for two different reasons is one they cannot argue with,
// and [Opened.Mismatch] says what disagreed, because a human approving through
// one is saying the record is wrong and the deploy should proceed anyway.
//
// The mismatch is asked of [DriftDetector] rather than read from a table here: that
// store is not the factory's and no factory component may write it, so a gate that
// imported the package owning it would be a gate holding a second pool.
// [NoDriftDetector] is what a factory with none installed is composed with, and it
// answers no mismatch ever — which is the state the design describes as a factory
// whose every check reads a record the factory itself wrote.
//
// # The factory's own two verdicts, and the one asymmetry between them
//
// [Gate.AutoPass] is the factory approving where the firing put no human at the
// row, and it refuses a firing that did. [Gate.AutoReject] is the factory
// rejecting where a mechanical check failed, and it is allowed whatever the firing
// decided about a human. That asymmetry is the whole difference between them: the
// factory may not approve over a human, because nothing in the design removes a
// human from a gate; and it rejects before a human is asked, because the design
// has the merge row's checks — the acceptance criteria, every consumer contract,
// and the producer's own contract diff — each rejecting on its own terms
// before anyone gives a verdict. A human who was going to approve is not
// overruled: there is nothing left to approve, and a schema diff is not a judgment
// they could have made differently.
//
// The three checks that reject that way are named here —
// [AutoRejectedByContractDiff], [AutoRejectedByConsumerContract], and
// [AutoRejectedBySafeguardPredicate] — so a caller cannot report one under a name
// of its own, the arrangement the five holds already have. What computes each of
// them reads the contracts and the consumer contracts, which this package does not
// import. The acceptance criteria are not among them: a failing criterion is
// reported to the row and read by whoever gives the verdict, which is how it has
// worked since M2, and turning it into a mechanical rejection would change a
// milestone already built.
//
// A hold is the third kind of verdict and the only one that decides nothing: it
// leaves the event queued with the change still good, counts no attempt against
// the limit, and teaches the score nothing — which is what separates it from a
// reject, and why the score's own reader of outcomes ignores it.
//
// The factory's own hold is not a verdict, and four of the five are not fired
// here either. This package owns the vocabulary of all five —
// [HoldDependencyNotLive], [HoldNoRoomForAnotherEnvironment], [HoldWindowLimitReached],
// [HoldRollbackAwaitingRevert], and [HoldDriftMismatch] — so a caller cannot
// report one under a name of its own, and computing four of them is the caller's:
// they read the item's declared dependencies, the deploy records of their
// services, the service's open windows, and its newest rollback, none of which
// this package imports.
//
// The five split on one line, and it is not which record they read. Four lift
// themselves — a dependency becomes current, an environment is freed, a window
// closes, a revert ships — so the deploy waits, nothing is decided, and the next
// firing recomputes; a gate fired for one of them would ask a human to approve
// through something the factory is about to clear. [HoldDriftMismatch] does
// not lift itself, because every remedy the factory has reads the record in
// question. That is the one this package fires: it puts a human at the row, and it
// is what the notifier pages about.
//
// Of the four, only [HoldNoRoomForAnotherEnvironment] is written anywhere. The
// other three are computed from records that already exist, and the design gives
// such a hold no row of its own — a record for it would be a decision where
// nothing is decided. What that costs is that how long the factory has been
// holding is answerable for the written one alone.
//
// Who may write what: this package owns no table. It appends into the decision
// log through [decisionlog.Writer], which owns that table, and it writes nowhere
// else — a gate decides an event and edits nothing.
//
// What defines it: the two-row decision, what the open event names, and the
// item-and-build rule are
// ../../end-goal/how-humans-do-it/03-gates/01-where-a-gate-is-and-what-decides-it.md.
// The actions per row are
// ../../end-goal/how-humans-do-it/03-gates/03-actions-at-each-gate.md, the three
// kinds of hold are
// ../../end-goal/how-humans-do-it/03-gates/04-what-a-gate-may-change.md, and the
// the three rows themselves are
// ../../end-goal/how-humans-do-it/03-gates/07-what-particular-gates-decide/06-deploy-to-candidate-environment.md,
// ../../end-goal/how-humans-do-it/03-gates/07-what-particular-gates-decide/07-merge-to-master.md, and
// ../../end-goal/how-humans-do-it/03-gates/07-what-particular-gates-decide/08-deploy-to-production.md. The vector
// and the number are ../../end-goal/how-humans-do-it/04-risk-score/README.md; the
// threshold and the safeguard are
// ../../end-goal/how-humans-do-it/09-gate-policy/02-one-shape-across-all-of-them.md.
// What Decomposition decides, one verdict covering the whole decomposition, and
// a rejection re-decomposing the set rather than sending an item back are
// ../../end-goal/how-humans-do-it/02-intent-into-items/03-decomposition/README.md; the
// merge row's checks rejecting on their own terms before a verdict is given are
// ../../end-goal/how-humans-do-it/03-gates/07-what-particular-gates-decide/07-merge-to-master.md.
package gate
