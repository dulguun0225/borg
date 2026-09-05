package environment_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/record"
)

// TestATargetDeclaresWhetherThePlatformBehindItServesAShare: the declaration is
// written with the target, and it is the fact the score reads when it picks a
// strategy — so it survives the round trip through the one column the targets are
// stored in, order and all.
func TestATargetDeclaresWhetherThePlatformBehindItServesAShare(t *testing.T) {
	ctx, pool, w, _ := newTable(t)

	targets := []environment.Target{
		{Address: "/srv/targets/one", ServesAShare: true},
		{Address: "/srv/targets/two", ServesAShare: true},
	}
	created, err := w.Create(ctx, owner, productionSpec(targets...))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	read, err := environment.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !slices.Equal(read.Targets, targets) {
		t.Fatalf("the targets read back as %+v, want %+v", read.Targets, targets)
	}
	if !read.EveryTargetServesAShare() {
		t.Error("an environment whose targets all serve a share reads as one that does not")
	}

	// A target with no address is one no deploy can reach.
	noAddress := productionSpec([]environment.Target{{ServesAShare: true}}...)
	noAddress.Name = "staging"
	noAddress.Kind = environment.KindCustomer
	if _, err := w.Create(ctx, owner, noAddress); !errors.Is(err, environment.ErrTargetAddressEmpty) {
		t.Errorf("Create with an address of nothing = %v, want ErrTargetAddressEmpty", err)
	}
}

// TestATargetLeavesTheFieldTheWayAnEnvironmentIsWithdrawn: an owner removes the
// address, refused while any service's deploy record marks that target complete
// for a release, so the deployer's removal on that one target comes first.
func TestATargetLeavesTheFieldTheWayAnEnvironmentIsWithdrawn(t *testing.T) {
	ctx, pool, w, _ := newTable(t)

	created, err := w.Create(ctx, owner, productionSpec(
		environment.Target{Address: "/srv/targets/one"},
		environment.Target{Address: "/srv/targets/two"},
	))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// One deploy record still marks that target complete for a release.
	err = w.RemoveTarget(ctx, owner, created.ID, "/srv/targets/two", 1)
	if !errors.Is(err, environment.ErrSoftwareStandsOnIt) {
		t.Errorf("removing a target software still stands on = %v, want ErrSoftwareStandsOnIt", err)
	}
	if err := w.RemoveTarget(ctx, owner, created.ID, "/srv/targets/three", 0); !errors.Is(err, environment.ErrTargetNotHeld) {
		t.Errorf("removing an address the environment does not hold = %v, want ErrTargetNotHeld", err)
	}
	if err := w.RemoveTarget(ctx, owner, created.ID, "/srv/targets/two", 0); err != nil {
		t.Fatalf("RemoveTarget: %v", err)
	}

	read, err := environment.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !slices.Equal(read.Addresses(), []string{"/srv/targets/one"}) {
		t.Errorf("the targets read back as %v, want the one that was kept", read.Addresses())
	}

	// The last target may not go: an environment with no address is one no
	// deploy can reach, so an environment down to one is withdrawn instead.
	if err := w.RemoveTarget(ctx, owner, created.ID, "/srv/targets/one", 0); !errors.Is(err, environment.ErrTargetsEmpty) {
		t.Errorf("removing the last target = %v, want ErrTargetsEmpty", err)
	}

	// A target is added last, the order being the one a rollout reaches them in.
	if err := w.AddTarget(ctx, owner, created.ID, environment.Target{Address: "/srv/targets/three", ServesAShare: true}); err != nil {
		t.Fatalf("AddTarget: %v", err)
	}
	if err := w.AddTarget(ctx, owner, created.ID, environment.Target{Address: "/srv/targets/three"}); !errors.Is(err, environment.ErrTargetAlreadyHeld) {
		t.Errorf("adding an address the environment already holds = %v, want ErrTargetAlreadyHeld", err)
	}
	if read, err = environment.Get(ctx, pool, created.ID); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !slices.Equal(read.Addresses(), []string{"/srv/targets/one", "/srv/targets/three"}) {
		t.Errorf("the targets read back as %v, want the new one last", read.Addresses())
	}
}

// TestAPersistentEnvironmentIsWithdrawnByAnOwner: it ends by an owner's
// withdrawal at Factory, refused while any deploy record on it marks a target
// complete for a release — the owner has the deployer remove each service from it
// first. A candidate's is torn down instead and refuses the write.
func TestAPersistentEnvironmentIsWithdrawnByAnOwner(t *testing.T) {
	ctx, pool, w, token := newTable(t)

	customer := productionSpec()
	customer.Kind = environment.KindCustomer
	customer.Name = "staging"
	customer.Platform.CanComposeOnDemand = false
	created, err := w.Create(ctx, owner, customer)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := w.Withdraw(ctx, owner, created.ID, 2); !errors.Is(err, environment.ErrSoftwareStandsOnIt) {
		t.Errorf("withdrawing an environment software still stands on = %v, want ErrSoftwareStandsOnIt", err)
	}
	if err := w.Withdraw(ctx, owner, created.ID, 0); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}

	read, err := environment.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Live() {
		t.Error("a withdrawn environment reads as live")
	}
	if read.WithdrawnAt == "" {
		t.Error("the withdrawal wrote no time")
	}

	// Nothing is written on a withdrawn environment after the withdrawal.
	if err := w.Withdraw(ctx, owner, created.ID, 0); !errors.Is(err, environment.ErrAlreadyWithdrawn) {
		t.Errorf("withdrawing twice = %v, want ErrAlreadyWithdrawn", err)
	}
	if err := w.AddTarget(ctx, owner, created.ID, environment.Target{Address: "/srv/late"}); !errors.Is(err, environment.ErrAlreadyWithdrawn) {
		t.Errorf("adding a target to a withdrawn environment = %v, want ErrAlreadyWithdrawn", err)
	}

	// A candidate's environment is not an owner's to withdraw.
	env, err := environment.NewCandidates(pool, token).Compose(ctx, deployer, "it_a", theProject,
		oneTarget("/srv/candidate"), credential, environment.Composition{})
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if err := w.Withdraw(ctx, owner, env.ID, 0); !errors.Is(err, environment.ErrNotAnOwnersKind) {
		t.Errorf("withdrawing a candidate's environment = %v, want ErrNotAnOwnersKind", err)
	}
	if err := w.Withdraw(ctx, owner, "env_missing", 0); !errors.Is(err, environment.ErrNotFound) {
		t.Errorf("withdrawing a missing environment = %v, want ErrNotFound", err)
	}
	if err := w.Withdraw(ctx, record.Actor{}, created.ID, 0); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("withdrawing with no actor = %v, want ErrKindUnknown", err)
	}
}
