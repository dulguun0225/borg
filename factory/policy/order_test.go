package policy_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/halt"
	"github.com/dulguun0225/borg/factory/legalhold"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/safeguard"
)

// TestAWriteThatMintsARecordAppendsTheVersionFirst is
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md's
// "the log appends the version first and Factory writes the scope record's
// field second, the trail's copy before the value in force": the order holds
// for a write that creates a record too, which needs the id minted before the
// version rather than by the record's own writer.
func TestAWriteThatMintsARecordAppendsTheVersionFirst(t *testing.T) {
	ctx, in := newFactory(t)

	placed, version, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.WindowLimit,
		safeguard.Subject{Kind: safeguard.SubjectService, ID: in.service.ID},
		safeguard.Bound{Number: 3}, safeguard.Routing{})
	if err != nil {
		t.Fatalf("AddSafeguard: %v", err)
	}
	if version.SafeguardID != placed.ID {
		t.Errorf("the version names safeguard %q and the record is %q", version.SafeguardID, placed.ID)
	}
	if placed.At <= version.At {
		t.Errorf("the safeguard was written at %s and the version at %s, and the version goes first",
			placed.At, version.At)
	}

	held, _, err := in.factory.SetLegalHold(ctx, owner,
		legalhold.Subject{Kind: legalhold.SubjectFactory}, "an audit")
	if err != nil {
		t.Fatalf("SetLegalHold: %v", err)
	}
	holdVersion := newestVersion(t, ctx, in)
	if held.At <= holdVersion.At {
		t.Errorf("the legal hold was written at %s and the version at %s, and the version goes first",
			held.At, holdVersion.At)
	}

	declared, areaVersion, err := in.factory.DeclareArea(ctx, owner, "billing",
		area.InsideProject(in.project.ID), area.Hazard{})
	if err != nil {
		t.Fatalf("DeclareArea: %v", err)
	}
	if areaVersion.Scope.ID != declared.ID {
		t.Errorf("the version names area %q and the record is %q", areaVersion.Scope.ID, declared.ID)
	}
	if declared.At <= areaVersion.At {
		t.Errorf("the area was written at %s and the version at %s, and the version goes first",
			declared.At, areaVersion.At)
	}
}

// TestAStepTakenAgainThatMintsARecordWritesNothing is the same section's "the
// version is keyed on the write and the field on its scope and parameter, so a
// step taken again writes nothing". The key check was skipped for every write
// that minted a record, so an owner's step repeated placed a second safeguard,
// a second halt and a second area.
func TestAStepTakenAgainThatMintsARecordWritesNothing(t *testing.T) {
	ctx, in := newFactory(t)

	subject := safeguard.Subject{Kind: safeguard.SubjectService, ID: in.service.ID}
	first, firstVersion, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.WindowLimit,
		subject, safeguard.Bound{Number: 3}, safeguard.Routing{})
	if err != nil {
		t.Fatalf("AddSafeguard: %v", err)
	}
	again, againVersion, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.WindowLimit,
		subject, safeguard.Bound{Number: 3}, safeguard.Routing{})
	if err != nil {
		t.Fatalf("AddSafeguard again: %v", err)
	}
	if again.ID != first.ID || againVersion.ID != firstVersion.ID {
		t.Errorf("the step taken again placed safeguard %s under version %s, and the first were %s and %s",
			again.ID, againVersion.ID, first.ID, firstVersion.ID)
	}
	standing, err := safeguard.All(ctx, in.pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(standing) != 1 {
		t.Errorf("%d safeguards stand after one step taken twice, want 1", len(standing))
	}

	// A safeguard differing in what it bounds is a write of its own and not a
	// step taken twice: the bound is part of the key.
	other, otherVersion, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.WindowLimit,
		subject, safeguard.Bound{Number: 2}, safeguard.Routing{})
	if err != nil {
		t.Fatalf("AddSafeguard with another bound: %v", err)
	}
	if other.ID == first.ID || otherVersion.ID == firstVersion.ID {
		t.Error("a safeguard with another bound was taken for the same write")
	}

	set, setVersion, err := in.factory.SetHalt(ctx, owner, "a bad release")
	if err != nil {
		t.Fatalf("SetHalt: %v", err)
	}
	setAgain, setAgainVersion, err := in.factory.SetHalt(ctx, owner, "a bad release")
	if err != nil {
		t.Fatalf("SetHalt again: %v", err)
	}
	if setAgain.ID != set.ID || setAgainVersion.ID != setVersion.ID {
		t.Errorf("the halt set again is %s under version %s, and the first were %s and %s",
			setAgain.ID, setAgainVersion.ID, set.ID, setVersion.ID)
	}
	halts, err := halt.Standing(ctx, in.pool)
	if err != nil {
		t.Fatalf("Standing: %v", err)
	}
	if len(halts) != 1 {
		t.Errorf("%d halts stand after one step taken twice, want 1", len(halts))
	}
}

