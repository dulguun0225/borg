package screens

// Factory is [Views.Factory]'s view: gate and risk policy in force, the
// fleet, the lent credentials a fleet entry may run on, the constraints and
// projects, the report channel's own numbers, and the factory's own numbers
// over the one factory-owned span. Absent from it, because nothing builds
// them yet: advisories, the mutation score, and the design-system numbers
// _Work, Ops, Factory, People_ lists — each left out rather than shown as
// zero, so a screen answering nothing is read as unbuilt and not as a quiet
// week.
type Factory struct {
	Parameters      []Parameter
	Safeguards      []Safeguard
	Halts           []Halt
	LegalHolds      []LegalHold
	Environments    []Environment
	FleetEntries    []FleetEntry
	LentCredentials []LentCredentialSummary
	RolePrompts     []RolePrompt
	// FleetProposals is always empty at this milestone: both of its writers
	// — the install's first-start step and dispatch's argued run — are
	// M14's.
	FleetProposals []FleetProposal
	Constraints    []Constraint
	Projects       []Project
	Areas          []Area
	Numbers        Numbers
	// ReportChannel is what the one way into the factory from outside it did.
	// It is the zero value where this composition holds no report store, which
	// is every subcommand that makes one pass and exits.
	ReportChannel ReportChannel

	StoppedAtDispatch   []DispatchCause
	ResolvedFactorGates []ResolvedFactorCount
	HumanLoad           []HumanLoad
	// ApproveUndone carries one row per named human plus one row whose
	// HumanKey is empty, for the factory as a whole.
	ApproveUndone      []ApproveUndonePair
	SelfApprovalCounts []SelfApprovalCount
	AutoPassRates      []AutoPassRate
	HeldOutBands       []HeldOutBand
	SpendCeilings      []SpendCeiling
	PageChannel        PageChannel
	LoadSplits         []LoadSplit

	// Seam5Enforced is whether this factory enforces seam 5, off at install
	// and turned on once at Factory. It is on the view because a document-kind
	// constraint may require it, and an item under such a constraint waits at
	// dispatch until this says yes.
	Seam5Enforced bool

	// RecordDecidingRows is the four rows outside every item that decide a
	// record rather than an item, pending a disposition here.
	RecordDecidingRows []RecordDecidingRow
	// RolePromptGateRow is the row every version of a role prompt fires,
	// nil where none is awaiting a decision.
	RolePromptGateRow *RolePromptGateRow
}

// Parameter is one authored value in force: its name, the subject it is
// authored on, its value, and which of the three sources — authored,
// supplied, or clamped by a safeguard — the effective value came from.
type Parameter struct {
	Name    string
	Subject string
	Value   string
	Source  string
}

// Safeguard is one safeguard: the parameter it binds, its subject, its
// direction, its bound, and who its rows route to.
type Safeguard struct {
	ID            string
	Parameter     string
	Subject       string
	Direction     string
	Bound         string
	RoutedToDuty  int64
	RoutedToHuman string
}

// Halt is the one authored record whose subject is the factory.
type Halt struct {
	ID     string
	Reason string
	At     string // RFC 3339 UTC
}

// LegalHold is a subject and a reason: a service, a project, or the whole
// factory.
type LegalHold struct {
	ID      string
	Subject string
	Reason  string
	At      string // RFC 3339 UTC
}

// Environment is production, a customer's, or a candidate's.
type Environment struct {
	ID      string
	Kind    string
	Targets []string
}

// FleetEntry is a model at an effort in a role with a scope: the fleet
// entry's own nine fields, the scope among them held in the three columns
// fleetentry.Scope has — a project, a service and an area — so this struct has
// eleven fields for the design's nine. Each of the three may be empty, and all
// three empty is the whole factory.
type FleetEntry struct {
	ID                        string
	ModelVersion              string
	Effort                    string
	Role                      string
	ScopeProjectID            string
	ScopeServiceID            string
	ScopeAreaID               string
	Credential                string
	ProcessingLocation        string
	MaterialClasses           []string
	ReadAtOnceBound           int64
	DispatchesBetweenEvalRuns int64
}

