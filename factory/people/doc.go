// Package people owns the People declaration: which of the owner's twelve
// duties each per-person key holds, the named obligation a key holds outside
// the twelve, and the credentials a key lent the factory with the account
// kind, the spend ceiling and the rates on each. From the first record the
// identity is the key and never a name — [Mapping] is the one place a key
// maps to a name, kept outside the chain so an erasure can delete it alone.
//
// identity.go holds the vocabulary: a row names a [Duty] or an [Obligation]
// and never both, which is what [Holding] carries — [OfDuty] and
// [OfObligation] compose one. [Duties] is the twelve and [Obligations] the
// three: hosting the factory, installing the drift detector, and composing
// the fleet. schema.go holds [Table], [MappingTable], [CredentialTable],
// [RateTable], their id prefixes and [DDL].
//
// The mapping holds a key and a name and nothing else: the hours a service
// pages within are a field of the service record and a wait naming no service
// pages at any hour, so nothing per human is kept here.
//
// read.go holds [Declaration] and [Declaration.Holds], and the reads that
// take a pool and no [Writer]: [Get], [ByHolding], [Holders] — the
// notifier's read, returning keys — and [All], what the command-line interface
// prints. mapping.go holds [Mapping], [WriteMapping], [DeleteMapping],
// [Replay], what a restore is served through, [GetMapping], [NameOf], the read
// every screen and every page event resolves a key through, and [KeyNamed],
// the same read the other way for a caller handed a name and holding no key.
//
// credential.go holds the lent credential: [AccountKind] with
// [AccountKinds], [PeriodUnit] with [PeriodUnits], [Ceiling] with
// [Ceiling.PeriodStartAt] — the start of the period a time falls in, derived
// from the length and the start date rather than held on any record —
// [Credential], the reads [CredentialNamed] and [Credentials], and the writes
// [Writer.Lend], [Writer.AuthorCeiling] and [Writer.TakeBack]. rate.go holds
// [Rate], [Writer.AuthorRate], [RatesFor], [AllRates], [RateFor] and
// [Convert], the converted amount a run's units come to at the rates
// authored, absent where a kind the run returned has none.
//
// declare.go holds [Writer], [NewWriter], [Writer.Declare] and
// [Writer.Withdraw]. Every write to the holding table — a duty held or an
// obligation named — appends a policy version with People as caller before
// the table itself is written, the order every owner write to the
// declaration takes: the version first, the declaration second, so a stop
// between the two leaves a version naming what the table does not hold yet
// rather than the other way round. [Writer.versions] is a *[policy.Factory],
// composed in; a nil value appends no version. [Writer.Declare] is
// idempotent on the key and the holding, so declaring the same holding
// twice is one row, and [Writer.Withdraw] keeps the row and marks it, so a
// page delivered to a holder who has since stopped holding is still
// readable against the row that routed it. A key naming no holding at all
// is allowed — a [Mapping] with no row of [Table] behind it — added so a
// human can read the four screens without gating, approving, or otherwise
// acting anywhere.
//
// The tests are db_test.go, the holding table and the re-derivation,
// mapping_test.go, the mapping and its deletion, and credential_test.go, the
// lent credential, its ceiling and period, and the rates; all against the
// database.
//
// rederive.go holds [Rederive], called at the factory's start: it rewrites
// every duty and every obligation the newest policy version's declaration
// names that the table does not already hold standing, and, for a credential
// it names, the account kind and the ceiling's currency and period where the
// table disagrees, and appends no version of its own.
//
// Nothing enforces a duty's routing and nothing has to: a duty with no
// holder is not an error, and an empty table is a working factory. The one
// thing here the factory enforces is the spend ceiling on a lent credential,
// and what enforces it is dispatch: it reads [CredentialNamed], [RatesFor]
// and [Convert] onto the agent run record, and the sum over a period from
// [Ceiling.PeriodStartAt] is what it compares a ceiling against before a run.
//
// A version's snapshot names everything the declaration holds for a key: the
// duties, the obligations, and, per credential a key lent, its name, its
// account kind, its ceiling's amount, currency and period, and the rates
// authored on it — a key that lent two credentials is two rows of that
// snapshot. Extending [policy.PersonDeclaration] is package policy's, and
// this package does not import it for writing.
//
// Who may write what: [Writer] inserts a holding and withdraws it, and it
// refuses an actor that is not a human with [ErrNotAnOwner]. [Writer.Lend],
// [Writer.AuthorCeiling], [Writer.TakeBack] and [Writer.AuthorRate] are the
// only writers of the lent credential and its rates, refusing a non-human
// actor the same way. [WriteMapping]
// and [DeleteMapping] are the mapping's only writer, also refusing a
// non-human actor; [DeleteMapping] takes a caller-supplied check for
// whether a legal hold reaches a record the key is written on, because this
// package holds no join from a key to the records that name it, so a nil check
// never refuses.
//
// [DeleteMapping] takes its erasure-list appender from the caller for the same
// kind of reason: the erasure list has one writer and it is the report store,
// which this package does not import. The row lands before the deletion, keyed by the key, so a deletion made again
// appends nothing; a call supplying no appender is refused with
// [ErrNoErasureList], the row being what says the name must not come back with
// a restore. Both legal-hold checks are made before it, so a refused deletion
// appends no row. [Replay] is the other side of the same seam: the composition
// reads the rows naming a mapping and hands this the keys, and each is deleted
// again against whatever a restore brought back.
//
// What defines it: the twelve duties and the three obligations outside them are
// ../../end-goal/what-humans-do.md (C2850, C2852, C2855, C2856, C2857, C2858,
// C2859, C2861); the account kind and the rates beside a lent credential are
// ../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md
// (C2654, C2657, C2658, C2660, C2663, C2664, C2665, C2666, C2667, C2672,
// C2673, C2674, C2675); the spend ceiling, its currency, its period and what
// it is compared against are
// ../../end-goal/how-the-factory-works/10-fleet/08-a-spend-ceiling.md (C2516,
// C2517, C2518, C2519, C2522, C2523, C2526, C2534, C2543, C2544); a credential
// taken back is
// ../../end-goal/how-the-factory-works/10-fleet/06-a-credential-taken-back.md;
// the record, the per-person key, the key-to-name mapping, and the version
// every write but the mapping's appends are
// ../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md;
// what reads it is
// ../../end-goal/how-the-factory-works/08-operations/07-pages.md (C2133), where
// the notifier routes all three channels on it; the mapping's deletion refused
// under a legal hold is
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/03-a-legal-hold.md
// (C2323); the erasure-list row a deletion appends first, the one writer it
// meets and the replay after a restore are
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/02-reports.md
// (C0446, C0453, C0456); the opaque per-person key and the claimed-or-verified basis beside
// it are seam 1 of ../../end-goal/deferred.md#security-comes-last (C0041,
// C0065, C0085).
//
// This package owning the declaration and the enforced ceiling is
// ../../end-goal/how-the-factory-works/10-fleet/09-what-the-fleet-is-not.md
// (C2560).
//
// The ceiling as a field of the lent credential is
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/01-authored-and-not-among-the-eleven.md
// (C2288).
package people
