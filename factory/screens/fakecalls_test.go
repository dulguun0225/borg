// fakeCalls is every write screens.Calls may make: a func field per method,
// so a test supplies only the ones it needs and reads what it was handed
// off the closure it wrote. A method left nil answers success with a zero
// result, which is what a test that does not care about that write gets.
package screens_test

import (
	"context"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/screens"
)

type fakeCalls struct {
	// Work
	supplyIntent           func(context.Context, principal.Principal, screens.SupplyIntentArgs) (string, error)
	answerQuestion         func(context.Context, principal.Principal, screens.AnswerQuestionArgs) error
	confirmReading         func(context.Context, principal.Principal, screens.ConfirmReadingArgs) error
	acceptDelivery         func(context.Context, principal.Principal, screens.AcceptDeliveryArgs) error
	endIntent              func(context.Context, principal.Principal, screens.EndIntentArgs) error
	admitIntent            func(context.Context, principal.Principal, screens.AdmitIntentArgs) error
	admitReport            func(context.Context, principal.Principal, screens.AdmitReportArgs) error
	setPriority            func(context.Context, principal.Principal, screens.SetPriorityArgs) error
	endItem                func(context.Context, principal.Principal, screens.EndItemArgs) error
	decide                 func(context.Context, principal.Principal, screens.DecideArgs) error
	approveThroughHold     func(context.Context, principal.Principal, screens.ApproveThroughHoldArgs) error
	refer                  func(context.Context, principal.Principal, screens.ReferArgs) error
	editInPlace            func(context.Context, principal.Principal, screens.EditInPlaceArgs) error
	acknowledge            func(context.Context, principal.Principal, screens.AcknowledgeArgs) error
	takeOver               func(context.Context, principal.Principal, screens.TakeOverArgs) error
	supplyIntentConstraint func(context.Context, principal.Principal, screens.SupplyIntentConstraintArgs) (string, error)
	withdrawConstraint     func(context.Context, principal.Principal, screens.WithdrawConstraintArgs) error
	clearCeiling           func(context.Context, principal.Principal, screens.ClearCeilingArgs) error

	// Ops
	rollBack              func(context.Context, principal.Principal, screens.RollBackArgs) error
	raiseRevert           func(context.Context, principal.Principal, screens.RaiseRevertArgs) error
	startMitigation       func(context.Context, principal.Principal, screens.StartMitigationArgs) (string, error)
	endMitigation         func(context.Context, principal.Principal, screens.EndMitigationArgs) error
	markRollbackNotCaused func(context.Context, principal.Principal, screens.MarkRollbackNotCausedArgs) error
	firePage              func(context.Context, principal.Principal, screens.FirePageArgs) error

	// Factory
	createProject      func(context.Context, principal.Principal, screens.CreateProjectArgs) (string, error)
	declareArea        func(context.Context, principal.Principal, screens.DeclareAreaArgs) (string, error)
	authorParameter    func(context.Context, principal.Principal, screens.AuthorParameterArgs) error
	placeSafeguard     func(context.Context, principal.Principal, screens.PlaceSafeguardArgs) (string, error)
	withdrawSafeguard  func(context.Context, principal.Principal, screens.WithdrawSafeguardArgs) error
	setHalt            func(context.Context, principal.Principal, screens.SetHaltArgs) (string, error)
	withdrawHalt       func(context.Context, principal.Principal, screens.WithdrawHaltArgs) error
	setLegalHold       func(context.Context, principal.Principal, screens.SetLegalHoldArgs) (string, error)
	withdrawLegalHold  func(context.Context, principal.Principal, screens.WithdrawLegalHoldArgs) error
	writeFleetEntry    func(context.Context, principal.Principal, screens.WriteFleetEntryArgs) (string, error)
	withdrawFleetEntry func(context.Context, principal.Principal, screens.WithdrawFleetEntryArgs) error
	supplyConstraint   func(context.Context, principal.Principal, screens.SupplyConstraintArgs) (string, error)
	setSeam5Enforced   func(context.Context, principal.Principal, screens.SetSeam5EnforcedArgs) error
	retireService      func(context.Context, principal.Principal, screens.RetireServiceArgs) error
	endProject         func(context.Context, principal.Principal, screens.EndProjectArgs) error
	decideRecordRow    func(context.Context, principal.Principal, screens.DecideRecordRowArgs) error
	editRecordRow      func(context.Context, principal.Principal, screens.EditRecordRowArgs) error

	// People
	declareDuty        func(context.Context, principal.Principal, screens.DeclareDutyArgs) error
	withdrawDuty       func(context.Context, principal.Principal, screens.WithdrawDutyArgs) error
	declareObligation  func(context.Context, principal.Principal, screens.DeclareObligationArgs) error
	withdrawObligation func(context.Context, principal.Principal, screens.WithdrawObligationArgs) error
	lendCredential     func(context.Context, principal.Principal, screens.LendCredentialArgs) error
	takeBackCredential func(context.Context, principal.Principal, screens.TakeBackCredentialArgs) error
	authorCeiling      func(context.Context, principal.Principal, screens.AuthorCeilingArgs) error
	authorRate         func(context.Context, principal.Principal, screens.AuthorRateArgs) error
	writeMapping       func(context.Context, principal.Principal, screens.WriteMappingArgs) error
	deleteMapping      func(context.Context, principal.Principal, screens.DeleteMappingArgs) error
}

