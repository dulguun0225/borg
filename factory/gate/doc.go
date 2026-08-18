// Package gate is the gate component: it fires the one gate row M1 builds —
// Merge to master — and writes that firing's decision into the decision log
// through [decisionlog.Writer]. The other seven gate rows are not built until
// later milestones, so an item reaches this one having passed through stages
// that fire nothing.
//
// # The decision is two rows
//
// [Gate.Fire] appends the opening row when the gate fires, and [Gate.Decide]
// appends the closing row when the verdict is given, naming the row it
// closes. What forces the write at the firing is the factor vector: a human
// is meant to argue with the score's number before deciding, so the vector
// has to exist while they are deciding, and it cannot be computed when they
// give the verdict because the score version moves as outcomes arrive. The
// opening row is written as the component gate.merge_to_master; the closing
// row is written as the deciding human, who is its actor. Both rows are
// written against the item and the build and never against the release — one
// rule for all eight rows, so no reader has to know which side of the merge a
// gate is on.
//
// # The score behind the gate
//
// [Score] is the seam the real score replaces at M2, and [Stub] is the score
// until then. The stub computes no factor, so every factor of its vector is
// marked unavailable with the reason written on it, and its answer is always
// that a human decides: a factor the score cannot compute resolves to the
// value that puts a human at the gate, and the vector records which factor
// was unavailable and why rather than leaving a gap a reader has to
// interpret. [PolicyVersion] is the same arrangement for the policy an
// opening row requires — gate policy is authored from M2, and the constant's
// name says nothing was authored.
//
// Who may write what: this package owns no table. It appends into the
// decision log through [decisionlog.Writer], which owns that table, and it
// writes nowhere else.
//
// What defines it: the two-row decision, what the opening row names, and the
// item-and-build rule are
// ../../end-goal/how-humans-do-it/03-gates.md#where-a-gate-is-and-what-decides-it.
// The row this component fires, its two actions, and the human at it
// performing UAT are ../../end-goal/how-humans-do-it/03-gates.md#merge-to-master.
// The vector, its three factor groups, and what an uncomputable factor
// resolves to are ../../end-goal/how-humans-do-it/04-risk-score.md.
package gate
