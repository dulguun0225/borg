package safeguard

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// SubjectKind is what a safeguard is drawn on. [SubjectKinds] is the nine the
// design names in
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md.
type SubjectKind string

const (
	// SubjectStage is one of the factory's own stages, and reaches the gate row
	// that decides a role prompt version for it: most roles name the work of
	// one stage.
	SubjectStage SubjectKind = "stage"
	// SubjectService is a service record, and reaches every item on it.
	SubjectService SubjectKind = "service"
	// SubjectProject is a project — a persistent environment and every service
	// in it.
	SubjectProject SubjectKind = "project"
	// SubjectArea is an area record, and reaches every item whose area chain
	// crosses it.
	SubjectArea SubjectKind = "area"
	// SubjectContractElement is one element of one contract, named by the
	// contract's id and the element's name — which [contract.ElementSubject]
	// composes, because the element row's own id changes at every version and a
	// safeguard has to outlive one. It reaches the predicates in force on that
	// element, and it is the subject doc.go names as the reason a safeguard is a
	// record: that record's writer is the merge queue, so an owner authoring on it
	// would be a second writer with no seam.
	SubjectContractElement SubjectKind = "contract_element"
	// SubjectDesignSystemComponent is a component of the design system in force
	// on a project. One puts a human at Implementation for every item whose
	// build uses it, so an owner who distrusts one part of a design system does
	// not have to buy the check for a whole area. Nothing derives one yet: the
	// design system's own components are not built, so this kind's value is
	// stored and read by nothing.
	SubjectDesignSystemComponent SubjectKind = "design_system_component"
	// SubjectPredicateKindsList is "this section's own list": the list of
	// allowed predicate kinds a safeguard may extend. The design names it as a
	// subject in its own right rather than as a narrowing of a service, a
	// project or an area, the way the sample rates are narrowed — extending it
	// is global by nature, a kind of assertion decidable everywhere or nowhere
	// — and gives it no record of its own to be named by. The stored kind is
	// "factory_settings" because the factory-wide settings record, the record
	// [gatepolicy.AllowedPredicateKinds] is a field of, is the nearest one that
	// exists; a mechanism reading this subject does not read that record's
	// other fields through it.
	SubjectPredicateKindsList SubjectKind = "factory_settings"
	// SubjectReportStore is the report store. Nothing derives a safeguard
	// reading it yet: the report store is not built.
	SubjectReportStore SubjectKind = "report_store"
	// SubjectDriftDetectorLastCheck is the drift detector's last check.
	// Nothing derives a safeguard reading it yet: the drift detector's store is
	// outside this module.
	SubjectDriftDetectorLastCheck SubjectKind = "drift_detector_last_check"
)

// SubjectKinds is every subject kind the design names. The CHECK in [DDL]
// lists the same nine, and TestDDLListsEverySubjectKind fails if they stop
// agreeing.
var SubjectKinds = []SubjectKind{
	SubjectStage, SubjectService, SubjectProject, SubjectArea, SubjectContractElement,
	SubjectDesignSystemComponent, SubjectPredicateKindsList, SubjectReportStore, SubjectDriftDetectorLastCheck,
}

