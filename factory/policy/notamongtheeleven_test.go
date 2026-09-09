package policy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/principal"
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

	removed, from, calledBy := "", "unset", ""
	in.factory.Removal = func(_ context.Context, p principal.Principal, serviceID, environmentID string) error {
		removed, from, calledBy = serviceID, environmentID, p.Actor.Key
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
	if calledBy != owner.Key {
		t.Errorf("the call reaching the seam carries %q, want the owner %q whose write called for it",
			calledBy, owner.Key)
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

	if _, err := in.factory.RemoveFromEnvironment(ctx, owner, in.service.ID, in.prod.ID); err == nil {
		t.Errorf("removing through a factory with no deployer composed = nil, want a refusal")
	}

	removed, from, calledBy := "", "", ""
	in.factory.Removal = func(_ context.Context, p principal.Principal, serviceID, environmentID string) error {
		removed, from, calledBy = serviceID, environmentID, p.Actor.Key
		return nil
	}
	if _, err := in.factory.RemoveFromEnvironment(ctx, owner, in.service.ID, ""); !errors.Is(err, policy.ErrEnvironmentIDEmpty) {
		t.Errorf("removing from no environment = %v, want ErrEnvironmentIDEmpty", err)
	}
	before := newestVersion(t, ctx, in)
	version, err := in.factory.RemoveFromEnvironment(ctx, owner, in.service.ID, in.prod.ID)
	if err != nil {
		t.Fatalf("RemoveFromEnvironment: %v", err)
	}
	if removed != in.service.ID || from != in.prod.ID {
		t.Errorf("the deployer was asked to remove %q from %q, want %q from %q",
			removed, from, in.service.ID, in.prod.ID)
	}
	if calledBy != owner.Key {
		t.Errorf("the call reaching the seam carries %q, want the owner %q whose write called for it",
			calledBy, owner.Key)
	}
	// Every owner write at Factory appends a version, this one included: it
	// authors nothing and records that an owner called for the removal.
	if version.ID == before.ID || version.Action != policy.ActionRemoved {
		t.Errorf("the removal appended version %s (%q), and the one before was %s",
			version.ID, version.Action, before.ID)
	}
	if version.Scope.ID != in.prod.ID || version.Scope.Key != in.service.ID {
		t.Errorf("the version names %s, want the environment %s and the service %s",
			version.Scope, in.prod.ID, in.service.ID)
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
	in.factory.Removal = func(context.Context, principal.Principal, string, string) error { return nil }

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

// TestTheChannelsTwoRatesAreTwoParameters is
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md's
// "the report channel's two rates, per service and factory-wide": one is a
// field of the factory-wide settings record with one value per record and the
// other a row of it keyed by the service. They were one parameter keyed by the
// service, so the factory-wide rate was a value under an empty key that no
// safeguard, no version and no re-derivation could name apart from a service's.
func TestTheChannelsTwoRatesAreTwoParameters(t *testing.T) {
	ctx, in := newFactory(t)

	factoryWide, err := in.factory.AuthorReportChannelRate(ctx, owner, 200)
	if err != nil {
		t.Fatalf("AuthorReportChannelRate: %v", err)
	}
	perService, err := in.factory.AuthorServiceReportChannelRate(ctx, owner, in.service.ID, 50)
	if err != nil {
		t.Fatalf("AuthorServiceReportChannelRate: %v", err)
	}
	if factoryWide.Parameter == perService.Parameter {
		t.Errorf("both rates were authored as %q, and the design gives the channel two", factoryWide.Parameter)
	}
	if factoryWide.Scope.Key != "" || perService.Scope.Key != in.service.ID {
		t.Errorf("the factory-wide rate is keyed %q and the per-service one %q",
			factoryWide.Scope.Key, perService.Scope.Key)
	}

	both := newestVersion(t, ctx, in)
	named := map[gatepolicy.Parameter]float64{}
	for _, value := range both.Authored {
		named[value.Parameter] = value.Number
	}
	if named[gatepolicy.ReportChannelRate] != 200 || named[gatepolicy.ServiceReportChannelRate] != 50 {
		t.Errorf("the version names %v, want the factory-wide 200 beside the per-service 50", named)
	}

	inForce, err := in.reader.InForce(ctx, gatepolicy.ReportChannelRate, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	if inForce.Number != 200 {
		t.Errorf("the factory-wide rate in force is %v against a read naming a service, want 200", inForce.Number)
	}
	inForce, err = in.reader.InForce(ctx, gatepolicy.ServiceReportChannelRate, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	if inForce.Number != 50 {
		t.Errorf("the per-service rate in force is %v, want 50", inForce.Number)
	}
}

// TestAnUnauthoredParameterReadsTheValueTheDesignFixes is what
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/01-authored-and-not-among-the-eleven.md
// and
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/02-retention.md
// fix rather than have the score supply: "the fraction's
// default is all of them", "the hours' default is every hour", "no proof test
// runs at all where an owner authors no rate", "both are kept for the life of
// the install", "arrival is unbounded", and "a snapshot stands until an owner
// deletes it". Each was a sentence in the parameter's unit and nothing read it,
// so every one of them resolved to the number nothing.
func TestAnUnauthoredParameterReadsTheValueTheDesignFixes(t *testing.T) {
	ctx, in := newFactory(t)
	subjects := in.subjects("merge_to_master")

	numbers := []struct {
		parameter gatepolicy.Parameter
		want      float64
	}{
		{gatepolicy.KeptFraction, 1},
		{gatepolicy.ProofTestRate, 0},
	}
	for _, n := range numbers {
		inForce, err := in.reader.InForce(ctx, n.parameter, subjects)
		if err != nil {
			t.Fatalf("InForce(%s): %v", n.parameter, err)
		}
		if inForce.Number != n.want || inForce.Unbounded {
			t.Errorf("an unauthored %s reads %v (unbounded %v), want %v",
				n.parameter, inForce.Number, inForce.Unbounded, n.want)
		}
		if inForce.Source != policy.FromFactory {
			t.Errorf("an unauthored %s reads from %s, want the factory's own", n.parameter, inForce.Source)
		}
	}

	unbounded := []gatepolicy.Parameter{
		gatepolicy.DecisionLogRetention, gatepolicy.ReportRetention, gatepolicy.SnapshotRetention,
		gatepolicy.ReportChannelRate, gatepolicy.ServiceReportChannelRate, gatepolicy.PagingHours,
		// C2269, C2298, C2283, C2289: authored outright with nothing supplied,
		// so an unauthored read is the factory's own with nothing for the
		// score to teach rather than whatever the supplied table happens to
		// hold for that name.
		gatepolicy.RemediationPeriod, gatepolicy.BackupRetention, gatepolicy.MaxConcurrentKeptFleets,
		gatepolicy.Objective, gatepolicy.MaxConcurrentCandidateEnvironments, gatepolicy.ChangeFreeze,
	}
	for _, parameter := range unbounded {
		inForce, err := in.reader.InForce(ctx, parameter, subjects)
		if err != nil {
			t.Fatalf("InForce(%s): %v", parameter, err)
		}
		if !inForce.Unbounded {
			t.Errorf("an unauthored %s reads %v, and the design bounds it with nothing",
				parameter, inForce.Number)
		}
		if inForce.Source != policy.FromFactory {
			t.Errorf("an unauthored %s reads from %s, want the factory's own", parameter, inForce.Source)
		}
	}
}

// TestEveryParameterNotAmongTheElevenResolves: package gatepolicy defines a
// parameter and package policy is what resolves one, so a name in that list the
// reader cannot answer for is a value an owner authors and nothing reads back.
func TestEveryParameterNotAmongTheElevenResolves(t *testing.T) {
	ctx, in := newFactory(t)
	subjects := in.subjects("merge_to_master")
	subjects.Severity, subjects.SeverityNamed = 7, true

	for _, d := range gatepolicy.NotAmongTheEleven {
		if _, err := in.reader.InForce(ctx, d.Parameter, subjects); err != nil {
			t.Errorf("InForce(%s): %v", d.Parameter, err)
		}
	}
}
