// The database tests of this package are in environment_test rather than in
// environment, because they open the pool through package postgres, which
// imports this one to apply its DDL. deps.txt records the edge as
// "test environment -> postgres".
//
// They are split by subject rather than kept in one file, which the 500-line
// bound requires: this file is the fixture and the persistent kinds an owner
// writes, target_test.go is a persistent environment's targets and its
// withdrawal, candidate_test.go is the candidate kind the deployer composes,
// cycle_test.go is the compose-and-reclaim cycle and what it costs, and
// threshold_test.go is the gate threshold an owner authors.
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package environment_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
)

var owner = record.Actor{Kind: record.KindHuman, Key: "owner", Basis: record.BasisClaimed}

var credential = secretref.MustNew("deploy.local")

// theProject is the project every environment in these tests belongs to.
// Production is one record per project, so the project is what makes two
// production records two and not a collision.
const theProject = "prj_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// composingPlatform is a platform that can compose an environment on demand,
// which is what a production environment's must be able to do.
var composingPlatform = environment.Platform{
	Name:               "local",
	Credential:         secretref.MustNew("platform.local"),
	CanComposeOnDemand: true,
}

// oneTarget is the target list a test that says nothing about targets uses.
func oneTarget(address string) []environment.Target {
	return []environment.Target{{Address: address}}
}

// productionSpec is production's record as an owner declares it.
func productionSpec(targets ...environment.Target) environment.Spec {
	if len(targets) == 0 {
		targets = oneTarget("/srv/targets/one")
	}
	return environment.Spec{
		Kind:       environment.KindProduction,
		ProjectID:  theProject,
		Name:       environment.ProductionName,
		Targets:    targets,
		Credential: credential,
		Platform:   composingPlatform,
	}
}

func newTable(t *testing.T) (context.Context, *pgxpool.Pool, *environment.Writer, lease.Token) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m2_env_" + hex.EncodeToString(suffix[:])

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
	return ctx, pool, environment.NewWriter(pool, token), token
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

// TestProductionIsCreatedWithItsTargetsItsCredentialAndItsPlatform: an
// environment is a record and not a name in code, and what it names is where a
// deploy into it is performed, what it is performed with, and what it is composed
// on.
func TestProductionIsCreatedWithItsTargetsItsCredentialAndItsPlatform(t *testing.T) {
	ctx, pool, w, _ := newTable(t)

	targets := []environment.Target{
		{Address: "/srv/targets/one", ServesAShare: true},
		{Address: "/srv/targets/two"},
	}
	created, err := w.Create(ctx, owner, productionSpec(targets...))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Kind != environment.KindProduction {
		t.Errorf("the record's kind is %q, want production", created.Kind)
	}

	read, err := environment.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !slices.Equal(read.Targets, targets) {
		t.Errorf("the targets read back as %v, want %v", read.Targets, targets)
	}
	if !slices.Equal(read.Addresses(), []string{"/srv/targets/one", "/srv/targets/two"}) {
		t.Errorf("the addresses read back as %v, and the order is the one a rollout reaches them in", read.Addresses())
	}
	if read.Credential != credential {
		t.Errorf("the credential reads back as %v, want the reference %v", read.Credential, credential)
	}
	if read.Platform != composingPlatform {
		t.Errorf("the platform reads back as %+v, want %+v", read.Platform, composingPlatform)
	}
	// The score picks the row with a control only where every target of the set
	// serves a share, there being no control where no share can be served.
	if read.EveryTargetServesAShare() {
		t.Error("an environment with a target that serves no share reads as serving a share throughout")
	}

	production, found, err := environment.Production(ctx, pool, theProject)
	if err != nil || !found || production.ID != created.ID {
		t.Fatalf("Production = %+v, %v, %v", production, found, err)
	}
	if _, found, err := environment.Production(ctx, pool, "prj_other"); err != nil || found {
		t.Errorf("Production of a project with none = found %v, %v", found, err)
	}
}

