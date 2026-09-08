package screens

// Home is [Views.Home]'s view: what waits on a human across every item, a
// row per component past its own interval, the readiness reading over the
// fleet, and, only where nothing waits, the digest.
type Home struct {
	Badge      Badge
	LastChecks []LastCheck
	Readiness  []RoleReadiness
	// Awaiting is what a safeguard on the report store is holding: an arrived
	// report waiting ungrouped, and an intent grouped from reports waiting
	// before anything is spent on it. Both are empty where an owner placed
	// neither safeguard, which is an install where nothing waits, and both sit
	// here rather than on the board because neither has an item to be a row of.
	Awaiting AwaitingAdmission
	// Digest is nil unless Badge.Total is zero: the digest is the part of
	// the home view that appears only at zero, so an empty screen means the
	// factory is working and a stopped component is the named row above,
	// not the same emptiness a quiet week produces.
	Digest *Digest
}

// Badge is what waits on a human, counted and broken into the parts a
// pending gate, a UAT assignment, an interview question, an escalation, one
// of the factory's own holds whose named cause is a record only a human
// writes, and dispatch's two constraint-caused stops each contribute. Total is
// the sum of the parts: a UAT assignment is counted there and not again under
// PendingGates.
//
// A role of [Home.Readiness] no fleet entry covers is one of the factory's own
// holds and is counted as one: a fleet entry that does not exist is a record
// only a human writes, which is what that part is for. So a fresh install's
// badge counts the roles it is waiting on before any item exists to hold.
type Badge struct {
	Total                 int64
	PendingGates          int64
	UATAssignments        int64
	InterviewQuestions    int64
	Escalations           int64
	FactoryHoldsForAHuman int64
	ConstraintCausedStops int64
	// Admissions is what a safeguard on the report store is holding: one per
	// report waiting ungrouped and one per report-derived intent waiting, which
	// is what [Home.Awaiting] renders. It is zero where an owner placed neither
	// safeguard.
	Admissions int64
}

// AwaitingAdmission is what the two safeguards on the report store are
// holding. It is the one thing that waits on a human with no item behind it:
// an unadmitted report has not been grouped into an intent yet, and an
// unadmitted intent has not been decomposed into items.
type AwaitingAdmission struct {
	// Reports is every arrived report waiting ungrouped, oldest first, with
	// its words, its kind, its harm mark and when it arrived. [Calls.AdmitReport]
	// is what admits one.
	Reports []ReportSummary
	// Intents is every intent grouped from reports waiting before an agent is
	// put on it, oldest first. [Calls.AdmitIntent] is what admits one, and it
	// is one action per group, the group already being one intent.
	Intents []IntentAwaitingAdmission
}

// IntentAwaitingAdmission is one intent grouped from reports that no human has
// admitted, as Work renders it while the safeguard holding one stands.
type IntentAwaitingAdmission struct {
	IntentID string
	// Statement is what the group of reports it was raised from says, which
	// intake wrote at the arrival and nothing rewrites.
	Statement string
	// ArrivedAt is when intake wrote the intent, in RFC 3339 UTC.
	ArrivedAt string
	// Reports is how many reports are grouped into it, which is the size of the
	// group this one action admits.
	Reports int
	// HarmMarked is whether any report of the group says a person is being
	// harmed by the software. Nothing infers it: it is the reporters' own field.
	HarmMarked bool
}

// LastCheck is one component's own record of its most recent pass, read
// against the interval it names whether or not that interval has passed —
// every one and never the newest of a class, so a component still checking
// one subject and stopped on another is not hidden by the aggregate.
type LastCheck struct {
	Component       string
	Checks          string // what it checks: a service's name, a target, or the component itself
	LastPass        string // RFC 3339 UTC
	IntervalSeconds int64
	FurtherPassOwed bool
}

// RoleReadiness is the readiness reading for one role: whether a fleet entry
// covers it, whether a role prompt version is in force for it, and how long
// the oldest item dispatch holds unmatched for that role has waited. Unlike
// a LastCheck row, a role with no matching entry reaches the badge, because
// that is a wait on a human and not a pass that merely ran late.
type RoleReadiness struct {
	Role                          string
	EntryCovers                   bool
	RolePromptInForce             bool
	OldestUnmatchedHoldAgeSeconds int64
}

