// Enforcement's own tests, and they are about what no end-to-end run can isolate:
// the two baselines, the range consumer contracts in force are read over, the
// deprecation list emptying, and what a store's forward promise refuses. Each is a
// query over records a run writes in passing, so a run that got one wrong would
// fail somewhere else and say something else.
//
// The rejection at the merge row, the three items of a migration, and the
// safeguard's predicate are demonstrated through the crude interface in
// cmd/factory, where there
// is a checkout to derive from and a process writing exchange documents. What is here
// is the arithmetic of the graph.
//
// These tests do not skip when the database is unreachable — the milestone is
// demonstrated by them running, so an unreachable database fails the run.
package contractcheck_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"slices"
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
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/safeguard"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/window"
)

var (
	theActor = record.Actor{Kind: record.KindComponent, Name: "test"}
	theOwner = record.Actor{Kind: record.KindHuman, Name: "owner"}
	theBy    = artifact.By{Authorship: artifact.AuthorshipAgent, Author: "fake-model-1"}
)

// theInterface is the name every producer here gives what it publishes, and
// theStore the name it gives its own store.
const (
	theInterface = "health"
	theStore     = "ledger"
)

// fakeCheckout is what a candidate's build publishes and declares, by item. It
// stands where the deploy agent would: the derivation is one toolchain's and what
// enforcement needs is the answer, so a test hands it one rather than writing Go
// source to a directory.
type fakeCheckout struct {
	publishes map[string][]contract.Form
	declares  map[string][]consumercontract.Draft
}

func (f *fakeCheckout) Publishes(_ context.Context, c contractcheck.Candidate) ([]contract.Form, error) {
	return f.publishes[c.ItemID], nil
}

func (f *fakeCheckout) Declares(_ context.Context, c contractcheck.Candidate, _ []string) ([]consumercontract.Draft, error) {
	return f.declares[c.ItemID], nil
}

// fakeExchanges is the documents one build wrote, by build. No entry is no document
// observed, which enforcement treats as a failure wherever there is a consumer
// contract to decide.
type fakeExchanges struct {
	observed map[string][]consumercontract.Document
}

func (f *fakeExchanges) Observed(_ context.Context, c contractcheck.Candidate) ([]consumercontract.Document, error) {
	return f.observed[c.BuildID], nil
}

