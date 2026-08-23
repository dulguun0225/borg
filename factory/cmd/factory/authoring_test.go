// The tests of the four authoring subcommands: what an owner types, parsed and
// dispatched the way the terminal dispatches it. Package policy's own tests are
// what demonstrate the writes; these are the wiring between an owner's words and
// those calls — which parameter reads which subject flag, a subject written as
// kind:name resolved to a record's id, and a direction that is never typed.
//
// Each test points DATABASE_URL at a schema of its own, because these
// subcommands open the pool through postgres.URL() rather than being handed one:
// they are what an owner runs, and an owner has a database and not a pool. None
// of them skips when the database is unreachable.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/safeguard"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
)

// newOwner gives a test a schema of its own with the whole schema applied,
// DATABASE_URL pointed at it for the length of the test, an installed factory,
// and a service decomposition wrote. What it returns is the pool, for reading back what
// a subcommand wrote.
func newOwner(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m2_owner_" + hex.EncodeToString(suffix[:])
	url := inSchema(t, postgres.URL(), schema)

	// The subcommands read the environment, so this is how a test hands them a
	// schema of their own. Every pool they open inside this test opens there.
	t.Setenv(postgres.URLEnv, url)

	pool, err := postgres.Open(ctx, url)
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
	return ctx, pool
}

// install is what the run's first take does, which everything an owner authors on
// depends on.
func install(t *testing.T, ctx context.Context, pool *pgxpool.Pool) environment.Environment {
	t.Helper()
	installed, err := policy.NewFactory(pool).Install(ctx,
		record.Actor{Kind: record.KindHuman, Name: "owner"},
		[]string{t.TempDir()}, secretref.MustNew("deploy.local"))
	if err != nil {
		t.Fatalf("installing: %v", err)
	}
	return installed.Production
}

func decomposeService(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) service.Service {
	t.Helper()
	svc, err := service.NewWriter(pool).Create(ctx,
		record.Actor{Kind: record.KindComponent, Name: "decomposition"}, name, "/repos/"+name)
	if err != nil {
		t.Fatalf("creating the service: %v", err)
	}
	return svc
}

// TestNothingToAuthorOnBeforeTheFactoryIsInstalled: the two records an owner
// authors on are created by the run's first take, and an error naming a missing
// version says that badly on its own — so the subcommand says what to do.
func TestNothingToAuthorOnBeforeTheFactoryIsInstalled(t *testing.T) {
	_, _ = newOwner(t)

	for _, c := range []struct {
		name string
		run  func() error
	}{
		{"policy", func() error { return policyCommand(nil) }},
		{"author", func() error {
			return authorCommand([]string{"-parameter", "attempt_limit", "-value", "5"})
		}},
	} {
		err := c.run()
		if err == nil {
			t.Errorf("%s on a factory nobody installed was accepted", c.name)
			continue
		}
		if !strings.Contains(err.Error(), "the factory is not installed") {
			t.Errorf("%s says %q, and what an owner needs to know is that nothing is installed", c.name, err)
		}
	}
}

// TestAnAreaIsDeclaredAndCanLieInsideAnother: an owner declares the groupings the
// rest of the factory is scoped against, and the inside is named rather than
// given as an id.
func TestAnAreaIsDeclaredAndCanLieInsideAnother(t *testing.T) {
	ctx, pool := newOwner(t)

	if err := areaCommand([]string{"payments"}); err != nil {
		t.Fatalf("area payments: %v", err)
	}
	if err := areaCommand([]string{"payments/refunds", "-inside", "payments"}); err != nil {
		t.Fatalf("area payments/refunds: %v", err)
	}

	inner, found, err := area.ByName(ctx, pool, "payments/refunds")
	if err != nil || !found {
		t.Fatalf("ByName = %+v, %v, %v", inner, found, err)
	}
	chain, err := area.Chain(ctx, pool, inner.ID)
	if err != nil {
		t.Fatalf("Chain: %v", err)
	}
	if len(chain) != 2 || chain[1].Name != "payments" {
		t.Errorf("the chain is %d areas ending at %q, want two ending at payments", len(chain), chain[len(chain)-1].Name)
	}
	if chain[0].Actor.Kind != record.KindHuman {
		t.Errorf("the area's actor is %+v, want the owner who declared it", chain[0].Actor)
	}

	if err := areaCommand([]string{"marketing", "-inside", "nothing"}); err == nil {
		t.Error("an area inside one nobody declared was accepted")
	}
	if err := areaCommand(nil); err == nil {
		t.Error("area with no name was accepted")
	}
}

