package screens

import (
	"context"

	"github.com/dulguun0225/borg/factory/principal"
)

// Calls is every write the four screens make, one method per action, grouped
// by screen in the order [Views] takes its own methods. Every method takes
// the calling principal; a method whose call creates an address a later
// call names returns that id, and every other method returns only an error.
// cmd/factory implements Calls, reaching each writer the way the terminal's
// own subcommand already does — so this interface adds no writer to the
// graph. Three of the methods are the exception to the subcommand and not to
// the writer: [FirePage], the one call the design names as new, which package
// notifier already defines and no subcommand fires;
// [ApproveThroughHold], which fires the production deploy row a hold the
// factory computes stopped, through the same gate component every other
// verdict here reaches; and [SetSeam5Enforced], which turns on the
// factory-wide field a document-kind constraint requires — a field package
// policy has always been able to write and nothing has ever called, leaving
// the hold that reads it with no way to clear.
type Calls interface {
	// Work
	SupplyIntent(ctx context.Context, p principal.Principal, args SupplyIntentArgs) (string, error)
	AnswerQuestion(ctx context.Context, p principal.Principal, args AnswerQuestionArgs) error
	ConfirmReading(ctx context.Context, p principal.Principal, args ConfirmReadingArgs) error
	AcceptDelivery(ctx context.Context, p principal.Principal, args AcceptDeliveryArgs) error
	EndIntent(ctx context.Context, p principal.Principal, args EndIntentArgs) error
	AdmitIntent(ctx context.Context, p principal.Principal, args AdmitIntentArgs) error
	AdmitReport(ctx context.Context, p principal.Principal, args AdmitReportArgs) error
	SetPriority(ctx context.Context, p principal.Principal, args SetPriorityArgs) error
	EndItem(ctx context.Context, p principal.Principal, args EndItemArgs) error
	Decide(ctx context.Context, p principal.Principal, args DecideArgs) error
	ApproveThroughHold(ctx context.Context, p principal.Principal, args ApproveThroughHoldArgs) error
	Refer(ctx context.Context, p principal.Principal, args ReferArgs) error
	EditInPlace(ctx context.Context, p principal.Principal, args EditInPlaceArgs) error
	Acknowledge(ctx context.Context, p principal.Principal, args AcknowledgeArgs) error
	TakeOver(ctx context.Context, p principal.Principal, args TakeOverArgs) error
	SupplyIntentConstraint(ctx context.Context, p principal.Principal, args SupplyIntentConstraintArgs) (string, error)
	WithdrawConstraint(ctx context.Context, p principal.Principal, args WithdrawConstraintArgs) error
	ClearCeiling(ctx context.Context, p principal.Principal, args ClearCeilingArgs) error

	// Ops
	RollBack(ctx context.Context, p principal.Principal, args RollBackArgs) error
	RaiseRevert(ctx context.Context, p principal.Principal, args RaiseRevertArgs) error
	StartMitigation(ctx context.Context, p principal.Principal, args StartMitigationArgs) (string, error)
	EndMitigation(ctx context.Context, p principal.Principal, args EndMitigationArgs) error
	MarkRollbackNotCaused(ctx context.Context, p principal.Principal, args MarkRollbackNotCausedArgs) error
	FirePage(ctx context.Context, p principal.Principal, args FirePageArgs) error

	// Factory
	CreateProject(ctx context.Context, p principal.Principal, args CreateProjectArgs) (string, error)
	DeclareArea(ctx context.Context, p principal.Principal, args DeclareAreaArgs) (string, error)
	AuthorParameter(ctx context.Context, p principal.Principal, args AuthorParameterArgs) error
	PlaceSafeguard(ctx context.Context, p principal.Principal, args PlaceSafeguardArgs) (string, error)
	WithdrawSafeguard(ctx context.Context, p principal.Principal, args WithdrawSafeguardArgs) error
	SetHalt(ctx context.Context, p principal.Principal, args SetHaltArgs) (string, error)
	WithdrawHalt(ctx context.Context, p principal.Principal, args WithdrawHaltArgs) error
	SetLegalHold(ctx context.Context, p principal.Principal, args SetLegalHoldArgs) (string, error)
	WithdrawLegalHold(ctx context.Context, p principal.Principal, args WithdrawLegalHoldArgs) error
	WriteFleetEntry(ctx context.Context, p principal.Principal, args WriteFleetEntryArgs) (string, error)
	WithdrawFleetEntry(ctx context.Context, p principal.Principal, args WithdrawFleetEntryArgs) error
	SupplyConstraint(ctx context.Context, p principal.Principal, args SupplyConstraintArgs) (string, error)
	SetSeam5Enforced(ctx context.Context, p principal.Principal, args SetSeam5EnforcedArgs) error
	RetireService(ctx context.Context, p principal.Principal, args RetireServiceArgs) error
	EndProject(ctx context.Context, p principal.Principal, args EndProjectArgs) error
	DecideRecordRow(ctx context.Context, p principal.Principal, args DecideRecordRowArgs) error
	EditRecordRow(ctx context.Context, p principal.Principal, args EditRecordRowArgs) error

	// People
	DeclareDuty(ctx context.Context, p principal.Principal, args DeclareDutyArgs) error
	WithdrawDuty(ctx context.Context, p principal.Principal, args WithdrawDutyArgs) error
	DeclareObligation(ctx context.Context, p principal.Principal, args DeclareObligationArgs) error
	WithdrawObligation(ctx context.Context, p principal.Principal, args WithdrawObligationArgs) error
	LendCredential(ctx context.Context, p principal.Principal, args LendCredentialArgs) error
	TakeBackCredential(ctx context.Context, p principal.Principal, args TakeBackCredentialArgs) error
	AuthorCeiling(ctx context.Context, p principal.Principal, args AuthorCeilingArgs) error
	AuthorRate(ctx context.Context, p principal.Principal, args AuthorRateArgs) error
	WriteMapping(ctx context.Context, p principal.Principal, args WriteMappingArgs) error
	DeleteMapping(ctx context.Context, p principal.Principal, args DeleteMappingArgs) error
}
