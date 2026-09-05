// The fixtures every other file in this package builds a graph from: the fake
// checkout and exchanges standing in for the deploy agent's derivation, the
// graph of writers a test composes the check over, and the builders for a
// form, a draft, a shipped release, and a candidate.
//
// The rejection at the merge row, the three items of a migration, and the
// safeguard's predicate are demonstrated through the crude interface in
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
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
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
	w, err := g.windows.Open(ctx, record.Actor{Kind: record.KindComponent, Name: "health_monitor"}, window.OpenEvent{
		DeployID: dep.ID, ReleaseID: rel.ID, ServiceID: svc.ID, PassedAvailable: true,
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

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }

// closedOn is the read a test closes a window on: a pair of counts with a
// baseline in it, which is what an exit other than swept always has. The numbers
// are not what any of these tests assert over — what they assert is the exit —
// but a close with no read is refused, and rightly: an exit nobody can recompute
// is one nobody can argue with.
func closedOn() boundary.Observed {
	return boundary.Observed{Units: 200, Failures: 2, BaselineUnits: 200, BaselineFailures: 2}
}
