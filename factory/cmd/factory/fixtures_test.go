// The run harness every other test file shares: the schema-per-test setup,
// the paced deps, the analysis window fixtures, the payload decoders, and
// the intent-building helpers.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/targetseam"
)

const theModel = "fake-model-1"

// theService is the service every single-service test in this file runs on, and
// theSecondService is the one the contract tests add beside it: an interface has
// consumers, and the consumers are other services in the same factory.
const (
	theService       = "demo"
	theSecondService = "reader"
)

// theArea is the area the run names. Without one the score can read neither
// context factor and a human decides every gate, so the milestone's own
// demonstration needs one.
const theArea = "payments"

// theCeiling is how many candidate environments the tests give the substrate room
// for. It is high enough that no test meets it by accident, and the one test about
// the ceiling lowers it to one.
const theCeiling = 8

// attemptLimit is the limit in force in these tests: nothing here authors one,
// so it is what the score supplies. The tests that spend it read it from there
// rather than holding a number of their own, so authoring a different supplied
// value moves the tests with it.
var attemptLimit = func() int {
	supplied, ok := score.Starting(gatepolicy.AttemptLimit)
	if !ok {
		panic("the score supplies no attempt limit")
	}
	return int(supplied.Value)
}()

// newPath gives a test a schema of its own with the whole schema applied,
// temp directories for the repository and production's target, a secrets file
// holding the deploy credential, a target per environment whose started processes
// are stopped in cleanup — through the seam — and the deps the path runs over.
// input is what the scripted human types.
func newPath(t *testing.T, input string) (context.Context, deps, *bytes.Buffer) {
	t.Helper()
	return newPathOn(t, input, theService)
}

// newPathOn is [newPath] over more than one service, which is what a contract
// needs: an interface has consumers, and the consumers are other services in the
// same factory. Each gets a temporary repository of its own, because a service is
// one repository and no repository holds two.
func newPathOn(t *testing.T, input string, services ...string) (context.Context, deps, *bytes.Buffer) {
	t.Helper()
	known := make([]serviceRepo, 0, len(services))
	for _, name := range services {
		known = append(known, serviceRepo{name: name, repo: filepath.Join(t.TempDir(), name)})
	}
	return newPathIn(t, input, known)
}

// newPathIn is [newPathOn] with the repositories chosen by the caller. It exists
// because the repository a run works in is the service record's own field, written
// when decomposition creates the record — so a caller that wants a particular directory has
// to say so before the record is written and cannot rewrite the deps afterwards.
func newPathIn(t *testing.T, input string, known []serviceRepo) (context.Context, deps, *bytes.Buffer) {
	t.Helper()
	ctx := t.Context()
	services := make([]string, 0, len(known))
	for _, one := range known {
		services = append(services, one.name)
	}

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "factory_" + hex.EncodeToString(suffix[:])

	pool, err := postgres.Open(ctx, inSchema(t, postgres.URL(), schema))
	if err != nil {
		t.Fatalf("the database at %s is not reachable, and these tests do not skip: %v", postgres.URL(), err)
	}
	t.Cleanup(func() {
		// t.Context is already cancelled by the time cleanup runs.
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

	secrets := filepath.Join(t.TempDir(), "secrets")
	if err := os.WriteFile(secrets, []byte("deploy.local=unused\n"), 0o600); err != nil {
		t.Fatalf("writing the secrets file: %v", err)
	}
	resolver, err := secretref.Load(secrets)
	if err != nil {
		t.Fatalf("loading the secrets file: %v", err)
	}
	credential := secretref.MustNew("deploy.local")
	if _, err := resolver.Resolve(credential); err != nil {
		t.Fatalf("resolving the deploy credential: %v", err)
	}

	// One target per environment, the way the run composes them: production's is
	// the directory named here and each candidate environment's is one of its own.
	// Every target the run made is stopped in cleanup, a candidate environment torn
	// down mid-run having stopped its own already.
	targets := newTargetSet(func(dir string) targetseam.Target { return localtarget.New(dir) })
	t.Cleanup(func() {
		for dir, target := range targets.made {
			for _, name := range services {
				if err := target.Stop(context.Background(), name, credential); err != nil {
					t.Errorf("stopping the %s service on %s: %v", name, dir, err)
				}
			}
		}
	})

	out := &bytes.Buffer{}
	d := deps{
		pool:             pool,
		model:            &fakeModel{},
		modelName:        theModel,
		targets:          targets,
		dir:              t.TempDir(),
		credential:       credential,
		in:               strings.NewReader(input),
		out:              out,
		human:            "owner",
		services:         known,
		area:             theArea,
		candidateCeiling: theCeiling,
		watchFor:         theWatchFor,
		watchEvery:       theWatchEvery,
		// No draw selects: the sample is one firing in ten and a test that ran on
		// the runtime's own generator would pass or fail by chance, an item held out
		// being an item with no human at the row a test asserted one at. The test
		// that drives the sample composes a draw that always selects.
		draw: score.NeverDraw{},
	}
	installWindow(t, ctx, d, 1)
	return ctx, d, out
}

// The analysis window as these tests author it, and how long a run watches for.
//
// The supplied values are deliberately unreachable here and that is the design
// working rather than the tests fighting it: a size of two in a hundred needs traffic
// no test generates, and a cap of a day is exactly how long the second release of a
// service would wait behind a first that can never be passed. So the tests author a
// coarse size and a short cap, which is what an owner running a quiet service would
// do, and the run watches for longer than the cap so every window it opens closes
// before it returns.
const (
	theWindowSize       = 0.1
	theWindowConfidence = 0.95
	theWindowCap        = 1.0
	theWatchFor         = 4 * time.Second
	theWatchEvery       = 50 * time.Millisecond
)

// installWindow creates every service record the install names and authors the
// analysis window's four on each, before any run has opened a window.
//
// The service has to exist first, because those four are fields of its record — and
// it has to be authored before the first window opens, because a window copies the
// size, the confidence, and the cap onto itself at the open and an owner authoring
// afterwards does not move a window already open. Creating the service here is what
// [TestDecompositionReachesAnExistingService] proves decomposition is happy with: a service the
// work changes may exist already, and decomposition writes a service's identity once.
func installWindow(t *testing.T, ctx context.Context, d deps, limit float64) {
	t.Helper()
	owner := record.Actor{Kind: record.KindHuman, Name: d.human}
	if _, err := policy.NewFactory(d.pool).Install(ctx, owner, []string{d.dir}, d.credential); err != nil {
		t.Fatalf("installing the factory: %v", err)
	}
	factory := policy.NewFactory(d.pool)
	for _, named := range d.services {
		svc, found, err := service.ByName(ctx, d.pool, named.name)
		if err != nil {
			t.Fatalf("reading the service: %v", err)
		}
		if !found {
			svc, err = service.NewWriter(d.pool).Create(ctx, decompositionActor, named.name, named.repo)
			if err != nil {
				t.Fatalf("writing the service: %v", err)
			}
		}
		for _, authoring := range []struct {
			what  string
			write func() (policy.Version, error)
		}{
			{"the size", func() (policy.Version, error) {
				return factory.AuthorWindowSize(ctx, owner, svc.ID, theWindowSize)
			}},
			{"the confidence", func() (policy.Version, error) {
				return factory.AuthorWindowConfidence(ctx, owner, svc.ID, theWindowConfidence)
			}},
			{"the cap", func() (policy.Version, error) {
				return factory.AuthorWindowCap(ctx, owner, svc.ID, theWindowCap)
			}},
			{"window limit", func() (policy.Version, error) { return factory.AuthorWindowLimit(ctx, owner, svc.ID, limit) }},
		} {
			if _, err := authoring.write(); err != nil {
				t.Fatalf("authoring %s of the analysis window on %s: %v", authoring.what, named.name, err)
			}
		}
	}
}

// inSchema points a connection URL at one schema and nothing else, so every
// unqualified name in the DDL and in the writers' statements resolves there.
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

// only is the one candidate of a single-intent run, which is what most of these
// tests assert over.
func only(t *testing.T, s shipped) *candidate {
	t.Helper()
	if len(s.candidates) != 1 {
		t.Fatalf("the run has %d candidates, want one", len(s.candidates))
	}
	return s.candidates[0]
}

// approvals is a scripted human approving every row that puts one there. A row
// that auto-passes consumes nothing, so a script with more approvals than rows is
// harmless and a script with fewer is what fails.
const approvals = "approve\napprove\napprove\n"

// openingPayload and closingPayload unmarshal what a decision row says, which
// every assertion over a firing reads through.
func openingPayload(t *testing.T, row decisionlog.Row) gate.OpeningPayload {
	t.Helper()
	var payload gate.OpeningPayload
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		t.Fatalf("reading the opening payload of %s: %v", row.ID, err)
	}
	return payload
}

func closingPayload(t *testing.T, row decisionlog.Row) gate.ClosingPayload {
	t.Helper()
	var payload gate.ClosingPayload
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		t.Fatalf("reading the closing payload of %s: %v", row.ID, err)
	}
	return payload
}