func (f *fakeCalls) SupplyIntent(ctx context.Context, p principal.Principal, a screens.SupplyIntentArgs) (string, error) {
	if f.supplyIntent == nil {
		return "", nil
	}
	return f.supplyIntent(ctx, p, a)
}

func (f *fakeCalls) AnswerQuestion(ctx context.Context, p principal.Principal, a screens.AnswerQuestionArgs) error {
	if f.answerQuestion == nil {
		return nil
	}
	return f.answerQuestion(ctx, p, a)
}

func (f *fakeCalls) ConfirmReading(ctx context.Context, p principal.Principal, a screens.ConfirmReadingArgs) error {
	if f.confirmReading == nil {
		return nil
	}
	return f.confirmReading(ctx, p, a)
}

func (f *fakeCalls) AcceptDelivery(ctx context.Context, p principal.Principal, a screens.AcceptDeliveryArgs) error {
	if f.acceptDelivery == nil {
		return nil
	}
	return f.acceptDelivery(ctx, p, a)
}

func (f *fakeCalls) EndIntent(ctx context.Context, p principal.Principal, a screens.EndIntentArgs) error {
	if f.endIntent == nil {
		return nil
	}
	return f.endIntent(ctx, p, a)
}

func (f *fakeCalls) AdmitIntent(ctx context.Context, p principal.Principal, a screens.AdmitIntentArgs) error {
	if f.admitIntent == nil {
		return nil
	}
	return f.admitIntent(ctx, p, a)
}

func (f *fakeCalls) AdmitReport(ctx context.Context, p principal.Principal, a screens.AdmitReportArgs) error {
	if f.admitReport == nil {
		return nil
	}
	return f.admitReport(ctx, p, a)
}

func (f *fakeCalls) SetPriority(ctx context.Context, p principal.Principal, a screens.SetPriorityArgs) error {
	if f.setPriority == nil {
		return nil
	}
	return f.setPriority(ctx, p, a)
}

func (f *fakeCalls) EndItem(ctx context.Context, p principal.Principal, a screens.EndItemArgs) error {
	if f.endItem == nil {
		return nil
	}
	return f.endItem(ctx, p, a)
}

func (f *fakeCalls) Decide(ctx context.Context, p principal.Principal, a screens.DecideArgs) error {
	if f.decide == nil {
		return nil
	}
	return f.decide(ctx, p, a)
}

func (f *fakeCalls) ApproveThroughHold(ctx context.Context, p principal.Principal, a screens.ApproveThroughHoldArgs) error {
	if f.approveThroughHold == nil {
		return nil
	}
	return f.approveThroughHold(ctx, p, a)
}

func (f *fakeCalls) Refer(ctx context.Context, p principal.Principal, a screens.ReferArgs) error {
	if f.refer == nil {
		return nil
	}
	return f.refer(ctx, p, a)
}

func (f *fakeCalls) EditInPlace(ctx context.Context, p principal.Principal, a screens.EditInPlaceArgs) error {
	if f.editInPlace == nil {
		return nil
	}
	return f.editInPlace(ctx, p, a)
}

func (f *fakeCalls) Acknowledge(ctx context.Context, p principal.Principal, a screens.AcknowledgeArgs) error {
	if f.acknowledge == nil {
		return nil
	}
	return f.acknowledge(ctx, p, a)
}

func (f *fakeCalls) TakeOver(ctx context.Context, p principal.Principal, a screens.TakeOverArgs) error {
	if f.takeOver == nil {
		return nil
	}
	return f.takeOver(ctx, p, a)
}

