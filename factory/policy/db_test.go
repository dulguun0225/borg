package policy_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/safeguard"
	"github.com/dulguun0225/borg/factory/service"
)

// TestAFailedWriteAppendsNoVersion: the write and the version are one
// transaction, so a value that moved without the version moving is not a state
// the store can be left in — and neither is a version naming a write that did not
// happen.
func TestAFailedWriteAppendsNoVersion(t *testing.T) {
	ctx, in := newFactory(t)

	before, err := policy.All(ctx, in.pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}

	if _, err := in.factory.AuthorItemSizeTarget(ctx, owner, "ar_nothing", 400); !errors.Is(err, area.ErrNotFound) {
		t.Fatalf("authoring on an area that does not exist = %v, want ErrNotFound", err)
	}
	if _, err := in.factory.AuthorWindowLimit(ctx, owner, in.service.ID, 0); !errors.Is(err, service.ErrNotPositive) {
		t.Fatalf("authoring a window limit of zero = %v, want ErrNotPositive", err)
	}

	after, err := policy.All(ctx, in.pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("a refused write left %d versions, want the %d that were there", len(after), len(before))
	}
}

// TestEveryAuthoringWriteMovesTheVersion: an owner re-authors gate policy, and a
// decision read back against today's values is not the decision that was made —
// so what changes the policy changes the version, and the version names the write.
func TestEveryAuthoringWriteMovesTheVersion(t *testing.T) {
	ctx, in := newFactory(t)

	installVersion, err := policy.InForce(ctx, in.pool)
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}

	authored, err := in.factory.AuthorGateThreshold(ctx, owner, in.prod.ID, "merge_to_master", 0.5)
	if err != nil {
		t.Fatalf("AuthorGateThreshold: %v", err)
	}
	if authored.Supersedes != installVersion.ID {
		t.Errorf("the authoring version supersedes %q, want the install's %q", authored.Supersedes, installVersion.ID)
	}
	if authored.Action != policy.ActionAuthored || authored.Parameter != gatepolicy.RiskThreshold {
		t.Errorf("the version says %q %q", authored.Action, authored.Parameter)
	}
	if authored.Subject.Kind != "environment" || authored.Subject.ID != in.prod.ID ||
		authored.Subject.Qualifier != "merge_to_master" {
		t.Errorf("the version names subject %s, want production's record and the row", authored.Subject)
	}
	if authored.Actor != owner {
		t.Errorf("the version's actor is %+v, want the owner", authored.Actor)
	}

	placed, added, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.WindowLimit,
		safeguard.Subject{Kind: safeguard.SubjectService, ID: in.service.ID}, safeguard.Bound{Number: 2})
	if err != nil {
		t.Fatalf("AddSafeguard: %v", err)
	}
	if added.Action != policy.ActionSafeguardAdded || added.SafeguardID != placed.ID {
		t.Errorf("the safeguard's version says %q of safeguard %q", added.Action, added.SafeguardID)
	}

	withdrawn, err := in.factory.WithdrawSafeguard(ctx, owner, placed.ID)
	if err != nil {
		t.Fatalf("WithdrawSafeguard: %v", err)
	}
	if withdrawn.Action != policy.ActionWithdrawn || withdrawn.SafeguardID != placed.ID {
		t.Errorf("the withdrawal's version says %q of safeguard %q", withdrawn.Action, withdrawn.SafeguardID)
	}

	inForce, err := policy.InForce(ctx, in.pool)
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	if inForce.ID != withdrawn.ID {
		t.Errorf("the version in force is %s, want the newest write %s", inForce.ID, withdrawn.ID)
	}

	read, err := policy.Get(ctx, in.pool, authored.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read != authored {
		t.Errorf("the version reads back as %+v", read)
	}
	if _, err := in.factory.WithdrawSafeguard(ctx, owner, "sfg_nothing"); !errors.Is(err, safeguard.ErrNotFound) {
		t.Errorf("withdrawing a safeguard that does not exist = %v, want ErrNotFound", err)
	}
}
