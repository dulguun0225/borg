package policy_test

import (
	"context"
	"slices"
	"testing"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/safeguard"
)

// TestASafeguardNeverWidens: a safeguard is a bound and not a precedence, so a
// ceiling of five over an authored two leaves the two — read as a precedence it
// would raise the number, which is a safeguard adding throughput and removing
// safety.
func TestASafeguardNeverWidens(t *testing.T) {
	ctx, in := newFactory(t)

	if _, err := in.factory.AuthorWindowLimit(ctx, owner, in.service.ID, 2); err != nil {
		t.Fatalf("AuthorWindowLimit: %v", err)
	}
	if _, _, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.WindowLimit,
		safeguard.Subject{Kind: safeguard.SubjectService, ID: in.service.ID}, safeguard.Bound{Number: 5}, safeguard.Routing{}); err != nil {
		t.Fatalf("AddSafeguard: %v", err)
	}

	all, err := in.reader.All(ctx, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	limit := effectiveOf(t, all, gatepolicy.WindowLimit)
	if limit.Number != 2 {
		t.Errorf("a safeguard's ceiling of 5 over an authored 2 reads %v, want 2", limit.Number)
	}
	if limit.Clamped {
		t.Error("the safeguard is recorded as having clamped a value already narrower than itself")
	}
	if len(limit.Safeguards) != 1 {
		t.Errorf("the safeguard that clamped nothing is not named: %v", limit.Safeguards)
	}

	// A floor is the same rule the other way: a safeguard puts a floor under the
	// window's confidence, and one under the authored value leaves it.
	if _, err := in.factory.AuthorWindowConfidence(ctx, owner, in.service.ID, 0.99); err != nil {
		t.Fatalf("AuthorWindowConfidence: %v", err)
	}
	if _, _, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.WindowConfidence,
		safeguard.Subject{Kind: safeguard.SubjectService, ID: in.service.ID}, safeguard.Bound{Number: 0.9}, safeguard.Routing{}); err != nil {
		t.Fatalf("AddSafeguard: %v", err)
	}
	all, err = in.reader.All(ctx, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if confidence := effectiveOf(t, all, gatepolicy.WindowConfidence); confidence.Number != 0.99 {
		t.Errorf("a safeguard's floor of 0.9 under an authored 0.99 reads %v, want 0.99", confidence.Number)
	}
}

// TestASafeguardOnTheThresholdAddsAHumanRatherThanMovingTheNumber: the risk
// threshold's safeguard is the one that is not arithmetic, and what it does is
// the whole of what a gate reads from it.
func TestASafeguardOnTheThresholdAddsAHumanRatherThanMovingTheNumber(t *testing.T) {
	ctx, in := newFactory(t)

	before, err := in.reader.AtGate(ctx, ownerReading, in.subjects("deploy_to_production"))
	if err != nil {
		t.Fatalf("AtGate: %v", err)
	}
	supplied := startingValue(t, gatepolicy.RiskThreshold)
	if before.HumanBySafeguard || before.Threshold != supplied || before.ThresholdFrom != policy.FromSupplied {
		t.Errorf("with nothing authored the gate reads %+v, want the supplied threshold and no safeguard", before)
	}

	placed, version, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.RiskThreshold,
		safeguard.Subject{Kind: safeguard.SubjectService, ID: in.service.ID, Key: "deploy_to_production"},
		safeguard.Bound{Number: 0}, safeguard.Routing{})
	if err != nil {
		t.Fatalf("AddSafeguard: %v", err)
	}

	after, err := in.reader.AtGate(ctx, ownerReading, in.subjects("deploy_to_production"))
	if err != nil {
		t.Fatalf("AtGate: %v", err)
	}
	if !after.HumanBySafeguard {
		t.Error("the safeguard adds no human at the row")
	}
	if after.Threshold != before.Threshold {
		t.Errorf("the safeguard moved the threshold to %v from %v", after.Threshold, before.Threshold)
	}
	if !slices.Contains(after.Safeguards, placed.ID) {
		t.Errorf("the firing names safeguards %v, want the one placed", after.Safeguards)
	}
	if after.PolicyVersion != version.ID {
		t.Errorf("the firing names policy version %q, want the one the safeguard appended %q", after.PolicyVersion, version.ID)
	}

	// The other row has no safeguard: a safeguard on a gate row reaches that row
	// and no other.
	elsewhere, err := in.reader.AtGate(ctx, ownerReading, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("AtGate: %v", err)
	}
	if elsewhere.HumanBySafeguard {
		t.Error("a safeguard on the deploy row reached the merge row")
	}

	// Writing the withdrawal does not stop it applying: a withdrawal is decided
	// and not merely written, and until the gate row that decides it approves it
	// the safeguard stands.
	written, _, err := in.factory.WriteSafeguardWithdrawal(ctx, owner, placed.ID)
	if err != nil {
		t.Fatalf("WriteSafeguardWithdrawal: %v", err)
	}
	pending, err := in.reader.AtGate(ctx, ownerReading, in.subjects("deploy_to_production"))
	if err != nil {
		t.Fatalf("AtGate: %v", err)
	}
	if !pending.HumanBySafeguard {
		t.Error("a pending withdrawal took the human off the row before the row that decides it approved it")
	}

	// The approval is where it leaves force, and the firing that follows names
	// no safeguard.
	if _, err := in.factory.ApproveSafeguardWithdrawal(ctx, approver, written.ID, decidedAt); err != nil {
		t.Fatalf("ApproveSafeguardWithdrawal: %v", err)
	}
	withdrawn, err := in.reader.AtGate(ctx, ownerReading, in.subjects("deploy_to_production"))
	if err != nil {
		t.Fatalf("AtGate: %v", err)
	}
	if withdrawn.HumanBySafeguard || len(withdrawn.Safeguards) != 0 {
		t.Errorf("an approved withdrawal leaves the safeguard applying: %+v", withdrawn)
	}
}

