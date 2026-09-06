package criterion

import "github.com/dulguun0225/borg/factory/record"

// Table is the criterion table this package owns.
const Table = "criterion"

// WithdrawalTable is the table of withdrawals: one row per criterion a spec
// version withdraws. Withdrawal is recorded on the withdrawing spec version
// and never on the criterion, so a version the gate rejects takes its
// withdrawal down with it.
const WithdrawalTable = "criterion_withdrawal"

// ResultTable is the table of what deciding a criterion against a build
// produced, one row per build, run, and criterion.
const ResultTable = "criterion_result"

// IDPrefix is what [record.NewID] is called with for a row of this table.
// The encoding derivation reads the same prefix: an encoding names a criterion
// by an id of this shape, and [Encodings] finds it by that shape.
const IDPrefix = "cr"

// WithdrawalIDPrefix is what [record.NewID] is called with for a withdrawal
// row. The identity of a withdrawal is the spec version and the criterion, so
// the id is the row's and never what anything points at.
const WithdrawalIDPrefix = "crw"

// MutationTable is the table of mutation scores: one row per build and run
// mutated, beside that run's criteria results.
const MutationTable = "criterion_mutation"

// ResultIDPrefix is what [record.NewID] is called with for a result row. The
// identity of a result is the build, the run, and the criterion, so the id is
// the row's and never what anything points at — the prefix differs from
// [IDPrefix] so that [Encodings], which finds a criterion id by its shape,
// never reads one of these.
const ResultIDPrefix = "crr"

// FormatVersion is what this package writes into format_version on every
// insert into [Table].
const FormatVersion = "criterion/1"

// FormatVersionWithdrawal is what this package writes into format_version on
// every insert into [WithdrawalTable].
const FormatVersionWithdrawal = "criterion_withdrawal/1"

// MutationIDPrefix is what [record.NewID] is called with for a mutation row.
// The identity of a mutation score is the build and the run, so the id is the
// row's and never what anything points at, and the prefix differs from
// [IDPrefix] so that [Encodings] never reads one of these as a criterion id.
const MutationIDPrefix = "crm"

// FormatVersionResult is what this package writes into format_version on
// every insert into [ResultTable].
const FormatVersionResult = "criterion_result/1"

// FormatVersionMutation is what this package writes into format_version on
// every insert into [MutationTable].
const FormatVersionMutation = "criterion_mutation/1"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than
// restated, so the actor field and its constraints are the same ones every
// record table carries.
//
// service_id, spec_artifact_id, and item_id are id fields and not foreign keys,
// each checked for being present and not for pointing at anything; record's
// doc.go states that rule and its cost once. no_pattern_reason is required
// exactly on a criterion fitting no pattern, because such a sentence is
// admitted only with a tagged reason, and a reason on a matched sentence would
// let the tag stop meaning that.
//
// The three provenance columns are links and not marks, which is what makes
// each of the three questions queries.go answers a query rather than a scan of
// sentences: requirement_id is the requirement the criterion answers, required
// of every criterion that fits a pattern; constraint_derived names each
// constraint record the drafting stage held as its evidence, and is an array
// because one criterion can stand for several; hazard_derived names the area
// whose hazardous operation the criterion bounds, and is empty on a criterion
// that bounds none.
//
// The mutation row is the reading on the build the Merge to master gate reads:
// what the mutation of one run produced, beside that run's criteria results.
// The score itself is no column — it is derived from the two counts at the
// read, the way undecided is, so the number cannot disagree with the counts it
// is made of. could_not_derive and the counts are exclusive: a derivation that
// could not be made counts nothing, and one that was made tested at least one
// mutant, which is what keeps a mutation of nothing from reading as a score of
// zero. The mutation happens at the candidate run, so run is numbered from 1
// like every run the deployer performs.
//
// A result row carries the run and the composition copied onto it. Keyed by
// build and criterion alone, a second run would overwrite the first and the
// disagreement undecided is computed from would be gone before anything read
// it. place says which of the two places decided it, and the two CHECKs on it
// are what the split costs: the build's own process is run 0 and has no
// environment, so it carries no composition, and a run on the candidate
// environment is numbered from 1 by the deployer and always carries one.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	service_id text not null,
	spec_artifact_id text not null,
	item_id text not null,
	sentence text not null,
	pattern text not null,
	no_pattern_reason text not null,
	requirement_id text not null,
	constraint_derived text[] not null,
	hazard_derived text not null,
	` + record.Constraints + `,
	constraint service_id_present check (service_id <> ''),
	constraint spec_artifact_id_present check (spec_artifact_id <> ''),
	constraint item_id_present check (item_id <> ''),
	constraint sentence_present check (sentence <> ''),
	constraint pattern_known check (pattern in ('always_true', 'event', 'state',
		'unwanted_condition', 'optional_feature', 'state_with_an_event_inside_it', 'no_pattern')),
	constraint no_pattern_reason_matches_pattern check ((pattern = 'no_pattern') = (no_pattern_reason <> '')),
	constraint requirement_id_present_on_a_pattern check (pattern = 'no_pattern' or requirement_id <> '')
)`,

	`create table if not exists ` + WithdrawalTable + ` (
	` + record.Columns + `,
	spec_artifact_id text not null,
	item_id text not null,
	criterion_id text not null,
	` + record.Constraints + `,
	constraint spec_artifact_id_present check (spec_artifact_id <> ''),
	constraint item_id_present check (item_id <> ''),
	constraint criterion_id_present check (criterion_id <> ''),
	constraint one_row_per_version_and_criterion unique (spec_artifact_id, criterion_id)
)`,

	`create table if not exists ` + ResultTable + ` (
	` + record.Columns + `,
	build_id text not null,
	run int not null,
	criterion_id text not null,
	outcome text not null,
	place text not null,
	composition text not null,
	` + record.Constraints + `,
	constraint build_id_present check (build_id <> ''),
	constraint criterion_id_present check (criterion_id <> ''),
	constraint outcome_observed check (outcome in ('passed', 'failed')),
	constraint place_known check (place in ('build', 'candidate_environment')),
	constraint run_matches_place check (
		(place = 'build' and run = 0) or (place = 'candidate_environment' and run >= 1)
	),
	constraint composition_matches_place check ((place = 'build') = (composition = '')),
	constraint one_row_per_build_run_and_criterion unique (build_id, run, criterion_id)
)`,

	`create table if not exists ` + MutationTable + ` (
	` + record.Columns + `,
	build_id text not null,
	run int not null,
	toolchain text not null,
	tool text not null,
	coverage text not null,
	mutants_tested int not null,
	mutants_detected int not null,
	could_not_derive text not null,
	` + record.Constraints + `,
	constraint build_id_present check (build_id <> ''),
	constraint run_is_a_candidate_environments check (run >= 1),
	constraint counts_not_negative check (mutants_tested >= 0 and mutants_detected >= 0),
	constraint detected_within_tested check (mutants_detected <= mutants_tested),
	constraint could_not_derive_counts_nothing check (
		could_not_derive = '' or (mutants_tested = 0 and mutants_detected = 0)
	),
	constraint a_derivation_tested_a_mutant check (could_not_derive <> '' or mutants_tested >= 1),
	constraint one_row_per_build_and_run unique (build_id, run)
)`,
}
