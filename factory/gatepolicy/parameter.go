package gatepolicy

import (
	"errors"
	"fmt"
	"slices"
)

// Parameter is one value an owner may author, or one a safeguard binds without
// anyone authoring it. Thirteen are authored across gate policy's eleven rows —
// [Definitions] — seven more are authored and are not among the eleven —
// [NotAmongTheEleven] — and one is only ever a safeguard's, [SafeguardOnly].
type Parameter string

const (
	// RiskThreshold is where the score stops auto-passing and puts a human at
	// the gate. It is a field of an environment record per gate row, and of the
	// factory-wide settings record for the row that decides what an agent is told.
	RiskThreshold Parameter = "risk_threshold"
	// ExposureBound is where the exposure factor stops being weighed and puts a
	// human at Implementation instead. It is a field of the service record: the
	// exposure list it is read against is derived from one service's build against
	// that service's current release.
	ExposureBound Parameter = "exposure_bound"
	// AdvisorySeverity is the bound at or above which a matching advisory rejects
	// at Implementation and holds at Deploy to production. It is a field of the
	// factory-wide settings record, one pass over one feed reaching every project.
	AdvisorySeverity Parameter = "advisory_severity"
	// AttemptLimit is how many times a stage is retried, how many rounds the
	// interview asks, and how many times decomposition runs again on a rejected
	// set. It is a field of the factory-wide settings record, keyed by [KeyStage].
	AttemptLimit Parameter = "attempt_limit"
	// ItemSizeTarget is how large an item is meant to be, above the minimum that
	// it ships by itself. It is a field of the area record.
	ItemSizeTarget Parameter = "item_size_target"
	// AllowedPredicateKinds is what kinds of assertion a consumer contract may
	// draw from. It is a field of the factory-wide settings record.
	AllowedPredicateKinds Parameter = "allowed_predicate_kinds"
	// WindowSize is the smallest regression the comparison must rule out to close
	// an analysis window passed. It is a field of the service record, one value
	// per [Quantity].
	WindowSize Parameter = "window_size"
	// WindowConfidence is how sure the comparison must be before rolling a release
	// back. It is a field of the service record.
	WindowConfidence Parameter = "window_confidence"
	// WindowPower is how reliably a regression of the size in force is caught
	// rather than reaching passed. It is a field of the service record, one value
	// per [Quantity] as the size is.
	WindowPower Parameter = "window_power"
	// WindowCap is the elapsed time that ends a window which will never reach its
	// volume. It is a field of the service record.
	WindowCap Parameter = "window_cap"
	// WindowLimit is how many analysis windows one service may hold open at once.
	// It is a field of the service record.
	WindowLimit Parameter = "window_limit"
	// HeldOutSampleRate is how often the score auto-passes a change it would have
	// gated, to keep unbiased signal on the authors and areas it has stopped
	// trusting. It is a field of the factory-wide settings record, the sample being
	// one formula's and no service's.
	HeldOutSampleRate Parameter = "held_out_sample_rate"
	// ReviewSampleRate is how often a change the score would have auto-passed is
	// put in front of a duty's human anyway. It is a field of the factory-wide
	// settings record, keyed by [KeyDuty].
	ReviewSampleRate Parameter = "review_sample_rate"

	// DecisionLogRetention is how long the decision log is kept. It is a field of
	// the factory-wide settings record, authored and not among the eleven.
	DecisionLogRetention Parameter = "decision_log_retention"
	// ReportRetention is how long the report store keeps a report. It is a field
	// of the factory-wide settings record, authored and not among the eleven.
	ReportRetention Parameter = "report_retention"
	// BackupRetention is how far back a backup may reach, authored outright with
	// nothing supplied. It is a field of the factory-wide settings record.
	BackupRetention Parameter = "backup_retention"
	// RetentionFloor is how low an authored value or a safeguard may ever take
	// [DecisionLogRetention]. It is a field of the factory-wide settings record and
	// is written two ways only: at the gate row that decides a shortening, or by
	// the constraint kind whose subject is a factory parameter.
	RetentionFloor Parameter = "retention_floor"
	// RemediationPeriod is how long a matching advisory of one severity may stand
	// before the intent it raised pages. It is a field of the factory-wide settings
	// record, keyed by [KeySeverity] and authored outright with nothing supplied.
	RemediationPeriod Parameter = "remediation_period"
	// ReportChannelRate is what bounds arrival at the way in, per service and
	// factory-wide. It is a field of the factory-wide settings record, authored
	// outright with nothing supplied.
	ReportChannelRate Parameter = "report_channel_rate"
	// HarmMarkPageCap is how many intents one service's reports marked as
	// describing harm to a person may page per interval. It is a field of the
	// factory-wide settings record, shipped with a default rather than supplied.
	HarmMarkPageCap Parameter = "harm_mark_page_cap"

	// SafeguardPredicate is a predicate an owner asserts on one element of a
	// contract, where the derivation of a consumer contract cannot see the read.
	// Nothing authors it: it exists as a safeguard and only as one, so it is listed
	// in [SafeguardOnly] rather than in [Definitions].
	SafeguardPredicate Parameter = "safeguard_predicate"
	// DriftDetectorLastCheckMaxAge is how old the drift detector's own last check
	// may be before the production deploy row holds. Nothing authors it either:
	// the detector supplies its own interval, so what an owner may add is a
	// safeguard on that record and nothing else, and it is listed in
	// [SafeguardOnly] for that reason.
	DriftDetectorLastCheckMaxAge Parameter = "drift_detector_last_check_max_age"
	// ExplicitThreshold is the absolute number a service's quantity is read
	// against beside the comparison, with [ExplicitThresholdSize] as the smallest
	// change from it worth catching. Both are fields of the service record, and
	// neither is one of gate policy's eleven rows.
	ExplicitThreshold Parameter = "explicit_threshold"
	// ExplicitThresholdSize is the size an owner sets when they set the number.
	ExplicitThresholdSize Parameter = "explicit_threshold_size"
)