// TestEachParameterReadsTheSubjectItsScopeNames: the record a parameter is a
// field of is a fact of the parameter and not a choice, so the subcommand reads
// the flag that parameter needs and refuses where the subject is missing.
func TestEachParameterReadsTheSubjectItsScopeNames(t *testing.T) {
	ctx, pool := newOwner(t)
	production := install(t, ctx, pool)
	svc := decomposeService(t, ctx, pool, "checkout")
	if err := areaCommand([]string{"payments"}); err != nil {
		t.Fatalf("area: %v", err)
	}
	ar, _, err := area.ByName(ctx, pool, "payments")
	if err != nil {
		t.Fatalf("ByName: %v", err)
	}

	for _, c := range []struct {
		args []string
		want float64
		read func() (float64, bool)
	}{
		{
			[]string{"-parameter", "risk_threshold", "-value", "0.2", "-gate", "merge_to_master"}, 0.2,
			func() (float64, bool) {
				authored, err := environment.GateThreshold(ctx, pool, production.ID, "merge_to_master")
				if err != nil {
					t.Fatalf("GateThreshold: %v", err)
				}
				return authored.Number, authored.Present
			},
		},
		{
			[]string{"-parameter", "attempt_limit", "-value", "5", "-stage", "implementation"}, 5,
			func() (float64, bool) {
				fp, err := factorysettings.Get(ctx, pool)
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				authored, err := factorysettings.AttemptLimit(ctx, pool, fp.ID, item.StageImplementation)
				if err != nil {
					t.Fatalf("AttemptLimit: %v", err)
				}
				return authored.Number, authored.Present
			},
		},
		{
			[]string{"-parameter", "item_size_target", "-value", "400", "-area", "payments"}, 400,
			func() (float64, bool) {
				read, err := area.Get(ctx, pool, ar.ID)
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				return read.ItemSizeTarget.Number, read.ItemSizeTarget.Present
			},
		},
		{
			[]string{"-parameter", "window_limit", "-value", "2", "-service", "checkout"}, 2,
			func() (float64, bool) {
				read, err := service.Get(ctx, pool, svc.ID)
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				return read.Parameters.WindowLimit.Number, read.Parameters.WindowLimit.Present
			},
		},
		{
			[]string{"-parameter", "window_confidence", "-value", "0.99", "-service", "checkout"}, 0.99,
			func() (float64, bool) {
				read, err := service.Get(ctx, pool, svc.ID)
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				return read.Parameters.WindowConfidence.Number, read.Parameters.WindowConfidence.Present
			},
		},
		{
			[]string{"-parameter", "risk_threshold", "-value", "0.15", "-gate", "role_prompt_or_skill"}, 0.15,
			func() (float64, bool) {
				fp, err := factorysettings.Get(ctx, pool)
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				return fp.RolePromptOrSkillThreshold.Number, fp.RolePromptOrSkillThreshold.Present
			},
		},
	} {
		if err := authorCommand(c.args); err != nil {
			t.Errorf("author %v: %v", c.args, err)
			continue
		}
		value, present := c.read()
		if !present {
			t.Errorf("author %v left nothing authored", c.args)
		}
		if value != c.want {
			t.Errorf("author %v stored %v, want %v", c.args, value, c.want)
		}
	}

	// The allowed predicate kinds are the one list, and they are authored as one.
	if err := authorCommand([]string{"-parameter", "allowed_predicate_kinds", "-value", "status,schema"}); err != nil {
		t.Fatalf("author the allowed: %v", err)
	}
	fp, err := factorysettings.Get(ctx, pool)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(fp.AllowedPredicateKinds) != 2 {
		t.Errorf("the allowed reads %v, want the two authored", fp.AllowedPredicateKinds)
	}

	// Every authoring write appended a policy version, so the sequence is as long
	// as the writes plus the two the install made.
	versions, err := policy.All(ctx, pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(versions) != 9 {
		t.Errorf("%d policy versions exist, want two creations and seven authorings", len(versions))
	}
}

// TestAuthoringRefusesWhatItCannotResolve: a parameter that is not one of the
// eight, a value of the wrong shape, and a subject the parameter needs and the
// owner did not give.
func TestAuthoringRefusesWhatItCannotResolve(t *testing.T) {
	ctx, pool := newOwner(t)
	install(t, ctx, pool)

	for _, c := range []struct {
		name string
		args []string
	}{
		{"no parameter", []string{"-value", "2"}},
		{"no value", []string{"-parameter", "k"}},
		{"a parameter that does not exist", []string{"-parameter", "gut_feel", "-value", "2"}},
		{"a word where a number belongs", []string{"-parameter", "window_limit", "-value", "two", "-service", "checkout"}},
		{"no service for a service-scoped parameter", []string{"-parameter", "window_limit", "-value", "2"}},
		{"no area for an area-scoped parameter", []string{"-parameter", "item_size_target", "-value", "400"}},
		{"an area nobody declared", []string{"-parameter", "item_size_target", "-value", "400", "-area", "nothing"}},
		{"a service nobody decomposed", []string{"-parameter", "window_limit", "-value", "2", "-service", "nothing"}},
	} {
		if err := authorCommand(c.args); err == nil {
			t.Errorf("author with %s was accepted", c.name)
		}
	}

	// Nothing was authored, so nothing moved the policy version past the
	// install's two creations.
	versions, err := policy.All(ctx, pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(versions) != 2 {
		t.Errorf("%d policy versions exist after refused writes, want the install's two", len(versions))
	}
}

// TestASafeguardIsPlacedOnASubjectByNameAndWithdrawnById: the direction is never
// typed, the subject is written kind:name, and withdrawing is what stops a
// mechanism reading it.
func TestASafeguardIsPlacedOnASubjectByNameAndWithdrawnById(t *testing.T) {
	ctx, pool := newOwner(t)
	install(t, ctx, pool)
	decomposeService(t, ctx, pool, "checkout")
	if err := areaCommand([]string{"payments"}); err != nil {
		t.Fatalf("area: %v", err)
	}

	for _, c := range []struct {
		args      []string
		parameter gatepolicy.Parameter
		direction gatepolicy.Direction
	}{
		{[]string{"-parameter", "risk_threshold", "-subject", "gate_row:deploy_to_production"},
			gatepolicy.RiskThreshold, gatepolicy.DirectionAddsAHuman},
		{[]string{"-parameter", "window_limit", "-subject", "service:checkout", "-bound", "2"},
			gatepolicy.WindowLimit, gatepolicy.DirectionCeiling},
		{[]string{"-parameter", "item_size_target", "-subject", "area:payments", "-bound", "300"},
			gatepolicy.ItemSizeTarget, gatepolicy.DirectionCeiling},
		{[]string{"-parameter", "allowed_predicate_kinds", "-subject", "factory_settings:", "-bound", "status,schema"},
			gatepolicy.AllowedPredicateKinds, gatepolicy.DirectionFloor},
	} {
		if err := safeguardCommand(c.args); err != nil {
			t.Fatalf("safeguard %v: %v", c.args, err)
		}
	}

	safeguards, err := safeguard.All(ctx, pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(safeguards) != 4 {
		t.Fatalf("%d safeguards are placed, want four", len(safeguards))
	}
	for _, p := range safeguards {
		if p.Withdrawn {
			t.Errorf("safeguard %s is withdrawn the moment it was placed", p.ID)
		}
		if p.Subject.Kind == safeguard.SubjectService && !strings.HasPrefix(p.Subject.ID, "svc_") {
			t.Errorf("the safeguard on a service names %q, want the record's id", p.Subject.ID)
		}
		if p.Subject.Kind == safeguard.SubjectArea && !strings.HasPrefix(p.Subject.ID, "ar_") {
			t.Errorf("the safeguard on an area names %q, want the record's id", p.Subject.ID)
		}
	}

	// A safeguard on the factory-wide settings record names the record's id, because
	// that is what the mechanism reading safeguards on it reads them by — a safeguard
	// naming the word would apply to nothing.
	fp, err := factorysettings.Get(ctx, pool)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	onTheRecord := 0
	for _, p := range safeguards {
		if p.Subject.Kind == safeguard.SubjectFactorySettings {
			onTheRecord++
			if p.Subject.ID != fp.ID {
				t.Errorf("the safeguard on the factory-wide settings record names %q, want %s", p.Subject.ID, fp.ID)
			}
		}
	}
	if onTheRecord != 1 {
		t.Errorf("%d safeguards name the factory-wide settings record, want the one", onTheRecord)
	}

	// The safeguard on the allowed predicate kinds reaches the parameter it was
	// drawn on: what an owner reads afterwards is the union, which is the whole of
	// what a safeguard on a list does.
	allowed, err := policy.NewReader(pool, score.Version{}).All(ctx, policy.Subjects{
		GateRow: "merge_to_master", Stage: item.StageImplementation,
	})
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	for _, e := range allowed {
		if e.Parameter != gatepolicy.AllowedPredicateKinds {
			continue
		}
		// The factory's own kinds are the floor an owner extends, so the safeguard's
		// two names are added to them rather than replacing them.
		want := len(gatepolicy.PredicateKinds) + 2
		if len(e.List) != want || !e.Clamped {
			t.Errorf("the allowed reads %v clamped %v, want the factory's own %d plus the two the safeguard added",
				e.List, e.Clamped, len(gatepolicy.PredicateKinds))
		}
	}

	if err := safeguardCommand([]string{"-withdraw", safeguards[0].ID}); err != nil {
		t.Fatalf("safeguard -withdraw: %v", err)
	}
	safeguards, err = safeguard.All(ctx, pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	withdrawn := 0
	for _, p := range safeguards {
		if p.Withdrawn {
			withdrawn++
		}
	}
	if withdrawn != 1 {
		t.Errorf("%d safeguards are withdrawn, want the one", withdrawn)
	}
}

// TestASafeguardRefusesWhatItCannotBind: a subject kind this milestone has no
// record for, a subject that is not written kind:name, a bound of the wrong
// shape, and a gate row that is not one of the rows built.
func TestASafeguardRefusesWhatItCannotBind(t *testing.T) {
	ctx, pool := newOwner(t)
	install(t, ctx, pool)

	for _, c := range []struct {
		name string
		args []string
	}{
		{"nothing at all", nil},
		{"a project", []string{"-parameter", "window_limit", "-subject", "project:payments", "-bound", "2"}},
		{"a subject with no kind", []string{"-parameter", "window_limit", "-subject", "checkout", "-bound", "2"}},
		{"a gate row nobody built", []string{"-parameter", "risk_threshold", "-subject", "gate_row:deploy_to_staging"}},
		{"a word where a bound belongs", []string{"-parameter", "window_limit", "-subject", "factory_settings:", "-bound", "two"}},
		{"a parameter that does not exist", []string{"-parameter", "gut_feel", "-subject", "factory_settings:", "-bound", "2"}},
		{"a safeguard withdrawn that does not exist", []string{"-withdraw", "sfg_nothing"}},
	} {
		if err := safeguardCommand(c.args); err == nil {
			t.Errorf("safeguard with %s was accepted", c.name)
		}
	}

	placed, err := safeguard.All(ctx, pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(placed) != 0 {
		t.Errorf("%d safeguards were placed by refused calls", len(placed))
	}
}

// TestPolicyReadsWhatIsInForce is the print an owner reads before authoring
// anything: it resolves against the records it is given and does not fail on the
// ones it is not.
func TestPolicyReadsWhatIsInForce(t *testing.T) {
	ctx, pool := newOwner(t)
	install(t, ctx, pool)
	decomposeService(t, ctx, pool, "checkout")
	if err := areaCommand([]string{"payments"}); err != nil {
		t.Fatalf("area: %v", err)
	}

	if err := policyCommand(nil); err != nil {
		t.Errorf("policy with no subjects: %v", err)
	}
	if err := policyCommand([]string{"-service", "checkout", "-area", "payments",
		"-gate", "deploy_to_production", "-stage", "spec"}); err != nil {
		t.Errorf("policy over every subject: %v", err)
	}
	if err := safeguardCommand([]string{"-parameter", "window_limit", "-subject", "service:checkout", "-bound", "2"}); err != nil {
		t.Fatalf("safeguard: %v", err)
	}
	if err := policyCommand([]string{"-service", "checkout"}); err != nil {
		t.Errorf("policy with a safeguard placed: %v", err)
	}
	if err := policyCommand([]string{"-service", "nothing"}); err == nil {
		t.Error("policy over a service nobody decomposed was accepted")
	}

	// What the print reads is what the reader reads, so the assertion over its
	// content is on the reader: every parameter resolves, and the two with a
	// mechanism at this milestone say so.
	effectives, err := policy.NewReader(pool, score.Version{}).All(ctx, policy.Subjects{
		GateRow: "merge_to_master", Stage: item.StageImplementation,
	})
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(effectives) != len(gatepolicy.Definitions) {
		t.Errorf("%d parameters resolved, want %d", len(effectives), len(gatepolicy.Definitions))
	}
}

// TestASubcommandOutsideTheSetIsRefused: dispatch names what there is, so a typo
// is answered with the list rather than with nothing happening.
func TestASubcommandOutsideTheSetIsRefused(t *testing.T) {
	if err := dispatch(nil); err == nil || !strings.Contains(err.Error(), subcommands) {
		t.Errorf("dispatch with no subcommand = %v, want the list of them", err)
	}
	err := dispatch([]string{"authour"})
	if err == nil || !strings.Contains(err.Error(), subcommands) {
		t.Errorf("dispatch of a misspelt subcommand = %v, want the list of them", err)
	}
	if err := walkCommand(nil); err == nil {
		t.Error("walk with no deploy id was accepted")
	}
	if err := runCommand(nil); err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("run with no flags = %v, want a required flag named", err)
	}
}

// TestThePrioritySubcommandReordersAQueue is duty 9's other write on an item: an
// owner reorders a queue, through dispatch rather than beside it. It is the fifth
// subcommand and it has no screen until Work arrives.
func TestThePrioritySubcommandReordersAQueue(t *testing.T) {
	ctx, pool := newOwner(t)
	install(t, ctx, pool)
	svc := decomposeService(t, ctx, pool, "checkout")

	it, err := item.NewDecomposition(pool).Create(ctx, record.Actor{Kind: record.KindComponent, Name: "decomposition"},
		item.New{IntentID: "in_a", ServiceID: svc.ID, Branch: "item/a"})
	if err != nil {
		t.Fatalf("decomposing the item: %v", err)
	}

	if err := dispatch([]string{"priority", it.ID, "-priority", "7"}); err != nil {
		t.Fatalf("priority: %v", err)
	}
	read, err := item.Get(ctx, pool, it.ID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	if read.Priority != 7 {
		t.Errorf("the item's priority is %d, the owner set 7", read.Priority)
	}

	if err := dispatch([]string{"priority", "-priority", "7"}); err == nil {
		t.Error("priority with no item id was accepted")
	}
	if err := dispatch([]string{"priority", "it_missing", "-priority", "1"}); !errors.Is(err, item.ErrNotFound) {
		t.Errorf("priority on a missing item = %v, want ErrNotFound", err)
	}
}
