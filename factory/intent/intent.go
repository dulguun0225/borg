package intent

import (
	"encoding/json"
	"fmt"

	"github.com/dulguun0225/borg/factory/record"
)

// Source is where an intent came from. There are three, and each is a caller
// of [Intake] rather than a writer of its own.
type Source string

const (
	// SourceOwner is a request an owner typed.
	SourceOwner Source = "owner"
	// SourceReports is end-user reports a grouper turned into one intent.
	// Nothing writes it yet; the grouper is not built.
	SourceReports Source = "reports"
	// SourceDetector is something the factory found itself. An intent of
	// this source carries the evidence that raised it, has no requester, and
	// so has no intended effect, no proposed tier, and no acceptance round.
	SourceDetector Source = "detector"
)

// Sources is every source an intent may have. The CHECK constraint in [DDL]
// lists the same three, and TestDDLListsEverySourceAndState fails if the two
// stop agreeing.
var Sources = []Source{SourceOwner, SourceReports, SourceDetector}

// State is where an intent is. It advances in place, because the state says
// where the intent is and is not read back to justify a decision. It is what
// stops every component that could move an item: none of dispatch, the gate
// component, and the merge queue acts on an item whose intent is
// [StateUnrefined], [StateReDecomposing], [StateEscalated], or [StateDropped].
type State string

const (
	// StateUnrefined is how every intent arrives, and what anything sent
	// back to the intent writes again.
	StateUnrefined State = "unrefined"
	// StateRefined means the factory has enough to decompose the intent and
	// author a spec per item, which is the same thing as ready to decompose.
	StateRefined State = "refined"
	// StateReDecomposing is where a re-decomposition of a set in progress is
	// being decided, which is what stops that intent's items while the
	// Decomposition firing is open.
	StateReDecomposing State = "re_decomposing"
	// StateEscalated is where the rounds or the re-decompositions exceeded
	// the attempt limit. It is written rather than recomputed from either
	// count: the limit is authored and changes, and a decision read back
	// against a value that was not in force when it was taken is not the
	// decision that was taken.
	StateEscalated State = "escalated"
	// StateDropped is where a human at Work ended the intent for good.
	StateDropped State = "dropped"
	// StateDelivered is where the person who asked confirmed that what
	// shipped is what they asked for.
	StateDelivered State = "delivered"
)

// States is every state an intent may have. The CHECK constraint in [DDL]
// lists the same six, and TestDDLListsEverySourceAndState fails if the two
// stop agreeing.
var States = []State{
	StateUnrefined, StateRefined, StateReDecomposing,
	StateEscalated, StateDropped, StateDelivered,
}

// SentBackBy is what wrote [StateUnrefined] over a later state. The field
// holds the last one, so what reopened the interview is readable on the
// intent rather than reconstructed from the log.
type SentBackBy string

const (
	// SentBackByReworkRequest is a rework request naming the intent.
	SentBackByReworkRequest SentBackBy = "rework_request"
	// SentBackByGateReject is a gate's reject naming the intent.
	SentBackByGateReject SentBackBy = "gate_reject"
	// SentBackByReplacementConstraint is a replacement constraint's raise
	// landing on the intent.
	SentBackByReplacementConstraint SentBackBy = "replacement_constraint"
	// SentBackByAcceptanceCorrection is a correction at the acceptance
	// round, written by [Intake.CorrectAcceptance] and by nothing else.
	SentBackByAcceptanceCorrection SentBackBy = "acceptance_correction"
)

// SentBackBys is everything that may send an intent back. The CHECK
// constraint in [DDL] lists the same four and the empty value an intent
// nothing has sent back carries.
var SentBackBys = []SentBackBy{
	SentBackByReworkRequest, SentBackByGateReject,
	SentBackByReplacementConstraint, SentBackByAcceptanceCorrection,
}

// Tier is the ordinal value that orders which item is admitted first, in the
// shape every authored value has: a value in force and the policy version it
// was in force under, so an override is a new version and not a rewrite of
// what the requester proposed. Value 0 with an empty version is a tier
// nothing has written, and [DDL] refuses either one without the other.
//
// The design does not fix how many values the scale has, so nothing here
// bounds it above.
type Tier struct {
	Value         int
	PolicyVersion string
}