// of is the intents a test gives a run, one per statement, each decomposed on the one
// service the test's install names. It is a helper because most of these tests are
// single-service and a run's input is a statement plus the services its decomposition yields
// items on — writing the pair out at every call site would say the same thing forty
// times.
func of(statements ...string) []asked {
	asks := make([]asked, 0, len(statements))
	for _, statement := range statements {
		asks = append(asks, asked{statement: statement, services: []string{theService}})
	}
	return asks
}

// across is one intent whose decomposition yields an item per service named, in that order,
// each waiting on the one before it. It is what a contract migration is: one intent,
// several items, several services, and the intent is what joins them.
func across(statement string, services ...string) asked {
	return asked{statement: statement, services: services}
}

// theRepo is the repository of the first service the install names, which is the
// only one a single-service test has.
func theRepo(d deps) string { return d.services[0].repo }

// theServiceRecord is the record of the first service the install names, read
// after a run has decomposed on it. Every step that used to take the path's one service now
// takes a record, which is what two services cost.
func theServiceRecord(t *testing.T, ctx context.Context, p *path) service.Service {
	t.Helper()
	svc, found, err := service.ByName(ctx, p.d.pool, p.d.services[0].name)
	if err != nil {
		t.Fatalf("reading the service: %v", err)
	}
	if !found {
		t.Fatalf("no service is named %q", p.d.services[0].name)
	}
	return svc
}

// authorOne is one intent decomposed into one item on the install's one service, for a
// test that drives the steps rather than calling run. Decomposition yields one item, so no
// Decomposition row fires — the row fires where there is a set to ratify.
func authorOne(t *testing.T, ctx context.Context, p *path, statement string, out *bytes.Buffer) *candidate {
	t.Helper()
	set, candidates, err := p.authorIntent(ctx,
		asked{statement: statement, services: []string{theService}}, statement)
	if err != nil {
		t.Fatalf("authoring %q: %v\noutput so far:\n%s", statement, err, out)
	}
	if len(candidates) != 1 {
		t.Fatalf("authoring %q yielded %d candidates, want one", statement, len(candidates))
	}
	if set.decided {
		t.Fatalf("a decomposition of one item fired Decomposition, and that row fires where there is a set to ratify")
	}
	c := candidates[0]
	p.byItem[c.itemID] = c
	p.authored[c.itemID] = true
	return c
}
