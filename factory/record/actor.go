package record

import (
	"errors"
	"fmt"
	"slices"
)

// Kind is what an actor is. There are three, and a record names one of them.
type Kind string

const (
	// KindHuman is a person: the owner, an approver, whoever undid a shipped
	// change.
	KindHuman Kind = "human"
	// KindComponent is a part of the factory: a gate, a writer, the merge
	// queue.
	KindComponent Kind = "component"
	// KindAgent is a model in a role the factory runs, dispatched to a
	// stage. An agent is not a component and calls as the stage it was
	// dispatched to, so it is not folded into KindComponent.
	KindAgent Kind = "agent"
)

// Kinds is every kind an actor may have. The CHECK constraint in
// [Constraints] lists the same three, and TestConstraintsListEveryKind fails
// if the two lists stop agreeing.
var Kinds = []Kind{KindHuman, KindComponent, KindAgent}

// Basis is how an actor's key was obtained. There are two, and only a human
// actor carries one.
type Basis string

const (
	// BasisClaimed is a key nothing has verified: the state every key holds
	// from the first record, before anything checks a caller.
	BasisClaimed Basis = "claimed"
	// BasisVerified is a key seam 5 of ../../end-goal/deferred.md#security-comes-last
	// has checked.
	BasisVerified Basis = "verified"
)

// Bases is every basis an actor may have. The CHECK constraint in
// [Constraints] lists the same two, and TestConstraintsListEveryKind checks
// both lists.
var Bases = []Basis{BasisClaimed, BasisVerified}

// Actor is who wrote a record. Key is never empty, and what it holds depends
// on Kind: the per-person opaque key for a human, never a name; the
// component's name for a component; the model version for an agent. Basis is
// required on a human — whether the key is claimed or verified — and empty on
// the other two kinds, which nothing verifies.
//
// Key is not resolved against anything here: the mapping from a human's key
// to a name is the People declaration's, outside this chain, so that erasing
// the mapping leaves the chain, its links, and its counts standing.
type Actor struct {
	Kind  Kind
	Key   string
	Basis Basis
}

var (
	// ErrKindUnknown is returned for an actor whose kind is empty or is none
	// of [Kinds].
	ErrKindUnknown = errors.New("record: actor kind is unknown")
	// ErrKeyEmpty is returned for an actor with no key.
	ErrKeyEmpty = errors.New("record: actor key is empty")
	// ErrBasisEmpty is returned for a human actor with no basis.
	ErrBasisEmpty = errors.New("record: actor basis is empty")
	// ErrBasisUnknown is returned for a human actor whose basis is neither
	// [BasisClaimed] nor [BasisVerified].
	ErrBasisUnknown = errors.New("record: actor basis is unknown")
	// ErrBasisNotEmpty is returned for a component or agent actor that
	// carries a basis, which only a human's key has one.
	ErrBasisNotEmpty = errors.New("record: actor basis is set on a non-human kind")
)

// Validate reports whether the actor may be written. A writer calls it before
// it stores a record; the store refuses the same cases through [Constraints],
// so a path that skips this method does not get a record in without an
// actor.
func (a Actor) Validate() error {
	if !slices.Contains(Kinds, a.Kind) {
		return fmt.Errorf("%w: %q", ErrKindUnknown, a.Kind)
	}
	if a.Key == "" {
		return fmt.Errorf("%w: kind %q", ErrKeyEmpty, a.Kind)
	}
	if a.Kind == KindHuman {
		if a.Basis == "" {
			return fmt.Errorf("%w: key %q", ErrBasisEmpty, a.Key)
		}
		if !slices.Contains(Bases, a.Basis) {
			return fmt.Errorf("%w: %q", ErrBasisUnknown, a.Basis)
		}
		return nil
	}
	if a.Basis != "" {
		return fmt.Errorf("%w: kind %q basis %q", ErrBasisNotEmpty, a.Kind, a.Basis)
	}
	return nil
}
