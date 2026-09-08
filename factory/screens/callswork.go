package screens

// SupplyIntentArgs is intake's own writer, called from Work: what an owner
// requests. Services is the services an intent changing more than one
// names, empty for one; ProjectName defaults to the project run installs
// when empty, the way the terminal's -project flag already does.
type SupplyIntentArgs struct {
	Statement   string
	Services    []string
	ProjectName string
}

// AnswerQuestionArgs is an answer to one round of the factory's own
// interview.
type AnswerQuestionArgs struct {
	QuestionID string
	Answer     string
}

// ConfirmReadingArgs is the acceptance round's one round more, once every
// item of an intent has shipped: whether what shipped is what was asked
// for, or, where it is not, the correction that reopens the interview.
type ConfirmReadingArgs struct {
	IntentID   string
	Confirmed  bool
	Correction string
}

// AcceptDeliveryArgs is a human accepting a commit master holds that the
// queue did not make.
type AcceptDeliveryArgs struct {
	ServiceID string
	Commit    string
}

// EndIntentArgs ends an intent for good.
type EndIntentArgs struct {
	IntentID string
}

// AdmitIntentArgs is the admission a safeguard on the report store makes a
// report-derived intent wait for: with one in force the intent arrives and
// waits, no agent is put on it and no interview round runs, until a human
// admits it here. It is one action per group, the group already being one
// intent.
type AdmitIntentArgs struct {
	IntentID string
}

// AdmitReportArgs is the admission the second safeguard on the report store
// makes one arrived report wait for: with it in force the report waits
// ungrouped until a human admits it, and only an admitted report reaches the
// grouper.
type AdmitReportArgs struct {
	ReportID string
}

// SetPriorityArgs writes the one field that orders every queue an item waits
// in.
type SetPriorityArgs struct {
	ItemID   string
	Priority int64
}

// EndItemArgs ends an item for good.
type EndItemArgs struct {
	ItemID string
}

// DecideArgs writes a verdict against the open event a row was rendered
// from: the same call a verdict typed at the terminal writes, and one field
// more — OpenedInWorkAt, when the actor opened the row in Work, which
// decisionlog has carried on every close event since the log's shapes were
// written and which the terminal has no answer for.
type DecideArgs struct {
	OpenEventID string
	// Verdict is approve, reject, or hold.
	Verdict string
	// Reason is required for reject and for hold.
	Reason         string
	OpenedInWorkAt string // RFC 3339 UTC
}

// ApproveThroughHoldArgs is the emergency action at the production deploy row
// — approve now, not skip. It names the item and not an open event: the holds
// the factory computes there stop the row being fired at all, so there is no
// open event to name and this call is what fires it, with the holds on the
// firing and the approve naming them.
//
// Reason is required. What approving through accepts is whatever the hold was
// preventing, so a row closed this way carries why in the same field a reject
// and a hold do.
type ApproveThroughHoldArgs struct {
	ItemID         string
	Reason         string
	OpenedInWorkAt string // RFC 3339 UTC
}

// ReferArgs closes a row with the fourth verdict, which re-fires it.
type ReferArgs struct {
	OpenEventID string
	Reason      string
}

// EditInPlaceArgs is a version a human writes together with the factory at a
// gate that may change it: the open event the edit answers, and the
// version's own text.
type EditInPlaceArgs struct {
	OpenEventID string
	VersionText string
}

// AcknowledgeArgs is a holder saying they have a row.
type AcknowledgeArgs struct {
	OpenEventID string
}

// TakeOverArgs is duty 12: an item the factory escalated, taken over and
// returned to a named stage.
type TakeOverArgs struct {
	ItemID string
	Stage  string
}

// SupplyIntentConstraintArgs is a constraint whose reach is one intent,
// supplied at Work on that intent.
type SupplyIntentConstraintArgs struct {
	IntentID  string
	Statement string
	// BindsFrom and ReviewDate are calendar dates, empty where none was
	// authored; Zone is the IANA zone both were authored in.
	BindsFrom  string
	ReviewDate string
	Zone       string
	// RequiresSeam5Enforced is the one thing a document-kind constraint makes
	// dispatch read: every item decomposed from this intent waits at dispatch
	// until [Factory.Seam5Enforced] says the factory enforces seam 5.
	RequiresSeam5Enforced bool
}

// WithdrawConstraintArgs withdraws a constraint of any reach, including the
// permanent ones supplied at Factory.
type WithdrawConstraintArgs struct {
	ConstraintID string
}

// ClearCeilingArgs authorises an overage for the period standing on the
// named credential alone; it does not reset the sum a further report is
// compared against.
type ClearCeilingArgs struct {
	Credential string
}