// Written reports whether a tier has been written at all.
func (t Tier) Written() bool { return t.Value != 0 || t.PolicyVersion != "" }

// Evidence is what raised an intent the factory raised itself, and what a
// detector attaches on rather than raising a second intent for the same
// condition. Every field is optional and a detector fills the ones its
// condition is keyed by; [Evidence.Key] is the stored form, and two evidences
// are the same evidence when their keys are equal.
//
// Keyed too finely it raises an intent per observation; too coarsely it
// attaches two problems to one intent, which decomposition then has to split.
type Evidence struct {
	ServiceID       string `json:"service_id,omitempty"`
	ConsumerID      string `json:"consumer_id,omitempty"`
	ContractID      string `json:"contract_id,omitempty"`
	Element         string `json:"element,omitempty"`
	ReleaseID       string `json:"release_id,omitempty"`
	ConstraintID    string `json:"constraint_id,omitempty"`
	ObjectivePeriod string `json:"objective_period,omitempty"`
	// CriterionID is the criterion a package criterion caller keys the intent
	// becoming unreliable raises by, so a second crossing while that intent is
	// open joins it rather than raising a second one.
	CriterionID string `json:"criterion_id,omitempty"`
}

// Empty reports whether the evidence names nothing at all.
func (e Evidence) Empty() bool { return e == Evidence{} }

// Key is the evidence as it is stored and as it is matched: a JSON object
// whose fields are written in the order they are declared and whose empty
// ones are omitted, so equal evidence has equal bytes. An empty evidence has
// an empty key, which is what an intent nobody raised from evidence stores.
func (e Evidence) Key() (string, error) {
	if e.Empty() {
		return "", nil
	}
	encoded, err := json.Marshal(e)
	if err != nil {
		return "", fmt.Errorf("intent: encoding the evidence: %w", err)
	}
	return string(encoded), nil
}

// Intent is one intent as it is stored.
type Intent struct {
	ID        string
	Actor     record.Actor
	At        string
	Source    Source
	Statement string
	State     State
	Rounds    int
	// ReDecompositions is how many times decomposition has re-decomposed this
	// intent, which is the second of the two counts an intent keeps. It is
	// counted against the same attempt limit the rounds are and in a field of
	// its own, because the two are different stretches of work: an owner
	// answering an escalated interview clears that count alone, and one field
	// would spend an interview's rounds out of decomposition's budget.
	ReDecompositions int
	Tier             Tier
	// ProjectID is the project the request came in under, where a source
	// supplied one. It is where decomposition places a service the work
	// creates, and it is never rewritten once written.
	ProjectID string
	// IntendedEffect is who the change is for and what they should be able to
	// do, or stop having to do, once it ships. It is written at the
	// confirming round and is empty on an intent the factory raised.
	IntendedEffect string
	// Evidence is what raised an intent the factory raised, stored as
	// [Evidence.Key] and empty on the other two sources.
	Evidence string
	// Deadline is the trigger's own time plus a constraint's period, an
	// instant in UTC, and empty where no trigger has occurred.
	Deadline string
	// ConstraintID is the constraint that arrived with the request and binds
	// only what is decomposed from it.
	ConstraintID string
	// SentBackBy is the last thing that wrote unrefined over a later state.
	SentBackBy SentBackBy
	// Outcome is the acceptance round's verdict on the intended effect,
	// computed once at the intent's close, and empty on an intent the factory
	// raised.
	Outcome string
}

// Question is one question of an intent's interview as it is stored. Answer
// and AnsweredAt are both empty until an answer is written onto it.
type Question struct {
	ID         string
	Actor      record.Actor
	At         string
	IntentID   string
	Round      int
	Question   string
	Answer     string
	AnsweredAt string
}

// Answered reports whether the question has its answer. The field is what a
// reader tells an answered question from an unanswered one by, the record
// being written twice rather than once per state.
func (q Question) Answered() bool { return q.AnsweredAt != "" }
