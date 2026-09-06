package policy_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/halt"
	"github.com/dulguun0225/borg/factory/legalhold"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/safeguard"
)

// TestAHaltAndItsWithdrawalEachAppendAVersion: setting a halt and withdrawing
// it each append one, so the interval the factory stood halted is a fact of the
// trail with an actor at each end rather than something a later reader
// reconstructs from what stopped arriving. It is never edited: the withdrawal
// is a second record, and it is not in force until the row that decides it
// approves it.
func TestAHaltAndItsWithdrawalEachAppendAVersion(t *testing.T) {
	ctx, in := newFactory(t)

	set, version, err := in.factory.SetHalt(ctx, owner, "the factory is writing changes nobody asked for")
	if err != nil {
		t.Fatalf("SetHalt: %v", err)
	}
	if version.Action != policy.ActionHaltSet || version.HaltID != set.ID {
		t.Errorf("the halt's version says %q of halt %q", version.Action, version.HaltID)
	}
	if !slices.Contains(version.Halts, set.ID) {
		t.Errorf("the version names the halts in force as %v", version.Halts)
	}

	written, pending, err := in.factory.WriteHaltWithdrawal(ctx, owner, set.ID)
	if err != nil {
		t.Fatalf("WriteHaltWithdrawal: %v", err)
	}
	if pending.Action != policy.ActionWithdrawalWritten {
		t.Errorf("the pending withdrawal's version says %q", pending.Action)
	}
	if !slices.Contains(pending.Halts, set.ID) {
		t.Error("a pending withdrawal took the halt out of force before the row that decides it approved it")
	}
	standing, err := halt.Standing(ctx, in.pool)
	if err != nil {
		t.Fatalf("Standing: %v", err)
	}
	if len(standing) != 1 {
		t.Errorf("%d halts stand while the withdrawal is pending, want the one that was set", len(standing))
	}

	approved, err := in.factory.ApproveHaltWithdrawal(ctx, approver, written.ID, decidedAt)
	if err != nil {
		t.Fatalf("ApproveHaltWithdrawal: %v", err)
	}
	if approved.Action != policy.ActionWithdrawalApproved {
		t.Errorf("the approval's version says %q", approved.Action)
	}
	if slices.Contains(approved.Halts, set.ID) {
		t.Errorf("the approved withdrawal leaves the halt in force: %v", approved.Halts)
	}
	standing, err = halt.Standing(ctx, in.pool)
	if err != nil {
		t.Fatalf("Standing: %v", err)
	}
	if len(standing) != 0 {
		t.Errorf("%d halts stand after the withdrawal was approved", len(standing))
	}
}

// TestALegalHoldTakesTheSameThreeWrites: a hold is set, its withdrawal is
// written, and it ends only where a gate row routed away from the human who
// wrote it approves that withdrawal.
func TestALegalHoldTakesTheSameThreeWrites(t *testing.T) {
	ctx, in := newFactory(t)

	subject := legalhold.Subject{Kind: legalhold.SubjectService, ID: in.service.ID}
	set, version, err := in.factory.SetLegalHold(ctx, owner, subject, "counsel asked for it")
	if err != nil {
		t.Fatalf("SetLegalHold: %v", err)
	}
	if version.Action != policy.ActionLegalHoldSet || !slices.Contains(version.LegalHolds, set.ID) {
		t.Errorf("the hold's version says %q and names %v", version.Action, version.LegalHolds)
	}

	written, pending, err := in.factory.WriteLegalHoldWithdrawal(ctx, owner, set.ID)
	if err != nil {
		t.Fatalf("WriteLegalHoldWithdrawal: %v", err)
	}
	if !slices.Contains(pending.LegalHolds, set.ID) {
		t.Error("a pending withdrawal took the hold out of force before the row that decides it approved it")
	}
	reaching, err := legalhold.Reaching(ctx, in.pool, subject)
	if err != nil {
		t.Fatalf("Reaching: %v", err)
	}
	if !reaching {
		t.Error("the hold stopped reaching its subject while its withdrawal was still pending")
	}

	approved, err := in.factory.ApproveLegalHoldWithdrawal(ctx, approver, written.ID, decidedAt)
	if err != nil {
		t.Fatalf("ApproveLegalHoldWithdrawal: %v", err)
	}
	if slices.Contains(approved.LegalHolds, set.ID) {
		t.Errorf("the approved withdrawal leaves the hold in force: %v", approved.LegalHolds)
	}
	reaching, err = legalhold.Reaching(ctx, in.pool, subject)
	if err != nil {
		t.Fatalf("Reaching: %v", err)
	}
	if reaching {
		t.Error("the hold still reaches its subject after the withdrawal was approved")
	}
}

