package contractcheck

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/lastcheck"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
)

// Actor is who this component's writes are made as: the brownout intent and the
// removal intent the detector takes in through intake. Every other operation here
// reads.
var Actor = record.Actor{Kind: record.KindComponent, Key: "contract_check", Basis: record.BasisClaimed}

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
// declares about what it reads and sends. It is an interface because reaching a
// checkout is the deployer's work and this component reaches none — the
// arrangement the merge queue already has for everything it needs done to a
// repository.
//
// Both derivations are one toolchain's, which is why they are behind this seam and
// not inlined: a second toolchain is a second implementation of these two methods
// and no change to the rest of enforcement.
type Checkout interface {
	// Publishes is every form the candidate's build publishes.
	Publishes(ctx context.Context, c Candidate) ([]contract.Form, error)
	// Declares is what the extractor made of the candidate's build: the
	// predicates it found, the constructs it could not follow, or the cause it
	// could not derive at all.
	Declares(ctx context.Context, c Candidate, allowed []string) (consumercontract.Derived, error)
	// DeclaresSchemaChange is whether the candidate's build declares a schema
	// change at all — the reading the build's own process made of its checkout.
	// It is asked because a form that moves is not a change to apply: a store
	// contract's form is derived from the code, and a build can move it without
	// shipping anything for a deploy to run. The two exercises the candidate
	// environment performs on a change — applying it twice, and the snapshot
	// before a destructive one — are asked for only where a change is declared,
	// so a candidate that declares none is not refused for a change it does not
	// carry.
	DeclaresSchemaChange(ctx context.Context, c Candidate) (bool, error)
	// DeclaresBackfill is what a backfill item's build declares: the store
	// contract, the element it fills, and the element it fills from. A backfill
	// item is a release whose change is data and not form — it declares no
	// schema diff and opens no contract version, only that pair — so neither the
	// diff nor [Checkout.DeclaresSchemaChange] says there is a change to
	// exercise, and this is what does. Empty is every item that is not one.
	DeclaresBackfill(ctx context.Context, c Candidate) (deploy.Backfill, error)
}

// Exchanges is what the candidate's run wrote: one document per unit of work,
// which is what a predicate is decided against. It is an interface for the reason
// the health monitor's signal is one — what emits it is the software the factory wrote
// and where it lands is the platform's arrangement, so a check that read a file
// would be a check that only works on one kind of target.
type Exchanges interface {
	// Observed is every exchange document the candidate's build wrote on its own
	// environment. None is a real answer: a predicate decided against nothing is
	// undecided, and undecided is read at the gate the way a failure is.
	Observed(ctx context.Context, c Candidate) ([]consumercontract.Document, error)
}

// StoreState is the candidate environment's own store after the run, which is
// what decides a store migration's middle items: their diff is empty by
// construction, both forms being present throughout, so the diff alone would pass
// them unconditionally.
//
// It is a seam for the reason [Exchanges] is: reaching a store is the deployer's
// work through the environment's credential, and this component reaches none.
type StoreState interface {
	// Rows is what the candidate's run left in its environment's own store for
	// one store contract, one document per row, which every store declaration in
	// force — read and write — is decided against. None is undecided, the way no
	// exchange is.
	Rows(ctx context.Context, c Candidate, storeName string) ([]consumercontract.Document, error)
	// AppliedTwice is what a second run of the candidate's change changed on that
	// environment. Every change is authored to be applied twice without effect,
	// because an engine that cannot put a change and its history row in one
	// transaction can leave a change applied with no row, and a second
	// application that changes anything is a rejection at Merge to master. A
	// backfill's change takes the same exercise for its own reason: it is
	// written to be rerun from where it stopped, so a deploy rerun after a stop
	// finishes the copy.
	AppliedTwice(ctx context.Context, c Candidate) (SecondApplication, error)
	// Snapshot is the snapshot the candidate environment took and verified before
	// a change that destroys stored data. One it cannot take and verify is a
	// rejection at Merge to master, the deploy of such a change resting on it.
	Snapshot(ctx context.Context, c Candidate) (Snapshot, error)
}

// SecondApplication is what a second run of the candidate's change on its
// environment did — the schema change it declares, or the backfill's copy, which
// take the same exercise.
type SecondApplication struct {
	// Ran is whether the environment ran it twice at all. False is a candidate
	// with no change to run.
	Ran bool
	// Changed is whether the second application changed anything, and What says
	// what it changed for the words a rejection is read in.
	Changed bool
	What    string
}

// Snapshot is the snapshot taken before a destructive change.
type Snapshot struct {
	// Taken and Verified are the two steps: a snapshot the deployer cannot take
	// and verify is a deploy not performed, and the candidate environment
	// exercises it the same way.
	Taken    bool
	Verified bool
	// Name is where what the change destroys can still be read, and Why is what
	// stopped it where it was not taken or not verified.
	Name string
	Why  string
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
	// Two of the nine predicate kinds are decidable against nothing else, so a
	// component without this would report a consumer's assumption as undecided
	// where a run would have decided it.
	ErrNoExchanges = errors.New("contractcheck: a check with no run to observe cannot decide a consumer contract")
	// ErrNoStoreState is returned by [New] for a component with no candidate
	// store to read. A store migration's middle items have an empty diff by
	// construction, so a component without this would pass them unconditionally.
	ErrNoStoreState = errors.New("contractcheck: a check with no candidate store to read cannot decide a store migration")
)

// Check is enforcement over one factory: the producer's own diff, every consumer
// contract, the consumer contracts in force, the store migration a candidate
// declares, the deprecation list, and the detector that raises a brownout and a
// removal.
//
// It writes two records and only two beyond its own last check — the brownout
// intent and the removal intent, both through intake — and everything else it
// does is a read of the graph. That is what makes "what does this break" a query
// rather than an estimate.
type Check struct {
	pool      *pgxpool.Pool
	policy    *policy.Reader
	intake    *intent.Intake
	checks    *lastcheck.Writer
	interval  time.Duration
	checkout  Checkout
	exchanges Exchanges
	store     StoreState
}

// New returns the check over pool, reading what is in force through the policy,
// taking an intent in through intake, writing the pass over the deprecation
// list's own last check through checks with the interval it promises the next
// pass within, and reading a candidate through the checkout, its run through
// exchanges, and its environment's store through store.
//
// A nil intake is allowed and the three seams are not. A factory that cannot take
// an intent in still enforces — the diff and the consumer contracts are most of
// what enforcement does — and what it loses is the detector, which is the one
// thing here that writes, its own last check included: a pass that never runs
// leaves nothing to check in on. A nil checks is likewise allowed and [Check.Raise]
// then writes no last check, the way a health monitor with no writer of its own
// does not.
//
// Which backfills a deploy record marks complete is no seam: it is a read of the
// deploy records this component already reads what is running from, and a seam
// there would be a second answer able to disagree with the record.
func New(pool *pgxpool.Pool, p *policy.Reader, intake *intent.Intake,
	checks *lastcheck.Writer, interval time.Duration,
	checkout Checkout, exchanges Exchanges, store StoreState) (*Check, error) {
	if checkout == nil {
		return nil, ErrNoCheckout
	}
	if exchanges == nil {
		return nil, ErrNoExchanges
	}
	if store == nil {
		return nil, ErrNoStoreState
	}
	return &Check{
		pool: pool, policy: p, intake: intake, checks: checks, interval: interval,
		checkout: checkout, exchanges: exchanges, store: store,
	}, nil
}
