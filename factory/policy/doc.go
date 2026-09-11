// Package policy is Factory — the one component every authored value, every
// safeguard, every halt, every legal hold and every redaction goes through —
// and the read of the value in force.
//
// # The code
//
// factory.go is [Factory] and [NewFactory], the [Created] ids Factory mints for
// a write that creates a record, and the one write path every method below
// takes: the version in force is read, the write's key derived, and the version
// and the record write put in one fenced transaction, the version first. A
// write whose key is the one the version in force already carries writes
// nothing, a creation included — which is what the minted id is for, a record
// writer that chose its own id being unable to hand it to a version already
// appended. [Factory.Declaration],
// [Factory.AutoPassRates] and [Factory.Removal] are the three functions the
// composition supplies, for what this package may not read and may not reach —
// a retirement through a factory composed with no deployer is [ErrNoDeployer].
// AutoPassRates is required at construction: [NewFactory] takes it as an
// argument and panics on a nil one, a threshold write with none composed
// otherwise freezing no rate in silence. Declaration and Removal stay unset
// until the composition assigns them.
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
// environment, writes no record here and appends a version that authors
// nothing,
// [Factory.WithdrawEnvironment], [Factory.SetMaxConcurrentCandidateEnvironments],
// [Factory.AuthorStrategyDefault] and [Factory.DeclareArea].
// parameters.go is the thirteen parameters of gate policy's eleven rows, one
// Author call each, with [Factory.ConfirmGateThreshold] and
// [Factory.ConfirmRolePromptOrSkillThreshold] beside the two threshold
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
// admission.go is [Admissions] with [Reader.ReportStoreAdmissions], the two
// safeguards drawn on the report store read in force.
// stop.go is the halt and the legal hold with the same three calls each.
// redaction.go is [Factory.WriteRedaction], the record one erasure writes:
// the erasure-list row it names is appended before the call is made, so the
// key redaction.Key derives is what a step taken again is recognised by —
// this write finds the record the first performance wrote and appends
// nothing. It is refused while a legal hold reaches the target, half of that
// reading being the caller's, and this same call records the refusal: a
// version naming the target and why, with no erasure-list row and no
// redaction record, which is the one write here that performs nothing.
// [Factory.RecordRedactionRefusal] is the same write for a caller whose own,
// narrower reading refuses an erasure before any erasure-list row has landed —
// [Factory.WriteRedaction] cannot make it there, because calling it would
// perform the redaction that row is meant to precede. people.go is
// [Factory.AppendPeopleVersion], the append a write at People calls for.
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
// what it names, and appends no version. Every field a version names is
// re-derived. A write that sets a second value beside the first — the objective
// and its period, the paging hours, the operation cap and its overflow, the
// search budget's two numbers, the page cap and its interval — names both on
// the version, the first as the number and the rest as the list, so the pair is
// written together: one number of a pair written alone would leave the record
// in a state its own CHECK refuses. The People declaration a version names is
// re-derived by package people, the direction between the two being People to
// here.
//
// reader.go is [Reader], [NewReader] and [Subjects], the records a read is
// performed against. A [Reader] holds one [score.Version] rather than reading
// the newest at each answer, so every value one gate firing reads comes from
// the version its own decision row names. effective.go is [Effective], one
// parameter as it is in force: what an owner authored, the value the design
// fixes where the field is empty, what the score supplies where the design
// fixes none, and the newest safeguard on each subject clamping the result, in
// that order. Decision-log retention takes one read more, the retention floor
// no authored value and no safeguard may take it under. source.go is [Source] —
// [FromAuthored], [FromSupplied], [FromNothing], and [FromFactory] for a value
// the design fixes rather than has the score supply.
//
// gate.go is [Reader.AtGate], [RolePromptOrSkillRow] and [Applied], what a gate
// firing writes onto its open event, carrying the threshold — read from the
// environment record per row, or from the factory-wide settings record at the
// one row with no environment — whether a safeguard adds a human, and the
// score version in force at that row. Which environment record is the row's is
// read off the record's own kind: a deploy row into a persistent environment
// reads the environment it deploys into and every other row reads production's,
// a candidate's own environment being created at the gate that decides its
// deploy and so unable to hold the threshold that decides it. The policy
// version is named for the trail and never what the threshold is read from,
// and [Reader.currentVersionID] is what names it: a read of the factory-wide
// settings record's own field, [factorysettings.SetCurrentPolicyVersionID]
// being what [Factory.append] writes it with in the same transaction as every
// version — the version below is the copy the audit trail keeps, and never
// what this reads, so no gate reads the log to fire. A version is still what
// that name can point at: [Factory.Install] guarantees one stands before any
// firing, every path able to fire a gate installing first, so
// [Reader.AtGate] refuses [ErrNoVersion] rather than naming none. The
// score version in force at the row is the newest where nobody authored a
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
// an owner does not, a safeguard being a bound and the newest record for a
// subject being the one in force, the version as a row of the
// log, the order of the two writes and the re-derivation at the start are
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md
// (C2199, C2200, C2201, C2202, C2203, C2206, C2207, C2208, C2209, C2210, C2213,
// C2216, C2217, C2218, C2219, C2228, C2229, C2233, C2234, C2235, C2236, C2237,
// C2239, C2240, C2241, C2242, C2243, C2244, C2245, C2246, C2247, C2248, C2249,
// C2250, C2251, C2252, C2253, C2255, C2256, C2257, C2258, C2259, C2260, C2261,
// C2262, C2263, C2264, C2265, C2266, C2269, C2270, C2273).
//
// The parameters are
// ../../end-goal/how-the-factory-works/09-gate-policy/01-what-is-in-it.md
// (C2187, C2188, C2189, C2190, C2191, C2192, C2193, C2195, C2196, C2197,
// C2198).
//
// What is authored beside them, retention, and the gate row that decides a
// shortening are
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/README.md
// (C2328).
//
// The redaction, and the safeguard whose subject is the report store with each
// of the two waits an owner adds a human with, are
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/02-reports.md
// (C0438, C0439, C0445).
//
// The legal hold is
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/03-a-legal-hold.md
// (C2322, C2325, C2326), and the halt is
// ../../end-goal/how-the-factory-works/09-gate-policy/04-stopping-the-factory.md
// (C2332, C2334, C2335, C2336, C2350, C2351, C2352).
//
// A service's retirement, a project's end, and the removal performed for one
// environment are
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/04-retirement.md
// (C0697, C0698, C0700, C0707, C0708).
//
// The policy version on every decision is
// ../../end-goal/what-the-factory-does/02-traceability.md (C2908, C2909,
// C2910), and the shapes the log holds are ../../end-goal/deferred.md (C0050,
// C0053, C0065, C0072, C0090, C0091, C0093, C0105, C0110, C0121). Every owner
// write at Factory and the write at People are
// ../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md
// (C2572, C2612, C2614, C2615, C2618, C2619, C2643, C2663, C2664, C2665, C2666,
// C2668).
//
// The policy version freezing the realized rate is
// ../../end-goal/how-the-factory-works/04-risk-score/01-factors-at-least.md
// (C1293).
//
// [Factory.Rederive] rewriting fields the newest version names is
// ../../end-goal/one-process.md (C2766).
package policy
