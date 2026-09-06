// The fixtures every other file in this package builds a graph from: the
// graph of writers a test composes the check over, and the builders for a
// form, a draft, a shipped release, and a candidate. The four seams the
// component is composed with are fakes_test.go's.
//
// The rejection at the merge row, the three items of a migration, and the
// safeguard's predicate are demonstrated through the command-line interface in
// cmd/factory, where there is a checkout to derive from and a process writing
// exchange documents. What is here is the arithmetic of the graph, and these
// tests do not skip when the database is unreachable — an unreachable
// database fails the run.
package contractcheck_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/contractcheck"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/targetseam"
	"github.com/dulguun0225/borg/factory/window"
)

var (
	theActor = record.Actor{Kind: record.KindComponent, Key: "test", Basis: record.BasisClaimed}
	theOwner = record.Actor{Kind: record.KindHuman, Key: "owner", Basis: record.BasisClaimed}
	// theApprover is the human at the gate row that decides a safeguard's
	// withdrawal, which is routed away from whoever wrote it.
	theApprover = record.Actor{Kind: record.KindHuman, Key: "approver", Basis: record.BasisClaimed}
	theBy       = artifact.By{Authorship: artifact.AuthorshipAgent, Author: "fake-model-1"}
)

// theInterface is the name every producer here gives what it publishes, and
// theStore the name it gives its own store.
const (
	theInterface = "health"
	theStore     = "ledger"
)

// graph is one test's records and the writers it writes them through.
type graph struct {
	pool       *pgxpool.Pool
	token      lease.Token
	builds     *build.Writer
	releases   *release.Writer
	deploys    *deploy.Writer
	windows    *window.Writer
	items      *item.Decomposition
	store      *artifact.Store
	factory    *policy.Factory
	checkout   *fakeCheckout
	exchanges  *fakeExchanges
	storeState *fakeStoreState
	check      *contractcheck.Check
	// production is the environment record every deploy here is written against,
	// and the one the producer's own diff reads what is running from.
	production string
	// productionTargets is that environment's own targets, in the order every
	// deploy here is started with — one target is what the test install gives
	// production, and both services deploy onto it.
	productionTargets []string
	// producer and consumer are the two service records: an interface has
	// consumers, and the consumers are other services in the same factory.
	producer service.Service
	consumer service.Service
}

func newGraph(t *testing.T) (context.Context, graph) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m5_check_" + hex.EncodeToString(suffix[:])

	pool, err := postgres.Open(ctx, inSchema(t, postgres.URL(), schema))
	if err != nil {
		t.Fatalf("the database at %s is not reachable, and these tests do not skip: %v", postgres.URL(), err)
	}
	t.Cleanup(func() {
		drop, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := pool.Exec(drop, `drop schema if exists `+pgx.Identifier{schema}.Sanitize()+` cascade`); err != nil {
			t.Errorf("dropping schema %s: %v", schema, err)
		}
		pool.Close()
	})
	if _, err := pool.Exec(ctx, `create schema `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("creating schema %s: %v", schema, err)
	}
	if err := postgres.Apply(ctx, pool); err != nil {
		t.Fatalf("applying the schema: %v", err)
	}
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}

	g := graph{
		pool:     pool,
		token:    token,
		builds:   build.NewWriter(pool, token),
		releases: release.NewWriter(pool, token),
		deploys:  deploy.NewWriter(pool, token),
		windows:  window.NewWriter(pool, token),
		items:    item.NewDecomposition(pool, token),
		store:    artifact.NewStore(pool, token),
		factory:  policy.NewFactory(pool, token),
		checkout: &fakeCheckout{
			publishes:      map[string][]contract.Form{},
			declares:       map[string][]consumercontract.Draft{},
			noSchemaChange: map[string]bool{},
		},
		exchanges:  &fakeExchanges{observed: map[string][]consumercontract.Document{}},
		storeState: newFakeStoreState(),
	}
	installed, err := g.factory.Install(ctx, theOwner, "acme", []string{t.TempDir()}, secretref.MustNew("deploy.local"), 8)
	if err != nil {
		t.Fatalf("installing the factory: %v", err)
	}
	g.production = installed.Production.ID
	g.productionTargets = installed.Production.Addresses()

	writer := service.NewWriter(pool, token)
	g.producer, err = writer.Create(ctx, theActor, "producer", t.TempDir(), installed.Project.ID)
	if err != nil {
		t.Fatalf("writing the producer: %v", err)
	}
	g.consumer, err = writer.Create(ctx, theActor, "consumer", t.TempDir(), installed.Project.ID)
	if err != nil {
		t.Fatalf("writing the consumer: %v", err)
	}

	g.check, err = contractcheck.New(pool, policy.NewReader(pool, token, score.Version{}), intent.NewIntake(pool, token),
		g.checkout, g.exchanges, g.storeState)
	if err != nil {
		t.Fatalf("composing the check: %v", err)
	}
	return ctx, g
}