// Digest is what shipped, was decided, and was auto-approved over the one
// factory-owned span, shown only where [Home.Badge]'s total is zero.
type Digest struct {
	Releases      int64
	Decisions     int64
	AutoApprovals int64
}

// Work is the board [Views.Work] serves: the rows Filter selected. Ordering
// is the client's: nothing here sorts what it renders.
type Work struct {
	Rows []WorkRow
}

// WorkRow is one row of the board: an item's identity and where it stands,
// enough to route to [Views.Item] for the rest.
type WorkRow struct {
	ItemID   string
	Stage    string
	Priority int64
	// Waiting is what the row is waiting on in plain words, or empty where
	// it is not waiting on anything.
	Waiting string
	// Stop is the stop dispatch itself wrote onto this item and stage, or
	// nil where nothing holds it.
	Stop *DispatchStop
}

// Item is [Views.Item]'s view: one item's timeline in the order intent,
// spec, plan, tasks, implementation, rollout, and the release it ends in
// happened, with each gate shown inline at the point where it fired.
type Item struct {
	ID string
	// IntentID is the intent this item answers, empty where the item names
	// none. It is what [Calls.AdmitIntent] names.
	IntentID        string
	IntentStatement string
	// Reports is the end-user reports grouped into this item's intent,
	// oldest first, and empty for an intent no report raised. They are
	// rendered under the intent's own entry and nowhere else: a browsable
	// list of them is the one place the design refuses to put them.
	Reports   []ReportSummary
	Versions  []ArtifactVersion
	Decisions []DecisionSummary
	// ImplementationDiff is the build's diff against master, shown at the
	// Implementation gate and empty before it — the one place the product
	// renders code.
	ImplementationDiff string
	// Release is nil until the item reaches one.
	Release *ReleaseSummary
	Deploys []DeploySummary
	Windows []WindowSummary
	// PartlyDelivered is true where the intent this item answers became
	// more than one item and they did not all reach a release.
	PartlyDelivered bool
	// Stop is the stop the factory caused, with the link to where at
	// Factory the record that lifts it is written, or nil where nothing
	// holds this item.
	Stop *DispatchStop
	// FactoryHold is the hold standing at this item's production deploy
	// row, in the words the hold names, and empty where none stands. It is
	// no record: the holds there are computed from records that already
	// exist and the design gives such a hold no row, so what a screen shows
	// is the reading taken when the view was read.
	FactoryHold string
	// ApprovableThrough is true where FactoryHold may be approved through
	// from here, which is the emergency action the design keeps at that row
	// — approve now, not skip. The hold stops the row being fired at all,
	// so this action fires it rather than deciding a row already open, and
	// [Calls.ApproveThroughHold] is what it calls.
	ApprovableThrough bool
	// IntentAwaitsAdmission is true where this item's intent was grouped from
	// reports and no human has admitted it, while the safeguard holding one
	// stands. It is what offers [Calls.AdmitIntent] here as well as on the home
	// view, for an item whose intent was admitted after it was decomposed.
	IntentAwaitsAdmission bool
}

// ReportSummary is one end user's report as Work renders it under the intent
// it was grouped into. It names no person: the channel carries no identity,
// and the opaque key the report may carry is not on this view.
type ReportSummary struct {
	ID string
	// Kind is the reporter's own word for it: a bug report or a complaint.
	Kind string
	// HarmMarked is the one field the reporter sets beside the kind: whether
	// the software is harming a person. Nothing infers it.
	HarmMarked  bool
	CollectedAt string // RFC 3339 UTC, the instant the way in collected it
	// NoticeID is the notice in force when the report was collected, and
	// empty where none was — which the view says as much as it says which
	// one was shown.
	NoticeID string
	// Admitted is false where a human has still to admit the report, which
	// only one of the two safeguards on the report store makes it wait for.
	Admitted bool
	// Text is the words the reporter wrote, read through every redaction
	// naming this report.
	Text string
}

// ArtifactVersion is one artifact version authored for an item: a spec, a
// plan, tasks, or an entry of the shipped chain.
type ArtifactVersion struct {
	ID         string
	Kind       string
	AuthoredAt string // RFC 3339 UTC
}