// Kind is what a parameter's value is, which decides how a safeguard clamps it and
// how a value is written and printed.
type Kind string

const (
	// KindFraction is a value between nothing and one.
	KindFraction Kind = "fraction"
	// KindCount is a whole number above zero.
	KindCount Kind = "count"
	// KindSeconds is an elapsed time in seconds.
	KindSeconds Kind = "seconds"
	// KindSeverity is a severity on the advisory feed's own scale. It is its own
	// kind because the scale is the feed's and not the factory's: nothing here
	// bounds it above, where a fraction is bounded at one.
	KindSeverity Kind = "severity"
	// KindList is a list of names, clamped by union.
	KindList Kind = "list"
	// KindPredicate is one predicate on one element of a contract: a
	// [PredicateKind] and, where that kind takes one, its argument. It is the shape a
	// safeguard's predicate takes as its bound and of nothing else, and it is not a
	// number, so nothing clamps it arithmetically — a safeguard of this kind adds a
	// predicate to the ones derived and removes none.
	KindPredicate Kind = "predicate"
)

// Direction is which way a safeguard on a parameter may move the value in force.
// All of them point toward more protection, which is the whole rule in
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md.
type Direction string

const (
	// DirectionCeiling caps the value in force.
	DirectionCeiling Direction = "ceiling"
	// DirectionFloor raises the value in force, and for a list is the union of
	// the names a safeguard adds and the value in force.
	DirectionFloor Direction = "floor"
	// DirectionAddsAHuman adds a human at the gate and carries no bound. It is
	// the risk threshold's direction and no other's.
	DirectionAddsAHuman Direction = "adds_a_human"
	// DirectionNone is a parameter no safeguard reaches: one authored outright
	// with nothing supplied and nothing to clamp, and the field an owner turns on
	// once. A safeguard on such a parameter is refused where it is written rather
	// than ignored where it would be read.
	DirectionNone Direction = "none"
)