// LentCredentialSummary is one lent credential, enough for the fleet entry
// form to list which credentials a new entry may run on without reading
// People: its own name, the human who lent it and their name, the account
// kind, and whether it has already been taken back.
type LentCredentialSummary struct {
	Name       string
	LenderKey  string
	LenderName string
	Kind       string // person or organisation
	TakenBack  bool
}

// RolePrompt is one role's own prompt: the version in force, and any version
// awaiting the gate that decides it.
type RolePrompt struct {
	Role           string
	VersionInForce string
	// AwaitingGateVersion is empty where no version awaits a decision.
	AwaitingGateVersion string
}

// FleetProposal is a fleet proposal awaiting a disposition. The type exists
// so [Factory.FleetProposals] has one to be a slice of; nothing writes one
// at this milestone.
type FleetProposal struct {
	ID string
}

// Project is an owner's widest grouping of work.
type Project struct {
	ID   string
	Name string
}

// Area is an owner-declared grouping of the software, inside one area or one
// project.
type Area struct {
	ID   string
	Name string
	// Inside is the id of the area or the project this one lies inside,
	// which is one of the two and never both.
	Inside string
	// ProjectID is the project the chain of areas above this one ends at,
	// which is Inside itself for an area declared inside a project directly.
	ProjectID string
	Severity  string
}

// Numbers is the read-time queries Factory reports over the one
// factory-owned span: throughput, rework rate, gate rejection rate, and cost
// per feature.
type Numbers struct {
	ThroughputPerService map[string]int64
	ReworkRate           float64
	// GateRejectionRate is keyed by gate row.
	GateRejectionRate map[string]float64
	CostPerFeature    []ModelCost
	// CostMeasured is false where a rate is missing for some kind of unit a
	// feature ran on, which labels CostPerFeature as units per intent, per
	// model version, rather than a converted total.
	CostMeasured bool
	// IntentOutcomes is each intent's outcome, read beside cost per feature:
	// the acceptance round's verdict on the intended effect for a requested
	// intent, the rate of reports before and after the release for one
	// grouped from reports, and nothing for one the factory raised — absence
	// being the answer and not a gap.
	IntentOutcomes []IntentOutcome
}

// IntentOutcome is one closed intent's outcome as the close computed it,
// with the source that says which of the two kinds of verdict it is.
type IntentOutcome struct {
	IntentID string
	Source   string
	Outcome  string
}

// ModelCost is one model version's own share of cost per feature, or, where
// IsTotal, the converted total across every model a feature ran on.
type ModelCost struct {
	ModelVersion string
	Amount       float64
	Currency     string
	IsTotal      bool
}

// ReportChannel is what Factory reports of the report channel: how many
// reports arrived and were never grouped, how many the way in refused, how
// many the store could not read, and the two lists an owner reads beside
// them — the services serving a way in older than this factory's, and the
// services whose project has no notice for the way in to show.
//
// Refused is the one counter the report store keeps, and every other number
// on this screen is a query at read time. It has to be: the record a query
// would count is the write the rate exists to refuse. What that costs is
// that a lost counter is lost, where Ungrouped is derived from the reports
// again.
type ReportChannel struct {
	Ungrouped int64
	// RefusedOverTheChannel is the refusals made against no service: a
	// submission naming no deploy this factory placed a way in at is counted
	// on the whole channel, so a safeguard narrowing one service's rate is not
	// evaded by submitting under another service's name.
	RefusedOverTheChannel int64
	Services              []ServiceReportCounts
	OnAnOldWayIn          []ServiceOnAnOldWayIn
}

// ServiceReportCounts is one service's own counts, beside whether the project
// it lies in has a notice in force.
type ServiceReportCounts struct {
	ServiceID   string
	ServiceName string
	Refused     int64
	// NoNoticeInForce is whether the project this service lies in has no
	// notice, which is what the way in shows before a submission.
	NoNoticeInForce bool
}

// ServiceOnAnOldWayIn is one service whose current release's build names a
// shipped-bundle identity other than the running factory's, which is the way
// in it serves. The way in moves only when the factory is upgraded and the
// service builds again, so this is a list read off the build record rather
// than an inference from reports that stopped.
type ServiceOnAnOldWayIn struct {
	ServiceID   string
	ServiceName string
	// Identity is what the current release's build names, and
	// FactoryIdentity is the running factory's own.
	Identity        string
	FactoryIdentity string
}

