package policy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/safeguard"
	"github.com/dulguun0225/borg/factory/service"
)

// TestEveryAuthoredValueBesideTheElevenTakesAVersionAndIsInForce: each of the
// values the design names on the service record and on production's environment
// record is authored through this package — a version appended first, the field
// second — and reads back through the value in force.
func TestEveryAuthoredValueBesideTheElevenTakesAVersionAndIsInForce(t *testing.T) {
	ctx, in := newFactory(t)

	authored := []struct {
		parameter gatepolicy.Parameter
		value     float64
		write     func() (policy.Version, error)
	}{
		{gatepolicy.BakeVolume, 5000, func() (policy.Version, error) {
			return in.factory.AuthorBakeVolume(ctx, owner, in.service.ID, 5000)
		}},
		{gatepolicy.BacklogCap, 4, func() (policy.Version, error) {
			return in.factory.AuthorBacklogCap(ctx, owner, in.service.ID, 4)
		}},
		{gatepolicy.MutationFloor, 0.8, func() (policy.Version, error) {
			return in.factory.AuthorMutationFloor(ctx, owner, in.service.ID, 0.8)
		}},
		{gatepolicy.KeptFraction, 0.5, func() (policy.Version, error) {
			return in.factory.AuthorKeptFraction(ctx, owner, in.service.ID, 0.5)
		}},
		{gatepolicy.MaxConcurrentKeptFleets, 3, func() (policy.Version, error) {
			return in.factory.AuthorMaxConcurrentKeptFleets(ctx, owner, in.service.ID, 3)
		}},
		{gatepolicy.RecentHistoryRunLength, 20000, func() (policy.Version, error) {
			return in.factory.AuthorRecentHistoryRunLength(ctx, owner, in.service.ID, 20000)
		}},
		{gatepolicy.ProofTestRate, 0.25, func() (policy.Version, error) {
			return in.factory.AuthorProofTestRate(ctx, owner, in.service.ID, 0.25)
		}},
		{gatepolicy.InstanceHourRate, 0.12, func() (policy.Version, error) {
			return in.factory.AuthorInstanceHourRate(ctx, owner, in.service.ID, 0.12)
		}},
		{gatepolicy.EnvironmentHourRate, 0.4, func() (policy.Version, error) {
			return in.factory.AuthorEnvironmentHourRate(ctx, owner, in.service.ID, 0.4)
		}},
		{gatepolicy.MutantCap, 40, func() (policy.Version, error) {
			return in.factory.AuthorMutantCap(ctx, owner, in.service.ID, 40)
		}},
		{gatepolicy.FailureRecordKeyCap, 100, func() (policy.Version, error) {
			return in.factory.AuthorFailureRecordKeyCap(ctx, owner, in.service.ID, 100)
		}},
		{gatepolicy.UnreliableBound, 0.2, func() (policy.Version, error) {
			return in.factory.AuthorUnreliableBound(ctx, owner, in.service.ID, 0.2)
		}},
		{gatepolicy.IncidentItemBound, 7200, func() (policy.Version, error) {
			return in.factory.AuthorIncidentItemBound(ctx, owner, in.service.ID, 7200)
		}},
		{gatepolicy.SnapshotRetention, 86400, func() (policy.Version, error) {
			return in.factory.AuthorSnapshotRetention(ctx, owner, in.service.ID, 86400)
		}},
		{gatepolicy.RecentHistorySize, 0.02, func() (policy.Version, error) {
			return in.factory.AuthorRecentHistorySize(ctx, owner, in.service.ID,
				gatepolicy.QuantityErrorRate, 0.02)
		}},
	}

	for _, one := range authored {
		version, err := one.write()
		if err != nil {
			t.Fatalf("authoring %s: %v", one.parameter, err)
		}
		if version.Parameter != one.parameter {
			t.Errorf("the version for %s names parameter %q", one.parameter, version.Parameter)
		}
		effective, err := in.reader.InForce(ctx, one.parameter, in.subjects("deploy_to_production"))
		if err != nil {
			t.Fatalf("InForce(%s): %v", one.parameter, err)
		}
		if effective.Source != policy.FromAuthored || effective.Number != one.value {
			t.Errorf("%s in force = %+v, want the authored %v", one.parameter, effective, one.value)
		}
	}

	// The two values authored together with a second number, and the two lists:
	// each is named on its version by key and no parameter, because re-deriving
	// one number of a pair would leave the record in a state its own CHECK
	// refuses.
	if _, err := in.factory.AuthorSearchBudget(ctx, owner, in.service.ID, 6, 3600); err != nil {
		t.Fatalf("AuthorSearchBudget: %v", err)
	}
	if _, err := in.factory.AuthorOperationCap(ctx, owner, in.service.ID, 200, "other"); err != nil {
		t.Fatalf("AuthorOperationCap: %v", err)
	}
	if _, err := in.factory.AuthorChangeFreezePeriod(ctx, owner, in.service.ID,
		"2026-12-24T00:00:00.000000000Z", "2026-12-27T00:00:00.000000000Z"); err != nil {
		t.Fatalf("AuthorChangeFreezePeriod: %v", err)
	}

	read, err := service.Get(ctx, in.pool, in.service.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.SearchBudgetBuilds.Number != 6 || read.SearchBudgetSeconds.Number != 3600 {
		t.Errorf("the search budget reads back as %+v %+v", read.SearchBudgetBuilds, read.SearchBudgetSeconds)
	}
	if read.OperationCap.Number != 200 || read.OverflowOperation != "other" {
		t.Errorf("the operation cap reads back as %+v %q", read.OperationCap, read.OverflowOperation)
	}
	frozen, _, err := service.Frozen(ctx, in.pool, in.service.ID, "2026-12-25T00:00:00.000000000Z")
	if err != nil || !frozen {
		t.Errorf("the service is frozen inside the period it authored = %v, %v", frozen, err)
	}
}

// TestTheStrategyDefaultIsAuthoredOnProductionsRecord: the default is
// production's alone, and authoring it appends a version like every other owner
// write at Factory.
func TestTheStrategyDefaultIsAuthoredOnProductionsRecord(t *testing.T) {
	ctx, in := newFactory(t)

	version, err := in.factory.AuthorStrategyDefault(ctx, owner, in.prod.ID, gatepolicy.StrategyWithControl)
	if err != nil {
		t.Fatalf("AuthorStrategyDefault: %v", err)
	}
	if version.Parameter != gatepolicy.StrategyDefault {
		t.Errorf("the version names parameter %q, want the strategy default", version.Parameter)
	}
	read, err := environment.Get(ctx, in.pool, in.prod.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.StrategyDefault != gatepolicy.StrategyWithControl {
		t.Errorf("production's strategy default = %q, want %q", read.StrategyDefault, gatepolicy.StrategyWithControl)
	}
}

// TestASafeguardOnTheExplicitThresholdReachesTheFieldTheHealthMonitorReads: the
// explicit threshold is set by a safeguard and nothing else, and placing one
// adds a check rather than clamping a number — so the number and the size beside
// it land on the service record, which is where the health monitor reads them.
func TestASafeguardOnTheExplicitThresholdReachesTheFieldTheHealthMonitorReads(t *testing.T) {
	ctx, in := newFactory(t)
	quantity := string(gatepolicy.QuantityErrorRate)
	subject := safeguard.Subject{Kind: safeguard.SubjectService, ID: in.service.ID, Key: quantity}

	// The number alone writes no field: the owner sets the size when they set the
	// number, so the pair is what the record holds.
	if _, _, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.ExplicitThreshold,
		subject, safeguard.Bound{Number: 0.01}, safeguard.Routing{}); err != nil {
		t.Fatalf("AddSafeguard on the threshold: %v", err)
	}
	read, err := service.Get(ctx, in.pool, in.service.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, held := read.ExplicitThreshold[gatepolicy.QuantityErrorRate]; held {
		t.Errorf("the record holds a threshold with no size beside it: %+v", read.ExplicitThreshold)
	}

	if _, _, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.ExplicitThresholdSize,
		subject, safeguard.Bound{Number: 0.002}, safeguard.Routing{}); err != nil {
		t.Fatalf("AddSafeguard on the size: %v", err)
	}
	read, err = service.Get(ctx, in.pool, in.service.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	threshold, held := read.ExplicitThreshold[gatepolicy.QuantityErrorRate]
	if !held || threshold.Number != 0.01 || threshold.Size != 0.002 {
		t.Fatalf("the explicit threshold on the record = %+v (held %v), want the pair the two safeguards name",
			threshold, held)
	}

	// It adds and does not clamp: the value in force is the number the safeguard
	// named, and nothing was narrowed.
	effective, err := in.reader.InForce(ctx, gatepolicy.ExplicitThreshold, in.subjects("deploy_to_production"))
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	if effective.Clamped {
		t.Errorf("the explicit threshold reads as clamped: %+v", effective)
	}
	if len(effective.Safeguards) != 1 {
		t.Errorf("the safeguards on the explicit threshold are %v, want the one placed", effective.Safeguards)
	}
}

// TestARetirementCallsTheDeployersRemoval: the write that retires a service
// calls the deployer, and a factory composed with no deployer refuses it rather
// than writing retired and ending nothing.
func TestARetirementCallsTheDeployersRemoval(t *testing.T) {
	ctx, in := newFactory(t)

	if _, err := in.factory.RetireService(ctx, owner, in.service.ID, 0, 0, 0); err == nil {
		t.Errorf("retiring through a factory with no deployer composed = nil, want a refusal")
	}

	removed, from := "", "unset"
	in.factory.Removal = func(_ context.Context, serviceID, environmentID string) error {
		removed, from = serviceID, environmentID
		return nil
	}
	if _, err := in.factory.RetireService(ctx, owner, in.service.ID, 0, 0, 0); err != nil {
		t.Fatalf("RetireService: %v", err)
	}
	if removed != in.service.ID {
		t.Errorf("the deployer was asked to remove %q, want %q", removed, in.service.ID)
	}
	if from != "" {
		t.Errorf("the removal names environment %q, and a retirement reaches every persistent one", from)
	}
	read, err := service.Get(ctx, in.pool, in.service.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !read.Retired() {
		t.Errorf("the service reads as standing after it was retired: %+v", read)
	}
}

// TestARemovalForOneEnvironmentIsPerformedForThatOne is
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/04-retirement.md's
// "the owner has the deployer remove each service from it first, the same
// removal performed for that one environment, called from Factory": the call
// reaches the deployer naming the environment, writes nothing on the service
// record, and is refused where nothing composed a deployer or where it names no
// environment.
func TestARemovalForOneEnvironmentIsPerformedForThatOne(t *testing.T) {
	ctx, in := newFactory(t)

	if err := in.factory.RemoveFromEnvironment(ctx, owner, in.service.ID, in.prod.ID); err == nil {
		t.Errorf("removing through a factory with no deployer composed = nil, want a refusal")
	}

	removed, from := "", ""
	in.factory.Removal = func(_ context.Context, serviceID, environmentID string) error {
		removed, from = serviceID, environmentID
		return nil
	}
	if err := in.factory.RemoveFromEnvironment(ctx, owner, in.service.ID, ""); !errors.Is(err, policy.ErrEnvironmentIDEmpty) {
		t.Errorf("removing from no environment = %v, want ErrEnvironmentIDEmpty", err)
	}
	if err := in.factory.RemoveFromEnvironment(ctx, owner, in.service.ID, in.prod.ID); err != nil {
		t.Fatalf("RemoveFromEnvironment: %v", err)
	}
	if removed != in.service.ID || from != in.prod.ID {
		t.Errorf("the deployer was asked to remove %q from %q, want %q from %q",
			removed, from, in.service.ID, in.prod.ID)
	}
	read, err := service.Get(ctx, in.pool, in.service.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Retired() {
		t.Error("the service reads as retired, and a removal for one environment writes nothing on the record")
	}
}

// TestAProjectEndsOnceEveryServiceInItIsRetired: the project is ended at Factory
// and its production environment is withdrawn in the same write, refused while a
// service in it still stands.
func TestAProjectEndsOnceEveryServiceInItIsRetired(t *testing.T) {
	ctx, in := newFactory(t)
	in.factory.Removal = func(context.Context, string, string) error { return nil }

	if _, err := in.factory.EndProject(ctx, owner, in.project.ID, 0); err == nil {
		t.Errorf("ending a project holding a service that is not retired = nil, want a refusal")
	}
	if _, err := in.factory.RetireService(ctx, owner, in.service.ID, 0, 0, 0); err != nil {
		t.Fatalf("RetireService: %v", err)
	}
	if _, err := in.factory.EndProject(ctx, owner, in.project.ID, 0); err != nil {
		t.Fatalf("EndProject: %v", err)
	}

	// The project this service is in has a production environment of its own,
	// written in the same event the project was, and it ends with it.
	production, found, err := environment.Production(ctx, in.pool, in.project.ID)
	if err != nil || !found {
		t.Fatalf("Production of the project = found %v, %v", found, err)
	}
	if production.WithdrawnAt == "" {
		t.Errorf("production's environment stands after the project ended: %+v", production)
	}
}