// Scope is the record a parameter is authored on.
type Scope string

const (
	// ScopeEnvironment is a field of an environment record.
	ScopeEnvironment Scope = "environment"
	// ScopeService is a field of the service record.
	ScopeService Scope = "service"
	// ScopeArea is a field of the area record.
	ScopeArea Scope = "area"
	// ScopeFactorySettings is a field of the factory-wide settings record.
	ScopeFactorySettings Scope = "factory_settings"
	// ScopeNothing is no record at all: the parameter is a safeguard's and nobody
	// authors a value for it, so there is no field for one to be a field of.
	ScopeNothing Scope = "nothing"
)

// Key is what a parameter's value is keyed by inside the record its scope names,
// where one value per record is not what the design gives it. A parameter with
// [KeyNone] has one value per record.
type Key string

const (
	// KeyNone is one value per record its scope names.
	KeyNone Key = ""
	// KeyGateRow is one value per gate row. The risk threshold takes it: a row's
	// threshold is a field of an environment record, per row.
	KeyGateRow Key = "gate_row"
	// KeyStage is one value per stage, with the interview's rounds and
	// decomposition's re-decompositions counted against the same parameter — it is
	// one parameter and not three, so they are two more values under this key and
	// not two more parameters.
	KeyStage Key = "stage"
	// KeyQuantity is one value per [Quantity]: a detectable change in an error
	// rate and one in a latency quantile are not one number.
	KeyQuantity Key = "quantity"
	// KeyDuty is one value per duty, the twelve being the factory's own the way a
	// stage is.
	KeyDuty Key = "duty"
	// KeySeverity is one value per advisory severity.
	KeySeverity Key = "advisory_severity"
	// KeyService is one value per service on a record that is not the service's:
	// the report channel's per-service rate and the harm mark's page cap are
	// fields of the factory-wide settings record and keyed by the service.
	KeyService Key = "service"
)

// Definition is everything this package knows about one parameter.
type Definition struct {
	Parameter Parameter
	// Row is the gate-policy row the parameter belongs to. One row carries the
	// analysis window's size, confidence and power; every other row of the eleven
	// carries one parameter; and a parameter outside [Definitions] has none, gate
	// policy's rows being the eleven.
	Row       string
	Kind      Kind
	Direction Direction
	Scope     Scope
	// Key is what the value is keyed by inside the record its scope names.
	Key Key
	// Limits is what the parameter limits, in the words gate policy's own table
	// uses. It is here so that a printer can say what a number does without a
	// reader holding the document open.
	Limits string
	// Unit is what the number means, for a printer and for an owner typing
	// one. A parameter of KindList has none.
	Unit string
	// ReaderAtThisMilestone says which mechanism reads the value in force, and
	// is empty for a parameter nothing reads yet. It is here so that a printer can
	// say so rather than leaving an owner to discover that what they authored
	// changed nothing.
	ReaderAtThisMilestone string
}

// ErrUnknown is returned by [Define] for a name that is in none of the three
// lists.
var ErrUnknown = errors.New("gatepolicy: not one of gate policy's parameters")

// Define is one parameter's definition, from [Definitions], [NotAmongTheEleven]
// or [SafeguardOnly]. A name in none of them is [ErrUnknown] rather than a zero
// definition, so a caller that took a parameter from an owner's input cannot
// resolve one that does not exist.
func Define(p Parameter) (Definition, error) {
	for _, d := range slices.Concat(Definitions, NotAmongTheEleven, SafeguardOnly) {
		if d.Parameter == p {
			return d, nil
		}
	}
	return Definition{}, fmt.Errorf("%w: %q", ErrUnknown, p)
}

// Rows is every gate-policy row, in the order [Definitions] lists them, each
// named once however many parameters it carries.
func Rows() []string {
	var rows []string
	for _, d := range Definitions {
		if !slices.Contains(rows, d.Row) {
			rows = append(rows, d.Row)
		}
	}
	return rows
}