// TestASafeguardOnAnAreaReachesAnItemInTheChain: a safeguard drawn on any area
// in the chain reaches an item in the narrowest, which is why the walk exists —
// without it an owner who declared a narrower area inside one with a safeguard
// on it would lose it.
func TestASafeguardOnAnAreaReachesAnItemInTheChain(t *testing.T) {
	ctx, in := newFactory(t)

	inner, err := area.NewWriter(in.pool, in.token).Declare(ctx, owner, "payments/refunds", area.InsideArea(in.area.ID), area.Hazard{})
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}
	if _, _, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.RiskThreshold,
		safeguard.Subject{Kind: safeguard.SubjectArea, ID: in.area.ID, Key: "merge_to_master"},
		safeguard.Bound{Number: 0}, safeguard.Routing{}); err != nil {
		t.Fatalf("AddSafeguard: %v", err)
	}

	subjects := in.subjects("merge_to_master")
	subjects.AreaID = inner.ID
	applied, err := in.reader.AtGate(ctx, ownerReading, subjects)
	if err != nil {
		t.Fatalf("AtGate: %v", err)
	}
	if !applied.HumanBySafeguard {
		t.Error("a safeguard on the outer area does not reach an item in the inner one")
	}
}

// TestTheTwoAdmissionsOnTheReportStore: an owner places each admission the way
// they place any safeguard — one parameter, the report store as the subject,
// no bound — and the mechanism that reads it sees it in force. Neither is read
// where neither was placed, which is what an install that never asked for the
// wait costs.
//
// The subject is the factory-wide settings record's id, the report store having
// no record of its own, so a safeguard drawn on anything else reaches nothing.
func TestTheTwoAdmissionsOnTheReportStore(t *testing.T) {
	ctx, in := newFactory(t)

	store := safeguard.Subject{Kind: safeguard.SubjectReportStore, ID: in.settings.ID}
	admissions, err := in.reader.ReportStoreAdmissions(ctx)
	if err != nil {
		t.Fatalf("ReportStoreAdmissions: %v", err)
	}
	if admissions.ArrivingReports || admissions.ReportDerivedIntents {
		t.Errorf("an install with no such safeguard reads %+v, want nothing waiting", admissions)
	}

	if _, _, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.ReportAdmission,
		store, safeguard.Bound{}, safeguard.Routing{}); err != nil {
		t.Fatalf("placing the safeguard on arriving reports: %v", err)
	}
	if admissions, err = in.reader.ReportStoreAdmissions(ctx); err != nil {
		t.Fatalf("ReportStoreAdmissions: %v", err)
	}
	if !admissions.ArrivingReports || admissions.ReportDerivedIntents {
		t.Errorf("one safeguard placed reads %+v, want the arriving reports held alone", admissions)
	}

	if _, _, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.ReportDerivedIntentAdmission,
		store, safeguard.Bound{}, safeguard.Routing{}); err != nil {
		t.Fatalf("placing the safeguard on report-derived intents: %v", err)
	}
	if admissions, err = in.reader.ReportStoreAdmissions(ctx); err != nil {
		t.Fatalf("ReportStoreAdmissions: %v", err)
	}
	if !admissions.ArrivingReports || !admissions.ReportDerivedIntents {
		t.Errorf("both safeguards placed read %+v, want both held", admissions)
	}

	// A safeguard drawn on a service reaches nothing here: the report store is
	// one store and the subject an owner names for it is the store itself.
	elsewhere, _, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.ReportAdmission,
		safeguard.Subject{Kind: safeguard.SubjectService, ID: in.service.ID},
		safeguard.Bound{}, safeguard.Routing{})
	if err != nil {
		t.Fatalf("placing a safeguard on a service: %v", err)
	}
	if elsewhere.Subject.Kind != safeguard.SubjectService {
		t.Errorf("the safeguard drawn elsewhere reads %+v", elsewhere.Subject)
	}

	// Withdrawing one is how the wait is taken back: what was waiting on it
	// moves on the next pass rather than waiting for a human nobody owes.
	if err := withdraw(t, ctx, in, gatepolicy.ReportAdmission, store); err != nil {
		t.Fatalf("withdrawing the safeguard on arriving reports: %v", err)
	}
	if admissions, err = in.reader.ReportStoreAdmissions(ctx); err != nil {
		t.Fatalf("ReportStoreAdmissions: %v", err)
	}
	if admissions.ArrivingReports || !admissions.ReportDerivedIntents {
		t.Errorf("after the withdrawal the admissions read %+v, want the intents' alone", admissions)
	}
}

// withdraw withdraws every safeguard on one parameter and subject, which is
// what an owner does at Factory to take a wait back. A withdrawal is decided at
// a row and not merely written, so this writes it and then approves it the way
// that row's verdict does.
func withdraw(t *testing.T, ctx context.Context, in installed,
	parameter gatepolicy.Parameter, subject safeguard.Subject) error {
	t.Helper()
	placed, err := safeguard.BySubjects(ctx, in.pool, parameter, []safeguard.Subject{subject})
	if err != nil {
		return err
	}
	for _, one := range placed {
		written, _, err := in.factory.WriteSafeguardWithdrawal(ctx, owner, one.ID)
		if err != nil {
			return err
		}
		if _, err := in.factory.ApproveSafeguardWithdrawal(ctx, owner, written.ID, "dl_a_close_event"); err != nil {
			return err
		}
	}
	return nil
}