// graph is one test's records and the writers it writes them through.
type graph struct {
	pool      *pgxpool.Pool
	builds    *build.Writer
	releases  *release.Writer
	deploys   *deploy.Writer
	windows   *window.Writer
	items     *item.Decomposition
	store     *artifact.Store
	factory   *policy.Factory
	checkout  *fakeCheckout
	exchanges *fakeExchanges
	check     *contractcheck.Check
	// production is the environment record every deploy here is written against,
	// and the one the producer's own diff reads what is running from.
	production string
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

	g := graph{
		pool:     pool,
		builds:   build.NewWriter(pool),
		releases: release.NewWriter(pool),
		deploys:  deploy.NewWriter(pool),
		windows:  window.NewWriter(pool),
		items:    item.NewDecomposition(pool),
		store:    artifact.NewStore(pool),
		factory:  policy.NewFactory(pool),
		checkout: &fakeCheckout{
			publishes: map[string][]contract.Form{},
			declares:  map[string][]consumercontract.Draft{},
		},
		exchanges: &fakeExchanges{observed: map[string][]consumercontract.Document{}},
	}
	installed, err := g.factory.Install(ctx, theOwner, []string{t.TempDir()}, secretref.MustNew("deploy.local"))
	if err != nil {
		t.Fatalf("installing the factory: %v", err)
	}
	g.production = installed.Production.ID

	writer := service.NewWriter(pool)
	g.producer, err = writer.Create(ctx, theActor, "producer", t.TempDir())
	if err != nil {
		t.Fatalf("writing the producer: %v", err)
	}
	g.consumer, err = writer.Create(ctx, theActor, "consumer", t.TempDir())
	if err != nil {
		t.Fatalf("writing the consumer: %v", err)
	}

	g.check, err = contractcheck.New(pool, policy.NewReader(pool, score.Version{}), intent.NewIntake(pool), g.checkout, g.exchanges)
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

func element(name, kind string, populated, deprecated bool) contract.Element {
	return contract.Element{Name: name, Type: kind, Populated: populated, Deprecated: deprecated}
}

func published(elements ...contract.Element) contract.Form {
	return contract.Form{Name: theInterface, Kind: contract.KindInterface, Elements: elements}
}

func stored(elements ...contract.Element) contract.Form {
	return contract.Form{Name: theStore, Kind: contract.KindStore, Elements: elements}
}

func draft(producer service.Service, interfaceName, element string,
	kind gatepolicy.PredicateKind, argument string) consumercontract.Draft {
	return consumercontract.Draft{
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
	it, err := g.items.Create(ctx, theActor, item.New{
		IntentID: record.NewID("in"), ServiceID: svc.ID, Branch: "item/" + record.NewID("in"),
	})
	if err != nil {
		t.Fatalf("decomposing the item: %v", err)
	}
	bl, err := g.builds.Create(ctx, theActor, it.ID, record.NewID("commit"))
	if err != nil {
		t.Fatalf("writing the build: %v", err)
	}
	if len(declares) > 0 {
		if _, _, err := g.store.SubmitConsumerContract(ctx, theActor, theBy, it.ID, svc.ID,
			"derived from the build", declares); err != nil {
			t.Fatalf("submitting the consumer contract: %v", err)
		}
	}
	rel, err := g.releases.MintWith(ctx, theActor, svc.ID, bl.ID, it.ID,
		func(ctx context.Context, tx pgx.Tx, r release.Release) error {
			_, err := contract.PublishAll(ctx, tx, theActor, svc.ID, r.ID, r.Number, it.ID, forms)
			return err
		})
	if err != nil {
		t.Fatalf("minting the release: %v", err)
	}
	dep, err := g.deploys.Start(ctx, theActor, svc.ID, g.production, deploy.OfRelease(rel.ID, bl.ID))
	if err != nil {
		t.Fatalf("starting the deploy: %v", err)
	}
	if err := g.deploys.Complete(ctx, dep.ID); err != nil {
		t.Fatalf("completing the deploy: %v", err)
	}
	w, err := g.windows.Open(ctx, record.Actor{Kind: record.KindComponent, Name: "health_monitor"}, window.Opening{
		DeployID: dep.ID, ReleaseID: rel.ID, ServiceID: svc.ID, ClearedAvailable: true,
		Size: 0.1, Confidence: 0.95, CapSeconds: 1, Formula: "test",
		PolicyVersion: "pv_1", ScoreVersion: "sv_1",
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

// candidateOf is a candidate on one service whose build publishes and declares what
// the fake checkout is given, with an item and a build of its own and a candidate
// environment id that is not production's.
func candidateOf(t *testing.T, ctx context.Context, g graph, svc service.Service,
	forms []contract.Form, declares []consumercontract.Draft, documents []consumercontract.Document) contractcheck.Candidate {
	t.Helper()
	it, err := g.items.Create(ctx, theActor, item.New{
		IntentID: record.NewID("in"), ServiceID: svc.ID, Branch: "item/" + record.NewID("in"),
	})
	if err != nil {
		t.Fatalf("decomposing the candidate's item: %v", err)
	}
	bl, err := g.builds.Create(ctx, theActor, it.ID, record.NewID("commit"))
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

// TestABreakingDiffIsRejectedWhereAConsumerStillDeclaresTheElement, and passes once
// nothing does — which is the whole of "without the migration already shipped ahead
// of it".
func TestABreakingDiffIsRejectedWhereAConsumerStillDeclaresTheElement(t *testing.T) {
	ctx, g := newGraph(t)

	full := published(element("Status", "string", true, false), element("Detail", "string", false, false))
	ship(t, ctx, g, g.producer, []contract.Form{full}, nil, window.ExitTimedOut)
	ship(t, ctx, g, g.consumer, nil, []consumercontract.Draft{
		draft(g.producer, theInterface, "Status", gatepolicy.PredicateRead, ""),
		draft(g.producer, theInterface, "Detail", gatepolicy.PredicateRead, ""),
	}, window.ExitTimedOut)

	trimmed := published(element("Status", "string", true, false))
	removing := candidateOf(t, ctx, g, g.producer, []contract.Form{trimmed}, nil, ok())
	checked, err := g.check.Enforce(ctx, removing, g.production)
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if checked.Passed() {
		t.Fatalf("a removal of an element the consumer declares passed: %+v", checked.Broken)
	}
	if checked.Check() != gate.AutoRejectedByContractDiff {
		t.Errorf("the check that rejected is %q, want the producer's own diff", checked.Check())
	}
	if !slices.Contains(checked.Affected, g.consumer.ID) {
		t.Errorf("the affected services are %v, want the consumer %s", checked.Affected, g.consumer.ID)
	}
	// The rejection names the consumer it would break, which is the whole point of
	// the graph answering who is affected.
	if !contains(checked.Why(), g.consumer.ID) {
		t.Errorf("the rejection does not name the consumer: %s", checked.Why())
	}

	// The consumer's next release stops reading it, which empties the list with
	// nobody withdrawing anything — and then the same removal passes.
	ship(t, ctx, g, g.consumer, nil, []consumercontract.Draft{
		draft(g.producer, theInterface, "Status", gatepolicy.PredicateRead, ""),
	}, window.ExitTimedOut)
	again := candidateOf(t, ctx, g, g.producer, []contract.Form{trimmed}, nil, ok())
	checked, err = g.check.Enforce(ctx, again, g.production)
	if err != nil {
		t.Fatalf("the second Enforce: %v", err)
	}
	if !checked.Passed() {
		t.Fatalf("the removal is still refused after the list emptied: %s", checked.Why())
	}
	if len(checked.Broken) != 1 || len(checked.Broken[0].Change.Breaking) != 1 {
		t.Fatalf("the diff is recorded as %+v, and a breaking change that is allowed is still breaking", checked.Broken)
	}
	if checked.Broken[0].Next != (contract.Semver{Major: 2}) {
		t.Errorf("the removal would mint %s, want a major", checked.Broken[0].Next)
	}
}

// TestConsumerContractsInForceRunFromTheLastKnownGoodToTheNewest, and a service
// with no window closed cleared or timed out has none and every release it has is
// in the range.
func TestConsumerContractsInForceRunFromTheLastKnownGoodToTheNewest(t *testing.T) {
	ctx, g := newGraph(t)

	// Three releases of the consumer: the first two declare an element the third
	// stops declaring, and only the second's window closes at the cap. The last
	// known-good release is the release the newest closed window watched, so
	// releases 2 and 3 are in force and release 1 is not.
	ship(t, ctx, g, g.consumer, nil, []consumercontract.Draft{
		draft(g.producer, theInterface, "Gone", gatepolicy.PredicateRead, ""),
	}, window.ExitCondemned)
	second, secondWindow := ship(t, ctx, g, g.consumer, nil, []consumercontract.Draft{
		draft(g.producer, theInterface, "Detail", gatepolicy.PredicateRead, ""),
	}, window.ExitTimedOut)
	ship(t, ctx, g, g.consumer, nil, []consumercontract.Draft{
		draft(g.producer, theInterface, "Status", gatepolicy.PredicateRead, ""),
	}, "")

	in, err := g.check.ConsumerContractsInForce(ctx, g.consumer.ID)
	if err != nil {
		t.Fatalf("ConsumerContractsInForce: %v", err)
	}
	if !in.HasLastKnownGood || in.LastKnownGoodNumber != second.Number || in.LastKnownGoodWindowID != secondWindow {
		t.Fatalf("the last known-good release is %+v, want release %d set by window %s", in, second.Number, secondWindow)
	}
	if in.HighestNumber != 3 {
		t.Errorf("the newest release is %d, want three", in.HighestNumber)
	}
	if len(in.ItemIDs) != 2 {
		t.Fatalf("the range holds %d items, want the two from the last known-good release up", len(in.ItemIDs))
	}
	named := map[string]bool{}
	for _, p := range in.Predicates {
		named[p.Element] = true
	}
	if !named["Detail"] || !named["Status"] {
		t.Errorf("the consumer contracts in force name %v, want Detail and Status", named)
	}
	if named["Gone"] {
		t.Error("a release below the last known-good release is still in force, and a rollback cannot return to it")
	}
}

// TestAServiceWithNoClosedWindowHasNoLastKnownGoodAndEveryReleaseInForce: which is
// the direction a first release's missing rollback target already takes.
func TestAServiceWithNoClosedWindowHasNoLastKnownGoodAndEveryReleaseInForce(t *testing.T) {
	ctx, g := newGraph(t)

	ship(t, ctx, g, g.consumer, nil, []consumercontract.Draft{
		draft(g.producer, theInterface, "Status", gatepolicy.PredicateRead, ""),
	}, "")
	ship(t, ctx, g, g.consumer, nil, []consumercontract.Draft{
		draft(g.producer, theInterface, "Detail", gatepolicy.PredicateRead, ""),
	}, "")

	in, err := g.check.ConsumerContractsInForce(ctx, g.consumer.ID)
	if err != nil {
		t.Fatalf("ConsumerContractsInForce: %v", err)
	}
	if in.HasLastKnownGood {
		t.Fatalf("a service with no closed window has a last known-good release: %+v", in)
	}
	if len(in.ItemIDs) != 2 || len(in.Predicates) != 2 {
		t.Fatalf("the range holds %d items and %d predicates, want both releases'", len(in.ItemIDs), len(in.Predicates))
	}

	// A service with no release at all has nothing in force: every consumer
	// contract it derived belongs to a candidate that has not merged.
	empty, err := g.check.ConsumerContractsInForce(ctx, g.producer.ID)
	if err != nil {
		t.Fatalf("ConsumerContractsInForce on a service with no release: %v", err)
	}
	if empty.HighestNumber != 0 || len(empty.Predicates) != 0 {
		t.Fatalf("a service with no release has %+v in force", empty)
	}
}

// TestTheDeprecationListIsAQueryAndTheDetectorRaisesTheRemovalOnce.
func TestTheDeprecationListIsAQueryAndTheDetectorRaisesTheRemovalOnce(t *testing.T) {
	ctx, g := newGraph(t)

	marked := published(element("Status", "string", true, false), element("Detail", "string", false, true))
	ship(t, ctx, g, g.producer, []contract.Form{marked}, nil, window.ExitTimedOut)
	ship(t, ctx, g, g.consumer, nil, []consumercontract.Draft{
		draft(g.producer, theInterface, "Detail", gatepolicy.PredicateRead, ""),
	}, window.ExitTimedOut)

	list, err := g.check.Deprecated(ctx)
	if err != nil {
		t.Fatalf("Deprecated: %v", err)
	}
	if len(list) != 1 || list[0].Element.Name != "Detail" {
		t.Fatalf("the marked elements are %+v, want Detail alone", list)
	}
	if list[0].Empty() {
		t.Fatal("the list on Detail is empty and the consumer still declares it")
	}
	if !slices.Contains(list[0].Consumers(), g.consumer.ID) {
		t.Errorf("the list names %v, want the consumer", list[0].Consumers())
	}
	if raised, err := g.check.RaiseRemovals(ctx); err != nil || len(raised) != 0 {
		t.Fatalf("the detector raised %d removals while a consumer still declares the element, %v", len(raised), err)
	}

	// The consumer's next release stops reading it. Nothing withdrew anything: the
	// next derivation found nothing, and the query stopped seeing it.
	ship(t, ctx, g, g.consumer, nil, []consumercontract.Draft{
		draft(g.producer, theInterface, "Status", gatepolicy.PredicateRead, ""),
	}, window.ExitTimedOut)

	list, err = g.check.Deprecated(ctx)
	if err != nil {
		t.Fatalf("the second Deprecated: %v", err)
	}
	if len(list) != 1 || !list[0].Empty() {
		t.Fatalf("the list on Detail is %+v after the consumer stopped reading it", list)
	}
	raised, err := g.check.RaiseRemovals(ctx)
	if err != nil {
		t.Fatalf("RaiseRemovals: %v", err)
	}
	if len(raised) != 1 || !raised[0].New {
		t.Fatalf("the detector raised %+v, want one new intent", raised)
	}
	want := contractcheck.RemovalStatement(g.producer.Name, theInterface, "Detail")
	if raised[0].Intent.Statement != want {
		t.Errorf("the intent's statement is %q, want %q", raised[0].Intent.Statement, want)
	}

	// A second pass takes nothing in: the intent is still unrefined, and the
	// statement is the handle.
	again, err := g.check.RaiseRemovals(ctx)
	if err != nil {
		t.Fatalf("the second RaiseRemovals: %v", err)
	}
	if len(again) != 1 || again[0].New || again[0].Intent.ID != raised[0].Intent.ID {
		t.Fatalf("the second pass raised %+v, want the intent the first one took in", again)
	}
}

// TestASafeguardsPredicateBlocksTheRemovalAndIsToldApartFromAConsumerContract: a
// safeguard never stops the item existing, only passing, and what a reader of that
// rejection needs is the safeguard and its author.
func TestASafeguardsPredicateBlocksTheRemovalAndIsToldApartFromAConsumerContract(t *testing.T) {
	ctx, g := newGraph(t)

	full := published(element("Status", "string", true, false), element("Detail", "string", false, true))
	ship(t, ctx, g, g.producer, []contract.Form{full}, nil, window.ExitTimedOut)

	con, found, err := contract.ByName(ctx, g.pool, g.producer.ID, theInterface)
	if err != nil || !found {
		t.Fatalf("ByName = found %v, %v", found, err)
	}
	placed, _, err := g.factory.AddSafeguard(ctx, theOwner, gatepolicy.SafeguardPredicate,
		safeguard.Subject{Kind: safeguard.SubjectContractElement, ID: contract.ElementSubject(con.ID, "Detail")},
		safeguard.Bound{Predicate: safeguard.Predicate{Kind: gatepolicy.PredicateRead}})
	if err != nil {
		t.Fatalf("adding the safeguard: %v", err)
	}

	// The detector still raises the removal — a safeguard never stops the item existing.
	raised, err := g.check.RaiseRemovals(ctx)
	if err != nil {
		t.Fatalf("RaiseRemovals: %v", err)
	}
	if len(raised) != 1 || !raised[0].New {
		t.Fatalf("the detector raised %+v with a safeguard standing, want the removal intent", raised)
	}

	trimmed := published(element("Status", "string", true, false))
	removing := candidateOf(t, ctx, g, g.producer, []contract.Form{trimmed}, nil, ok())
	checked, err := g.check.Enforce(ctx, removing, g.production)
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if checked.Passed() {
		t.Fatal("the removal passed with a safeguard's predicate naming the element")
	}
	if checked.Check() != gate.AutoRejectedBySafeguardPredicate {
		t.Errorf("the check that rejected is %q, want the safeguard's predicate", checked.Check())
	}
	if !contains(checked.Why(), placed.ID) || !contains(checked.Why(), theOwner.Name) {
		t.Errorf("the rejection names neither the safeguard nor its author: %s", checked.Why())
	}

	// Withdrawing it lets the next candidate through, which is how an invented read
	// is taken back.
	if _, err := g.factory.WithdrawSafeguard(ctx, theOwner, placed.ID); err != nil {
		t.Fatalf("withdrawing the safeguard: %v", err)
	}
	after := candidateOf(t, ctx, g, g.producer, []contract.Form{trimmed}, nil, ok())
	checked, err = g.check.Enforce(ctx, after, g.production)
	if err != nil {
		t.Fatalf("the second Enforce: %v", err)
	}
	if !checked.Passed() {
		t.Fatalf("the removal is still refused after the safeguard was withdrawn: %s", checked.Why())
	}
}

// TestAStoresForwardPromiseRefusesAPopulatedAddition, which nothing on an interface
// refuses and no list empties to allow.
func TestAStoresForwardPromiseRefusesAPopulatedAddition(t *testing.T) {
	ctx, g := newGraph(t)

	ship(t, ctx, g, g.producer, []contract.Form{stored(element("ID", "string", true, false))}, nil, window.ExitTimedOut)

	populated := candidateOf(t, ctx, g, g.producer, []contract.Form{
		stored(element("ID", "string", true, false), element("Amount", "int64", true, false)),
	}, nil, nil)
	checked, err := g.check.Enforce(ctx, populated, g.production)
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if checked.Passed() {
		t.Fatal("a store gained an always-populated element and the build being restored does not write it")
	}
	if checked.Check() != gate.AutoRejectedByContractDiff {
		t.Errorf("the check that rejected is %q", checked.Check())
	}
	if !contains(checked.Why(), "rollback restores") {
		t.Errorf("the rejection does not name the store's own consumer: %s", checked.Why())
	}

	// The same element added optional is what the first item of a store migration
	// does, and it passes.
	optional := candidateOf(t, ctx, g, g.producer, []contract.Form{
		stored(element("ID", "string", true, false), element("Amount", "int64", false, false)),
	}, nil, nil)
	checked, err = g.check.Enforce(ctx, optional, g.production)
	if err != nil {
		t.Fatalf("the second Enforce: %v", err)
	}
	if !checked.Passed() {
		t.Fatalf("adding the element optional is refused too: %s", checked.Why())
	}
}

// TestAConsumerContractIsDecidedAgainstTheCandidatesOwnRun: all five kinds are
// decidable against an observed exchange, and no exchange at all is a failure rather
// than a pass.
func TestAConsumerContractIsDecidedAgainstTheCandidatesOwnRun(t *testing.T) {
	ctx, g := newGraph(t)

	full := published(element("Status", "string", true, false))
	ship(t, ctx, g, g.producer, []contract.Form{full}, nil, window.ExitTimedOut)
	ship(t, ctx, g, g.consumer, nil, []consumercontract.Draft{
		draft(g.producer, theInterface, "Status", gatepolicy.PredicateRead, ""),
		draft(g.producer, theInterface, "Status", gatepolicy.PredicateDomain, "ok|error"),
	}, window.ExitTimedOut)

	// A candidate whose run writes a value outside the domain the consumer declared:
	// the form is unchanged, so the diff says nothing, and what catches it is the
	// consumer contract decided against the run.
	outside := candidateOf(t, ctx, g, g.producer, []contract.Form{full}, nil,
		[]consumercontract.Document{{"Status": "unknown"}})
	checked, err := g.check.Enforce(ctx, outside, g.production)
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if checked.Passed() {
		t.Fatal("a value outside the declared domain passed, and no schema diff would have caught it")
	}
	if checked.Check() != gate.AutoRejectedByConsumerContract {
		t.Errorf("the check that rejected is %q, want a consumer contract", checked.Check())
	}
	if checked.Observed != 1 {
		t.Errorf("%d exchange documents were observed, want the one written", checked.Observed)
	}

	// A candidate whose run wrote nothing has not shown that the assumption holds.
	silent := candidateOf(t, ctx, g, g.producer, []contract.Form{full}, nil, nil)
	checked, err = g.check.Enforce(ctx, silent, g.production)
	if err != nil {
		t.Fatalf("the second Enforce: %v", err)
	}
	if checked.Passed() {
		t.Fatal("a producer that wrote no exchange document passed a consumer contract it never showed holding")
	}

	// And one whose run stays inside it passes.
	inside := candidateOf(t, ctx, g, g.producer, []contract.Form{full}, nil,
		[]consumercontract.Document{{"Status": "ok"}, {"Status": "error"}})
	checked, err = g.check.Enforce(ctx, inside, g.production)
	if err != nil {
		t.Fatalf("the third Enforce: %v", err)
	}
	if !checked.Passed() {
		t.Fatalf("a run inside the declared domain is refused: %s", checked.Why())
	}
}

// TestTheTwoBaselinesAreDifferent: a producer's own diff runs against what is
// running, and a consumer contract against what its producer's newest
// release publishes.
func TestTheTwoBaselinesAreDifferent(t *testing.T) {
	ctx, g := newGraph(t)

	// The producer's release 1 is running. Release 2 has merged and is not
	// deployed, and it removed the element — so what runs still has it and the newest
	// does not.
	full := published(element("Status", "string", true, false), element("Detail", "string", false, false))
	ship(t, ctx, g, g.producer, []contract.Form{full}, nil, window.ExitTimedOut)
	merged, err := g.items.Create(ctx, theActor, item.New{
		IntentID: record.NewID("in"), ServiceID: g.producer.ID, Branch: "item/merged",
	})
	if err != nil {
		t.Fatalf("decomposing the merged item: %v", err)
	}
	bl, err := g.builds.Create(ctx, theActor, merged.ID, record.NewID("commit"))
	if err != nil {
		t.Fatalf("writing the build: %v", err)
	}
	trimmed := published(element("Status", "string", true, false))
	if _, err := g.releases.MintWith(ctx, theActor, g.producer.ID, bl.ID, merged.ID,
		func(ctx context.Context, tx pgx.Tx, r release.Release) error {
			_, err := contract.PublishAll(ctx, tx, theActor, g.producer.ID, r.ID, r.Number,
				merged.ID, []contract.Form{trimmed})
			return err
		}); err != nil {
		t.Fatalf("minting the merged release: %v", err)
	}

	// A consumer candidate that newly declares the element the producer has already
	// removed on master fails at its own gate, because a consumer contract is
	// checked against the version its producer's newest release publishes.
	consuming := candidateOf(t, ctx, g, g.consumer, nil, []consumercontract.Draft{
		draft(g.producer, theInterface, "Detail", gatepolicy.PredicateRead, ""),
	}, nil)
	checked, err := g.check.Enforce(ctx, consuming, g.production)
	if err != nil {
		t.Fatalf("Enforce on the consumer: %v", err)
	}
	if checked.Passed() {
		t.Fatal("a consumer newly declaring an element its producer's newest release does not publish passed")
	}
	if len(checked.Unmet) != 1 || checked.Unmet[0].Draft.Element != "Detail" {
		t.Fatalf("the unmet consumer contracts are %+v", checked.Unmet)
	}

	// The producer's own next candidate diffs against what is running, which still
	// has the element: it is running release 1.
	producing := candidateOf(t, ctx, g, g.producer, []contract.Form{trimmed}, nil, ok())
	checked, err = g.check.Enforce(ctx, producing, g.production)
	if err != nil {
		t.Fatalf("Enforce on the producer: %v", err)
	}
	if len(checked.Broken) != 1 {
		t.Fatalf("the producer's diff produced %+v", checked.Broken)
	}
	if !checked.Broken[0].Had || checked.Broken[0].From != contract.FirstVersion {
		t.Fatalf("the producer's diff ran against %+v, want the version release 1 publishes",
			checked.Broken[0].From)
	}
	if len(checked.Broken[0].Change.Removed) != 1 {
		t.Errorf("the diff against what is running removed %v, want the element", checked.Broken[0].Change.Removed)
	}
}

// TestACandidateCreatingAContractBreaksNothing: an interface a rejected candidate
// publishes points at nothing, so a candidate that would create one has no form to
// diff against.
func TestACandidateCreatingAContractBreaksNothing(t *testing.T) {
	ctx, g := newGraph(t)

	first := candidateOf(t, ctx, g, g.producer,
		[]contract.Form{published(element("Status", "string", true, false))}, nil, ok())
	checked, err := g.check.Enforce(ctx, first, g.production)
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if !checked.Passed() {
		t.Fatalf("a candidate that would create a contract is refused: %s", checked.Why())
	}
	if len(checked.Broken) != 1 || checked.Broken[0].Had {
		t.Fatalf("the diff read a baseline for a contract nothing has published: %+v", checked.Broken)
	}
	if checked.Broken[0].Next != contract.FirstVersion {
		t.Errorf("it would mint %s, want the first version", checked.Broken[0].Next)
	}
}

// TestACheckWithNoSeamIsRefused: a check that cannot read what a candidate publishes
// has nothing to diff, and one with no run to observe would report a consumer's
// assumption as met when it had not been read.
func TestACheckWithNoSeamIsRefused(t *testing.T) {
	ctx, g := newGraph(t)
	_ = ctx

	if _, err := contractcheck.New(g.pool, policy.NewReader(g.pool, score.Version{}), nil, nil, g.exchanges); err == nil {
		t.Error("a check with no checkout was composed")
	}
	if _, err := contractcheck.New(g.pool, policy.NewReader(g.pool, score.Version{}), nil, g.checkout, nil); err == nil {
		t.Error("a check with no run to observe was composed")
	}
}

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }

// closedOn is the read a test closes a window on: a pair of counts with a
// baseline in it, which is what an exit other than swept always has. The numbers
// are not what any of these tests assert over — what they assert is the exit —
// but a close with no read is refused, and rightly: an exit nobody can recompute
// is one nobody can argue with.
func closedOn() boundary.Observed {
	return boundary.Observed{Units: 200, Failures: 2, BaselineUnits: 200, BaselineFailures: 2}
}
