package environment_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gatepolicy"
)

// TestDDLListsEveryStrategy keeps the CHECK constraint and
// [gatepolicy.Strategies] from disagreeing: the constraint is SQL text rather
// than built from the slice, so this is what says they still name the same two,
// beside the empty value an owner has authored no default at.
func TestDDLListsEveryStrategy(t *testing.T) {
	const open = "constraint strategy_default_known check (strategy_default in ("
	statement := environment.DDL[0]
	i := strings.Index(statement, open)
	if i < 0 {
		t.Fatalf("the DDL has no %q list", open)
	}
	rest := statement[i+len(open):]
	listed := strings.Split(rest[:strings.Index(rest, ")")], ",")
	if len(listed) != len(gatepolicy.Strategies)+1 {
		t.Fatalf("the constraint lists %d values, want the %d strategies and the empty one",
			len(listed), len(gatepolicy.Strategies))
	}
	if got := strings.TrimSpace(listed[0]); got != "''" {
		t.Errorf("the constraint's first value is %s, want the empty one an owner authored no default at", got)
	}
	for n, s := range gatepolicy.Strategies {
		if got, want := strings.TrimSpace(listed[n+1]), "'"+string(s)+"'"; got != want {
			t.Errorf("the constraint lists %s where Strategies has %s", got, want)
		}
	}
}

// TestTheStrategyDefaultIsProductionsAlone: a strategy decides whether a control
// runs, and a control exists only where organic traffic does — so the default an
// owner authors is a field of production's record and a write against any other
// kind is refused.
func TestTheStrategyDefaultIsProductionsAlone(t *testing.T) {
	ctx, pool, w, token := newTable(t)

	production, err := w.Create(ctx, owner, productionSpec())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	read, err := environment.Get(ctx, pool, production.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.StrategyDefault != "" {
		t.Errorf("a production environment nobody authored a default on = %q", read.StrategyDefault)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	err = environment.SetStrategyDefault(ctx, tx, token, owner, production.ID, gatepolicy.StrategyWithControl)
	if err != nil {
		t.Fatalf("SetStrategyDefault: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	read, err = environment.Get(ctx, pool, production.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.StrategyDefault != gatepolicy.StrategyWithControl {
		t.Errorf("the strategy default reads back as %q, want %q", read.StrategyDefault, gatepolicy.StrategyWithControl)
	}

	customer, err := w.Create(ctx, owner, environment.Spec{
		Kind: environment.KindCustomer, ProjectID: theProject, Name: "staging",
		Targets: oneTarget("/srv/targets/staging"), Credential: credential, Platform: composingPlatform,
	})
	if err != nil {
		t.Fatalf("Create the customer's: %v", err)
	}

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = environment.SetStrategyDefault(ctx, tx, token, owner, customer.ID, gatepolicy.StrategyWithControl)
	if !errors.Is(err, environment.ErrNotAProductionEnvironment) {
		t.Errorf("a strategy default on a customer's environment = %v, want ErrNotAProductionEnvironment", err)
	}
	err = environment.SetStrategyDefault(ctx, tx, token, owner, production.ID, "canary")
	if !errors.Is(err, gatepolicy.ErrStrategyUnknown) {
		t.Errorf("a default naming a strategy no deployer performs = %v, want ErrStrategyUnknown", err)
	}
}
