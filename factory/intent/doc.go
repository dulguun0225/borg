// Package intent owns the intent, its questions, and intake — the one writer
// of both records.
//
// # The code
//
// intent.go holds [Intent] and [Question], the [Source] an intent arrived from
// — one of [Sources] — the [State] it is in — one of [States] — and
// [Question.Answered]. intake.go holds [Intake] and [NewIntake] with the five
// writes: [Intake.TakeIn], [Intake.Ask], [Intake.Answer],
// [Intake.MarkRefined], and [Intake.CountReDecomposition], whose one caller is
// decomposition. read.go holds [Get], [Unrefined], and [Questions]. schema.go
// holds [Table], [QuestionTable], [IDPrefix], [QuestionIDPrefix], and [DDL],
// whose CHECK constraints list every source and every state.
//
// A statement is written once and never updated; the state and the round count
// advance in place, an intent being an ordinary record that nothing chains.
//
// A question is a record written twice: [Intake.Ask] writes it and
// [Intake.Answer] writes the answer onto it, and a reader tells the two apart
// by the answered_at field, which [Question.Answered] reads. The answer is
// write-once — answering an answered question is [ErrAlreadyAnswered] — and an
// empty answer is [ErrAnswerEmpty], refused again by the answered_together
// constraint. Each question names the round it was asked in, and the intent's
// rounds field counts rounds rather than questions.
//
// The interview's rounds and decomposition's re-decompositions are two fields
// and never one, so an interview's rounds are not spent out of
// decomposition's budget.
//
// # Who may write what
//
// [Intake] is the one writer of both tables. The actor is a parameter of every
// method and not a field of the writer, so the three sources are three callers
// of one entrance. A write to an existing row — [Intake.Answer],
// [Intake.MarkRefined] — validates its caller's actor and stores it nowhere:
// the row keeps the actor that created it, so the record does not say who
// answered or who marked it refined.
//
// intent_question.intent_id is an id field and not a foreign key, like every
// link between records; record's doc.go states that rule and its cost once.
// [Intake.Ask] checks further, reading the intent in the same transaction, so
// a question written through the writer names an intent that exists and a row
// inserted around it may not.
//
// What defines it: the three sources and the one writer are
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/README.md;
// the three states, the question record written twice, the write-once answer,
// and the round count are
// ../../end-goal/how-the-factory-works/02-intent-into-items/02-the-interview.md.
package intent
