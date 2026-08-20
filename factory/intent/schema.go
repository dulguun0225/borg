package intent

import "github.com/dulguun0225/borg/factory/record"

// Table is the intent table this package owns.
const Table = "intent"

// QuestionTable is the question table this package owns.
const QuestionTable = "intent_question"

// IDPrefix is what [record.NewID] is called with for an intent.
const IDPrefix = "in"

// QuestionIDPrefix is what [record.NewID] is called with for a question.
const QuestionIDPrefix = "q"

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
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	source text not null,
	statement text not null,
	state text not null,
	rounds int not null,
	recuts int not null,
	` + record.Constraints + `,
	constraint source_known check (source in ('owner', 'reports', 'detector')),
	constraint statement_present check (statement <> ''),
	constraint state_known check (state in ('unrefined', 'refined', 'escalated')),
	constraint rounds_not_negative check (rounds >= 0),
	constraint recuts_not_negative check (recuts >= 0)
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
}
