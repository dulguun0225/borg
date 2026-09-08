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
// intake.go holds [Intake], [NewIntake], [Arrival], [Intake.TakeIn],
// [Intake.SetDeadline], [Intake.SetProject] and [Intake.Admit]. interview.go holds [Intake.OpenRound], [Intake.Ask]
// and [Intake.Answer]. confirm.go holds [Confirmation], [Intake.Confirm],
// [Correction] and [Intake.Correct]. state.go holds [Intake.SendBack],
// [Intake.MarkReDecomposing], [Intake.ClearReDecomposing], [Intake.Escalate]
// and [Intake.Drop]. acceptance.go holds [Intake.AcceptanceRound], [Delivery],
// [Intake.Delivered] and [Intake.CorrectAcceptance]. requirementwrite.go holds
// [Intake.DeriveForItem], [Intake.SupersedeDerived] and
// [Intake.MarkUnanswerable]. redact.go holds [Intake.Redact],
// [Intake.RedactionPass] and [Intake.Replay]. notifier.go holds [Notifier], [NoNotifier] and
// [ErrNotifierNotComposed]. read.go holds [Get],
// [OnEvidence], [Waiting], [InProject] — every intent of one project, which is
// what a screen listing what arrived reads, an intent having no item until
// decomposition runs — [Questions], [Requirements], [EveryRequirement],
// [ForItem] and [Escaped].
//
// # The statement's erasure
//
// The statement summarizing a group of reports is one of the three things a
// redaction names, and intake is what destroys its bytes: [Intake.Redact]
// overwrites the spans one redaction names, [Intake.RedactionPass] is this
// package's own pass over the redactions naming statements — a record package
// redaction writes and this one reads, so no component writes another's
// record — and [Intake.Replay] destroys again what the erasure list says was
// removed, which is what a restore is served through before this package
// serves anything.
//
// Neither write writes a row of that list: it has one writer and it is the
// report store, and the row for a statement is appended through that store by
// the erasure action at Factory, keyed by the key the action computed, before
// the redaction record exists and before anything here is called. So the row
// lands first and the record last, a step taken again appends nothing, and a
// stop leaves the event visibly owing. [Intake.Replay] reads that list, which
// is a read and not a write.
//
// Every read here that returns an intent serves its statement through the
// redactions naming it, from the moment the redaction exists and whether or
// not the pass has run, so a lagging destruction serves nothing meanwhile.
// The bytes are overwritten in place and the length is unchanged, so the row,
// its links and every count over it stand with the words gone. The erasure is
// the one exception to the sentence below.
//
// # The admission and the recurrence link
//
// [Intake.Admit] records that a human admitted the intent at Work, which is
// what a report-derived intent waits for while the safeguard on the report
// store stands: until it is written, dispatch puts no agent on the intent and
// no interview round runs. Only [SourceReports] takes one — no other source
// arrives through a channel a stranger writes into — and admitting one already
// admitted changes nothing, so the instant on the row is the first admission's.
// Nothing here reads the safeguard: whether the wait is owed at all is gate
// policy's, read by the component that would spend on the intent.
//
// [Arrival.RecurrenceOf] is the intent a new one recurs on, written at the
// arrival and never afterwards. It is what the grouper writes where a report
// matches work whose timeline is finished: the fix shipped, so evidence that
// it did not work is a new intent and never a reopening, and the link is what
// keeps the two readable as one run of the same problem. It is refused on any
// source but [SourceReports], in the writer and again by [DDL], no other source
// arriving in a quantity that recurs; nothing here reads the link back, the
// screens being what render it.
//
// A statement is written once and never updated; the state, the two counts,
// and the fields the confirming round writes advance in place, an intent being
// an ordinary record that nothing chains. The tier is one of those fields for
// a request and not for a detector's intent: [Intake.Confirm] writes it only
// where the confirmation carries one, so the tier the arrival wrote for a
// detector's intent stays where it is.
//
// The project is the one field written once after the arrival and refused a
// second write: [Arrival.ProjectID] writes it where a source supplies one, and
// [Intake.SetProject] fills it where decomposition has to place a service the
// work creates and nothing else answers. A second call is
// [ErrProjectAlreadyWritten], the refusal being the writer's because a column
// cannot see what stood in it before an update.
//
// A question is a record written twice: it is asked and the answer is written
// onto it, and a reader tells the two apart by the answered_at field, which
// [Question.Answered] reads. The answer is write-once — answering an answered
// question is [ErrAlreadyAnswered] — and an empty answer is [ErrAnswerEmpty],
// refused again by the answered_together constraint. Each question names the
// round it was asked in: [Intake.OpenRound] advances the round count and
// [Intake.Ask] attaches to the round already open, because the attempt limit
// counts rounds and counting the questions would count a round that asked
// three as three. [Intake.Ask] takes an unrefined intent and no other, the
// interview being the state an intent is in; the acceptance round is asked by
// [Intake.AcceptanceRound] instead, which takes a refined one.
//
// The interview's rounds and decomposition's re-decompositions are two fields
// and never one, so an interview's rounds are not spent out of decomposition's
// budget. A human's [Intake.Answer] on a question of an intent the interview
// escalated clears the escalation and starts the round count again, which is
// the interview's counterpart to package item's clearing of a stage's
// escalation; a component's answer clears nothing, and neither does any answer
// on an intent decomposition escalated. Which of the two an escalation is, is
// read off the counts against the limit the caller passes [Intake.Answer].
//
// A requirement's id is opaque, stable and never reused. A statement fitting
// none of the six patterns is admitted with a tagged escape reason and no
// pattern, and [Escaped] is the count of those. [Intake.Confirm] writes one
// reading against the one before it: a statement that restates an earlier one
// unchanged keeps its record and its id, every other requirement of the
// earlier reading is superseded in the same call and points at the statements
// that named it, and the pointer is empty where the requester retracted the
// statement. The sweep reads the reading and not the shares: a derived
// requirement is superseded with the item that carried it, by
// [Intake.SupersedeDerived], which decomposition calls beside the item's own
// supersession — two records with two writers and one event.
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
// [Intake.ClearReDecomposing], [Intake.SetProject], [Intake.DeriveForItem],
// [Intake.SupersedeDerived] and
// [Intake.MarkUnanswerable]; and a named human at Ops through [Intake.TakeIn]
// when they ask for a rollback's revert.
//
// [Intake] calls one component and it is the notifier, at the three writes that
// leave something waiting on a human: [Intake.Ask], [Intake.Escalate] and
// [Intake.AcceptanceRound]. It is [Notifier], an interface the composition
// supplies.
//
// A write to an existing row validates its caller's actor and stores it
// nowhere: the row keeps the actor that created it, so the record does not say
// who answered, who confirmed, or who sent it back. [Intake.Drop] is the one
// method that reads the actor's kind, refusing anything but a human, because a
// human at Work is what ends an intent for good.
//
// The attempt limit is nowhere in this package. [Intake.OpenRound] and
// [Intake.MarkReDecomposing] return the count they reached, and
// [Intake.Escalate] and [Intake.Answer] take the limit as an argument, because
// the limit is authored with gate policy, and a value in force is package
// policy's read.
//
// intent_question.intent_id and requirement.intent_id are id fields and not
// foreign keys, like every link between records; record's doc.go states that
// rule and its cost once. Every write through this writer reads the intent in
// the same transaction, so a question or a requirement written through it
// names an intent that exists and a row inserted around it may not.
//
// What waits on Work, which is not built: the answering half of the acceptance
// round. This package writes the round at [Intake.AcceptanceRound], called by
// the factory once every item of the intent is live, and the notifier delivers
// it; the verdict on it is a human's, and [Intake.Delivered] and
// [Intake.CorrectAcceptance] are what that human's screen would call. Until
// there is one, the caller that composes this package makes those two calls at
// a terminal. The same holds for [Intake.Answer] and [Intake.Drop], which Work
// owns in the design and a terminal performs here.
//
// What is not built and is a parameter here rather than a substitute: the
// report store, so an intent grouped from reports has no outcome this package
// can compute and [Delivery.Outcome] is the caller's; the constraint record,
// so [Arrival.ConstraintID] and [Arrival.Deadline] are an id and an instant
// the caller supplies; the project record, so [Arrival.ProjectID] is an id
// this package stores and does not resolve; and the landing of a send-back
// [Intake.SendBack] refuses on a re-decomposing intent, which no field here
// remembers — the caller sends it back again once the firing has closed.
//
// What defines it: the three sources, the one writer, the project, the evidence
// key and the tier at arrival are
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/README.md
// (C0505, C0510, C0511, C0512, C0513, C0514, C0515, C0516, C0518, C0522,
// C0523);
//
// the six states, the rounds, the question record written twice, the write-once
// answer, the confirming round with the intended effect and the tier, the
// requirement record with its three kinds and its supersession, the acceptance
// round and the outcome are
// ../../end-goal/how-the-factory-works/02-intent-into-items/02-the-interview.md
// (C0526, C0527, C0528, C0529, C0531, C0532, C0533, C0534, C0537, C0544, C0545,
// C0546, C0547, C0548, C0549, C0550, C0551, C0552, C0553, C0554, C0555, C0556,
// C0558, C0561, C0562, C0563, C0564, C0565, C0566, C0567, C0568, C0569, C0571,
// C0572, C0573, C0580, C0582, C0585, C0586, C0587, C0589, C0591, C0592, C0593,
// C0596, C0601, C0603, C0604, C0607, C0608, C0613, C0615);
//
// the deadline is
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/01-constraints-and-the-design-system.md
// (C0275, C0285, C0288, C0291, C0295, C0296);
//
// the re-decomposition count, the derived requirement and the unanswerable mark
// are
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md
// (C0765, C0771, C0772, C0775, C0776, C0779, C0782, C0787);
//
// the redaction of a statement, the pass that destroys it and the replay after
// a restore, the recurrence link a report matching finished work raises, and
// the admission a report-derived intent waits for at Work, are
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/02-reports.md
// (C0399, C0438, C0447, C0448, C0449, C0456);
//
// the six patterns are
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/03-the-six-patterns.md
// (C1072, C1075, C1077);
//
// and the revert a named human at Ops asks for through intake is
// ../../end-goal/how-the-factory-works/06-releases/06-rollback.md (C1742,
// C1743, C1744, C1745).
package intent
