package intent

import "github.com/dulguun0225/borg/factory/record"

// Table is the intent table this package owns.
const Table = "intent"

// QuestionTable is the question table this package owns.
const QuestionTable = "intent_question"

// RequirementTable is the requirement table this package owns.
const RequirementTable = "requirement"

// IDPrefix is what [record.NewID] is called with for an intent.
const IDPrefix = "in"

// QuestionIDPrefix is what [record.NewID] is called with for a question.
const QuestionIDPrefix = "q"

// RequirementIDPrefix is what [record.NewID] is called with for a
// requirement. The id is opaque, stable, and never reused, the rule a
// criterion id keeps.
const RequirementIDPrefix = "rq"

// FormatVersion is what this package writes into format_version on every
// insert into [Table].
const FormatVersion = "intent/1"

// FormatVersionQuestion is what this package writes into format_version on
// every insert into [QuestionTable].
const FormatVersionQuestion = "intent_question/1"

// FormatVersionRequirement is what this package writes into format_version on
// every insert into [RequirementTable].
const FormatVersionRequirement = "requirement/1"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated,
// so the actor field and its constraints are the same ones every record table
// carries.
//
// The answered_together constraint is how a question stays readable as
// answered or not: unanswered is both fields empty, answered is a non-empty
// answer with answered_at in [record.TimeLayout], and every other pairing is
// refused whichever writer produced it — a non-empty answer with no time, a
// time in any other format, and an empty answer stamped with a valid time,
// which would read as answered while saying nothing.
//
// The constraints on the intent row that name the source are the design's
// three asymmetries between an intent somebody asked for and one the factory
// raised, refused in the store as well as in the writer: the factory's own
// carries the evidence that raised it and no other source does, it has no
// intended effect because its evidence stands where the statement would, and
// it has no outcome because no acceptance round gives it one.
//
// A requirement's pattern is one of the six or empty, and empty is admitted
// only with an escape reason, which is what [Escaped] counts. superseded_at
// and superseded_by are written together: an empty pointer with a time is a
// requirement the requester retracted, and both empty is one still in force,
// so the two cases a single column would conflate stay apart.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	source text not null,
	statement text not null,
	state text not null,
	rounds int not null,
	re_decompositions int not null,
	tier int not null,
	tier_policy_version text not null,
	project_id text not null,
	intended_effect text not null,
	evidence text not null,
	deadline text not null,
	constraint_id text not null,
	sent_back_by text not null,
	outcome text not null,
	` + record.Constraints + `,
	constraint source_known check (source in ('owner', 'reports', 'detector')),
	constraint statement_present check (statement <> ''),
	constraint state_known check (state in ('unrefined', 'refined', 're_decomposing', 'escalated', 'dropped', 'delivered')),
	constraint rounds_not_negative check (rounds >= 0),
	constraint re_decompositions_not_negative check (re_decompositions >= 0),
	constraint tier_not_negative check (tier >= 0),
	constraint tier_and_its_policy_version_together check ((tier = 0) = (tier_policy_version = '')),
	constraint evidence_on_the_factorys_own check ((source = 'detector') = (evidence <> '')),
	constraint intended_effect_not_on_the_factorys_own check (source <> 'detector' or intended_effect = ''),
	constraint outcome_not_on_the_factorys_own check (source <> 'detector' or outcome = ''),
	constraint deadline_is_time_layout check (deadline = '' or deadline ~ '` + record.TimePattern + `'),
	constraint sent_back_by_known check (sent_back_by in
		('', 'rework_request', 'gate_reject', 'replacement_constraint', 'acceptance_correction'))
)`,

	`create table if not exists ` + QuestionTable + ` (
	` + record.Columns + `,
	intent_id text not null,
	round int not null,
	question text not null,
	answer text not null,
	answered_at text not null,
	` + record.Constraints + `,
	constraint intent_id_present check (intent_id <> ''),
	constraint round_positive check (round >= 1),
	constraint question_present check (question <> ''),
	constraint answered_together check (
		(answer = '' and answered_at = '')
		or (answer <> '' and answered_at ~ '` + record.TimePattern + `')
	)
)`,

	`create table if not exists ` + RequirementTable + ` (
	` + record.Columns + `,
	intent_id text not null,
	statement text not null,
	pattern text not null,
	escape_reason text not null,
	kind text not null,
	derived_from text not null,
	item_id text not null,
	superseded_at text not null,
	superseded_by text not null,
	unanswerable_reason text not null,
	` + record.Constraints + `,
	constraint intent_id_present check (intent_id <> ''),
	constraint statement_present check (statement <> ''),
	constraint pattern_known check (pattern in
		('always_true', 'event', 'state', 'unwanted_condition', 'optional_feature', 'state_with_event', '')),
	constraint escape_reason_with_no_pattern check ((pattern = '') = (escape_reason <> '')),
	constraint kind_known check (kind in ('confirmed', 'enumerated_from_evidence', 'derived')),
	constraint derived_names_what_it_derives_from check ((kind = 'derived') = (derived_from <> '')),
	constraint derived_names_an_item check ((kind = 'derived') = (item_id <> '')),
	constraint superseded_together check ((superseded_at = '') = (superseded_by = '')),
	constraint superseded_at_is_time_layout check (superseded_at = '' or superseded_at ~ '` + record.TimePattern + `')
)`,
}