// TestProductionIsOneRecordPerProject: production exists everywhere and every
// project has one, so the name is unique within the project and a second
// production record in one project is refused by the store.
func TestProductionIsOneRecordPerProject(t *testing.T) {
	ctx, pool, w, _ := newTable(t)

	if _, err := w.Create(ctx, owner, productionSpec()); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := w.Create(ctx, owner, productionSpec()); err == nil {
		t.Error("a second production record in one project was accepted")
	}

	second := productionSpec()
	second.ProjectID = "prj_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := w.Create(ctx, owner, second); err != nil {
		t.Fatalf("production in a second project: %v", err)
	}

	// A customer's environment in the same project is another record and not a
	// collision, the name being what tells the two apart.
	customer := productionSpec()
	customer.Kind = environment.KindCustomer
	customer.Name = "staging"
	customer.Platform.CanComposeOnDemand = false
	if _, err := w.Create(ctx, owner, customer); err != nil {
		t.Fatalf("a customer's environment: %v", err)
	}
	byName, found, err := environment.ByName(ctx, pool, theProject, "staging")
	if err != nil || !found || byName.Kind != environment.KindCustomer {
		t.Fatalf("ByName = %+v, %v, %v", byName, found, err)
	}

	// The store refuses a second production record written around the writer.
	if _, err := pool.Exec(ctx, `insert into `+environment.Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, kind, project_id, name,
		 targets, credential, platform_name, platform_credential, can_compose_on_demand,
		 max_concurrent_candidate_environments, item_id, composed_from, seed_version, value_set_version,
		 torn_down_at, torn_down_reason, withdrawn_at)
		values ('env_p2', $1, 'human', 'owner', 'claimed', $2, 'production', $3, 'production-again',
		 'noshare /srv', 'deploy.local', 'local', 'platform.local', true, 0, '', '', '', '', '', '', '')`,
		environment.FormatVersion, record.Now(), theProject); err == nil {
		t.Error("the store accepted a second production record in one project")
	}
}

// TestTheKindIsTheSeamAndThereAreThree: the kind is fixed at creation and two
// writers never write a record of the other's kind. An owner writes the two
// persistent kinds, the deployer writes a candidate's, each refuses the other's,
// and a kind that is not one of the three is refused by the writer and by the
// store.
func TestTheKindIsTheSeamAndThereAreThree(t *testing.T) {
	ctx, pool, w, _ := newTable(t)

	candidate := productionSpec()
	candidate.Kind = environment.KindCandidate
	candidate.Name = "cand"
	if _, err := w.Create(ctx, owner, candidate); !errors.Is(err, environment.ErrNotAnOwnersKind) {
		t.Errorf("an owner creating a candidate's environment = %v, want ErrNotAnOwnersKind", err)
	}
	unknown := productionSpec()
	unknown.Kind = environment.Kind("preview")
	if _, err := w.Create(ctx, owner, unknown); !errors.Is(err, environment.ErrKindUnknown) {
		t.Errorf("Create of a kind that is not one of the three = %v, want ErrKindUnknown", err)
	}
	noTarget := productionSpec()
	noTarget.Targets = nil
	if _, err := w.Create(ctx, owner, noTarget); !errors.Is(err, environment.ErrTargetsEmpty) {
		t.Errorf("Create with no target = %v, want ErrTargetsEmpty", err)
	}
	noProject := productionSpec()
	noProject.ProjectID = ""
	if _, err := w.Create(ctx, owner, noProject); !errors.Is(err, environment.ErrProjectIDEmpty) {
		t.Errorf("Create naming no project = %v, want ErrProjectIDEmpty", err)
	}
	if _, err := w.Create(ctx, record.Actor{}, productionSpec()); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("Create with no actor = %v, want ErrKindUnknown", err)
	}

	if _, err := pool.Exec(ctx, `insert into `+environment.Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, kind, project_id, name,
		 targets, credential, platform_name, platform_credential, can_compose_on_demand,
		 max_concurrent_candidate_environments, item_id, composed_from, seed_version, value_set_version,
		 torn_down_at, torn_down_reason, withdrawn_at)
		values ('env_x', $1, 'human', 'owner', 'claimed', $2, 'preview', $3, 'pre',
		 'noshare /srv', 'deploy.local', 'local', 'platform.local', true, 0, '', '', '', '', '', '', '')`,
		environment.FormatVersion, record.Now(), theProject); err == nil {
		t.Error("the store accepted a kind written around the writer")
	}
	// A candidate's record names its item and a persistent one names none, which
	// the store enforces in both directions.
	if _, err := pool.Exec(ctx, `insert into `+environment.Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, kind, project_id, name,
		 targets, credential, platform_name, platform_credential, can_compose_on_demand,
		 max_concurrent_candidate_environments, item_id, composed_from, seed_version, value_set_version,
		 torn_down_at, torn_down_reason, withdrawn_at)
		values ('env_y', $1, 'component', 'deployer', '', $2, 'candidate', $3, 'candidate/none',
		 'noshare /srv', 'deploy.local', '', '', false, 0, '', '', '', '', '', '', '')`,
		environment.FormatVersion, record.Now(), theProject); err == nil {
		t.Error("the store accepted a candidate's environment naming no item")
	}
	if _, err := pool.Exec(ctx, `insert into `+environment.Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, kind, project_id, name,
		 targets, credential, platform_name, platform_credential, can_compose_on_demand,
		 max_concurrent_candidate_environments, item_id, composed_from, seed_version, value_set_version,
		 torn_down_at, torn_down_reason, withdrawn_at)
		values ('env_z', $1, 'human', 'owner', 'claimed', $2, 'customer', $3, 'staging',
		 'noshare /srv', 'deploy.local', 'local', 'platform.local', false, 0, 'it_a', '', '', '', '', '', '')`,
		environment.FormatVersion, record.Now(), theProject); err == nil {
		t.Error("the store accepted a persistent environment naming an item")
	}
}

