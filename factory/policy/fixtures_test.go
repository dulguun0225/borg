// The database tests of this package are in policy_test rather than in policy,
// because they open the pool through package postgres, which imports this one to
// apply its DDL. deps.txt records the edge as "test policy -> postgres".
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package policy_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/project"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
)

var (
	owner              = record.Actor{Kind: record.KindHuman, Key: "person:owner", Basis: record.BasisClaimed}
	decompositionActor = record.Actor{Kind: record.KindComponent, Key: "decomposition"}
)

var credential = secretref.MustNew("deploy.local")

// installed is a factory an owner could author on: the two records Install
// creates, plus a service and an area for the parameters that are fields of
// those. The service is written by decomposition, which is its other writer, and the
// area by an owner.
type installed struct {
	pool     *pgxpool.Pool
	token    lease.Token
	factory  *policy.Factory
	reader   *policy.Reader
	settings factorysettings.Settings
	prod     environment.Environment
	service  service.Service
	area     area.Area
}

// subjects is what a gate firing on this service in this area reads against.
// Quantity names the error rate, the one quantity the health monitor reads at
// this milestone, so a test reading the window's size or power through
// [policy.Reader.All] finds what it authored against that quantity.
func (i installed) subjects(row string) policy.Subjects {
	return policy.Subjects{
		GateRow:       row,
		EnvironmentID: i.prod.ID,
		ServiceID:     i.service.ID,
		AreaID:        i.area.ID,
		Stage:         item.StageImplementation,
		Quantity:      string(gatepolicy.QuantityErrorRate),
	}
}

func newFactory(t *testing.T) (context.Context, installed) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m2_policy_" + hex.EncodeToString(suffix[:])

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

	factory := policy.NewFactory(pool, token)
	install, err := factory.Install(ctx, owner, "acme", []string{"/srv/targets"}, credential, 8)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	prj, err := project.NewWriter(pool, token).Create(ctx, owner, "storefront")
	if err != nil {
		t.Fatalf("creating the project: %v", err)
	}
	svc, err := service.NewWriter(pool, token).Create(ctx, decompositionActor, "checkout", "/repos/checkout", prj.ID)
	if err != nil {
		t.Fatalf("creating the service: %v", err)
	}
	ar, err := area.NewWriter(pool, token).Declare(ctx, owner, "payments", area.InsideProject(prj.ID), area.Hazard{})
	if err != nil {
		t.Fatalf("declaring the area: %v", err)
	}
	return ctx, installed{
		// The reader is composed with the version in force, which is what a run
		// does: the supplied half of every value is a field of that version.
		pool: pool, token: token, factory: factory, reader: policy.NewReader(pool, scoreVersion(t, ctx, pool, token)),
		settings: install.Settings, prod: install.Production, service: svc, area: ar,
	}
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

func effectiveOf(t *testing.T, all []policy.Effective, parameter gatepolicy.Parameter) policy.Effective {
	t.Helper()
	for _, e := range all {
		if e.Parameter == parameter {
			return e
		}
	}
	t.Fatalf("nothing resolved %s", parameter)
	return policy.Effective{}
}

// startingValue is what the score supplies for a parameter before any outcome has
// moved it, which is what a factory with no outcomes in it reads.
func startingValue(t *testing.T, parameter gatepolicy.Parameter) float64 {
	t.Helper()
	supplied, found := score.Starting(parameter)
	if !found {
		t.Fatalf("the score supplies no %s", parameter)
	}
	return supplied.Value
}

// scoreVersion is the version in force, appended if there is none. The reader is
// composed with it rather than reading the newest at each answer: a supplied value
// moves as outcomes arrive, and a reader that re-read it could give one gate
// firing a threshold from a version its own decision row does not name.
func scoreVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, token lease.Token) score.Version {
	t.Helper()
	version, err := score.NewWriter(pool, token).Ensure(ctx, record.Actor{Kind: record.KindComponent, Key: "score"})
	if err != nil {
		t.Fatalf("ensuring the score version: %v", err)
	}
	return version
}
