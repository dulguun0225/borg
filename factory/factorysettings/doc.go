// Package factorysettings owns the factory-wide settings record: the parameters an
// owner authors that no customer record's scope reaches.
//
// Three things are on it, and each is here for the same reason — the mechanism
// it limits is the factory's own and reaches every project at once. The attempt
// limit is per stage and the stages are the factory's; the list of allowed
// predicate kinds is one list the factory owns and an owner extends; and the
// threshold the role-prompt-or-skill gate row reads is a field of it because
// that row has no project and so no production environment to read. The record
// exists before any project does and an owner may never open it: where a field
// is unauthored the value in force is what the score supplies.
//
// There is one row. The store enforces that with a constant column and a
// unique constraint on it, rather than leaving it to whichever caller creates
// the record first.
//
// # What is not here
//
// Report retention and decision-log retention are fields of this record in the
// design and are not built: they are authored and factory-wide but are not gate
// policy, and nothing at this milestone keeps or deletes a report or a log row,
// so a field for either would be one nothing writes and nothing reads.
//
// Who may write what: [Writer.Ensure] creates the record, as Factory. The two
// authoring calls — [SetAttemptLimit] and [SetAllowedPredicateKinds], with
// [SetRolePromptOrSkillThreshold] beside them — take a transaction and are called by
// package policy inside the one that appends the policy version, so the field
// and the version commit together or not at all.
//
// What defines it:
// ../../end-goal/how-humans-do-it/09-gate-policy.md#one-shape-across-all-of-them,
// which sets what shares this record and why the two that share it could not go
// on production's environment record; the limit itself is
// ../../end-goal/how-humans-do-it/03-gates.md#the-attempt-limit.
package factorysettings
