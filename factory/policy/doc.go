// Package policy is Factory — the one component every authored value, every
// safeguard, every halt and every legal hold goes through — and the read of the
// value in force.
//
// # The code
//
// factory.go is [Factory] and [NewFactory], the [Created] a write that mints a
// record hands its version, and the one write path every method below takes:
// the version in force is read, the write's key derived, and the version and
// the record write put in one fenced transaction. A write whose key is the one
// the version in force already carries writes nothing. [Factory.Declaration],
// [Factory.AutoPassRates] and [Factory.Removal] are the three functions the
// composition supplies, for what this package may not read and may not reach —
// a retirement through a factory composed with no deployer is [ErrNoDeployer].
//
// version.go is [Version] — a row of the decision log and no table of this
// package's — with [Caller], [Action], [Scope], [AuthoredValue],
// [AutoPassRate], [DeclarationSnapshot], the payload it serialises to, and the
// key a repeated write derives again. versions.go is the reads:
// [Reader.Versions], [Reader.Newest], [Reader.Version] and
// [Reader.AuthoredAutoPassRate], each of them a read of the log through
// [decisionlog.Reader]. lock.go is [AdvisoryLockKey].
//
// records.go is the records Factory creates: [Factory.Install],
// [Factory.CreateProject], which writes production's environment in the same
// event, [Factory.EndProject], which ends the two in one write once every
// service in the project is retired, [Factory.CreateEnvironment],
// [Factory.RemoveFromEnvironment], which performs the deployer's removal for one
// environment and writes no record here,
// [Factory.WithdrawEnvironment], [Factory.SetMaxConcurrentCandidateEnvironments],
// [Factory.AuthorStrategyDefault] and [Factory.DeclareArea].
// parameters.go is the thirteen parameters of gate policy's eleven rows, one
// Author call each, with [Factory.ConfirmGateThreshold] beside the two threshold
// writes: a threshold write names the score version in force at it, and the
// confirmation names one without moving the number. Both are what
// [score.InForceAt] reads back. settings.go is what an owner authors on the factory-wide
// settings record beside them — the retentions, the report channel's rates, the
// remediation period, the harm mark's cap, and seam 5 — with
// [Factory.AuthorDecisionLogRetention] refusing a shortening
// ([ErrShorteningIsDecided]), [Factory.WriteRetentionShortening] writing that
// value pending, and [Factory.ApproveRetentionShortening] putting the pending
// one in force at the close of the row that decided it. service.go is what an owner writes on a service record beside the
// eleven — [Factory.MarkServiceProvisioned] and [Factory.RetireService], and an
// Author call for each of the twelve the design names there and the values
// authored beside them. safeguards.go is [Factory.AddSafeguard], which is also
// where a safeguard on the explicit threshold writes the number and the size
// beside it onto the service record, and
// [Factory.WriteSafeguardWithdrawal] and [Factory.ApproveSafeguardWithdrawal].
// stop.go is the halt and the legal hold with the same three calls each.
// people.go is [Factory.AppendPeopleVersion], the append a write at People
// calls for.
//
// Four of those writes are decided at a gate row rather than authored — the
// three withdrawals' approvals and the shortening of decision-log retention —
// and each takes the close event of that row, refusing a call that names none
// with [ErrNotDecidedAtARow] and naming it on the version as [Version.Decision].
// This package fires no row: package gate does, and its caller hands the close
// event here.
//
// rederive.go is [Factory.Rederive] and [Rederived]: the factory's start
// rewrites every authored field the newest version names that does not hold
// what it names, and appends no version. It re-derives the values whose
// parameter package gatepolicy names; a field a version names by key and no
// parameter is left as it stands. A write that sets a second value beside the
// first — the objective and its period, the paging hours, the operation cap and
// its overflow, the search budget's two numbers, and a change freeze period —
// is one of those: re-deriving one number of a pair would leave the record in a
// state its own CHECK refuses.
//
// reader.go is [Reader], [NewReader] and [Subjects], the records a read is
// performed against. A [Reader] holds one [score.Version] rather than reading
// the newest at each answer, so every value one gate firing reads comes from
// the version its own decision row names. effective.go is [Effective], one
// parameter as it is in force: what an owner authored, what the score supplies
// where the field is empty, and every safeguard reaching the subject clamping
// the result, in that order. source.go is [Source] — [FromAuthored],
// [FromSupplied], [FromNothing], and [FromFactory] for the list of allowed
// predicate kinds, the one parameter with a fourth read under the other three.
//
// gate.go is [Reader.AtGate], [RolePromptOrSkillRow] and [Applied], what a gate
// firing writes onto its open event, carrying the threshold — read from the
// environment record per row, or from the factory-wide settings record at the
// one row with no environment — whether a safeguard adds a human, and the
// score version in force at that row — the newest where nobody authored a
// threshold there, and the last one confirmed at the scope where somebody did.
// A firing computes its vector under [Applied.ScoreVersion], package gate
// reading that version back and assessing under it, so the vector, the number
// and the version a decision names are one version's. gatereads.go is the three parameters a gate reads beside it,
// [Reader.HeldOutSampleRate], [Reader.ReviewSampleRate] and
// [Reader.ExposureBound].
// window.go is [Reader.WindowParameters] and [Window]; attemptlimit.go and
// allowedpredicatekinds.go are the two parameters with a read of their own; and
// all.go is [Reader.All], every parameter in the order gate policy's table
// lists the rows, each saying what reads it, with [Reader.InForce] beside it:
// one parameter, whichever of package gatepolicy's three lists holds it, which
// is how a value authored and not among the eleven is read.
//
// safeguardpredicate.go is [Reader.SafeguardPredicatesOn] and
// [SafeguardPredicate], which resolve to assertions on one contract element
// rather than to a value. They are here because package safeguard has one
// reader and this is it.
//
// # Departures from the shape
//
// This package owns no table, so it has no schema.go and no db_test.go of a
// record's shape: the policy version is a row of the decision log, and every
// other record it writes belongs to the package that owns it.
//
// Who may write what: every write here is a call into the package that owns the
// record, inside the transaction that appends the version. The version itself
// is appended by the log's own writer, this package being one of its callers.
//
// What defines it: the eleven rows, the scope of each, the score supplying what
// an owner does not, a safeguard being a bound, the version as a row of the log,
// the order of the two writes and the re-derivation at the start are
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md.
// The parameters are
// ../../end-goal/how-the-factory-works/09-gate-policy/01-what-is-in-it.md.
// What is authored beside them, retention, and the gate row that decides a
// shortening are
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/README.md.
// The legal hold is
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/03-a-legal-hold.md,
// and the halt is
// ../../end-goal/how-the-factory-works/09-gate-policy/04-stopping-the-factory.md.
// A service's retirement, a project's end, and the removal performed for one
// environment are
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/04-retirement.md.
// The policy version on every decision is
// ../../end-goal/what-the-factory-does/02-traceability.md, and the shapes the
// log holds are ../../end-goal/deferred.md. Every owner write at Factory and
// the write at People are
// ../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md.
package policy
