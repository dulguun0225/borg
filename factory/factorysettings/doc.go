// Package factorysettings owns the factory-wide settings record: the attempt
// limit per stage, the list of allowed predicate kinds, and the threshold the
// role-prompt-or-skill gate row reads.
//
// schema.go is the two tables. [Table] is the record and [LimitTable] holds one
// row per stage an owner authored a limit for. There is one settings row, and
// the store enforces that with a constant column and a unique constraint on it
// rather than leaving it to whichever caller creates the record first.
//
// writer.go is [Settings], the record as it is stored; [Writer] and
// [Writer.Ensure], which creates it with nothing authored and is idempotent on
// that constraint; the three authoring calls [SetAttemptLimit],
// [SetAllowedPredicateKinds] and [SetRolePromptOrSkillThreshold], each taking a
// transaction; and the two reads, [Get] for the record and [AttemptLimit] for
// one stage's limit as a [gatepolicy.Authored]. An unauthored field is null or
// empty rather than zero, so what stands in its place is what the score
// supplies.
//
// Who may write what: [Writer.Ensure] creates the record, as Factory. The three
// authoring calls are called by package policy inside the transaction that
// appends the policy version, so the field and the version commit together or
// not at all.
//
// What defines it: what shares this record is
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md;
// the attempt limit itself is
// ../../end-goal/how-the-factory-works/03-gates/05-the-attempt-limit.md.
package factorysettings
