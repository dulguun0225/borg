// Package intent owns three records — the intent, the question, and the
// requirement — and intake, the one writer of all three. They share one
// package because they share one writer: a question is attached to an intent
// and a requirement is attached to an intent, and both take the intent's one
// writer.
//
// # The code
//
// intent.go holds [Intent] and [Question], the [Source] an intent arrived from
// — one of [Sources] — the [State] it is in — one of [States] — the
// [SentBackBy] that last reopened it — one of [SentBackBys] — the [Tier] and
// the [Evidence] with its [Evidence.Key]. requirement.go holds [Requirement],
// its [Kind] — one of [Kinds] — [NewRequirement] and [Derivation].
// pattern.go holds [Pattern], the six [Patterns], and [Classify].
// schema.go holds [Table], [QuestionTable], [RequirementTable], the three id
// prefixes, the three format versions, and [DDL]. errors.go holds every
// sentinel this package returns.
//
// intake.go holds [Intake], [NewIntake], [Arrival], [Intake.TakeIn] and
// [Intake.SetDeadline]. interview.go holds [Intake.OpenRound], [Intake.Ask]
// and [Intake.Answer]. confirm.go holds [Confirmation], [Intake.Confirm],
// [Correction] and [Intake.Correct]. state.go holds [Intake.SendBack],
// [Intake.MarkReDecomposing], [Intake.ClearReDecomposing], [Intake.Escalate]
// and [Intake.Drop]. acceptance.go holds [Intake.AcceptanceRound], [Delivery],
// [Intake.Delivered] and [Intake.CorrectAcceptance]. requirementwrite.go holds
// [Intake.DeriveForItem] and [Intake.MarkUnanswerable]. read.go holds [Get],
// [OnEvidence], [Questions], [Requirements], [EveryRequirement], [ForItem] and
// [Escaped].
//
// A statement is written once and never updated; the state, the two counts,
// and the fields the confirming round writes advance in place, an intent being
// an ordinary record that nothing chains.
//
// A question is a record written twice: it is asked and the answer is written
// onto it, and a reader tells the two apart by the answered_at field, which
// [Question.Answered] reads. The answer is write-once — answering an answered
// question is [ErrAlreadyAnswered] — and an empty answer is [ErrAnswerEmpty],
// refused again by the answered_together constraint. Each question names the
// round it was asked in: [Intake.OpenRound] advances the round count and
// [Intake.Ask] attaches to the round already open, because the attempt limit
// counts rounds and counting the questions would count a round that asked
// three as three. The interview's rounds and decomposition's re-decompositions
// are two fields and never one, so an interview's rounds are not spent out of
// decomposition's budget.
//
// A requirement's id is opaque, stable and never reused. A statement fitting
// none of the six patterns is admitted with a tagged escape reason and no
// pattern, and [Escaped] is the count of those. [Intake.Confirm] writes one
// reading against the one before it: a statement that restates an earlier one
// unchanged keeps its record and its id, every other requirement of the
// earlier reading is superseded in the same call and points at the statements
// that named it, and the pointer is empty where the requester retracted the
// statement. The sweep reads the reading and not the shares: a derived
// requirement is superseded by the re-decomposition, with the item that
// carried it, and no write here does that — the item's supersession is
// package item's and the call that would carry the requirement with it is not
// built.
//
// # Who may write what
//
// [Intake] is the one writer of all three tables. The actor is a parameter of
// every method and not a field of the writer, so its callers are callers of
// one entrance: an owner's request, the grouper (not built), and a detector at
// [Intake.TakeIn]; the factory at [Intake.OpenRound], [Intake.Ask],
// [Intake.Confirm], [Intake.Correct] and [Intake.AcceptanceRound]; Work at
// [Intake.Answer], [Intake.Delivered], [Intake.CorrectAcceptance] and
// [Intake.Drop]; decomposition at [Intake.MarkReDecomposing],
// [Intake.ClearReDecomposing], [Intake.DeriveForItem] and
// [Intake.MarkUnanswerable]; and a named human at Ops through [Intake.TakeIn]
// when they ask for a rollback's revert.
//
// A write to an existing row validates its caller's actor and stores it
// nowhere: the row keeps the actor that created it, so the record does not say
// who answered, who confirmed, or who sent it back. [Intake.Drop] is the one
// method that reads the actor's kind, refusing anything but a human, because a
// human at Work is what ends an intent for good.
//
// The attempt limit is nowhere in this package. [Intake.OpenRound] and
// [Intake.MarkReDecomposing] return the count they reached and
// [Intake.Escalate] takes the limit as an argument, because the limit is
// authored with gate policy, and a value in force is package policy's read.
//
// intent_question.intent_id and requirement.intent_id are id fields and not
// foreign keys, like every link between records; record's doc.go states that
// rule and its cost once. Every write through this writer reads the intent in
// the same transaction, so a question or a requirement written through it
// names an intent that exists and a row inserted around it may not.
//
// What is not built and is a parameter here rather than a substitute: the
// report store, so an intent grouped from reports has no outcome this package
// can compute and [Delivery.Outcome] is the caller's; the constraint record,
// so [Arrival.ConstraintID] and [Arrival.Deadline] are an id and an instant
// the caller supplies; and the project record, so [Arrival.ProjectID] is an id
// this package stores and does not resolve.
//
// What defines it: the three sources, the one writer, the project, the
// evidence key and the tier at arrival are
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/README.md;
// the six states, the rounds, the question record written twice, the
// write-once answer, the confirming round with the intended effect and the
// tier, the requirement record with its three kinds and its supersession, the
// acceptance round and the outcome are
// ../../end-goal/how-the-factory-works/02-intent-into-items/02-the-interview.md;
// the deadline is
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/01-constraints-and-the-design-system.md;
// the re-decomposition count, the derived requirement and the unanswerable
// mark are
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md;
// the six patterns are
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/03-the-six-patterns.md;
// and the revert a named human at Ops asks for through intake is
// ../../end-goal/how-the-factory-works/06-releases/06-rollback.md.
package intent
