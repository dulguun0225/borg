// Package gate is the gate component: it fires a gate row, asks the score and
// the policy what applies, and writes that firing's decision into the decision
// log through [decisionlog.Writer].
//
// # The two rows
//
// [MergeToMaster] and [DeployToProduction]. The merge row is where a candidate
// becomes a numbered release and where the verdict on the candidate is given;
// the production deploy row is where hold is an action, the merge having
// happened and the number already assigned, so hold is the only way to stop it.
// The other six rows of the default path are not built: the candidate deploy row
// needs the environment per candidate, and the four authoring rows need stages
// that fire one. An item reaches these two having passed through stages that
// fire nothing.
//
// [Actions] is what may be done at each, and it differs by row: reject is
// available up to the merge to master and nowhere after it, and hold is offered
// by the deploy rows alone. Pin strategy, the production deploy row's third
// action in the design, is refused with its reason — a target that runs a
// release as a local process moves a process rather than traffic, so the
// strategy that keeps a control is unavailable on this substrate and every
// deploy is straight.
//
// # The decision is two rows
//
// [Gate.Fire] appends the opening row when the gate fires, and [Gate.Decide] or
// [Gate.AutoPass] appends the closing row when the verdict is given, naming the
// row it closes. What forces the write at the firing is the factor vector: a
// human is meant to argue with the score's number before deciding, so the vector
// has to exist while they are deciding, and it cannot be computed at the verdict
// because the score version moves as outcomes arrive. The opening row is written
// as the component gate.<row>; the closing row is written as the deciding human
// where a human decides and as the gate component where the factory decides for
// itself. Both rows are written against the item and the build and never against
// the release — one rule for both rows, so no reader has to know which side of
// the merge a gate is on.
//
// The opening row names the values actually applied: the threshold in force,
// whether an owner authored it or the score supplied it, and the pins that
// reached the firing. That is what makes a decision readable against the policy
// it was taken under rather than against today's, which the policy version alone
// cannot give.
//
// # What decides whether a human decides
//
// Two things, and either is enough. The number the score reduced the vector to
// is at or above the threshold in force, or a pin adds a human at the row. A pin
// can only add: it never removes a human the number put there. [Opened.WhyHuman]
// says which, because a firing that reads the same to an owner for two different
// reasons is one they cannot argue with.
//
// A hold is the third kind of verdict and the only one that decides nothing: it
// leaves the event queued with the change still good, counts no attempt against
// the bound, and teaches the score nothing — which is what separates it from a
// reject, and why the score's own reader of outcomes ignores it. The factory's
// own hold over a record that already exists is not built here: an open window, a
// dependency that is not current, a rollback whose revert has not shipped, and a
// reconciler mismatch are records later milestones write, and none of them writes
// a decision anyway.
//
// Who may write what: this package owns no table. It appends into the decision
// log through [decisionlog.Writer], which owns that table, and it writes nowhere
// else — a gate decides an event and edits nothing.
//
// What defines it: the two-row decision, what the opening row names, and the
// item-and-build rule are
// ../../end-goal/how-humans-do-it/03-gates.md#where-a-gate-is-and-what-decides-it.
// The actions per row are
// ../../end-goal/how-humans-do-it/03-gates.md#actions-at-each-gate, the three
// kinds of hold are
// ../../end-goal/how-humans-do-it/03-gates.md#what-a-gate-may-change, and the
// two rows themselves are
// ../../end-goal/how-humans-do-it/03-gates.md#merge-to-master and
// ../../end-goal/how-humans-do-it/03-gates.md#deploy-to-production. The vector
// and the number are ../../end-goal/how-humans-do-it/04-risk-score.md; the
// threshold and the pin are
// ../../end-goal/how-humans-do-it/09-gate-policy.md#one-shape-across-all-of-them.
package gate
