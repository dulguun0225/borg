package record

import (
	"errors"
	"fmt"
	"slices"
)

// Kind is what an actor is. There are two, and a record names one of them.
type Kind string

const (
	// KindHuman is a person: the owner, an approver, whoever vetoed.
	KindHuman Kind = "human"
	// KindComponent is a part of the factory: a gate, a writer, an agent.
	KindComponent Kind = "component"
)

// Kinds is every kind an actor may have. The CHECK constraint in
// [Constraints] lists the same two, and TestConstraintsListEveryKind fails if
// the two lists stop agreeing.
var Kinds = []Kind{KindHuman, KindComponent}

// Actor is who wrote a record. Both fields are required: a kind that is one of
// [Kinds], and a name that is not empty. The name is not resolved against
// anything — no directory, no account — so it is whatever the component that
// decided identifies itself as.
type Actor struct {
	Kind Kind
	Name string
}

var (
	// ErrKindUnknown is returned for an actor whose kind is empty or is
	// neither human nor component.
	ErrKindUnknown = errors.New("record: actor kind is neither human nor component")
	// ErrNameEmpty is returned for an actor with no name.
	ErrNameEmpty = errors.New("record: actor name is empty")
)

// Validate reports whether the actor may be written. A writer calls it before
// it stores a record; the store refuses the same two cases through
// [Constraints], so a path that skips this method does not get a record in
// without an actor.
func (a Actor) Validate() error {
	if !slices.Contains(Kinds, a.Kind) {
		return fmt.Errorf("%w: %q", ErrKindUnknown, a.Kind)
	}
	if a.Name == "" {
		return fmt.Errorf("%w: kind %q", ErrNameEmpty, a.Kind)
	}
	return nil
}