// TestTheNewestSafeguardForASubjectIsTheOneInForce is the same section's "the
// value in force is a read of the newest record for that subject". A safeguard
// is never edited, so a subject carries several records over its life and
// applying all of them would apply a bound an owner replaced.
func TestTheNewestSafeguardForASubjectIsTheOneInForce(t *testing.T) {
	ctx, in := newFactory(t)

	if _, err := in.factory.AuthorWindowLimit(ctx, owner, in.service.ID, 8); err != nil {
		t.Fatalf("AuthorWindowLimit: %v", err)
	}
	subject := safeguard.Subject{Kind: safeguard.SubjectService, ID: in.service.ID}
	for _, bound := range []float64{2, 5} {
		if _, _, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.WindowLimit,
			subject, safeguard.Bound{Number: bound}, safeguard.Routing{}); err != nil {
			t.Fatalf("AddSafeguard at %v: %v", bound, err)
		}
	}

	limit, err := in.reader.InForce(ctx, gatepolicy.WindowLimit, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	if limit.Number != 5 {
		t.Errorf("the window limit in force is %v, want the newest safeguard's 5", limit.Number)
	}
	if len(limit.Safeguards) != 1 {
		t.Errorf("%d safeguards reached the read, want the newest for the subject", len(limit.Safeguards))
	}
}

// TestASafeguardOnTheStrategyMakesTheOneWithAControlTheStrategyInForce is the
// same section's "a rollout strategy that keeps a control" adding rather than
// clamping: such a safeguard bounds no value, so unioning it into the list left
// the value in force naming two strategies and the one an owner authored still
// among them.
func TestASafeguardOnTheStrategyMakesTheOneWithAControlTheStrategyInForce(t *testing.T) {
	ctx, in := newFactory(t)

	if _, err := in.factory.AuthorStrategyDefault(ctx, owner, in.prod.ID,
		gatepolicy.StrategyWithoutControl); err != nil {
		t.Fatalf("AuthorStrategyDefault: %v", err)
	}
	strategy, err := in.reader.InForce(ctx, gatepolicy.StrategyDefault, in.subjects("deploy_to_production"))
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	if len(strategy.List) != 1 || strategy.List[0] != string(gatepolicy.StrategyWithoutControl) {
		t.Fatalf("the strategy in force is %v, want the authored one alone", strategy.List)
	}

	if _, _, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.StrategyDefault,
		safeguard.Subject{Kind: safeguard.SubjectService, ID: in.service.ID},
		safeguard.Bound{}, safeguard.Routing{}); err != nil {
		t.Fatalf("AddSafeguard: %v", err)
	}
	strategy, err = in.reader.InForce(ctx, gatepolicy.StrategyDefault, in.subjects("deploy_to_production"))
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	if len(strategy.List) != 1 || strategy.List[0] != string(gatepolicy.StrategyWithControl) {
		t.Errorf("the strategy in force is %v, want the one with a control alone", strategy.List)
	}
	if !strategy.Clamped {
		t.Error("the safeguard reads as having moved nothing, and it moved the strategy")
	}
}

// TestNoValueInForceGoesUnderTheRetentionFloor is the same section's "a
// retention floor is a field of it too, bounding how low an authored value or a
// safeguard may ever take decision-log retention: neither may go under it".
// Where an owner authors none the log is kept for the life of the install,
// which is above every floor; the resolution read that as the number nothing,
// which is under every floor and reads as a log kept for no time at all.
func TestNoValueInForceGoesUnderTheRetentionFloor(t *testing.T) {
	ctx, in := newFactory(t)

	if _, err := in.factory.SetRetentionFloor(ctx, owner, 90*24*3600); err != nil {
		t.Fatalf("SetRetentionFloor: %v", err)
	}
	retention, err := in.reader.InForce(ctx, gatepolicy.DecisionLogRetention, policy.Subjects{})
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	if !retention.Unbounded || retention.Number != 0 {
		t.Errorf("an unauthored decision-log retention reads %v (unbounded %v), want the life of the install",
			retention.Number, retention.Unbounded)
	}
	if retention.Source != policy.FromFactory {
		t.Errorf("an unauthored decision-log retention reads from %s, want the factory's own",
			retention.Source)
	}

	// A safeguard on it may lengthen and never shorten, so one bounding it
	// under the floor moves nothing: the log kept for the life of the install
	// is already above every floor.
	if _, _, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.DecisionLogRetention,
		safeguard.Subject{Kind: safeguard.SubjectService, ID: in.service.ID},
		safeguard.Bound{Number: 7 * 24 * 3600}, safeguard.Routing{}); err != nil {
		t.Fatalf("AddSafeguard: %v", err)
	}
	retention, err = in.reader.InForce(ctx, gatepolicy.DecisionLogRetention, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	if !retention.Unbounded {
		t.Errorf("a safeguard took the retention to %v, and it may lengthen and never shorten",
			retention.Number)
	}

	// An authored value takes the floor with it: the store refuses one under
	// the floor at the write, and the read holds the same bound.
	written, _, err := in.factory.WriteRetentionShortening(ctx, owner, 120*24*3600)
	if err != nil {
		t.Fatalf("WriteRetentionShortening: %v", err)
	}
	if _, err := in.factory.ApproveRetentionShortening(ctx, approver, written.ID, decidedAt); err != nil {
		t.Fatalf("ApproveRetentionShortening: %v", err)
	}
	retention, err = in.reader.InForce(ctx, gatepolicy.DecisionLogRetention, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	if retention.Unbounded || retention.Number < 90*24*3600 {
		t.Errorf("the retention in force is %v (unbounded %v), and the floor under it is 90 days",
			retention.Number, retention.Unbounded)
	}
}
