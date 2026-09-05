package contractcheck

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
)

// Actor is who this component's one write is made as: the removal intent the
// detector takes in through intake. Every other operation here reads.
var Actor = record.Actor{Kind: record.KindComponent, Key: "contract_check"}

// Candidate is the candidate one check is about: the item, the service it
// changes, that service's name for the words a rejection is read in, the build,
// and the candidate's own environment — which is where the run being observed is.
type Candidate struct {
	ItemID        string
	ServiceID     string
	ServiceName   string
	BuildID       string
	EnvironmentID string
}

func (c Candidate) validate() error {
	for _, required := range []struct{ what, value string }{
		{"item", c.ItemID}, {"service", c.ServiceID}, {"service name", c.ServiceName},
		{"build", c.BuildID}, {"environment", c.EnvironmentID},
	} {
		if required.value == "" {
			return fmt.Errorf("%w: it names no %s", ErrCandidateIncomplete, required.what)
		}
	}
	return nil
}

// Checkout is what the candidate's build says: what it publishes, and what it
// declares about what it reads. It is an interface because reaching a checkout is
// the deploy agent's work and this component reaches none — the arrangement the
// merge queue already has for everything it needs done to a repository.
//
// Both derivations are one toolchain's, which is why they are behind this seam and
// not inlined: a second toolchain is a second implementation of these two methods
// and no change to the rest of enforcement.
type Checkout interface {
	// Publishes is every form the candidate's build publishes.
	Publishes(ctx context.Context, c Candidate) ([]contract.Form, error)
	// Declares is every predicate the candidate's build declares, drawn from the
	// allowed predicate kinds in force.
	Declares(ctx context.Context, c Candidate, allowed []string) ([]consumercontract.Draft, error)
}

// Exchanges is what the candidate's run wrote: one document per unit of work,
// which is what a predicate is decided against. It is an interface for the reason
// the health monitor's signal is one — what emits it is the software the factory wrote
// and where it lands is the substrate's arrangement, so a check that read a file
// would be a check that only works on one kind of target.
type Exchanges interface {
	// Observed is every exchange document the candidate's build wrote on its own
	// environment. None is a real answer: a producer that emitted nothing has not
	// shown that a consumer's assumption holds.
	Observed(ctx context.Context, c Candidate) ([]consumercontract.Document, error)
}

var (
	// ErrCandidateIncomplete is returned for a candidate this component was not
	// told enough about.
	ErrCandidateIncomplete = errors.New("contractcheck: the candidate is missing something every check needs")
	// ErrNoCheckout is returned by [New] for a component with no checkout to
	// read. A check that cannot read what a candidate publishes has nothing to
	// diff and nothing to decide, and it would pass every candidate silently.
	ErrNoCheckout = errors.New("contractcheck: a check with no checkout to read decides nothing")
	// ErrNoExchanges is returned by [New] for a component with no run to observe.
	// Two of the five predicate kinds are decidable against nothing else, so a
	// component without this would report a consumer's assumption as met when it
	// had not been read.
	ErrNoExchanges = errors.New("contractcheck: a check with no run to observe cannot decide a consumer contract")
)

// Check is enforcement over one factory: the producer's own diff, every consumer
// contract, the consumer contracts in force, the deprecation list, and
// the detector that raises a removal.
//
// It writes one record and only one — the removal intent, through intake — and
// everything else it does is a read of the graph. That is what makes "what does
// this break" a query rather than an estimate.
type Check struct {
	pool      *pgxpool.Pool
	policy    *policy.Reader
	intake    *intent.Intake
	checkout  Checkout
	exchanges Exchanges
}

// New returns the check over pool, reading what is in force through the policy,
// taking a removal intent in through intake, and reading a candidate through the
// checkout and its run through exchanges.
//
// A nil intake is allowed and the two seams are not. A factory that cannot take an
// intent in still enforces — the diff and the consumer contracts are most of what
// enforcement does — and what it loses is the detector, which is the one thing here
// that writes.
func New(pool *pgxpool.Pool, p *policy.Reader, intake *intent.Intake,
	checkout Checkout, exchanges Exchanges) (*Check, error) {
	if checkout == nil {
		return nil, ErrNoCheckout
	}
	if exchanges == nil {
		return nil, ErrNoExchanges
	}
	return &Check{pool: pool, policy: p, intake: intake, checkout: checkout, exchanges: exchanges}, nil
}