func inSchema(t *testing.T, base, schema string) string {
	t.Helper()
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parsing %s: %v", base, err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// element is a field of a form, every one of these tests' elements being a
// field and never an operation, an argument, or a message. [published] and
// [stored] set its position: output on an interface, since every one of these
// is something the consumer reads, and store on a store.
func element(name, kind string, populated, deprecated bool) contract.Element {
	return contract.Element{Name: name, Kind: contract.ElementField, Type: kind, Populated: populated, Deprecated: deprecated}
}

func published(elements ...contract.Element) contract.Form {
	return contract.Form{Name: theInterface, Kind: contract.KindInterface, Elements: positioned(elements, contract.PositionOutput)}
}

func stored(elements ...contract.Element) contract.Form {
	return contract.Form{Name: theStore, Kind: contract.KindStore, Elements: positioned(elements, contract.PositionStore)}
}

// positioned is elements with their position set, since [element] does not know
// which form it will be wrapped into.
func positioned(elements []contract.Element, position contract.Position) []contract.Element {
	with := make([]contract.Element, len(elements))
	for i, e := range elements {
		e.Position = position
		with[i] = e
	}
	return with
}

func draft(producer service.Service, interfaceName, element string,
	kind gatepolicy.PredicateKind, argument string) consumercontract.Draft {
	return consumercontract.Draft{
		Address:           interfaceName,
		ProducerService:   producer.Name,
		ProducerServiceID: producer.ID,
		Interface:         interfaceName,
		Element:           element,
		Kind:              kind,
		Argument:          argument,
	}
}

// ship writes the records one release leaves behind on one service: an item, a
// build, the release with the contract versions its forms publish, a completed
// production deploy, and a window over that deploy at the exit named. An empty exit
// leaves the window open.
//
// It is what a run does, written directly, so a test can put the releases and the
// windows in an order a run cannot easily produce — which is exactly the case the
// last known-good release exists for.
func ship(t *testing.T, ctx context.Context, g graph, svc service.Service,
	forms []contract.Form, declares []consumercontract.Draft, exit window.Exit) (release.Release, string) {
	t.Helper()
	return shipOnIntent(t, ctx, g, svc, newIntent(t, ctx, g), forms, declares, exit)
}

// finishIntent moves a detector's intent to delivered, the state
// [intent.OnEvidence] reads as finished. The evidence key stops a second
// brownout or removal arriving beside one still open on it — "an intent on the
// same evidence that has not finished is that intent" — so a test that ships a
// brownout and then wants a removal newly raised on the same evidence has to
// finish the brownout's own intent first, the way the item behind it finishing
// would.
func finishIntent(t *testing.T, ctx context.Context, g graph, intentID string) {
	t.Helper()
	intake := intent.NewIntake(g.pool, g.token)
	if _, err := intake.Confirm(ctx, theActor, intent.Confirmation{
		IntentID: intentID,
		Requirements: []intent.NewRequirement{{
			Statement:    "no requirement pattern fits a detector's own evidence",
			EscapeReason: "raised by the detector on its own evidence",
		}},
	}); err != nil {
		t.Fatalf("confirming the detector's intent %s: %v", intentID, err)
	}
	if err := intake.Delivered(ctx, theActor, intent.Delivery{IntentID: intentID}); err != nil {
		t.Fatalf("delivering the detector's intent %s: %v", intentID, err)
	}
}

// newIntent takes in a plain intent for a test to decompose an item from. The
// brownout walks a release back to the intent its item names, through
// [intent.Get], so an item minted onto a release here needs one that resolves
// and not the bare id [ship] used to carry.
func newIntent(t *testing.T, ctx context.Context, g graph) string {
	t.Helper()
	taken, err := intent.NewIntake(g.pool, g.token).TakeIn(ctx, theActor, intent.Arrival{
		Source: intent.SourceOwner, Statement: "test intent",
	})
	if err != nil {
		t.Fatalf("taking in an intent: %v", err)
	}
	return taken.ID
}

// shipOnIntent is [ship] over an item decomposed from a named intent, which is
// what a test names to put a marked element's brownout on the evidence its
// intent was raised on.
func shipOnIntent(t *testing.T, ctx context.Context, g graph, svc service.Service, intentID string,
	forms []contract.Form, declares []consumercontract.Draft, exit window.Exit) (release.Release, string) {
	t.Helper()
	it, err := g.items.Create(ctx, theActor, item.New{
		IntentID: intentID, ServiceID: svc.ID, Branch: "item/" + record.NewID("in"),
	}, "", "", nil)
	if err != nil {
		t.Fatalf("decomposing the item: %v", err)
	}
	bl, err := g.builds.Create(ctx, theActor, build.Draft{
		ItemID: it.ID, ServiceID: svc.ID, CommitHash: record.NewID("commit"), ArtifactDigest: record.NewID("digest"),
	})
	if err != nil {
		t.Fatalf("writing the build: %v", err)
	}
	if len(declares) > 0 {
		if _, _, _, err := g.store.SubmitConsumerContract(ctx, theActor, theBy, it.ID, svc.ID, "derived from the build", consumercontract.Derived{Extractor: consumercontract.GoExtractor("test"), Drafts: declares}, ""); err != nil {
			t.Fatalf("submitting the consumer contract: %v", err)
		}
	}
	rel, err := g.releases.MintWith(ctx, theActor,
		release.Minting{ServiceID: svc.ID, BuildID: bl.ID, Commit: bl.CommitHash, ItemID: it.ID},
		func(ctx context.Context, tx pgx.Tx, r release.Release) error {
			_, err := contract.PublishAll(ctx, tx, theActor, svc.ID, r.ID, r.Number, it.ID, forms)
			return err
		})
	if err != nil {
		t.Fatalf("minting the release: %v", err)
	}
	dep := shipDeploy(t, ctx, g, svc, deploy.OfRelease(rel.ID, bl.ID))
	w, err := g.windows.Open(ctx, record.Actor{Kind: record.KindComponent, Key: "health_monitor", Basis: record.BasisClaimed}, window.OpenEvent{
		DeployID: dep, ReleaseID: rel.ID, BuildID: bl.ID, ServiceID: svc.ID, PassedAvailable: true,
		Size:                   map[gatepolicy.Quantity]float64{gatepolicy.QuantityErrorRate: 0.1},
		Power:                  map[gatepolicy.Quantity]float64{gatepolicy.QuantityErrorRate: 0.8},
		Confidence:             0.95,
		CapSeconds:             1,
		BoundaryVersion:        boundary.Version,
		Targets:                g.productionTargets,
		EmissionVersionRelease: "emission/1",
		EmissionVersionControl: "emission/1",
		PolicyVersion:          "pv_1",
		ScoreVersion:           "sv_1",
	})
	if err != nil {
		t.Fatalf("opening the window: %v", err)
	}
	if exit != "" {
		if _, err := g.windows.Close(ctx, w.ID, exit, closedOn()); err != nil {
			t.Fatalf("closing the window at %s: %v", exit, err)
		}
	}
	return rel, w.ID
}

// markBackfill is what a backfill item's release leaves behind: a completed
// deploy record naming the store contract and the pair of elements the copy ran
// between. Enforcement reads that record, so a test that wants a backfill marked
// complete writes one rather than being told the answer.
func markBackfill(t *testing.T, ctx context.Context, g graph, svc service.Service, contractName, element, from string) {
	t.Helper()
	shipDeployWith(t, ctx, g, svc, deploy.OfBuild(record.NewID("bl")),
		deploy.Backfill{Contract: contractName, Element: element, FromElement: from})
}

// shipDeploy starts, reaches and completes a deploy of what onto production's
// one target, without a control — the same discipline package deploy states
// for a target that runs a release as a local process — and returns its id.
func shipDeploy(t *testing.T, ctx context.Context, g graph, svc service.Service, what deploy.What) string {
	t.Helper()
	return shipDeployWith(t, ctx, g, svc, what, deploy.Backfill{})
}

func shipDeployWith(t *testing.T, ctx context.Context, g graph, svc service.Service,
	what deploy.What, backfill deploy.Backfill) string {
	t.Helper()
	targets := make([]deploy.Reaching, len(g.productionTargets))
	for i, address := range g.productionTargets {
		targets[i] = deploy.Reaching{Address: address}
	}
	dep, err := g.deploys.Start(ctx, theActor, deploy.Beginning{
		ServiceID: svc.ID, EnvironmentID: g.production, What: what,
		Targets: targets, IntoProduction: true, StrategyPicked: deploy.StrategyWithoutControl,
		Backfill: backfill,
	})
	if err != nil {
		t.Fatalf("starting the deploy: %v", err)
	}
	for _, address := range g.productionTargets {
		if err := g.deploys.CompleteTarget(ctx, dep.ID, address, targetseam.ReplacementDrained); err != nil {
			t.Fatalf("completing target %s of %s: %v", address, dep.ID, err)
		}
	}
	if err := g.deploys.Complete(ctx, dep.ID); err != nil {
		t.Fatalf("completing the deploy: %v", err)
	}
	return dep.ID
}

// candidateOf is a candidate on one service whose build publishes and declares what
// the fake checkout is given, with an item and a build of its own and a candidate
// environment id that is not production's.
func candidateOf(t *testing.T, ctx context.Context, g graph, svc service.Service,
	forms []contract.Form, declares []consumercontract.Draft, documents []consumercontract.Document) contractcheck.Candidate {
	t.Helper()
	it, err := g.items.Create(ctx, theActor, item.New{
		IntentID: record.NewID("in"), ServiceID: svc.ID, Branch: "item/" + record.NewID("in"),
	}, "", "", nil)
	if err != nil {
		t.Fatalf("decomposing the candidate's item: %v", err)
	}
	bl, err := g.builds.Create(ctx, theActor, build.Draft{
		ItemID: it.ID, ServiceID: svc.ID, CommitHash: record.NewID("commit"), ArtifactDigest: record.NewID("digest"),
	})
	if err != nil {
		t.Fatalf("writing the candidate's build: %v", err)
	}
	g.checkout.publishes[it.ID] = forms
	g.checkout.declares[it.ID] = declares
	g.exchanges.observed[bl.ID] = documents
	return contractcheck.Candidate{
		ItemID: it.ID, ServiceID: svc.ID, ServiceName: svc.Name,
		BuildID: bl.ID, EnvironmentID: record.NewID("env"),
	}
}

// ok is one exchange document a good producer writes.
func ok() []consumercontract.Document {
	return []consumercontract.Document{{"Status": "ok", "Detail": "fine"}}
}

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }

// closedOn is the read a test closes a window on: a count on both arms, which is
// what an exit other than skipped always has. The numbers are not what most of
// these tests assert over — what they assert is the exit — but a close with no
// read is refused, and rightly: an exit nobody can recompute is one nobody can
// argue with. Both arms counting something is what the brownout reads as having
// received volume.
func closedOn() window.Closing {
	return window.Closing{
		On: window.Read{
			Quantities: map[gatepolicy.Quantity]boundary.Counts{
				gatepolicy.QuantityErrorRate: {Units: 200, Count: 2, BaselineUnits: 200, BaselineCount: 2},
			},
		},
		FinestSizeReached: map[gatepolicy.Quantity]float64{gatepolicy.QuantityErrorRate: 0.06},
	}
}
