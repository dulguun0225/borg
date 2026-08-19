package pin

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

// SubjectKind is what a pin is drawn on. doc.go says which of the design's
// subjects have a record at this milestone and why the other three are refused.
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
	// SubjectFactoryPolicy is the factory policy record, and reaches the
	// parameters that are fields of it.
	SubjectFactoryPolicy SubjectKind = "factory_policy"
)

// SubjectKinds is every subject kind this milestone stores. The CHECK in [DDL]
// lists the same four, and TestDDLListsEverySubjectKind fails if they stop
// agreeing.
var SubjectKinds = []SubjectKind{SubjectService, SubjectArea, SubjectGateRow, SubjectFactoryPolicy}

var (
	// ErrSubjectKindUnknown is returned for a subject kind outside
	// [SubjectKinds].
	ErrSubjectKindUnknown = errors.New("pin: the subject is not a kind this milestone has a record for")
	// ErrSubjectIDEmpty is returned for a pin naming no subject.
	ErrSubjectIDEmpty = errors.New("pin: a pin names the subject it is drawn on")
	// ErrBoundRefused is returned for a bound on a parameter whose pin adds a
	// human rather than bounding a value, and for a list on a numeric
	// parameter or a number on a list one.
	ErrBoundRefused = errors.New("pin: the bound is not the shape this parameter's pin takes")
	// ErrBoundMissing is returned for a numeric or list parameter pinned with
	// no bound. A bound of nothing would clamp nothing and read as a pin.
	ErrBoundMissing = errors.New("pin: a pin on this parameter carries a bound")
	// ErrNotFound is returned by [Withdraw] where no pin has the id.
	ErrNotFound = errors.New("pin: no pin has that id")
)

// Subject is what a pin is drawn on: a kind and the id or name of the thing.
type Subject struct {
	Kind SubjectKind
	ID   string
}

func (s Subject) String() string { return string(s.Kind) + ":" + s.ID }

// Pin is one pin as it is stored.
type Pin struct {
	ID        string
	Actor     record.Actor
	At        string
	Parameter gatepolicy.Parameter
	Subject   Subject
	Direction gatepolicy.Direction
	// Bound is the number the pin bounds the value in force by, and is
	// meaningless where the direction adds a human or the parameter is a list.
	Bound float64
	// BoundList is the names a pin on a list-valued parameter adds.
	BoundList []string
	Withdrawn bool
}

// Writer is the table's one writer: Factory.
type Writer struct {
	pool *pgxpool.Pool
}

// NewWriter returns the writer over pool.
func NewWriter(pool *pgxpool.Pool) *Writer { return &Writer{pool: pool} }

// Insert writes one pin inside tx. Its one caller is package policy, which
// calls it inside the transaction that appends the policy version, so the pin
// and the version commit together or not at all.
//
// The direction is not an argument: it is read from the parameter's definition,
// because the direction differs per parameter and points the same way in each,
// so an owner placing a pin chooses the subject and the bound and never which
// way the bound points.
func Insert(ctx context.Context, tx pgx.Tx, actor record.Actor, parameter gatepolicy.Parameter,
	subject Subject, bound float64, boundList []string) (Pin, error) {
	if err := actor.Validate(); err != nil {
		return Pin{}, err
	}
	definition, err := gatepolicy.Define(parameter)
	if err != nil {
		return Pin{}, err
	}
	if !slices.Contains(SubjectKinds, subject.Kind) {
		return Pin{}, fmt.Errorf("%w: %q", ErrSubjectKindUnknown, subject.Kind)
	}
	if subject.ID == "" {
		return Pin{}, ErrSubjectIDEmpty
	}
	if err := checkBound(definition, bound, boundList); err != nil {
		return Pin{}, err
	}

	p := Pin{
		ID:        record.NewID(IDPrefix),
		Actor:     actor,
		At:        record.Now(),
		Parameter: parameter,
		Subject:   subject,
		Direction: definition.Direction,
		Bound:     bound,
		BoundList: boundList,
	}
	var storedBound *float64
	if definition.Direction != gatepolicy.DirectionAddsAHuman && definition.Kind != gatepolicy.KindList {
		storedBound = &p.Bound
	}
	_, err = tx.Exec(ctx, `insert into `+Table+`
		(id, actor_kind, actor_name, at, parameter, subject_kind, subject_id, direction, bound, bound_list, withdrawn)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, false)`,
		p.ID, string(p.Actor.Kind), p.Actor.Name, p.At, string(p.Parameter),
		string(p.Subject.Kind), p.Subject.ID, string(p.Direction), storedBound,
		strings.Join(boundList, "\n"),
	)
	if err != nil {
		return Pin{}, fmt.Errorf("pin: placing a pin on %s at %s: %w", parameter, subject, err)
	}
	return p, nil
}

