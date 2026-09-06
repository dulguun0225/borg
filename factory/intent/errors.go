package intent

import "errors"

var (
	// ErrSourceUnknown is returned by [Intake.TakeIn] for a source outside
	// [Sources].
	ErrSourceUnknown = errors.New("intent: the source is none of owner, reports, detector")
	// ErrStatementEmpty is returned for an intent or a requirement with no
	// statement.
	ErrStatementEmpty = errors.New("intent: the statement is empty")
	// ErrQuestionEmpty is returned for a question with no text.
	ErrQuestionEmpty = errors.New("intent: the question is empty")
	// ErrAnswerEmpty is returned for an answer with no text. The answer is
	// write-once, so an empty one would stamp the question answered with
	// nothing in it and the stage that asked would proceed on nothing.
	ErrAnswerEmpty = errors.New("intent: the answer is empty")
	// ErrIntentIDEmpty is returned for a record naming no intent. record's
	// doc.go states what a link is checked for.
	ErrIntentIDEmpty = errors.New("intent: the intent id is empty")
	// ErrIntentNotFound is returned where the named intent does not exist.
	ErrIntentNotFound = errors.New("intent: no intent has that id")
	// ErrQuestionNotFound is returned where the named question does not
	// exist.
	ErrQuestionNotFound = errors.New("intent: no question has that id")
	// ErrRequirementNotFound is returned where the named requirement does not
	// exist.
	ErrRequirementNotFound = errors.New("intent: no requirement has that id")
	// ErrAlreadyAnswered is returned for a question that has its answer. The
	// answer is write-once, so an owner who answered badly waits to be asked
	// again.
	ErrAlreadyAnswered = errors.New("intent: the answer is write-once, and this question has one")
	// ErrQuestionElsewhere is returned where a question named in a call
	// belongs to another intent.
	ErrQuestionElsewhere = errors.New("intent: that question belongs to another intent")

	// ErrEvidenceEmpty is returned by [Intake.TakeIn] for a detector's intent
	// with no evidence. A detector raises an intent for a condition and not
	// for an observation, and the evidence is what keys it.
	ErrEvidenceEmpty = errors.New("intent: an intent the factory raised carries the evidence that raised it")
	// ErrTierIncomplete is returned for a tier with a value and no policy
	// version, or a policy version and no value: an authored value and the
	// version it was in force under are written together.
	ErrTierIncomplete = errors.New("intent: a tier is a value and the policy version it is in force under")

	// ErrProjectIDEmpty is returned by [Intake.SetProject] for a fill naming
	// no project.
	ErrProjectIDEmpty = errors.New("intent: the project id is empty")
	// ErrProjectAlreadyWritten is returned by [Intake.SetProject] for an
	// intent that already names a project. The field is filled once and never
	// rewritten, so an approval keeps pointing at what was approved.
	ErrProjectAlreadyWritten = errors.New("intent: the project is written once, and this intent has one")

	// ErrNoOpenRound is returned by [Intake.Ask] where no round has been
	// opened. A question attaches to the open round, and the round is what
	// the attempt limit counts.
	ErrNoOpenRound = errors.New("intent: no round is open on this intent")
	// ErrNotUnrefined is returned where a write needs an unrefined intent and
	// the intent is in another state.
	ErrNotUnrefined = errors.New("intent: this write needs an unrefined intent")
	// ErrNotRefined is returned where a write needs a refined intent and the
	// intent is in another state.
	ErrNotRefined = errors.New("intent: this write needs a refined intent")
	// ErrNotReDecomposing is returned by [Intake.ClearReDecomposing] for an
	// intent that is not re-decomposing.
	ErrNotReDecomposing = errors.New("intent: this write needs a re-decomposing intent")
	// ErrFinished is returned where a write would move an intent that is
	// already dropped or delivered. Both are ends, and neither is written
	// over.
	ErrFinished = errors.New("intent: the intent is already dropped or delivered")
	// ErrEscalated is returned by [Intake.SendBack] for an escalated intent.
	// An escalation is a human's to clear, so a send-back does not write over
	// one: what reaches such an intent is the attachment its caller makes.
	ErrEscalated = errors.New("intent: an escalation is a human's to clear, and a send-back does not write over one")
	// ErrReDecomposing is returned by [Intake.SendBack] for a re-decomposing
	// intent. The open Decomposition firing closes first and the send-back
	// lands then, which is a second call.
	ErrReDecomposing = errors.New("intent: an open Decomposition firing closes before a send-back lands")

	// ErrRequirementsEmpty is returned by [Intake.Confirm] for a reading with
	// no statements in it.
	ErrRequirementsEmpty = errors.New("intent: the reading has no requirements in it")
	// ErrEscapeReasonMissing is returned for a statement fitting none of the
	// six patterns and carrying no tagged reason. A form everything can
	// escape is not a form.
	ErrEscapeReasonMissing = errors.New("intent: a statement fitting no pattern is admitted only with a tagged reason")
	// ErrEscapeReasonUnwanted is returned for a tagged reason on a statement
	// that fits one of the six patterns.
	ErrEscapeReasonUnwanted = errors.New("intent: this statement fits a pattern and needs no escape reason")
	// ErrSupersedesUnknown is returned by [Intake.Confirm] where a statement
	// names a requirement that is not in force for this intent.
	ErrSupersedesUnknown = errors.New("intent: that requirement is not in force for this intent")
	// ErrIntendedEffectEmpty is returned by [Intake.Confirm] where a
	// requester's intent confirms no intended effect.
	ErrIntendedEffectEmpty = errors.New("intent: the confirming round confirms an intended effect")
	// ErrNoRequester is returned where a call asks a requester for something
	// on an intent the factory raised: an intended effect, a proposed tier, a
	// confirming round, or an acceptance round. Nobody can say the evidence
	// was misread.
	ErrNoRequester = errors.New("intent: an intent the factory raised has no requester")
	// ErrRequesterOwed is returned where a call skips something a requester
	// owes on an intent somebody asked for.
	ErrRequesterOwed = errors.New("intent: an intent somebody asked for is answered by its requester")

	// ErrDerivedFromNotInForce is returned by [Intake.DeriveForItem] where the
	// requirement a share derives from is not in force for the intent.
	ErrDerivedFromNotInForce = errors.New("intent: a share derives from a requirement in force")
	// ErrItemIDEmpty is returned by [Intake.DeriveForItem] for a share naming
	// no item.
	ErrItemIDEmpty = errors.New("intent: the item id is empty")
	// ErrReasonEmpty is returned by [Intake.MarkUnanswerable] for a mark with
	// no tagged reason.
	ErrReasonEmpty = errors.New("intent: the reason is empty")
	// ErrAlreadyUnanswerable is returned by [Intake.MarkUnanswerable] for a
	// requirement that already carries the mark.
	ErrAlreadyUnanswerable = errors.New("intent: the mark is write-once, and this requirement has one")

	// ErrLimitNotPositive is returned by [Intake.Escalate] for a limit of
	// zero or less. The limit is authored with gate policy and is a count of
	// rounds.
	ErrLimitNotPositive = errors.New("intent: the attempt limit is not positive")
	// ErrLimitNotExceeded is returned by [Intake.Escalate] where neither of
	// the intent's two counts exceeds the limit it was called with.
	ErrLimitNotExceeded = errors.New("intent: neither count exceeds the attempt limit")
	// ErrSentBackByUnknown is returned by [Intake.SendBack] for a cause
	// outside [SentBackBys].
	ErrSentBackByUnknown = errors.New("intent: that is not one of the four things that send an intent back")
	// ErrNotAHuman is returned by [Intake.Drop]: a human at Work ends an
	// intent for good, and no component does.
	ErrNotAHuman = errors.New("intent: a human ends an intent for good")
	// ErrOutcomeEmpty is returned by [Intake.Delivered] where a requested
	// intent closes with no outcome. The outcome is computed once at the
	// close and never afterwards.
	ErrOutcomeEmpty = errors.New("intent: the outcome is computed at the close")
)
