package screens

// CreateProjectArgs writes a project and, in the same event, production's
// environment for it.
type CreateProjectArgs struct {
	Name string
}

// DeclareAreaArgs declares an area inside another area or, where
// InsideAreaID is empty, inside ProjectName.
type DeclareAreaArgs struct {
	Name         string
	InsideAreaID string
	ProjectName  string
}

// AuthorParameterArgs mirrors the terminal's author subcommand: the
// parameter and its value as strings, and the subject flags the parameter
// needs, the rest left empty — the record a parameter is a field of being a
// fact of the parameter rather than a choice.
type AuthorParameterArgs struct {
	Parameter   string
	Value       string
	ServiceID   string
	AreaID      string
	GateRow     string
	Stage       string
	Quantity    string
	ProjectName string
}

// PlaceSafeguardArgs takes no direction: the direction differs per
// parameter and points the same way in each, so an owner chooses only the
// subject and the bound.
//
// ServiceName is read for one subject kind alone — a gate row, where a
// row-scoped safeguard is drawn on the service the row fires for and keyed by
// the row — and is empty for every other, whose subject names its own record.
type PlaceSafeguardArgs struct {
	Parameter   string
	SubjectKind string
	SubjectName string
	ServiceName string
	Bound       string
	RouteDuty   int64
	RouteHuman  string
}

// WithdrawSafeguardArgs writes a safeguard's withdrawal, pending until an
// approved decides it.
type WithdrawSafeguardArgs struct {
	SafeguardID string
}

// SetHaltArgs is the one authored record whose subject is the factory.
type SetHaltArgs struct {
	Reason string
}

// WithdrawHaltArgs writes a halt's withdrawal, pending until an approved
// decides it.
type WithdrawHaltArgs struct {
	HaltID string
}

// SetLegalHoldArgs is a subject and a reason: a service, a project, or the
// whole factory.
type SetLegalHoldArgs struct {
	SubjectKind string
	SubjectName string
	Reason      string
}

// WithdrawLegalHoldArgs writes a legal hold's withdrawal, pending until an
// approved decides it.
type WithdrawLegalHoldArgs struct {
	LegalHoldID string
}

// WriteFleetEntryArgs is the fleet entry's nine fields: the model at an
// effort in a role with a scope, the credential it runs on, the classes of
// material it may be handed, how much it reads at once, and how many
// dispatches pass between evaluation-set runs. The scope is one of the nine
// and is held here in the three columns fleetentry.Scope has — a project, a
// service and an area — so this struct has eleven fields for the design's
// nine; each of the three may be empty, and all three empty scopes the entry
// to the whole factory.
//
// The three scope fields are names and not ids: an owner writes this entry by
// hand at Factory, and a name is what they have. The composition resolves each
// against its record and refuses one that resolves to nothing, the way a
// permanent constraint's reach is resolved. What is stored is the id, which is
// what [FleetEntry] carries back.
type WriteFleetEntryArgs struct {
	ModelVersion              string
	Effort                    string
	Role                      string
	ScopeProjectName          string
	ScopeServiceName          string
	ScopeAreaName             string
	Credential                string
	ProcessingLocation        string
	MaterialClasses           []string
	ReadAtOnceBound           int64
	DispatchesBetweenEvalRuns int64
}

// WithdrawFleetEntryArgs withdraws a fleet entry.
type WithdrawFleetEntryArgs struct {
	FleetEntryID string
}

// SupplyConstraintArgs is a permanent constraint: the factory, a project, or
// an area.
type SupplyConstraintArgs struct {
	Statement string
	// Kind is "document" or "notice"; empty means document, so every caller
	// that predates the notice kind is unchanged. A notice's reach must be
	// one project, which the constraint package refuses and this call
	// surfaces as [ErrRefused].
	Kind      string
	ReachKind string // factory, project, or area
	ReachName string
	// BindsFrom and ReviewDate are calendar dates, empty where none was
	// authored; Zone is the IANA zone both were authored in.
	BindsFrom  string
	ReviewDate string
	Zone       string
	// RequiresSeam5Enforced is the one thing a document-kind constraint makes
	// dispatch read: every item within this constraint's reach waits at
	// dispatch until [Factory.Seam5Enforced] says the factory enforces seam 5.
	RequiresSeam5Enforced bool
}

// SetSeam5EnforcedArgs turns enforcement of seam 5 on. It is one-way — off at
// install, turned on once, and never off again — so Enforced false is refused
// rather than stored.
type SetSeam5EnforcedArgs struct {
	Enforced bool
}

// RetireServiceArgs ends a service: the owner's write of retired on the
// service record, which is the one thing that ends one and what calls the
// deployer's removal.
//
// EnvironmentName, where given, performs the removal for that one environment
// and writes nothing on the service record — the step an owner takes before an
// environment other than production may be withdrawn, and the order is by hand:
// remove, then withdraw.
type RetireServiceArgs struct {
	ServiceID       string
	EnvironmentName string
}

// EndProjectArgs ends a project once every service in it is retired, and
// withdraws production's environment for it in the same write.
type EndProjectArgs struct {
	ProjectName string
}

// DecideRecordRowArgs is one of the five rows that decide a record rather
// than an item: a safeguard's withdrawal, a halt's withdrawal, a legal
// hold's ending, a shortening of decision-log retention, or the row every
// version of a role prompt fires. RowKind names which.
//
// RecordID is the record the row decides, which on the four withdrawals and
// shortenings is the withdrawal's or the shortening's own id and not the
// safeguard, halt, hold or settings record it removes a protection from — the
// withdrawal is what an owner decides, and [RecordDecidingRow] names the same
// id. On the role-prompt row it is the version awaiting the gate.
type DecideRecordRowArgs struct {
	RowKind        string
	RecordID       string
	Verdict        string
	Reason         string
	OpenedInWorkAt string // RFC 3339 UTC
}

// PerformErasureArgs is one erasure as an owner performs it here: the report
// whose words go, the half-open byte ranges of that report's text to destroy,
// and the reason the redaction record carries.
//
// An owner names the words once. The factory walks the links that exist from
// the report — the intent it was grouped into, that intent's statement, and
// the artifact versions authored against it — and finds the same words in
// each, so nothing here names a statement or a version.
type PerformErasureArgs struct {
	ReportID string
	Spans    []ErasureSpan
	Reason   string
}

// ErasureSpan is a half-open byte range of the report's text, [Start, End),
// which is the unit a redaction names and each target's own writer destroys.
type ErasureSpan struct {
	Start int
	End   int
}

// EditRecordRowArgs is the role-prompt row's third action: not a verdict but
// authoring a version and re-firing the row. It is available on the
// role-prompt row and refused by the composition on the four
// record-deciding rows, which have no version to author. Row is the row
// kind [DecideRecordRowArgs] already uses.
type EditRecordRowArgs struct {
	Row            string
	RecordID       string
	Version        string
	OpenedInWorkAt string // RFC 3339 UTC
}
