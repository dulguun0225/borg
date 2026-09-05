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
	"github.com/dulguun0225/borg/factory/record"
)

// SubjectKind is what a safeguard is drawn on. [SubjectKinds] is the five this
// package stores; the design's subjects are
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md.
type SubjectKind string

const (
	// SubjectService is a service record, and reaches every item on it.
	SubjectService SubjectKind = "service"
	// SubjectArea is an area record, and reaches every item whose area chain
	// crosses it.
	SubjectArea SubjectKind = "area"
	// SubjectGateRow is one of the gate rows, and reaches that row wherever it
	// fires.
	SubjectGateRow SubjectKind = "gate_row"
	// SubjectFactorySettings is the factory-wide settings record, and reaches the
	// parameters that are fields of it.
	SubjectFactorySettings SubjectKind = "factory_settings"
	// SubjectContractElement is one element of one contract, named by the
	// contract's id and the element's name — which [contract.ElementSubject]
	// composes, because the element row's own id changes at every version and a
	// safeguard has to outlive one. It reaches the predicates in force on that
	// element, and it is the subject doc.go names as the reason a safeguard is a
	// record: that record's writer is the merge queue, so an owner authoring on it
	// would be a second writer with no seam.
	SubjectContractElement SubjectKind = "contract_element"
)

// SubjectKinds is every subject kind this milestone stores. The CHECK in [DDL]
// lists the same five, and TestDDLListsEverySubjectKind fails if they stop
// agreeing.
var SubjectKinds = []SubjectKind{
	SubjectService, SubjectArea, SubjectGateRow, SubjectFactorySettings, SubjectContractElement,
}

var (
	// ErrSubjectKindUnknown is returned for a subject kind outside
	// [SubjectKinds].
	ErrSubjectKindUnknown = errors.New("safeguard: the subject is not a kind this milestone has a record for")
	// ErrSubjectIDEmpty is returned for a safeguard naming no subject.
	ErrSubjectIDEmpty = errors.New("safeguard: a safeguard names the subject it is drawn on")
	// ErrBoundRefused is returned for a bound on a parameter whose safeguard adds
	// a human rather than bounding a value, and for a bound of one of the three
	// shapes on a parameter that takes another — a list where a number belongs, a
	// number where a predicate does.
	ErrBoundRefused = errors.New("safeguard: the bound is not the shape this parameter's safeguard takes")
	// ErrBoundMissing is returned for a safeguard on a numeric or list parameter
	// with no bound. A bound of nothing would clamp nothing and read as a
	// safeguard.
	ErrBoundMissing = errors.New("safeguard: a safeguard on this parameter carries a bound")
	// ErrNotFound is returned by [Withdraw] where no safeguard has the id.
	ErrNotFound = errors.New("safeguard: no safeguard has that id")
)

// Subject is what a safeguard is drawn on: a kind and the id or name of the
// thing.
type Subject struct {
	Kind SubjectKind
	ID   string
}

func (s Subject) String() string { return string(s.Kind) + ":" + s.ID }

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
	Withdrawn bool
}

// Writer is the table's one writer: Factory.
type Writer struct {
	pool *pgxpool.Pool
}

// NewWriter returns the writer over pool.
func NewWriter(pool *pgxpool.Pool) *Writer { return &Writer{pool: pool} }

