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

// closeRefusals is what one close is refused against: who is closing it, the
// author named on the version under decision, who holds the row's duty at the
// close, who has already referred it, and the human the record's own routing
// bars. Everything here is read once, immediately before the append, so the
// refusal is evaluated against the roster in force at the close.
type closeRefusals struct {
	// actor is the name the close's actor resolves to, which is what an
	// artifact's author is written as. It is the actor's own key where no
	// resolver is composed.
	actor string
	// author is the author named on the artifact version the open event names,
	// and is empty at an event gate.
	author string
	// holders is who the People declaration records as holding the row's duty at
	// the close.
	holders []string
	// referrers is every holder who has already referred this row.
	referrers []string
	// notHuman is the human the record's own routing bars from deciding.
	notHuman string
}

// refusalsFor reads what one close is refused against. It is called immediately
// before the append and its answer is handed to the writer for that append
// alone.
func (g *Gate) refusalsFor(ctx context.Context, opened Opened, actor record.Actor) (closeRefusals, error) {
	refusals := closeRefusals{
		actor:     actor.Key,
		referrers: opened.Referrers,
		notHuman:  opened.WaitsOn.NotHuman,
	}
	if actor.Kind == record.KindHuman && g.humanName != nil {
		name, err := g.humanName(ctx, actor.Key)
		if err != nil {
			return closeRefusals{}, fmt.Errorf("gate: resolving who %s is: %w", actor.Key, err)
		}
		refusals.actor = name
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

// selfApproval reports whether this close is the version's own author closing a
// row no second holder of its duty exists for. Where one does, [closeRefusals.refuse]
// refuses the close instead; where none does, the row still fires to the editor
// and the close event carries the field.
func (r closeRefusals) selfApproval() bool {
	return r.author != "" && r.actor == r.author && !r.anotherHolderExists()
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
	if r.notHuman != "" && r.actor == r.notHuman {
		return fmt.Errorf("%w: %s", ErrClosedByTheActor, r.actor)
	}
	if r.author != "" && r.actor == r.author && r.anotherHolderExists() {
		return fmt.Errorf("%w: %s authored it, and %v holds the duty", ErrSelfApproval, r.actor, r.holders)
	}
	if verdict == VerdictRefer && r.alreadyTheOwners() {
		return fmt.Errorf("%w: %v have referred it and it already waits on the owner",
			ErrNobodyLeftToReferTo, r.referrers)
	}
	return nil
}

// leftToReferTo is every holder of the row's duty who has neither referred it
// nor is referring it now. A referred row re-fires to one of these, and a refer
// with none left is refused, the screen saying so.
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

// alreadyTheOwners reports whether the row already waits on the owner, which is
// where a row nobody holds goes. A refer there has nobody left to reach: the
// row widened once already, and what the human has instead is a reject whose
// reason says what they could not read.
func (r closeRefusals) alreadyTheOwners() bool { return len(r.holders) == 0 }