// TestNoneOfTheFourApprovalsIsWrittenWithoutTheCloseThatDecidedIt: each of them
// removes a protection, and the design gives each a gate row — so a call naming
// no close event is refused, and the record does not move. The version the
// approval appends names that close, which is how an auditor reads which
// decision put it in force.
func TestNoneOfTheFourApprovalsIsWrittenWithoutTheCloseThatDecidedIt(t *testing.T) {
	ctx, in := newFactory(t)

	placed, _, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.RiskThreshold,
		safeguard.Subject{Kind: safeguard.SubjectService, ID: in.service.ID, Key: "deploy_to_production"},
		safeguard.Bound{}, safeguard.Routing{})
	if err != nil {
		t.Fatalf("AddSafeguard: %v", err)
	}
	safeguardWithdrawal, _, err := in.factory.WriteSafeguardWithdrawal(ctx, owner, placed.ID)
	if err != nil {
		t.Fatalf("WriteSafeguardWithdrawal: %v", err)
	}
	set, _, err := in.factory.SetHalt(ctx, owner, "the factory is writing changes nobody asked for")
	if err != nil {
		t.Fatalf("SetHalt: %v", err)
	}
	haltWithdrawal, _, err := in.factory.WriteHaltWithdrawal(ctx, owner, set.ID)
	if err != nil {
		t.Fatalf("WriteHaltWithdrawal: %v", err)
	}
	hold, _, err := in.factory.SetLegalHold(ctx, owner,
		legalhold.Subject{Kind: legalhold.SubjectService, ID: in.service.ID}, "counsel asked for it")
	if err != nil {
		t.Fatalf("SetLegalHold: %v", err)
	}
	holdWithdrawal, _, err := in.factory.WriteLegalHoldWithdrawal(ctx, owner, hold.ID)
	if err != nil {
		t.Fatalf("WriteLegalHoldWithdrawal: %v", err)
	}

	for name, refused := range map[string]error{
		"a safeguard's withdrawal": firstError(
			in.factory.ApproveSafeguardWithdrawal(ctx, approver, safeguardWithdrawal.ID, "")),
		"a halt's withdrawal": firstError(
			in.factory.ApproveHaltWithdrawal(ctx, approver, haltWithdrawal.ID, "")),
		"a legal hold's withdrawal": firstError(
			in.factory.ApproveLegalHoldWithdrawal(ctx, approver, holdWithdrawal.ID, "")),
		"a shortening of decision-log retention": firstError(
			in.factory.ApproveRetentionShortening(ctx, approver, shortenTo(t, ctx, in, 30*24*3600), "")),
	} {
		if !errors.Is(refused, policy.ErrNotDecidedAtARow) {
			t.Errorf("approving %s with no close event = %v, want ErrNotDecidedAtARow", name, refused)
		}
	}

	// Nothing moved: the halt still stands, which is what says the refusal was
	// before the write and not after it.
	standing, err := halt.Standing(ctx, in.pool)
	if err != nil {
		t.Fatalf("Standing: %v", err)
	}
	if len(standing) != 1 {
		t.Errorf("%d halts stand after the refused approval, want the one that was set", len(standing))
	}

	// Named, the close event goes on the version the approval appends.
	approved, err := in.factory.ApproveHaltWithdrawal(ctx, approver, haltWithdrawal.ID, decidedAt)
	if err != nil {
		t.Fatalf("ApproveHaltWithdrawal: %v", err)
	}
	if approved.Decision != decidedAt {
		t.Errorf("the approval's version names decision %q, want the close event that decided it", approved.Decision)
	}
}

// firstError drops the version an approval returns and keeps the error, so the
// four refusals above read as one table.
func firstError(_ policy.Version, err error) error { return err }