// Insert writes one safeguard inside tx. Its one caller is package policy,
// which calls it inside the transaction that appends the policy version, so the
// safeguard and the version commit together or not at all.
//
// The direction is not an argument: it is read from the parameter's definition,
// because the direction differs per parameter and points the same way in each,
// so an owner placing a safeguard chooses the subject and the bound and never
// which way the bound points.
func Insert(ctx context.Context, tx pgx.Tx, actor record.Actor, parameter gatepolicy.Parameter,
	subject Subject, bound Bound) (Safeguard, error) {
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
	if err := checkBound(definition, bound); err != nil {
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
	}
	var storedBound *float64
	if definition.Direction != gatepolicy.DirectionAddsAHuman &&
		definition.Kind != gatepolicy.KindList && definition.Kind != gatepolicy.KindPredicate {
		storedBound = &p.Bound.Number
	}
	_, err = tx.Exec(ctx, `insert into `+Table+`
		(id, actor_kind, actor_name, at, parameter, subject_kind, subject_id, direction,
		bound, bound_list, predicate_kind, predicate_argument, withdrawn)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, false)`,
		p.ID, string(p.Actor.Kind), p.Actor.Name, p.At, string(p.Parameter),
		string(p.Subject.Kind), p.Subject.ID, string(p.Direction), storedBound,
		strings.Join(bound.List, "\n"), string(bound.Predicate.Kind), bound.Predicate.Argument,
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

// Withdraw marks one safeguard withdrawn inside tx, which is what stops a
// mechanism reading it. The row stays, so a safeguard that was in force when a
// decision was taken is still readable beside it.
func Withdraw(ctx context.Context, tx pgx.Tx, id string) error {
	tag, err := tx.Exec(ctx, `update `+Table+` set withdrawn = true where id = $1`, id)
	if err != nil {
		return fmt.Errorf("safeguard: withdrawing %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return nil
}

const selectSafeguards = `select id, actor_kind, actor_name, at, parameter, subject_kind,
	subject_id, direction, bound, bound_list, predicate_kind, predicate_argument, withdrawn
	from ` + Table

// BySubjects is every safeguard in force on one parameter across any of the
// subjects, which is the one read a mechanism a safeguard binds performs. A
// withdrawn safeguard is not in force and is not returned. It takes the pool
// and not a [Writer], because reading safeguards is not a reason to be handed
// the thing that places them.
//
// The subjects are a list because a mechanism reads more than one at a time: a
// gate firing on an item reads the gate row, the item's service, and every area
// in the item's chain, and a safeguard on any of them reaches the firing.
func BySubjects(ctx context.Context, pool *pgxpool.Pool, parameter gatepolicy.Parameter, subjects []Subject) ([]Safeguard, error) {
	if len(subjects) == 0 {
		return nil, nil
	}
	kinds := make([]string, 0, len(subjects))
	ids := make([]string, 0, len(subjects))
	for _, s := range subjects {
		kinds = append(kinds, string(s.Kind))
		ids = append(ids, s.ID)
	}
	rows, err := pool.Query(ctx, selectSafeguards+`
		where parameter = $1 and not withdrawn
		and (subject_kind, subject_id) in (select * from unnest($2::text[], $3::text[]))
		order by at, id`, string(parameter), kinds, ids)
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

// All is every safeguard ever placed, withdrawn ones included, in the order
// they were placed. It is what the crude interface prints; a mechanism reads
// [BySubjects].
func All(ctx context.Context, pool *pgxpool.Pool) ([]Safeguard, error) {
	rows, err := pool.Query(ctx, selectSafeguards+` order by at, id`)
	if err != nil {
		return nil, fmt.Errorf("safeguard: reading the safeguards: %w", err)
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
		return nil, fmt.Errorf("safeguard: reading the safeguards: %w", err)
	}
	return read, nil
}

func scan(rows pgx.Rows) (Safeguard, error) {
	var p Safeguard
	var kind, parameter, subjectKind, direction, boundList, predicateKind string
	var bound *float64
	err := rows.Scan(&p.ID, &kind, &p.Actor.Name, &p.At, &parameter, &subjectKind,
		&p.Subject.ID, &direction, &bound, &boundList, &predicateKind, &p.Bound.Predicate.Argument,
		&p.Withdrawn)
	if err != nil {
		return Safeguard{}, fmt.Errorf("safeguard: reading a safeguard: %w", err)
	}
	p.Actor.Kind = record.Kind(kind)
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
	return p, nil
}
