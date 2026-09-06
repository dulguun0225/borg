package policy_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/policy"
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
	if _, err := in.factory.ApproveRetentionShortening(ctx, approver, 90*24*3600); err != nil {
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
	if _, err := in.factory.ApproveRetentionShortening(ctx, approver, 30*24*3600); err != nil {
		t.Fatalf("ApproveRetentionShortening: %v", err)
	}
	settings, err = factorysettings.Get(ctx, in.pool)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if settings.DecisionLogRetentionSeconds.Number != 30*24*3600 {
		t.Errorf("the approved shortening left %v in force", settings.DecisionLogRetentionSeconds.Number)
	}

	// A value that is not shorter does not come through the row.
	if _, err := in.factory.ApproveRetentionShortening(ctx, approver, 60*24*3600); !errors.Is(err, policy.ErrNotAShortening) {
		t.Errorf("approving a lengthening at the row = %v, want ErrNotAShortening", err)
	}
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
	if _, err := in.factory.ApproveRetentionShortening(ctx, approver, 30*24*3600); !errors.Is(err, factorysettings.ErrUnderTheRetentionFloor) {
		t.Errorf("approving a value under the floor = %v, want ErrUnderTheRetentionFloor", err)
	}
	if _, err := in.factory.ApproveRetentionShortening(ctx, approver, 90*24*3600); err != nil {
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