var (
	// ErrSubjectKindUnknown is returned for a subject kind outside
	// [SubjectKinds].
	ErrSubjectKindUnknown = errors.New("safeguard: the subject is not one of the design's nine kinds")
	// ErrSubjectIDEmpty is returned for a safeguard naming no subject.
	ErrSubjectIDEmpty = errors.New("safeguard: a safeguard names the subject it is drawn on")
	// ErrSubjectKeyRequired is returned for a safeguard on a parameter with a
	// key of its own — the gate row for the risk threshold, the stage for the
	// attempt limit, and so on — naming no value of it: the design keeps that
	// key out of the subject kinds it lists, so a parameter's own key is
	// carried as this field rather than as a tenth subject kind.
	ErrSubjectKeyRequired = errors.New("safeguard: this parameter is keyed, and the safeguard names no key value")
	// ErrSubjectKeyRefused is returned for a safeguard naming a key value on a
	// parameter that has none.
	ErrSubjectKeyRefused = errors.New("safeguard: this parameter has no key, and the safeguard names one")
	// ErrBoundRefused is returned for a bound on a parameter whose safeguard adds
	// a human rather than bounding a value, and for a bound of one of the three
	// shapes on a parameter that takes another — a list where a number belongs, a
	// number where a predicate does.
	ErrBoundRefused = errors.New("safeguard: the bound is not the shape this parameter's safeguard takes")
	// ErrBoundMissing is returned for a safeguard on a numeric or list parameter
	// with no bound. A bound of nothing would clamp nothing and read as a
	// safeguard.
	ErrBoundMissing = errors.New("safeguard: a safeguard on this parameter carries a bound")
	// ErrRoutingRefused is returned for a routing naming a duty or a human on a
	// safeguard that bounds a value rather than adding one at a gate — there
	// is no row for it to route.
	ErrRoutingRefused = errors.New("safeguard: only a safeguard that adds a human at a gate routes")
	// ErrRoutingBothNamed is returned for a routing naming a duty and a human
	// at once: the field routes to one or the other, or to neither.
	ErrRoutingBothNamed = errors.New("safeguard: the routing names a duty and a human at once")
	// ErrRoutingDutyOutOfRange is returned for a routing naming a duty outside
	// the owner's twelve.
	ErrRoutingDutyOutOfRange = errors.New("safeguard: a routed duty is one of the owner's twelve")
	// ErrNotFound is returned by [Withdraw] and [InsertWithdrawal] where no
	// safeguard has the id.
	ErrNotFound = errors.New("safeguard: no safeguard has that id")
)

// Subject is what a safeguard is drawn on: a kind, the id or name of the
// thing, and, where the parameter has a key of its own, the value of that key.
type Subject struct {
	Kind SubjectKind
	ID   string
	// Key narrows the subject to one value of a parameter's own key: the gate
	// row for the risk threshold, the stage for the attempt limit, the duty
	// for the review sample rate, the severity for the remediation period, the
	// quantity for the window's size and power, or the service for the report
	// channel's per-service rate and the harm mark's page cap. It is empty for
	// a parameter with no key of its own — [gatepolicy.KeyNone] — and required
	// otherwise, checked against the parameter's own [gatepolicy.Definition] by
	// [Insert].
	Key string
}

func (s Subject) String() string {
	if s.Key == "" {
		return string(s.Kind) + ":" + s.ID
	}
	return string(s.Kind) + ":" + s.ID + "#" + s.Key
}

// Routing is where a safeguard's rows route, on the parameters whose direction
// adds a human at a gate. At most one field is set; where neither is, the rows
// go where every unheld row goes, to the owner. It is meaningless and refused
// on a parameter that only bounds a value, there being no row for it to route.
type Routing struct {
	// Duty is one of the owner's twelve, or zero for none.
	Duty int
	// HumanKey is the named human's per-person key, or empty for none.
	HumanKey string
}

// Validate refuses a routing naming both a duty and a human, and a duty
// outside the owner's twelve.
func (r Routing) Validate() error {
	if r.Duty != 0 && r.HumanKey != "" {
		return ErrRoutingBothNamed
	}
	if r.Duty != 0 && (r.Duty < 1 || r.Duty > 12) {
		return fmt.Errorf("%w: %d", ErrRoutingDutyOutOfRange, r.Duty)
	}
	return nil
}

// Bound is what a safeguard bounds by, in whichever of the three shapes its
// parameter takes: a number, a list of names, or one predicate. It is a struct
// so that a caller cannot pass one shape where another belongs, and so that a
// fourth shape is a field here rather than a fourth argument at every call site.
type Bound struct {
	// Number is the bound of a numeric parameter, and is meaningless where the
	// direction adds a human or the parameter takes another shape.
	Number float64
	// List is the names a safeguard on a list-valued parameter adds.
	List []string
	// Predicate is the assertion a safeguard's predicate adds, and is meaningful
	// on that parameter alone.
	Predicate Predicate
}

// Predicate is one assertion a safeguard's predicate adds: the kind, and the
// argument where that kind takes one. What the assertion is about is the
// safeguard's subject, which is the contract element — so this is the bound and
// not the whole predicate.
//
// The kind is package gatepolicy's, which is where the list of allowed
// predicate kinds a consumer contract draws from lives. A safeguard adds an
// assertion of the same kinds a derivation produces, not a kind of its own:
// what a safeguard covers is a read the derivation could not see, and the
// assertion about it is the ordinary one.
type Predicate struct {
	Kind     gatepolicy.PredicateKind
	Argument string
}

