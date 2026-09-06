package principal

import (
	"errors"
	"fmt"

	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrDispatchOnlyOnAnAgent is returned by [Principal.Validate] for a human's
	// or a component's principal carrying a dispatch or a scope. Neither is
	// dispatched to anything.
	ErrDispatchOnlyOnAnAgent = errors.New("principal: only an agent's principal names a dispatch and a scope")
	// ErrAgentNamesNoDispatch is returned for an agent's principal missing the
	// dispatch or the scope. The scope is what travels with every call the agent
	// makes, so an agent principal without one carries nothing a policy could
	// ever read.
	ErrAgentNamesNoDispatch = errors.New("principal: an agent's principal names the dispatch that put it on the item and the scope it was dispatched under")
)

// Principal is who a call is made as, carried from the entrance of the factory
// to whatever the call reaches. A human at a screen calls as the People row
// they say they are, an agent as its fleet entry plus the dispatch that put it
// on the item, and a component as itself.
//
// It decides nothing. Whatever the call reaches records it beside what was
// asked for and refuses on it never — populated, self-asserted, enforced by
// nothing.
type Principal struct {
	// Actor is the seam 1 actor: the kind, the key, and whether the key is
	// claimed or verified.
	Actor record.Actor
	// DispatchID is the dispatch that put an agent on an item, and is empty on a
	// human's and on a component's principal.
	DispatchID string
	// Scope is what the agent was dispatched under, which travels with every
	// call it makes. It is empty on a human's and on a component's principal.
	Scope string
}

// OfComponent is the principal a component calls as: itself, named the way its
// actor is. Its key is claimed, seam 5 naming no check a component's own name
// could be verified by.
func OfComponent(name string) Principal {
	return Principal{Actor: record.Actor{Kind: record.KindComponent, Key: name, Basis: record.BasisClaimed}}
}

// OfHuman is the principal a human at a screen calls as: the per-person opaque
// key of the People row they say they are, and the basis on which the key was
// obtained.
func OfHuman(key string, basis record.Basis) Principal {
	return Principal{Actor: record.Actor{Kind: record.KindHuman, Key: key, Basis: basis}}
}

// OfAgent is the principal an agent calls as: the model version its fleet entry
// names, the dispatch that put it on the item, and the scope it was dispatched
// under. Its key is claimed: seam 5 verifies an agent by checking its dispatch
// against its scope, and nothing here makes that check.
func OfAgent(modelVersion, dispatchID, scope string) Principal {
	return Principal{
		Actor:      record.Actor{Kind: record.KindAgent, Key: modelVersion, Basis: record.BasisClaimed},
		DispatchID: dispatchID,
		Scope:      scope,
	}
}

// Validate reports whether the principal may be carried on a call: the actor is
// one [record.Actor.Validate] admits, and the dispatch and the scope are on an
// agent's principal and on no other.
func (p Principal) Validate() error {
	if err := p.Actor.Validate(); err != nil {
		return err
	}
	if p.Actor.Kind != record.KindAgent {
		if p.DispatchID != "" || p.Scope != "" {
			return fmt.Errorf("%w: kind %q names dispatch %q and scope %q",
				ErrDispatchOnlyOnAnAgent, p.Actor.Kind, p.DispatchID, p.Scope)
		}
		return nil
	}
	if p.DispatchID == "" || p.Scope == "" {
		return fmt.Errorf("%w: %q names dispatch %q and scope %q",
			ErrAgentNamesNoDispatch, p.Actor.Key, p.DispatchID, p.Scope)
	}
	return nil
}

// IsZero reports whether the principal names nobody, which is what a caller
// that supplied none passes.
func (p Principal) IsZero() bool { return p == Principal{} }

// String is what a caller records beside what was asked for: the kind, the key,
// and an agent's dispatch and scope. It renders no secret, a principal holding
// none.
func (p Principal) String() string {
	if p.IsZero() {
		return "nobody"
	}
	rendered := string(p.Actor.Kind) + " " + p.Actor.Key
	if p.Actor.Kind == record.KindAgent {
		rendered += " dispatched by " + p.DispatchID + " under " + p.Scope
	}
	return rendered
}
