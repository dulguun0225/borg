package policy_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/safeguard"
)

// TestShorteningDecisionLogRetentionIsDecidedAndLengtheningIsNot: removing a
// protection is decided and not merely written, and here the protection is the
// evidence a shorter value destroys. A longer value adds protection and is in
// force on write.
//
// The first authored value is a shortening too: where an owner has authored
// none the log is kept for the life of the install, so any finite value is
// shorter than what is in force and comes through the row.
func TestShorteningDecisionLogRetentionIsDecidedAndLengtheningIsNot(t *testing.T) {
	ctx, in := newFactory(t)

	if _, err := in.factory.AuthorDecisionLogRetention(ctx, owner, 90*24*3600); !errors.Is(err, policy.ErrShorteningIsDecided) {
		t.Fatalf("the first authored value = %v, want ErrShorteningIsDecided", err)
	}
	// The value is written pending first, as a record naming who authored it:
	// the row that decides it is routed away from that actor, and a row can
	// only be routed away from an actor some record names.
	first := shortenTo(t, ctx, in, 90*24*3600)
	if written, err := factorysettings.GetShortening(ctx, in.pool, first); err != nil {
		t.Fatalf("GetShortening: %v", err)
	} else if written.Actor.Key != owner.Key || written.Approved {
		t.Errorf("the pending shortening is %+v, want the owner's and unapproved", written)
	}
	if pending, err := factorysettings.Get(ctx, in.pool); err != nil {
		t.Fatalf("Get: %v", err)
	} else if pending.DecisionLogRetentionSeconds.Present {
		t.Error("the shorter value is in force with nothing having decided it")
	}
	if _, err := in.factory.ApproveRetentionShortening(ctx, approver, first, decidedAt); err != nil {
		t.Fatalf("ApproveRetentionShortening of the first value: %v", err)
	}
	settings, err := factorysettings.Get(ctx, in.pool)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !settings.DecisionLogRetentionSeconds.Present || settings.DecisionLogRetentionSeconds.Number != 90*24*3600 {
		t.Errorf("the retention reads %+v, want the authored ninety days", settings.DecisionLogRetentionSeconds)
	}

	// Lengthening it again is in force on write.
	if _, err := in.factory.AuthorDecisionLogRetention(ctx, owner, 180*24*3600); err != nil {
		t.Fatalf("lengthening: %v", err)
	}

	// Shortening it is refused here: it takes a gate row of its own.
	before := newestVersion(t, ctx, in)
	if _, err := in.factory.AuthorDecisionLogRetention(ctx, owner, 30*24*3600); !errors.Is(err, policy.ErrShorteningIsDecided) {
		t.Fatalf("shortening the retention = %v, want ErrShorteningIsDecided", err)
	}
	if after := newestVersion(t, ctx, in); after.ID != before.ID {
		t.Error("the refused shortening appended a version")
	}
	settings, err = factorysettings.Get(ctx, in.pool)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if settings.DecisionLogRetentionSeconds.Number != 180*24*3600 {
		t.Errorf("the refused shortening moved the value in force to %v", settings.DecisionLogRetentionSeconds.Number)
	}

	// The row's approval is what writes it, and the actor is the human at that
	// row rather than whoever authored the shorter value.
	shorter := shortenTo(t, ctx, in, 30*24*3600)
	if _, err := in.factory.ApproveRetentionShortening(ctx, approver, shorter, decidedAt); err != nil {
		t.Fatalf("ApproveRetentionShortening: %v", err)
	}
	settings, err = factorysettings.Get(ctx, in.pool)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if settings.DecisionLogRetentionSeconds.Number != 30*24*3600 {
		t.Errorf("the approved shortening left %v in force", settings.DecisionLogRetentionSeconds.Number)
	}

	// A second approval of the one already decided is refused. What answers
	// first is that its value is the value in force and so no shortening;
	// lengthen, and the record's own refusal is what answers — one row decides
	// one shortening, and a second close on it would be a second decision.
	if _, err := in.factory.ApproveRetentionShortening(ctx, approver, shorter, decidedAt); !errors.Is(err,
		policy.ErrNotAShortening) {
		t.Errorf("approving the value already in force = %v, want ErrNotAShortening", err)
	}
	if _, err := in.factory.AuthorDecisionLogRetention(ctx, owner, 180*24*3600); err != nil {
		t.Fatalf("lengthening again: %v", err)
	}
	if _, err := in.factory.ApproveRetentionShortening(ctx, approver, shorter, decidedAt); !errors.Is(err,
		factorysettings.ErrShorteningAlreadyApproved) {
		t.Errorf("approving one shortening twice = %v, want ErrShorteningAlreadyApproved", err)
	}

	// A value that is not shorter does not come through the row at all: the
	// write that would propose it is refused before any row fires.
	if _, _, err := in.factory.WriteRetentionShortening(ctx, owner, 200*24*3600); !errors.Is(err, policy.ErrNotAShortening) {
		t.Errorf("proposing a lengthening at the row = %v, want ErrNotAShortening", err)
	}

	// And an approval naming no close event is refused, so the value cannot
	// move with nothing having decided it.
	standing := shortenTo(t, ctx, in, 15*24*3600)
	if _, err := in.factory.ApproveRetentionShortening(ctx, approver, standing, ""); !errors.Is(err,
		policy.ErrNotDecidedAtARow) {
		t.Errorf("approving with no close event = %v, want ErrNotDecidedAtARow", err)
	}
}