// IsZero reports whether no predicate is named, which is every safeguard but
// one that supplies a predicate.
func (p Predicate) IsZero() bool { return p.Kind == "" && p.Argument == "" }

// Safeguard is one safeguard as it is stored.
type Safeguard struct {
	ID        string
	Actor     record.Actor
	At        string
	Parameter gatepolicy.Parameter
	Subject   Subject
	Direction gatepolicy.Direction
	Bound     Bound
	Routing   Routing
	// Withdrawn is whether an approved [Withdrawal] names this safeguard. It is
	// computed at read from [WithdrawalTable] and carries no column of its own:
	// a safeguard is never edited, so nothing here ever writes this field back.
	Withdrawn bool
}

// Writer is the table's one writer: Factory.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewWriter returns the writer over pool, fencing every write with token.
func NewWriter(pool *pgxpool.Pool, token lease.Token) *Writer {
	return &Writer{pool: pool, token: token}
}

// Insert writes one safeguard inside tx. Its one caller is package policy,
// which calls it inside the transaction that appends the policy version, so the
// safeguard and the version commit together or not at all.
//
// token fences this write the way every write transaction in the module does:
// tx is begun by the caller — the policy version's own transaction — so this
// is where the fence is called rather than at a BeginTx of this package's own.
//
// The direction is not an argument: it is read from the parameter's definition,
// because the direction differs per parameter and points the same way in each,
// so an owner placing a safeguard chooses the subject, the bound and the
// routing and never which way the bound points.
func Insert(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor, parameter gatepolicy.Parameter,
	subject Subject, bound Bound, routing Routing) (Safeguard, error) {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return Safeguard{}, err
	}
	if err := actor.Validate(); err != nil {
		return Safeguard{}, err
	}
	definition, err := gatepolicy.Define(parameter)
	if err != nil {
		return Safeguard{}, err
	}
	if !slices.Contains(SubjectKinds, subject.Kind) {
		return Safeguard{}, fmt.Errorf("%w: %q", ErrSubjectKindUnknown, subject.Kind)
	}
	if subject.ID == "" {
		return Safeguard{}, ErrSubjectIDEmpty
	}
	if definition.Key == gatepolicy.KeyNone && subject.Key != "" {
		return Safeguard{}, fmt.Errorf("%w: %q on %s", ErrSubjectKeyRefused, subject.Key, parameter)
	}
	if definition.Key != gatepolicy.KeyNone && subject.Key == "" {
		return Safeguard{}, fmt.Errorf("%w: %s is keyed by %s", ErrSubjectKeyRequired, parameter, definition.Key)
	}
	if err := checkBound(definition, bound); err != nil {
		return Safeguard{}, err
	}
	if definition.Direction != gatepolicy.DirectionAddsAHuman && (routing.Duty != 0 || routing.HumanKey != "") {
		return Safeguard{}, fmt.Errorf("%w: a safeguard on %s bounds a value and routes no row", ErrRoutingRefused, parameter)
	}
	if err := routing.Validate(); err != nil {
		return Safeguard{}, err
	}

	p := Safeguard{
		ID:        record.NewID(IDPrefix),
		Actor:     actor,
		At:        record.Now(),
		Parameter: parameter,
		Subject:   subject,
		Direction: definition.Direction,
		Bound:     bound,
		Routing:   routing,
	}
	var storedBound *float64
	if definition.Direction != gatepolicy.DirectionAddsAHuman &&
		definition.Kind != gatepolicy.KindList && definition.Kind != gatepolicy.KindPredicate {
		storedBound = &p.Bound.Number
	}
	var storedDuty *int
	if routing.Duty != 0 {
		storedDuty = &routing.Duty
	}
	_, err = tx.Exec(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, parameter, subject_kind, subject_id,
		subject_key, direction, bound, bound_list, predicate_kind, predicate_argument, route_duty, route_human_key)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`,
		p.ID, FormatVersion, string(p.Actor.Kind), p.Actor.Key, string(p.Actor.Basis), p.At, string(p.Parameter),
		string(p.Subject.Kind), p.Subject.ID, p.Subject.Key, string(p.Direction), storedBound,
		strings.Join(bound.List, "\n"), string(bound.Predicate.Kind), bound.Predicate.Argument,
		storedDuty, routing.HumanKey,
	)
	if err != nil {
		return Safeguard{}, fmt.Errorf("safeguard: placing a safeguard on %s at %s: %w", parameter, subject, err)
	}
	return p, nil
}

// checkBound refuses a bound of the wrong shape for the parameter: a bound on a
// safeguard that adds a human, one shape where another belongs, and a missing
// bound where one is required.
func checkBound(d gatepolicy.Definition, bound Bound) error {
	switch {
	case d.Direction == gatepolicy.DirectionAddsAHuman:
		if bound.Number != 0 || len(bound.List) > 0 || !bound.Predicate.IsZero() {
			return fmt.Errorf("%w: a safeguard on %s adds a human and bounds no value", ErrBoundRefused, d.Parameter)
		}
	case d.Kind == gatepolicy.KindList:
		if bound.Number != 0 || !bound.Predicate.IsZero() {
			return fmt.Errorf("%w: %s is a list and its bound is a list", ErrBoundRefused, d.Parameter)
		}
		if len(bound.List) == 0 {
			return fmt.Errorf("%w: %s", ErrBoundMissing, d.Parameter)
		}
	case d.Kind == gatepolicy.KindPredicate:
		if bound.Number != 0 || len(bound.List) > 0 {
			return fmt.Errorf("%w: %s is a predicate and its bound is a predicate", ErrBoundRefused, d.Parameter)
		}
		if bound.Predicate.Kind == "" {
			return fmt.Errorf("%w: %s", ErrBoundMissing, d.Parameter)
		}
		if _, err := gatepolicy.DecidablePredicate(string(bound.Predicate.Kind)); err != nil {
			return err
		}
		if bound.Predicate.Kind.TakesAnArgument() != (bound.Predicate.Argument != "") {
			return fmt.Errorf("%w: a %s predicate and the argument %q",
				ErrBoundRefused, bound.Predicate.Kind, bound.Predicate.Argument)
		}
	default:
		if len(bound.List) > 0 || !bound.Predicate.IsZero() {
			return fmt.Errorf("%w: %s is a number and its bound is a number", ErrBoundRefused, d.Parameter)
		}
		if bound.Number <= 0 {
			return fmt.Errorf("%w: %s", ErrBoundMissing, d.Parameter)
		}
	}
	return nil
}

const selectSafeguards = `select id, actor_kind, actor_key, actor_key_basis, at, parameter, subject_kind,
	subject_id, subject_key, direction, bound, bound_list, predicate_kind, predicate_argument,
	route_duty, route_human_key
	from ` + Table

// BySubjects is every safeguard in force on one parameter across any of the
// subjects, which is the one read a mechanism a safeguard binds performs. A
// safeguard an approved [Withdrawal] names is not in force and is not
// returned. It takes the pool and not a [Writer], because reading safeguards
// is not a reason to be handed the thing that places them.
//
// The subjects are a list because a mechanism reads more than one at a time: a
// gate firing on an item reads the row's own subjects — the item's service and
// every area in the item's chain — and a safeguard on any of them reaches the
// firing. A subject's [Subject.Key] is matched exactly, so a caller asking
// about one gate row, stage, duty, severity, quantity or service reads only
// the safeguards keyed to it.
func BySubjects(ctx context.Context, pool *pgxpool.Pool, parameter gatepolicy.Parameter, subjects []Subject) ([]Safeguard, error) {
	if len(subjects) == 0 {
		return nil, nil
	}
	kinds := make([]string, 0, len(subjects))
	ids := make([]string, 0, len(subjects))
	keys := make([]string, 0, len(subjects))
	for _, s := range subjects {
		kinds = append(kinds, string(s.Kind))
		ids = append(ids, s.ID)
		keys = append(keys, s.Key)
	}
	rows, err := pool.Query(ctx, selectSafeguards+`
		where parameter = $1
		and (subject_kind, subject_id, subject_key) in (select * from unnest($2::text[], $3::text[], $4::text[]))
		and not exists (select 1 from `+WithdrawalTable+` w where w.safeguard_id = `+Table+`.id and w.approved)
		order by at, id`, string(parameter), kinds, ids, keys)
	if err != nil {
		return nil, fmt.Errorf("safeguard: reading the safeguards on %s: %w", parameter, err)
	}
	defer rows.Close()

	var read []Safeguard
	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			return nil, err
		}
		read = append(read, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("safeguard: reading the safeguards on %s: %w", parameter, err)
	}
	return read, nil
}

// All is every safeguard ever placed, an approved withdrawal's included, in
// the order they were placed, with [Safeguard.Withdrawn] read off
// [WithdrawalTable]. It is what the crude interface prints; a mechanism reads
// [BySubjects].
func All(ctx context.Context, pool *pgxpool.Pool) ([]Safeguard, error) {
	rows, err := pool.Query(ctx, `select id, actor_kind, actor_key, actor_key_basis, at, parameter, subject_kind,
		subject_id, subject_key, direction, bound, bound_list, predicate_kind, predicate_argument,
		route_duty, route_human_key,
		exists (select 1 from `+WithdrawalTable+` w where w.safeguard_id = `+Table+`.id and w.approved)
		from `+Table+` order by at, id`)
	if err != nil {
		return nil, fmt.Errorf("safeguard: reading the safeguards: %w", err)
	}
	defer rows.Close()

	var read []Safeguard
	for rows.Next() {
		p, err := scanWithWithdrawn(rows)
		if err != nil {
			return nil, err
		}
		read = append(read, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("safeguard: reading the safeguards: %w", err)
	}
	return read, nil
}

func scan(rows pgx.Rows) (Safeguard, error) {
	var p Safeguard
	var kind, basis, parameter, subjectKind, direction, boundList, predicateKind string
	var bound *float64
	var duty *int
	err := rows.Scan(&p.ID, &kind, &p.Actor.Key, &basis, &p.At, &parameter, &subjectKind,
		&p.Subject.ID, &p.Subject.Key, &direction, &bound, &boundList, &predicateKind, &p.Bound.Predicate.Argument,
		&duty, &p.Routing.HumanKey)
	if err != nil {
		return Safeguard{}, fmt.Errorf("safeguard: reading a safeguard: %w", err)
	}
	fill(&p, kind, basis, parameter, subjectKind, direction, boundList, predicateKind, bound, duty)
	return p, nil
}

func scanWithWithdrawn(rows pgx.Rows) (Safeguard, error) {
	var p Safeguard
	var kind, basis, parameter, subjectKind, direction, boundList, predicateKind string
	var bound *float64
	var duty *int
	err := rows.Scan(&p.ID, &kind, &p.Actor.Key, &basis, &p.At, &parameter, &subjectKind,
		&p.Subject.ID, &p.Subject.Key, &direction, &bound, &boundList, &predicateKind, &p.Bound.Predicate.Argument,
		&duty, &p.Routing.HumanKey, &p.Withdrawn)
	if err != nil {
		return Safeguard{}, fmt.Errorf("safeguard: reading a safeguard: %w", err)
	}
	fill(&p, kind, basis, parameter, subjectKind, direction, boundList, predicateKind, bound, duty)
	return p, nil
}

// fill sets the fields common to [scan] and [scanWithWithdrawn] once decoded,
// so the two queries' extra column is the only thing that differs between
// them.
func fill(p *Safeguard, kind, basis, parameter, subjectKind, direction, boundList, predicateKind string,
	bound *float64, duty *int) {
	p.Actor.Kind = record.Kind(kind)
	p.Actor.Basis = record.Basis(basis)
	p.Parameter = gatepolicy.Parameter(parameter)
	p.Subject.Kind = SubjectKind(subjectKind)
	p.Direction = gatepolicy.Direction(direction)
	p.Bound.Predicate.Kind = gatepolicy.PredicateKind(predicateKind)
	if bound != nil {
		p.Bound.Number = *bound
	}
	if boundList != "" {
		p.Bound.List = strings.Split(boundList, "\n")
	}
	if duty != nil {
		p.Routing.Duty = *duty
	}
}
