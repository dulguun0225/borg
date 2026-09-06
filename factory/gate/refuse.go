package gate

import (
	"context"
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/record"
)

// The two refusals the log's writer cannot evaluate on its own, and one the
// routing of a record adds. The writer refuses five closes: a reject with no
// reason, a second close on one opening, and a close on an abandoned row are its
// own; a refer with nobody left to refer to and a close by the author of the
// version under decision are supplied from here, because both depend on the
// People declaration and the artifact store.
//
// Everything compared here is a per-person key. The People declaration records
// who holds a duty by key, an artifact version records the actor that wrote it
// by key, and a safeguard's routing names a key: the mapping from a key to a
// name is outside the chain, so no refusal reads it.

// closeRefusals is what one close is refused against: who is closing it, the
// actor that wrote the version under decision, who holds the row's duty at the
// close, who has already referred it, whether the row already waits on the
// owner, and the human the record's own routing bars. Everything here is read
// once, immediately before the append, so the refusal is evaluated against the
// roster in force at the close.
type closeRefusals struct {
	// actor is the per-person key of whoever is closing the row.
	actor string
	// author is the actor the artifact version the open event names was written
	// by, and is the zero actor at an event gate.
	author record.Actor
	// holders is who the People declaration records as holding the row's duty at
	// the close, by per-person key.
	holders []string
	// human is the per-person key of the named human the row waits on, and is
	// empty where it waits on a duty or on the owner. A named human other than
	// the actor is a second decider where the duty has no other holder.
	human string
	// referrers is every holder who has already referred this row, by key.
	referrers []string
	// waitsOnTheOwner is whether the row being closed already waits on the
	// owner, which is where a row nobody is left to hold widens to.
	waitsOnTheOwner bool
	// notHuman is the per-person key of the human the record's own routing bars
	// from deciding.
	notHuman string
}

// refusalsFor reads what one close is refused against. It is called immediately
// before the append and its answer is handed to the writer for that append
// alone.
func (g *Gate) refusalsFor(ctx context.Context, opened Opened, actor record.Actor) (closeRefusals, error) {
	refusals := closeRefusals{
		actor:           actor.Key,
		referrers:       opened.Referrers,
		waitsOnTheOwner: opened.WaitsOn.TheOwner(),
		notHuman:        opened.WaitsOn.NotHuman,
		human:           opened.WaitsOn.Human,
	}
	author, err := g.authorOf(ctx, opened.ArtifactID)
	if err != nil {
		return closeRefusals{}, err
	}
	refusals.author = author
	if opened.WaitsOn.Duty != 0 {
		holders, err := g.holdersOf(ctx, opened.WaitsOn.Duty)
		if err != nil {
			return closeRefusals{}, err
		}
		refusals.holders = holders
	}
	return refusals, nil
}

// byTheAuthor reports whether this close is being given by the human who wrote
// the version under decision. Only a human authors a version somebody can then
// approve for themselves: an agent's key is a model version and the gate
// component's is its own name, and neither closes a row as a person.
func (r closeRefusals) byTheAuthor() bool {
	return r.author.Kind == record.KindHuman && r.author.Key != "" && r.author.Key == r.actor
}

// byTheWriter reports whether this close is being given by the human the
// record's own routing bars: the actor on a withdrawal, or the human who
// authored a shorter retention value.
func (r closeRefusals) byTheWriter() bool {
	return r.notHuman != "" && r.actor == r.notHuman
}

// selfApproval reports whether this close is the version's own author, or the
// writer of the record under decision, closing a row no second decider exists
// for. Where one does, [closeRefusals.refuse] refuses the close instead; where
// none does, the row still fires to that person and the close event carries the
// field — an install with one owner cannot separate who wrote a record from who
// decides it, and what the row buys there is a chained decision with an actor
// and a time on it.
func (r closeRefusals) selfApproval() bool {
	return (r.byTheAuthor() || r.byTheWriter()) && !r.anotherDeciderExists()
}

// anotherDeciderExists reports whether somebody other than whoever is closing
// the row could have closed it: the named human the row waits on, or a holder of
// its duty. A row that widens to the owner names neither, and the owner is not a
// row of the People declaration, so a row waiting on the owner has no second
// decider this package can name.
func (r closeRefusals) anotherDeciderExists() bool {
	if r.human != "" && r.human != r.actor {
		return true
	}
	return r.anotherHolderExists()
}

// anotherHolderExists reports whether the People declaration records a holder of
// the row's duty other than whoever is closing it.
func (r closeRefusals) anotherHolderExists() bool {
	for _, holder := range r.holders {
		if holder != r.actor {
			return true
		}
	}
	return false
}

// refuse is what the log's writer calls inside its own transaction, after its
// own checks pass and before the insert.
func (r closeRefusals) refuse(verdict Verdict) error {
	if r.byTheWriter() && r.anotherDeciderExists() {
		return fmt.Errorf("%w: %s wrote it, and %s could decide it",
			ErrClosedByTheActor, r.actor, r.deciders())
	}
	if r.byTheAuthor() && r.anotherDeciderExists() {
		return fmt.Errorf("%w: %s authored it, and %s could decide it",
			ErrSelfApproval, r.actor, r.deciders())
	}
	if verdict == VerdictRefer && r.nobodyLeftToReferTo() {
		return fmt.Errorf("%w: %v have referred it and it already waits on the owner",
			ErrNobodyLeftToReferTo, r.referrers)
	}
	return nil
}

// deciders is who could have closed the row instead, in the words the two
// refusals above name them by: the named human the row waits on, or the holders
// of its duty.
func (r closeRefusals) deciders() string {
	if r.human != "" && r.human != r.actor {
		return r.human
	}
	return fmt.Sprint(r.holders)
}

// leftToReferTo is every holder of the row's duty who has neither referred it
// nor is referring it now. A referred row re-fires to one of these, and where
// none is left it widens to the owner, which is where every unheld row goes.
func (r closeRefusals) leftToReferTo() []string {
	var left []string
	for _, holder := range r.holders {
		if holder == r.actor || slices.Contains(r.referrers, holder) {
			continue
		}
		left = append(left, holder)
	}
	return left
}

// nobodyLeftToReferTo reports whether this refer has nobody to reach: no holder
// of the duty who has not referred it, on a row that already waits on the owner.
// A refer by the last holder is not that case — it re-fires to the owner, the
// one widening every unheld row takes — and a refer at the widened row is,
// because the row cannot widen twice. What that human has left is a reject whose
// reason says what they could not read.
func (r closeRefusals) nobodyLeftToReferTo() bool {
	return len(r.leftToReferTo()) == 0 && r.waitsOnTheOwner
}