// shortenTo writes a shorter value pending, as the owner, and answers the
// record's id — which is what the gate row that decides it is fired over.
func shortenTo(t *testing.T, ctx context.Context, in installed, seconds int64) string {
	t.Helper()
	written, _, err := in.factory.WriteRetentionShortening(ctx, owner, seconds)
	if err != nil {
		t.Fatalf("WriteRetentionShortening to %d: %v", seconds, err)
	}
	return written.ID
}

// TestNeitherAnAuthoredValueNorTheRowGoesUnderTheRetentionFloor: the floor
// bounds how low decision-log retention may ever be taken, and it is written by
// the gate row that decides a shortening or by a records-retention constraint.
func TestNeitherAnAuthoredValueNorTheRowGoesUnderTheRetentionFloor(t *testing.T) {
	ctx, in := newFactory(t)

	if _, err := in.factory.SetRetentionFloor(ctx, owner, 60*24*3600); err != nil {
		t.Fatalf("SetRetentionFloor: %v", err)
	}
	// Every value that shortens the retention comes through the row, the first
	// one included, so the floor is what that row is refused by.
	if _, err := in.factory.ApproveRetentionShortening(ctx, approver,
		shortenTo(t, ctx, in, 30*24*3600), decidedAt); !errors.Is(err, factorysettings.ErrUnderTheRetentionFloor) {
		t.Errorf("approving a value under the floor = %v, want ErrUnderTheRetentionFloor", err)
	}
	if _, err := in.factory.ApproveRetentionShortening(ctx, approver,
		shortenTo(t, ctx, in, 90*24*3600), decidedAt); err != nil {
		t.Fatalf("ApproveRetentionShortening above the floor: %v", err)
	}
	// What an owner may still write ungated is a longer value, and a value
	// longer than one already above the floor cannot be under it — so an
	// authored value under the floor is unreachable rather than refused, and the
	// store's own constraint is what keeps it so.
	if _, err := in.factory.AuthorDecisionLogRetention(ctx, owner, 45*24*3600); !errors.Is(err, policy.ErrShorteningIsDecided) {
		t.Errorf("authoring a shorter value = %v, want ErrShorteningIsDecided", err)
	}
	settings, err := factorysettings.Get(ctx, in.pool)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if settings.DecisionLogRetentionSeconds.Number != 90*24*3600 {
		t.Errorf("the retention in force is %v, want the ninety days the row approved",
			settings.DecisionLogRetentionSeconds.Number)
	}
}

// TestTheRemediationPeriodIsReadAtTheSeverityItWasAuthoredFor: the period is one
// value per advisory severity, so the read names the severity it is asked for; a
// read naming none finds nothing authored, and a safeguard on it is keyed by the
// same severity and narrows the value in force.
func TestTheRemediationPeriodIsReadAtTheSeverityItWasAuthoredFor(t *testing.T) {
	ctx, in := newFactory(t)

	if _, err := in.factory.AuthorRemediationPeriod(ctx, owner, 7, 72*3600); err != nil {
		t.Fatalf("AuthorRemediationPeriod: %v", err)
	}

	unnamed := in.subjects("deploy_to_production")
	effective, err := in.reader.InForce(ctx, gatepolicy.RemediationPeriod, unnamed)
	if err != nil {
		t.Fatalf("InForce naming no severity: %v", err)
	}
	if effective.Source != policy.FromNothing {
		t.Errorf("a read naming no severity is %+v, want nothing authored and nothing supplied", effective)
	}

	named := unnamed
	named.Severity, named.SeverityNamed = 7, true
	effective, err = in.reader.InForce(ctx, gatepolicy.RemediationPeriod, named)
	if err != nil {
		t.Fatalf("InForce at the severity it was authored for: %v", err)
	}
	if effective.Source != policy.FromAuthored || effective.Number != 72*3600 {
		t.Errorf("the remediation period in force is %+v, want the authored seventy-two hours", effective)
	}

	// A value authored for one severity is not a value for another.
	other := unnamed
	other.Severity, other.SeverityNamed = 3, true
	effective, err = in.reader.InForce(ctx, gatepolicy.RemediationPeriod, other)
	if err != nil {
		t.Fatalf("InForce at another severity: %v", err)
	}
	if effective.Source != policy.FromNothing {
		t.Errorf("severity 3 reads %+v, want nothing: the period was authored for severity 7", effective)
	}

	// A safeguard narrows it further, and names the severity the way the
	// authored value does.
	placed, _, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.RemediationPeriod,
		safeguard.Subject{Kind: safeguard.SubjectService, ID: in.service.ID, Key: "7"},
		safeguard.Bound{Number: 24 * 3600}, safeguard.Routing{})
	if err != nil {
		t.Fatalf("AddSafeguard on the remediation period: %v", err)
	}
	effective, err = in.reader.InForce(ctx, gatepolicy.RemediationPeriod, named)
	if err != nil {
		t.Fatalf("InForce with the safeguard standing: %v", err)
	}
	if effective.Number != 24*3600 || !effective.Clamped {
		t.Errorf("the period reads %v clamped %v, want the safeguard's ceiling of a day",
			effective.Number, effective.Clamped)
	}
	if !slices.Contains(effective.Safeguards, placed.ID) {
		t.Errorf("the period names safeguards %v, want the one placed", effective.Safeguards)
	}
}
