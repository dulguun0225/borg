package screens

import (
	"context"
	"errors"

	"github.com/dulguun0225/borg/factory/principal"
)

// Views is every read the four screens make: one method per address, plus
// Home, which answers for no address at all, and the two board reads, Work
// and Ops. Every method takes the calling principal, the way every method on
// [Calls] does.
type Views interface {
	// Home is the home view: the badge and its parts, a row per last check
	// record past the interval it names, the readiness reading per role,
	// and, only where the badge is zero, the digest.
	Home(ctx context.Context, p principal.Principal) (Home, error)
	// Work is the board or list, narrowed to what Filter selects.
	Work(ctx context.Context, p principal.Principal, filter Filter) (Work, error)
	// Item is one item's own timeline.
	Item(ctx context.Context, p principal.Principal, id string) (Item, error)
	// Decision is one gate's own view: the open event, the vector, who it
	// is routed to, its acknowledgements, its close or its abandonment, and
	// its deliveries.
	Decision(ctx context.Context, p principal.Principal, id string) (Decision, error)
	// Ops is every service on every environment.
	Ops(ctx context.Context, p principal.Principal) (Ops, error)
	// ServiceOn is one service's own view on one environment.
	ServiceOn(ctx context.Context, p principal.Principal, serviceID, environmentID string) (Service, error)
	// Factory is the machine itself: policy in force, the fleet, the
	// constraints and projects, and the factory's own numbers.
	Factory(ctx context.Context, p principal.Principal) (Factory, error)
	// Constraint is one constraint record's own view.
	Constraint(ctx context.Context, p principal.Principal, id string) (Constraint, error)
	// People is every row of the People declaration.
	People(ctx context.Context, p principal.Principal) (People, error)
}

// ErrNotFound is what an address, or a record a call names by id, resolves
// to nothing: an id naming no item, no decision, no service on that
// environment, no constraint, no safeguard, and so on. The server answers it
// 404. It is the one error a method of [Views] or of [Calls] returns to mean
// exactly this and nothing else — every other error is a fault the server
// answers 500.
var ErrNotFound = errors.New("screens: no record at that address")

// Filter narrows [Views.Work]'s board. WaitingOnAHuman is the home view's own
// filter: everything on the board that also waits on a human — a pending
// gate, a UAT assignment, an interview question, an escalation, one of the
// factory's own holds whose named cause is a record only a human writes, or
// one of dispatch's two constraint-caused stops.
type Filter struct {
	WaitingOnAHuman bool
}
