// Package policy is Factory — the one component every authored value and every
// safeguard goes through — and the read of the value in force.
//
// factory.go is [Factory] and [NewFactory]: [Factory.Install], one Author call
// per parameter, [Factory.AddSafeguard] and [Factory.WithdrawSafeguard], each
// refusing an actor that is not a human with [ErrNotAnOwner]. Every one writes
// through the package that owns the record and appends a [Version] in the same
// transaction, so one component holds the whole of duty 8 and the records keep
// one writer each.
//
// version.go is [Version] with its [Action] and [Subject] and the reads
// [InForce], [Get] and [All]. A version names the write and not the value,
// because the value is the field on the record the subject names and one fact in
// two places could disagree. schema.go is [Table], [IDPrefix], [DDL] and
// [AdvisoryLockKey]: the table is append-only, a unique constraint on the
// predecessor refuses a forked sequence, and the lock is taken for the read of
// the version in force and the append that supersedes it, so a second writer
// waits rather than producing one.
//
// reader.go is [Reader], [NewReader] and [Subjects], the records a read is
// performed against. A [Reader] holds one [score.Version] rather than reading
// the newest at each answer, so every value one gate firing reads comes from the
// version its own decision row names. effective.go is [Effective], one parameter
// as it is in force: what an owner authored, what the score supplies where the
// field is empty, and a safeguard clamping the result, in that order. source.go
// is [Source] — [FromAuthored], [FromSupplied], [FromNothing], and [FromFactory]
// for the list of allowed predicate kinds, the one parameter with a fourth read
// under the other three.
//
// gate.go is [Reader.AtGate] and [Applied], what a gate firing writes onto its
// open event, carrying both the threshold and whether a safeguard adds a human.
// window.go is [Reader.WindowParameters] and [Window]; attemptlimit.go and
// allowedpredicatekinds.go are the two parameters with a read of their own; and
// all.go is [Reader.All], every parameter in the order gate policy's table lists
// the rows, each saying what reads it.
//
// safeguardpredicate.go is [Reader.SafeguardPredicatesOn] and
// [SafeguardPredicate], which resolve to assertions on one contract element
// rather than to a value. They are here because package safeguard has one reader
// and this is it.
//
// Who may write what: this package owns the policy version table and appends to
// it. Every other write it makes is a call into the package that owns the
// record, inside its own transaction.
//
// What defines it: the eleven rows, the scope of each, the score supplying what
// an owner does not, a safeguard being a bound, and Factory as the writer are
// ../../end-goal/how-the-factory-works/09-gate-policy/README.md. The policy version on
// every decision is ../../end-goal/what-the-factory-does/02-traceability.md.
package policy
