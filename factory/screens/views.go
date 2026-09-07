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

// The three errors a method of [Views] or of [Calls] returns to say what kind
// of answer this is. Anything else is a fault of the server's own, answered
// 500, and each of these is wrapped rather than returned bare so that the
// message says which record and which refusal.
var (
	// ErrNotFound is what an address, or a record a call names by id,
	// resolves to nothing: an id naming no item, no decision, no service on
	// that environment, no constraint, no safeguard, and so on. The server
	// answers it 404.
	ErrNotFound = errors.New("screens: no record at that address")
	// ErrNotPermitted is a call the caller is not the one to make: the
	// standing of the principal it carries, and nothing about the call's own
	// arguments. The server answers it 403.
	ErrNotPermitted = errors.New("screens: the principal this call carries acts nowhere")
	// ErrRefused is a call the server understood and declined on its
	// arguments or on what the records say — a field left empty, a value it
	// cannot read, a record not in the state the call asks of it. The server
	// answers it 422: the call reached the right address in a shape this
	// server reads, and what it asked for is what was refused.
	ErrRefused = errors.New("screens: the call was understood and refused")
)

// Filter narrows [Views.Work]'s board. WaitingOnAHuman is the home view's own
// filter: everything on the board that also waits on a human — a pending
// gate, a UAT assignment, an interview question, an escalation, one of the
// factory's own holds whose named cause is a record only a human writes, or
// one of dispatch's two constraint-caused stops.
type Filter struct {
	WaitingOnAHuman bool
}
