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

	before, err := in.reader.Versions(ctx, owner)
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}

	if _, err := in.factory.AuthorItemSizeTarget(ctx, owner, "ar_nothing", 400); !errors.Is(err, area.ErrNotFound) {
		t.Fatalf("authoring on an area that does not exist = %v, want ErrNotFound", err)
	}
	if _, err := in.factory.AuthorWindowLimit(ctx, owner, in.service.ID, 0); !errors.Is(err, service.ErrNotPositive) {
		t.Fatalf("authoring a window limit of zero = %v, want ErrNotPositive", err)
	}

	after, err := in.reader.Versions(ctx, owner)
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("a refused write left %d versions, want the %d that were there", len(after), len(before))
	}
}

// TestEveryAuthoringWriteMovesTheVersion: an owner re-authors gate policy, and a
// decision read back against today's values is not the decision that was made —
// so what changes the policy changes the version, and the version names the
// write and the whole authored state after it.
func TestEveryAuthoringWriteMovesTheVersion(t *testing.T) {
	ctx, in := newFactory(t)

	installVersion := newestVersion(t, ctx, in)

	authored, err := in.factory.AuthorGateThreshold(ctx, owner, in.prod.ID, "merge_to_master", 0.5)
	if err != nil {
		t.Fatalf("AuthorGateThreshold: %v", err)
	}
	if authored.ID == installVersion.ID {
		t.Error("authoring appended no version")
	}
	if authored.Caller != policy.CallerFactory {
		t.Errorf("the version says %q called for it, want Factory", authored.Caller)
	}
	if authored.Action != policy.ActionAuthored || authored.Parameter != gatepolicy.RiskThreshold {
		t.Errorf("the version says %q %q", authored.Action, authored.Parameter)
	}
	want := policy.Scope{Kind: policy.ScopeEnvironment, ID: in.prod.ID, Key: "merge_to_master"}
	if authored.Scope != want {
		t.Errorf("the version names scope %s, want production's record and the row", authored.Scope)
	}
	if authored.Actor != owner {
		t.Errorf("the version's actor is %+v, want the owner", authored.Actor)
	}
	if !namesAuthored(authored, gatepolicy.RiskThreshold, want, 0.5) {
		t.Errorf("the version's authored state is %+v, and it names no threshold of 0.5", authored.Authored)
	}

	// A second write names the first's value too: a version names every authored
	// parameter and the scope it was authored on, not only the one it moved.
	limit, err := in.factory.AuthorWindowLimit(ctx, owner, in.service.ID, 3)
	if err != nil {
		t.Fatalf("AuthorWindowLimit: %v", err)
	}
	if !namesAuthored(limit, gatepolicy.RiskThreshold, want, 0.5) {
		t.Errorf("the second version dropped the threshold the first authored: %+v", limit.Authored)
	}
	if !namesAuthored(limit, gatepolicy.WindowLimit,
		policy.Scope{Kind: policy.ScopeService, ID: in.service.ID}, 3) {
		t.Errorf("the second version does not name what it authored: %+v", limit.Authored)
	}

	placed, added, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.WindowLimit,
		safeguard.Subject{Kind: safeguard.SubjectService, ID: in.service.ID},
		safeguard.Bound{Number: 2}, safeguard.Routing{})
	if err != nil {
		t.Fatalf("AddSafeguard: %v", err)
	}
	if added.Action != policy.ActionSafeguardAdded || added.SafeguardID != placed.ID {
		t.Errorf("the safeguard's version says %q of safeguard %q", added.Action, added.SafeguardID)
	}
	if len(added.Safeguards) != 1 || added.Safeguards[0] != placed.ID {
		t.Errorf("the version names the safeguards in force as %v", added.Safeguards)
	}

	if inForce := newestVersion(t, ctx, in); inForce.ID != added.ID {
		t.Errorf("the version in force is %s, want the newest write %s", inForce.ID, added.ID)
	}

	read, err := in.reader.Version(ctx, owner, authored.ID)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if read.ID != authored.ID || read.Number != 0.5 {
		t.Errorf("the version reads back as %+v", read)
	}
	if _, _, err := in.factory.WriteSafeguardWithdrawal(ctx, owner, "sfg_nothing"); !errors.Is(err, safeguard.ErrNotFound) {
		t.Errorf("withdrawing a safeguard that does not exist = %v, want ErrNotFound", err)
	}
}

// namesAuthored reports whether a version's authored state holds one value.
func namesAuthored(v policy.Version, parameter gatepolicy.Parameter, scope policy.Scope, number float64) bool {
	for _, held := range v.Authored {
		if held.Parameter == parameter && held.Scope == scope && held.Number == number {
			return true
		}
	}
	return false
}