// TestDDLListsEveryKind keeps the CHECK constraint and [environment.Kinds] from
// disagreeing: the constraint is SQL text rather than built from the slice, so
// this is what says they still name the same kinds.
func TestDDLListsEveryKind(t *testing.T) {
	// The constraint is named in the search, because record's own actor_kind
	// constraint is a "kind in (" earlier in the same statement.
	const open = "constraint kind_known check (kind in ("
	statement := environment.DDL[0]
	i := strings.Index(statement, open)
	if i < 0 {
		t.Fatalf("the DDL has no %q list", open)
	}
	rest := statement[i+len(open):]
	listed := strings.Split(rest[:strings.Index(rest, ")")], ",")
	if len(listed) != len(environment.Kinds) {
		t.Fatalf("the constraint lists %d kinds, Kinds has %d", len(listed), len(environment.Kinds))
	}
	for n, k := range environment.Kinds {
		if got, want := strings.TrimSpace(listed[n]), "'"+string(k)+"'"; got != want {
			t.Errorf("the constraint lists %s where Kinds has %s", got, want)
		}
	}
}

// TestAProductionPlatformComposesAnEnvironmentOnDemand: an environment per
// candidate is the shape the design admits and nothing else, so a production
// environment declaring a platform that cannot compose one is refused where it is
// declared. A customer's environment is not, nothing being composed on demand
// there.
func TestAProductionPlatformComposesAnEnvironmentOnDemand(t *testing.T) {
	ctx, _, w, _ := newTable(t)

	cannot := productionSpec()
	cannot.Platform.CanComposeOnDemand = false
	if _, err := w.Create(ctx, owner, cannot); !errors.Is(err, environment.ErrPlatformCannotComposeOnDemand) {
		t.Errorf("production on a platform that cannot compose on demand = %v, want ErrPlatformCannotComposeOnDemand", err)
	}

	none := productionSpec()
	none.Platform = environment.Platform{}
	if _, err := w.Create(ctx, owner, none); !errors.Is(err, environment.ErrPlatformIncomplete) {
		t.Errorf("production declaring no platform = %v, want ErrPlatformIncomplete", err)
	}

	customer := productionSpec()
	customer.Kind = environment.KindCustomer
	customer.Name = "staging"
	customer.Platform.CanComposeOnDemand = false
	if _, err := w.Create(ctx, owner, customer); err != nil {
		t.Errorf("a customer's environment on a platform that composes nothing on demand = %v, want it accepted", err)
	}
}

// TestTheCeilingIsAuthoredOnProductionsRecord: a maximum concurrent candidate
// environments is a count authored outright on the production environment record
// beside the platform it declares, one per platform, nothing supplied.
func TestTheCeilingIsAuthoredOnProductionsRecord(t *testing.T) {
	ctx, pool, w, token := newTable(t)

	production, err := w.Create(ctx, owner, productionSpec())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if production.MaxConcurrentCandidateEnvironments != 0 {
		t.Errorf("an unauthored ceiling reads back as %d, want nothing authored", production.MaxConcurrentCandidateEnvironments)
	}

	customer := productionSpec()
	customer.Kind = environment.KindCustomer
	customer.Name = "staging"
	customer.Platform.CanComposeOnDemand = false
	other, err := w.Create(ctx, owner, customer)
	if err != nil {
		t.Fatalf("a customer's environment: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := environment.SetMaxConcurrentCandidateEnvironments(ctx, tx, token, owner, production.ID, 8); err != nil {
		t.Fatalf("SetMaxConcurrentCandidateEnvironments: %v", err)
	}
	err = environment.SetMaxConcurrentCandidateEnvironments(ctx, tx, token, owner, other.ID, 8)
	if !errors.Is(err, environment.ErrNotAProductionEnvironment) {
		t.Errorf("authoring the ceiling on a customer's environment = %v, want ErrNotAProductionEnvironment", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	read, err := environment.Get(ctx, pool, production.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.MaxConcurrentCandidateEnvironments != 8 {
		t.Errorf("the ceiling reads back as %d, want 8", read.MaxConcurrentCandidateEnvironments)
	}

	// The store refuses a ceiling on a kind that is not production's.
	if _, err := pool.Exec(ctx, `update `+environment.Table+`
		set max_concurrent_candidate_environments = 4 where id = $1`, other.ID); err == nil {
		t.Error("the store accepted a ceiling on a customer's environment")
	}
}

// TestTheCredentialsAreReferencesAndNoValues: nothing that renders this record
// renders a secret, which is the seam the store carries from the first record.
// The platform's credential is the second one on the record and reads the same
// way.
func TestTheCredentialsAreReferencesAndNoValues(t *testing.T) {
	ctx, pool, w, _ := newTable(t)

	created, err := w.Create(ctx, owner, productionSpec())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	var stored string
	if err := pool.QueryRow(ctx, `select `+environment.Table+`::text from `+environment.Table+` where id = $1`, created.ID).Scan(&stored); err != nil {
		t.Fatalf("reading the row as text: %v", err)
	}
	if !strings.Contains(stored, credential.Name()) {
		t.Errorf("the row does not name the credential reference: %s", stored)
	}
	if !strings.Contains(stored, composingPlatform.Credential.Name()) {
		t.Errorf("the row does not name the platform credential reference: %s", stored)
	}
	if strings.Contains(stored, "sk-") {
		t.Errorf("the row holds something that reads like a secret value: %s", stored)
	}
}