// DispatchCause is how many items are stopped at dispatch now under one of
// its six causes.
type DispatchCause struct {
	Cause string
	Count int64
}

// ResolvedFactorCount is how many gates a resolved factor put a human at,
// grouped by which factor the vector names.
type ResolvedFactorCount struct {
	Factor string
	Count  int64
}

// HumanLoad is the human's own load, per duty and per named human: how many
// rows wait now, how long a decided row waited, and how long it was open in
// front of them before the verdict.
type HumanLoad struct {
	Duty                     int64
	HumanKey                 string
	WaitingNow               int64
	MedianWaitSeconds        int64
	MedianOpenInFrontSeconds int64
}

// ApproveUndonePair is what one human approved and how often it was later
// undone or rolled back; HumanKey empty is the same pair for the factory as
// a whole.
type ApproveUndonePair struct {
	HumanKey string
	Approved int64
	Undone   int64
}

// SelfApprovalCount is how many of a human's approvals were of their own
// edit.
type SelfApprovalCount struct {
	HumanKey string
	Count    int64
}

// AutoPassRate is the realized auto-pass rate at a threshold in force,
// against the rate recorded on the policy version that set it, per factor
// set.
type AutoPassRate struct {
	FactorSet string
	Threshold float64
	Realized  float64
	Recorded  float64
}

// HeldOutBand is the share of held-out releases whose windows failed within
// one band of the score, per factor set, with the count of resolved held-out
// windows behind it.
type HeldOutBand struct {
	FactorSet     string
	Band          string
	FailedShare   float64
	ResolvedCount int64
}

// SpendCeiling is the spend ceiling on one credential: the ceiling, the
// burn rate, and when spending at that rate projects to exhaust it —
// Unbounded where no ceiling was authored.
type SpendCeiling struct {
	Credential          string
	Ceiling             float64
	Currency            string
	BurnRate            float64
	ProjectedExhaustion string // RFC 3339 UTC; empty where Unbounded or not projected
	Unbounded           bool
	// UnpricedRuns is how many runs in the period returned a kind with no rate
	// authored for it. Their units are in no sum, so a burn rate with any of
	// them is a lower bound and the projection beside it is later than the
	// truth. The same runs are what the credential fails closed on at dispatch,
	// which is cleared by authoring the rate.
	UnpricedRuns int64
}

// PageChannel is the four numbers the page channel reports: pages per
// service and per named human, the share unacknowledged when they widened,
// the interval from acknowledged to answered, and how many pages a human
// fired at Ops on their own judgment.
type PageChannel struct {
	PagesPerService               map[string]int64
	PagesPerHuman                 map[string]int64
	ShareUnacknowledgedAtWiden    float64
	AcknowledgedToAnsweredSeconds float64
	HumanFiredPages               int64
}

// LoadSplit is one human's load split at the first delivery a transport
// accepted and again at the first acknowledgement: the part before the
// first delivery is charged to the channel, the part between the first
// delivery and the first acknowledgement is the shared duty's, and the part
// after the first acknowledgement is that human's own. A row no delivery was
// ever accepted for is charged whole to the channel and to nobody: its wait
// is all before the first delivery, and the other two parts are zero.
type LoadSplit struct {
	Duty                                     int64
	HumanKey                                 string
	BeforeFirstDeliverySeconds               float64
	BetweenDeliveryAndAcknowledgementSeconds float64
	AfterFirstAcknowledgementSeconds         float64
}

// RecordDecidingRow is one of the four rows outside every item that decide a
// record rather than an item: a safeguard's withdrawal, a halt's
// withdrawal, a legal hold's ending, or a shortening of decision-log
// retention.
type RecordDecidingRow struct {
	Kind     string
	RecordID string
	OpenedAt string // RFC 3339 UTC
}

// RolePromptGateRow is the row every version of a role prompt fires: it
// names a version and no item, so it has no place on an item's timeline and
// is decided here instead.
type RolePromptGateRow struct {
	Role      string
	VersionID string
	OpenedAt  string // RFC 3339 UTC
}