// checkBound refuses a bound of the wrong shape for the parameter: a bound on a
// pin that adds a human, a list where a number belongs or the reverse, and a
// missing bound where one is required.
func checkBound(d gatepolicy.Definition, bound float64, boundList []string) error {
	switch {
	case d.Direction == gatepolicy.DirectionAddsAHuman:
		if bound != 0 || len(boundList) > 0 {
			return fmt.Errorf("%w: a pin on %s adds a human and bounds no value", ErrBoundRefused, d.Parameter)
		}
	case d.Kind == gatepolicy.KindList:
		if bound != 0 {
			return fmt.Errorf("%w: %s is a list and its bound is a list", ErrBoundRefused, d.Parameter)
		}
		if len(boundList) == 0 {
			return fmt.Errorf("%w: %s", ErrBoundMissing, d.Parameter)
		}
	default:
		if len(boundList) > 0 {
			return fmt.Errorf("%w: %s is a number and its bound is a number", ErrBoundRefused, d.Parameter)
		}
		if bound <= 0 {
			return fmt.Errorf("%w: %s", ErrBoundMissing, d.Parameter)
		}
	}
	return nil
}

// Withdraw marks one pin withdrawn inside tx, which is what stops a mechanism
// reading it. The row stays, so a pin that was in force when a decision was
// taken is still readable beside it.
func Withdraw(ctx context.Context, tx pgx.Tx, id string) error {
	tag, err := tx.Exec(ctx, `update `+Table+` set withdrawn = true where id = $1`, id)
	if err != nil {
		return fmt.Errorf("pin: withdrawing %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return nil
}

const selectPins = `select id, actor_kind, actor_name, at, parameter, subject_kind,
	subject_id, direction, bound, bound_list, withdrawn
	from ` + Table

// BySubjects is every pin in force on one parameter across any of the subjects,
// which is the one read a mechanism a pin binds performs. A withdrawn pin is
// not in force and is not returned. It takes the pool and not a [Writer],
// because reading pins is not a reason to be handed the thing that places them.
//
// The subjects are a list because a mechanism reads more than one at a time: a
// gate firing on an item reads the gate row, the item's service, and every area
// in the item's chain, and a pin on any of them reaches the firing.
func BySubjects(ctx context.Context, pool *pgxpool.Pool, parameter gatepolicy.Parameter, subjects []Subject) ([]Pin, error) {
	if len(subjects) == 0 {
		return nil, nil
	}
	kinds := make([]string, 0, len(subjects))
	ids := make([]string, 0, len(subjects))
	for _, s := range subjects {
		kinds = append(kinds, string(s.Kind))
		ids = append(ids, s.ID)
	}
	rows, err := pool.Query(ctx, selectPins+`
		where parameter = $1 and not withdrawn
		and (subject_kind, subject_id) in (select * from unnest($2::text[], $3::text[]))
		order by at, id`, string(parameter), kinds, ids)
	if err != nil {
		return nil, fmt.Errorf("pin: reading the pins on %s: %w", parameter, err)
	}
	defer rows.Close()

	var read []Pin
	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			return nil, err
		}
		read = append(read, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pin: reading the pins on %s: %w", parameter, err)
	}
	return read, nil
}

// All is every pin ever placed, withdrawn ones included, in the order they were
// placed. It is what the crude interface prints; a mechanism reads
// [BySubjects].
func All(ctx context.Context, pool *pgxpool.Pool) ([]Pin, error) {
	rows, err := pool.Query(ctx, selectPins+` order by at, id`)
	if err != nil {
		return nil, fmt.Errorf("pin: reading the pins: %w", err)
	}
	defer rows.Close()

	var read []Pin
	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			return nil, err
		}
		read = append(read, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pin: reading the pins: %w", err)
	}
	return read, nil
}

func scan(rows pgx.Rows) (Pin, error) {
	var p Pin
	var kind, parameter, subjectKind, direction, boundList string
	var bound *float64
	err := rows.Scan(&p.ID, &kind, &p.Actor.Name, &p.At, &parameter, &subjectKind,
		&p.Subject.ID, &direction, &bound, &boundList, &p.Withdrawn)
	if err != nil {
		return Pin{}, fmt.Errorf("pin: reading a pin: %w", err)
	}
	p.Actor.Kind = record.Kind(kind)
	p.Parameter = gatepolicy.Parameter(parameter)
	p.Subject.Kind = SubjectKind(subjectKind)
	p.Direction = gatepolicy.Direction(direction)
	if bound != nil {
		p.Bound = *bound
	}
	if boundList != "" {
		p.BoundList = strings.Split(boundList, "\n")
	}
	return p, nil
}