func (f *fakeCalls) SupplyIntentConstraint(ctx context.Context, p principal.Principal, a screens.SupplyIntentConstraintArgs) (string, error) {
	if f.supplyIntentConstraint == nil {
		return "", nil
	}
	return f.supplyIntentConstraint(ctx, p, a)
}

func (f *fakeCalls) WithdrawConstraint(ctx context.Context, p principal.Principal, a screens.WithdrawConstraintArgs) error {
	if f.withdrawConstraint == nil {
		return nil
	}
	return f.withdrawConstraint(ctx, p, a)
}

func (f *fakeCalls) ClearCeiling(ctx context.Context, p principal.Principal, a screens.ClearCeilingArgs) error {
	if f.clearCeiling == nil {
		return nil
	}
	return f.clearCeiling(ctx, p, a)
}

func (f *fakeCalls) RollBack(ctx context.Context, p principal.Principal, a screens.RollBackArgs) error {
	if f.rollBack == nil {
		return nil
	}
	return f.rollBack(ctx, p, a)
}

func (f *fakeCalls) RaiseRevert(ctx context.Context, p principal.Principal, a screens.RaiseRevertArgs) error {
	if f.raiseRevert == nil {
		return nil
	}
	return f.raiseRevert(ctx, p, a)
}

func (f *fakeCalls) StartMitigation(ctx context.Context, p principal.Principal, a screens.StartMitigationArgs) (string, error) {
	if f.startMitigation == nil {
		return "", nil
	}
	return f.startMitigation(ctx, p, a)
}

func (f *fakeCalls) EndMitigation(ctx context.Context, p principal.Principal, a screens.EndMitigationArgs) error {
	if f.endMitigation == nil {
		return nil
	}
	return f.endMitigation(ctx, p, a)
}

func (f *fakeCalls) MarkRollbackNotCaused(ctx context.Context, p principal.Principal, a screens.MarkRollbackNotCausedArgs) error {
	if f.markRollbackNotCaused == nil {
		return nil
	}
	return f.markRollbackNotCaused(ctx, p, a)
}

func (f *fakeCalls) FirePage(ctx context.Context, p principal.Principal, a screens.FirePageArgs) error {
	if f.firePage == nil {
		return nil
	}
	return f.firePage(ctx, p, a)
}

func (f *fakeCalls) CreateProject(ctx context.Context, p principal.Principal, a screens.CreateProjectArgs) (string, error) {
	if f.createProject == nil {
		return "", nil
	}
	return f.createProject(ctx, p, a)
}

func (f *fakeCalls) DeclareArea(ctx context.Context, p principal.Principal, a screens.DeclareAreaArgs) (string, error) {
	if f.declareArea == nil {
		return "", nil
	}
	return f.declareArea(ctx, p, a)
}

func (f *fakeCalls) AuthorParameter(ctx context.Context, p principal.Principal, a screens.AuthorParameterArgs) error {
	if f.authorParameter == nil {
		return nil
	}
	return f.authorParameter(ctx, p, a)
}

func (f *fakeCalls) PlaceSafeguard(ctx context.Context, p principal.Principal, a screens.PlaceSafeguardArgs) (string, error) {
	if f.placeSafeguard == nil {
		return "", nil
	}
	return f.placeSafeguard(ctx, p, a)
}

func (f *fakeCalls) WithdrawSafeguard(ctx context.Context, p principal.Principal, a screens.WithdrawSafeguardArgs) error {
	if f.withdrawSafeguard == nil {
		return nil
	}
	return f.withdrawSafeguard(ctx, p, a)
}

func (f *fakeCalls) SetHalt(ctx context.Context, p principal.Principal, a screens.SetHaltArgs) (string, error) {
	if f.setHalt == nil {
		return "", nil
	}
	return f.setHalt(ctx, p, a)
}

func (f *fakeCalls) WithdrawHalt(ctx context.Context, p principal.Principal, a screens.WithdrawHaltArgs) error {
	if f.withdrawHalt == nil {
		return nil
	}
	return f.withdrawHalt(ctx, p, a)
}

func (f *fakeCalls) SetLegalHold(ctx context.Context, p principal.Principal, a screens.SetLegalHoldArgs) (string, error) {
	if f.setLegalHold == nil {
		return "", nil
	}
	return f.setLegalHold(ctx, p, a)
}

func (f *fakeCalls) WithdrawLegalHold(ctx context.Context, p principal.Principal, a screens.WithdrawLegalHoldArgs) error {
	if f.withdrawLegalHold == nil {
		return nil
	}
	return f.withdrawLegalHold(ctx, p, a)
}

