package intent

import "github.com/dulguun0225/borg/factory/record"

// Source is where an intent came from. There are three, and each is a caller
// of [Intake] rather than a writer of its own.
type Source string

const (
	// SourceOwner is a request an owner typed. It is the one source M1
	// writes.
	SourceOwner Source = "owner"
	// SourceReports is end-user reports a grouper turned into one intent.
	// Nothing writes it yet; the grouper is a later milestone.
	SourceReports Source = "reports"
	// SourceDetector is something the factory found itself. Nothing writes it
	// yet; the detectors are later milestones.
	SourceDetector Source = "detector"
)

// Sources is every source an intent may have. The CHECK constraint in [DDL]
// lists the same three, and TestDDLListsEverySourceAndState fails if the two
// stop agreeing.
var Sources = []Source{SourceOwner, SourceReports, SourceDetector}

// State is where an intent is. It advances in place, because the state says
// where the intent is and is not read back to justify a decision.
type State string

const (
	// StateUnrefined is how every intent arrives.
	StateUnrefined State = "unrefined"
	// StateRefined means the factory has enough to cut the intent and author
	// a spec per item, which is the same thing as ready to cut.
	StateRefined State = "refined"
	// StateEscalated is where the interview's rounds exceeded the attempt
	// bound. Nothing in M1 writes it — the bound is not built — and the value
	// is in the CHECK because the design names it.
	StateEscalated State = "escalated"
)

// States is every state an intent may have. The CHECK constraint in [DDL]
// lists the same three, and TestDDLListsEverySourceAndState fails if the two
// stop agreeing.
var States = []State{StateUnrefined, StateRefined, StateEscalated}

// Intent is one intent as it is stored.
type Intent struct {
	ID        string
	Actor     record.Actor
	At        string
	Source    Source
	Statement string
	State     State
	Rounds    int
}

// Question is one question of an intent's interview as it is stored. Answer
// and AnsweredAt are both empty until [Intake.Answer] writes them together.
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
