package intent

import (
	"encoding/json"
	"fmt"

	"github.com/dulguun0225/borg/factory/record"
)

// Kind is which of three a requirement is.
type Kind string

const (
	// KindConfirmed is a statement the requester confirmed at the interview's
	// confirming round.
	KindConfirmed Kind = "confirmed"
	// KindEnumeratedFromEvidence is a statement the factory enumerated from
	// the evidence of an intent it raised itself, confirmed by nobody.
	KindEnumeratedFromEvidence Kind = "enumerated_from_evidence"
	// KindDerived is one item's share of a requirement several items answer,
	// derived by decomposition and written by intake at its call.
	KindDerived Kind = "derived"
)

// Kinds is every kind a requirement may have. The CHECK constraint in [DDL]
// lists the same three, and TestDDLListsEveryKindAndPattern fails if the two
// stop agreeing.
var Kinds = []Kind{KindConfirmed, KindEnumeratedFromEvidence, KindDerived}

// Requirement is one statement of what the factory understood is wanted, as
// it is stored. Its id is opaque, stable and never reused, the rule a
// criterion id keeps.
type Requirement struct {
	ID        string
	Actor     record.Actor
	At        string
	IntentID  string
	Statement string
	// Pattern is which of the six the statement fits, and empty where it fits
	// none, in which case EscapeReason says why it was admitted anyway.
	Pattern      Pattern
	EscapeReason string
	Kind         Kind
	// DerivedFrom is the requirement a derived one derives from, and empty on
	// the other two kinds.
	DerivedFrom string
	// ItemID is the item answering the share a derived requirement states,
	// and empty on the other two kinds.
	ItemID string
	// SupersededAt is when a later confirming round superseded this
	// requirement, and empty while it is in force.
	SupersededAt string
	// SupersededBy is the requirements that replaced this one, a list because
	// one statement may become two and empty where the requester retracted
	// it. It is written only where SupersededAt is.
	SupersededBy []string
	// UnanswerableReason is decomposition's tagged mark on a requirement it
	// judged unanswerable, and empty on every other requirement.
	UnanswerableReason string
}

// InForce reports whether the requirement is part of the reading in force:
// the set the last confirming round wrote, which is what decomposition's
// completeness check reads and no earlier one.
func (r Requirement) InForce() bool { return r.SupersededAt == "" }

// Unanswerable reports whether decomposition marked the requirement as one no
// item can answer.
func (r Requirement) Unanswerable() bool { return r.UnanswerableReason != "" }

// NewRequirement is one statement of a reading, as the confirming round
// supplies it. EscapeReason is given only for a statement fitting none of the
// six patterns, and Supersedes names the requirements of the earlier reading
// this statement replaces, which is empty at a first confirming round.
type NewRequirement struct {
	Statement    string
	EscapeReason string
	Supersedes   []string
}

// Derivation is one item's share of a requirement several items answer, as
// decomposition supplies it.
type Derivation struct {
	IntentID string
	// DerivedFrom is the requirement in force whose share this states.
	DerivedFrom string
	// ItemID is the item that answers the share.
	ItemID       string
	Statement    string
	EscapeReason string
}

// encodeSuperseded is the stored form of a supersession pointer: a JSON list,
// `[]` where the requester retracted the statement and nothing replaced it.
// An empty string is not a pointer at all, which is what a requirement in
// force stores, and [DDL] keeps the two apart by pairing the column with the
// time.
func encodeSuperseded(by []string) (string, error) {
	if by == nil {
		by = []string{}
	}
	encoded, err := json.Marshal(by)
	if err != nil {
		return "", fmt.Errorf("intent: encoding a supersession pointer: %w", err)
	}
	return string(encoded), nil
}

// decodeSuperseded reads back what encodeSuperseded wrote.
func decodeSuperseded(stored string) ([]string, error) {
	if stored == "" {
		return nil, nil
	}
	var by []string
	if err := json.Unmarshal([]byte(stored), &by); err != nil {
		return nil, fmt.Errorf("intent: reading a supersession pointer: %w", err)
	}
	return by, nil
}