// DecisionSummary is one gate shown inline on an item's timeline: the vector
// while it is pending, the number beside the verdict once it is written, and
// when the row was opened in Work.
type DecisionSummary struct {
	OpenEventID string
	GateRow     string
	Vector      map[string]string
	// Score is nil while the row is pending.
	Score *float64
	// Verdict is empty while the row is pending.
	Verdict          string
	OpenedAt         string // RFC 3339 UTC
	OpenedInWorkAt   string // RFC 3339 UTC; empty until an actor opens the row here
	Acknowledgements []Acknowledgement
}

// Acknowledgement is one holder saying they have a row: it decides nothing,
// and the row stays in front of every other holder of the duty.
type Acknowledgement struct {
	HumanKey string
	At       string // RFC 3339 UTC
}

// ReleaseSummary is the numbered release an item ended in.
type ReleaseSummary struct {
	ServiceID string
	Number    int64
}

// DeploySummary is one target's own completion of a deploy.
type DeploySummary struct {
	EnvironmentID string
	TargetID      string
	CompletedAt   string // RFC 3339 UTC
}

// WindowSummary is one analysis window opened over this item's release.
type WindowSummary struct {
	ID string
	// Exit is empty while the window is open.
	Exit string
}

// DispatchStop is a stop dispatch itself wrote onto an item and its stage,
// naming the cause and, where the cause is a record only a human writes, the
// address at Factory where lifting it is decided.
type DispatchStop struct {
	Cause string
	Since string // RFC 3339 UTC
	// LiftedAt is the Factory address the link points at, or empty where
	// the cause is not one a human lifts from Factory.
	LiftedAt string
}

// Decision is [Views.Decision]'s view: one gate's own open event, the
// vector, the versions in force at the firing, who it routes to, its
// acknowledgements, its close or its abandonment, and every delivery
// attempted for it.
type Decision struct {
	OpenEventID      string
	ItemID           string
	GateRow          string
	OpenedAt         string // RFC 3339 UTC
	OpenedInWorkAt   string // RFC 3339 UTC; empty until an actor opens the row here
	Vector           map[string]string
	Score            *float64
	VersionsInForce  []string
	RoutedToDuty     int64
	RoutedToHuman    string
	Acknowledgements []Acknowledgement
	// Closed and Abandoned are mutually exclusive, and both nil while the
	// row is still pending.
	Closed     *DecisionClose
	Abandoned  *DecisionAbandon
	Deliveries []Delivery
}

// DecisionClose is a decision's close event: the verdict, the reason a
// reject or a hold must carry, when it closed, and who closed it.
type DecisionClose struct {
	Verdict string
	Reason  string
	At      string // RFC 3339 UTC
	Actor   string
}

// DecisionAbandon is a decision an edit superseded, a supersession ended, or
// an escalation stopped: no verdict is coming.
type DecisionAbandon struct {
	At     string // RFC 3339 UTC
	Reason string
}

// Delivery is one attempt to reach a human about a decision: the channel,
// the recipient, whether the transport accepted it, and the first time one
// did.
type Delivery struct {
	Channel         string
	Recipient       string
	Accepted        bool
	FirstAcceptedAt string // RFC 3339 UTC; empty where none has been accepted yet
}

// Constraint is [Views.Constraint]'s view: what a constraint binds, over
// what reach, and whether it still stands. Only the fields a constraint of
// the document kind carries at this milestone are here — the pass over
// constraints in force, the design system a build names, and the
// dispatch-decided kind are M12's, per package constraint's own doc.go.
type Constraint struct {
	ID   string
	Kind string
	// Reach is the widest thing the constraint binds: "factory",
	// "project:<name>", "area:<name>", or "intent:<id>".
	Reach       string
	Statement   string
	SuppliedAt  string // RFC 3339 UTC
	BindsFrom   string // calendar date; empty where the constraint binds from arrival
	ReviewDate  string // calendar date; empty where none was authored
	Zone        string // the IANA zone BindsFrom and ReviewDate were authored in
	WithdrawnAt string // RFC 3339 UTC; empty where it still stands
	// Replaces is the constraint this one corrects, or empty for the first
	// version.
	Replaces string
}
