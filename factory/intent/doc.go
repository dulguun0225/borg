// Package intent owns the intent, its questions, and intake — the one writer
// of both records.
//
// # The intent
//
// An intent is what arrives: a statement from one of three sources — an
// owner's request, grouped end-user reports, or a detector, something the
// factory found itself. M1 only ever writes owner; the other two are the
// design's named sources and cost nothing to refuse now, so the CHECK
// constraint in [DDL] lists all three. The statement is written once and
// never updated. The state and the round count advance in place — an intent
// is an ordinary record, and nothing chains it.
//
// The state has three values. An intent arrives unrefined, and
// [Intake.MarkRefined] is the one transition this package writes: refined
// means the factory has enough to cut it and author a spec per item. Nothing
// in M1 writes escalated — the attempt bound that would is not built — and the
// value is in the CHECK because the design names it, so the schema does not
// change when the bound arrives.
//
// # The question
//
// A question is a record written twice: [Intake.Ask] writes it, and
// [Intake.Answer] writes the answer onto it. A reader tells an answered
// question from an unanswered one by the answered_at field —
// [Question.Answered] reads it — which is what a record written twice costs.
// The answer is write-once: answering an answered question is
// [ErrAlreadyAnswered], so an owner who answers badly waits to be asked again.
// That is why an empty answer is [ErrAnswerEmpty] and refused again by the
// answered_together constraint — a field written once with nothing in it is a
// question stamped answered that says nothing, and the round is spent.
//
// Each question names the round it was asked in, and the intent's rounds
// field counts rounds rather than questions, because the attempt bound counts
// rounds. M1's interview stopping rule is one round or none: the factory asks
// what it cannot author without and proceeds on the answer.
//
// # Who may write what
//
// [Intake] is the one writer of both tables. The three sources are three
// callers of one entrance rather than three components writing the same
// record, which is why the actor is a parameter of every method and not a
// field of the writer. A write to an existing row — [Intake.Answer],
// [Intake.MarkRefined] — validates its caller's actor and stores it nowhere:
// the row keeps the actor that created it. What that costs: the record does
// not say who answered or who marked it refined, and nothing in M1 reads
// either.
//
// intent_question.intent_id is an id field and not a foreign key, like every
// link between records. The store checks it for being present and not for
// pointing at anything — record's doc.go states that rule and its cost once —
// and [Intake.Ask] checks further, by reading the intent in the same
// transaction, so a question written through the writer names an intent that
// exists and a row inserted around it may not.
//
// What defines it:
// ../../end-goal/how-humans-do-it/02-intent-into-items.md#intake sets the
// three sources and the one writer, and
// ../../end-goal/how-humans-do-it/02-intent-into-items.md#the-interview sets
// the three states, the question record written twice, the write-once answer,
// and the round count.
package intent
