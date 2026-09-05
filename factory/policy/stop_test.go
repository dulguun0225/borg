package policy_test

import (
	"slices"
	"testing"

	"github.com/dulguun0225/borg/factory/halt"
	"github.com/dulguun0225/borg/factory/legalhold"
	"github.com/dulguun0225/borg/factory/policy"
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

	approved, err := in.factory.ApproveHaltWithdrawal(ctx, approver, written.ID)
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

	approved, err := in.factory.ApproveLegalHoldWithdrawal(ctx, approver, written.ID)
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
