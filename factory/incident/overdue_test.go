package incident_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/service"
)

// decompositionActor is who these tests create a service or an item as, the
// way item/db_test.go's decompositionActor is.
var decompositionActor = record.Actor{Kind: record.KindComponent, Key: "decomposition", Basis: record.BasisClaimed}

// graph is the writers [OverdueItems] reads behind: the incident it raises
// against, the service the bound is authored on, and the item the intent it
// names is decomposed into. It is its own schema-per-test setup rather than
// [newTable]'s, because that fixture hands out one writer and this one needs
// four. environmentID is the production environment every raising in these
// tests names, [Writer.Raise] refusing one that is not.
type graph struct {
	pool          *pgxpool.Pool
	incident      *incident.Writer
	services      *service.Writer
	items         *item.Decomposition
	dispatch      *item.Dispatch
	environmentID string
}

func newGraph(t *testing.T) (context.Context, graph) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m3_inc_overdue_" + hex.EncodeToString(suffix[:])

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
	return ctx, graph{
		pool:          pool,
		incident:      incident.NewWriter(pool, token),
		services:      service.NewWriter(pool, token),
		items:         item.NewDecomposition(pool, token, item.NoHolds{}),
		dispatch:      item.NewDispatch(pool, token),
		environmentID: environmentOfKind(t, ctx, pool, token, environment.KindProduction),
	}
}

// aService creates a service, authoring an incident-item bound where seconds
// is above nothing and leaving the field unauthored where it is not, so a
// test can read [incident.OverdueItems] against either the authored bound or
// the shipped default.
func (g graph) aService(ctx context.Context, t *testing.T, name string, seconds float64) service.Service {
	t.Helper()
	created, err := g.services.Create(ctx, decompositionActor, name, "/srv/repos/"+name, record.NewID("proj"))
	if err != nil {
		t.Fatalf("creating the service: %v", err)
	}
	if seconds <= 0 {
		return created
	}
	tx, err := g.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning the authoring of the bound: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := service.SetIncidentItemBound(ctx, tx, created.ID, seconds); err != nil {
		t.Fatalf("SetIncidentItemBound: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("committing the bound: %v", err)
	}
	read, err := service.Get(ctx, g.pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return read
}

// anItem decomposes one item under intentID and serviceID, at spec, which is
// where every item starts and so where one is still being worked.
func (g graph) anItem(ctx context.Context, t *testing.T, intentID, serviceID string) item.Item {
	t.Helper()
	it, err := g.items.Create(ctx, decompositionActor, item.New{
		IntentID: intentID, ServiceID: serviceID, Branch: "item/" + intentID,
		RequirementsAnswered: []string{"rq_test"},
	}, "", "")
	if err != nil {
		t.Fatalf("decomposing the item: %v", err)
	}
	return it
}

func TestOverdueItemsFindsAnItemStillBeingWorkedPastTheBound(t *testing.T) {
	ctx, g := newGraph(t)

	svc := g.aService(ctx, t, "checkout", 60)
	intentID := record.NewID("int")
	r := raising(g.environmentID)
	r.ServiceID, r.IntentID = svc.ID, intentID
	raised, err := g.incident.Raise(ctx, healthMonitor, r)
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	at, err := record.ParseTime(raised.At)
	if err != nil {
		t.Fatalf("parsing the raised time: %v", err)
	}
	it := g.anItem(ctx, t, intentID, svc.ID)

	before, err := incident.OverdueItems(ctx, g.pool, at.Add(30*time.Second))
	if err != nil {
		t.Fatalf("OverdueItems before the bound: %v", err)
	}
	if len(before) != 0 {
		t.Errorf("OverdueItems before the bound = %+v, want none", before)
	}

	after, err := incident.OverdueItems(ctx, g.pool, at.Add(61*time.Second))
	if err != nil {
		t.Fatalf("OverdueItems after the bound: %v", err)
	}
	if len(after) != 1 || after[0].IncidentID != raised.ID || after[0].ItemID != it.ID || after[0].ServiceID != svc.ID {
		t.Errorf("OverdueItems after the bound = %+v, want one naming incident %s, item %s, service %s",
			after, raised.ID, it.ID, svc.ID)
	}
}

func TestOverdueItemsExcludesAnItemNoLongerBeingWorked(t *testing.T) {
	ctx, g := newGraph(t)

	svc := g.aService(ctx, t, "checkout", 60)
	intentID := record.NewID("int")
	r := raising(g.environmentID)
	r.ServiceID, r.IntentID = svc.ID, intentID
	raised, err := g.incident.Raise(ctx, healthMonitor, r)
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	at, err := record.ParseTime(raised.At)
	if err != nil {
		t.Fatalf("parsing the raised time: %v", err)
	}
	it := g.anItem(ctx, t, intentID, svc.ID)
	if _, err := g.dispatch.Drop(ctx, decompositionActor, it.ID); err != nil {
		t.Fatalf("dropping the item: %v", err)
	}

	after, err := incident.OverdueItems(ctx, g.pool, at.Add(61*time.Second))
	if err != nil {
		t.Fatalf("OverdueItems: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("OverdueItems over a dropped item = %+v, want none", after)
	}
}

func TestOverdueItemsSkipsAnIncidentThatRaisedNoItem(t *testing.T) {
	ctx, g := newGraph(t)

	svc := g.aService(ctx, t, "checkout", 60)
	r := raising(g.environmentID)
	r.ServiceID = svc.ID
	raised, err := g.incident.Raise(ctx, healthMonitor, r)
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	at, err := record.ParseTime(raised.At)
	if err != nil {
		t.Fatalf("parsing the raised time: %v", err)
	}

	after, err := incident.OverdueItems(ctx, g.pool, at.Add(time.Hour))
	if err != nil {
		t.Fatalf("OverdueItems: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("OverdueItems over an incident that raised no intent = %+v, want none", after)
	}
}

// TestOverdueItemsUsesTheShippedDefaultWhereNoBoundIsAuthored is
// [service.ShippedIncidentItemBoundSeconds], three days, read where an owner
// authored no bound of their own.
func TestOverdueItemsUsesTheShippedDefaultWhereNoBoundIsAuthored(t *testing.T) {
	ctx, g := newGraph(t)

	svc := g.aService(ctx, t, "checkout", 0)
	intentID := record.NewID("int")
	r := raising(g.environmentID)
	r.ServiceID, r.IntentID = svc.ID, intentID
	raised, err := g.incident.Raise(ctx, healthMonitor, r)
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	at, err := record.ParseTime(raised.At)
	if err != nil {
		t.Fatalf("parsing the raised time: %v", err)
	}
	it := g.anItem(ctx, t, intentID, svc.ID)
	shipped := time.Duration(service.ShippedIncidentItemBoundSeconds) * time.Second

	under, err := incident.OverdueItems(ctx, g.pool, at.Add(shipped-time.Minute))
	if err != nil {
		t.Fatalf("OverdueItems under the shipped default: %v", err)
	}
	if len(under) != 0 {
		t.Errorf("OverdueItems under the shipped default = %+v, want none", under)
	}

	over, err := incident.OverdueItems(ctx, g.pool, at.Add(shipped+time.Minute))
	if err != nil {
		t.Fatalf("OverdueItems over the shipped default: %v", err)
	}
	if len(over) != 1 || over[0].ItemID != it.ID {
		t.Errorf("OverdueItems over the shipped default = %+v, want one naming item %s", over, it.ID)
	}
}