func (f *fakeCalls) WriteFleetEntry(ctx context.Context, p principal.Principal, a screens.WriteFleetEntryArgs) (string, error) {
	if f.writeFleetEntry == nil {
		return "", nil
	}
	return f.writeFleetEntry(ctx, p, a)
}

func (f *fakeCalls) WithdrawFleetEntry(ctx context.Context, p principal.Principal, a screens.WithdrawFleetEntryArgs) error {
	if f.withdrawFleetEntry == nil {
		return nil
	}
	return f.withdrawFleetEntry(ctx, p, a)
}

func (f *fakeCalls) SupplyConstraint(ctx context.Context, p principal.Principal, a screens.SupplyConstraintArgs) (string, error) {
	if f.supplyConstraint == nil {
		return "", nil
	}
	return f.supplyConstraint(ctx, p, a)
}

func (f *fakeCalls) SetSeam5Enforced(ctx context.Context, p principal.Principal, a screens.SetSeam5EnforcedArgs) error {
	if f.setSeam5Enforced == nil {
		return nil
	}
	return f.setSeam5Enforced(ctx, p, a)
}

func (f *fakeCalls) RetireService(ctx context.Context, p principal.Principal, a screens.RetireServiceArgs) error {
	if f.retireService == nil {
		return nil
	}
	return f.retireService(ctx, p, a)
}

func (f *fakeCalls) EndProject(ctx context.Context, p principal.Principal, a screens.EndProjectArgs) error {
	if f.endProject == nil {
		return nil
	}
	return f.endProject(ctx, p, a)
}

func (f *fakeCalls) DecideRecordRow(ctx context.Context, p principal.Principal, a screens.DecideRecordRowArgs) error {
	if f.decideRecordRow == nil {
		return nil
	}
	return f.decideRecordRow(ctx, p, a)
}

func (f *fakeCalls) EditRecordRow(ctx context.Context, p principal.Principal, a screens.EditRecordRowArgs) error {
	if f.editRecordRow == nil {
		return nil
	}
	return f.editRecordRow(ctx, p, a)
}

func (f *fakeCalls) DeclareDuty(ctx context.Context, p principal.Principal, a screens.DeclareDutyArgs) error {
	if f.declareDuty == nil {
		return nil
	}
	return f.declareDuty(ctx, p, a)
}

func (f *fakeCalls) WithdrawDuty(ctx context.Context, p principal.Principal, a screens.WithdrawDutyArgs) error {
	if f.withdrawDuty == nil {
		return nil
	}
	return f.withdrawDuty(ctx, p, a)
}

func (f *fakeCalls) DeclareObligation(ctx context.Context, p principal.Principal, a screens.DeclareObligationArgs) error {
	if f.declareObligation == nil {
		return nil
	}
	return f.declareObligation(ctx, p, a)
}

func (f *fakeCalls) WithdrawObligation(ctx context.Context, p principal.Principal, a screens.WithdrawObligationArgs) error {
	if f.withdrawObligation == nil {
		return nil
	}
	return f.withdrawObligation(ctx, p, a)
}

func (f *fakeCalls) LendCredential(ctx context.Context, p principal.Principal, a screens.LendCredentialArgs) error {
	if f.lendCredential == nil {
		return nil
	}
	return f.lendCredential(ctx, p, a)
}

func (f *fakeCalls) TakeBackCredential(ctx context.Context, p principal.Principal, a screens.TakeBackCredentialArgs) error {
	if f.takeBackCredential == nil {
		return nil
	}
	return f.takeBackCredential(ctx, p, a)
}

func (f *fakeCalls) AuthorCeiling(ctx context.Context, p principal.Principal, a screens.AuthorCeilingArgs) error {
	if f.authorCeiling == nil {
		return nil
	}
	return f.authorCeiling(ctx, p, a)
}

func (f *fakeCalls) AuthorRate(ctx context.Context, p principal.Principal, a screens.AuthorRateArgs) error {
	if f.authorRate == nil {
		return nil
	}
	return f.authorRate(ctx, p, a)
}

func (f *fakeCalls) WriteMapping(ctx context.Context, p principal.Principal, a screens.WriteMappingArgs) error {
	if f.writeMapping == nil {
		return nil
	}
	return f.writeMapping(ctx, p, a)
}

func (f *fakeCalls) DeleteMapping(ctx context.Context, p principal.Principal, a screens.DeleteMappingArgs) error {
	if f.deleteMapping == nil {
		return nil
	}
	return f.deleteMapping(ctx, p, a)
}
