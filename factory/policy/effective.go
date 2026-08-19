package policy

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorypolicy"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/pin"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/service"
)

// Source is where the value in force came from before any pin clamped it.
type Source string

const (
	// FromAuthored is a value an owner authored on the record its scope names.
	FromAuthored Source = "authored"
	// FromSupplied is what the score supplies where an owner authored nothing.
	FromSupplied Source = "supplied"
	// FromNothing is neither, which is the predicate catalog on a factory where
	// an owner has extended nothing: the score supplies no catalog, so an
	// unauthored one is empty rather than defaulted.
	FromNothing Source = "neither"
)

// Reader answers what is in force. It is a type rather than a set of functions
// so that a gate can hold one behind an interface and a test can hold a fake.
type Reader struct {
	pool *pgxpool.Pool
}

// NewReader returns the reader over pool.
func NewReader(pool *pgxpool.Pool) *Reader { return &Reader{pool: pool} }

// Subjects is what a read is performed against: the records whose fields hold
// each parameter, and through them the subjects a pin may be drawn on. A field
// left empty is a record the caller has none of, and the parameters scoped to it
// resolve to what the score supplies with no authored value to find.
type Subjects struct {
	GateRow       string
	EnvironmentID string
	ServiceID     string
	// AreaID is the narrowest area; the chain above it is walked, because a pin
	// drawn on any area in the chain reaches an item in the narrowest.
	AreaID string
	Stage  item.Stage
}

// Applied is what a gate firing applied: the policy version it was decided
// under, the threshold in force and where it came from, and whether a pin put a
// human at the row. It is written onto the opening row, which is what makes a
// decision readable against the policy it was taken under rather than against
// today's.
type Applied struct {
	PolicyVersion string
	Threshold     float64
	ThresholdFrom Source
	// HumanPinned is whether a pin adds a human at this row. A pin on the risk
	// threshold adds a human rather than moving the number, so this is the whole
	// of what such a pin does.
	HumanPinned bool
	// Pins are the ids of the pins that applied, so a reader of the decision can
	// follow them to the records rather than being told a number moved.
	Pins []string
}

// AtGate is what applies at one gate firing: the threshold in force for the row
// and whether a pin adds a human. Both reads run at the moment of firing, which
// is what the design requires of every check a gate makes.
func (r *Reader) AtGate(ctx context.Context, s Subjects) (Applied, error) {
	version, err := InForce(ctx, r.pool)
	if err != nil {
		return Applied{}, err
	}

	authored := gatepolicy.Authored{}
	if s.EnvironmentID != "" && s.GateRow != "" {
		authored, err = environment.GateThreshold(ctx, r.pool, s.EnvironmentID, s.GateRow)
		if err != nil {
			return Applied{}, err
		}
	}
	supplied, _ := score.Supplied(gatepolicy.RiskThreshold)

	pins, err := r.pinsOn(ctx, gatepolicy.RiskThreshold, s)
	if err != nil {
		return Applied{}, err
	}

	applied := Applied{
		PolicyVersion: version.ID,
		Threshold:     authored.Or(supplied),
		ThresholdFrom: sourceOf(authored),
	}
	for _, p := range pins {
		applied.HumanPinned = true
		applied.Pins = append(applied.Pins, p.ID)
	}
	return applied, nil
}

// AttemptBound is how many attempts one stage gets: what an owner authored on
// the factory policy record, the score's supplied value where they authored
// none, and a pinned ceiling over either.
func (r *Reader) AttemptBound(ctx context.Context, s Subjects) (Effective, error) {
	policyRecord, err := factorypolicy.Get(ctx, r.pool)
	if err != nil {
		return Effective{}, err
	}
	authored, err := factorypolicy.AttemptBound(ctx, r.pool, policyRecord.ID, s.Stage)
	if err != nil {
		return Effective{}, err
	}
	return r.resolve(ctx, gatepolicy.AttemptBound, authored, s)
}

// Effective is one parameter as it is in force: where the value came from, the
// value, the pins that clamped it, and what reads it.
type Effective struct {
	Parameter gatepolicy.Parameter
	Row       string
	Source    Source
	Number    float64
	List      []string
	// Pins are the ids of the pins in force on this parameter for these
	// subjects, whether or not they moved the value: a pin that clamped nothing
	// is still a pin an owner placed.
	Pins []string
	// Clamped is whether a pin actually moved the value. A pin is a bound and
	// not a precedence, so a pin narrower than the value in force moves it and
	// one wider than it does not.
	Clamped bool
	// HumanPinned is whether a pin on this parameter adds a human, which only
	// the risk threshold's does.
	HumanPinned bool
	// ReadBy is the mechanism that reads the value at this milestone, and is
	// empty for a parameter nothing reads yet.
	ReadBy string
}

// All is every parameter as it is in force against these subjects, in the order
// gate policy's own table lists the rows. It is what the crude interface prints,
// and it is the one place an owner can see that four of the eight are read by
// nothing yet.
func (r *Reader) All(ctx context.Context, s Subjects) ([]Effective, error) {
	var all []Effective
	for _, d := range gatepolicy.Definitions {
		authored, list, err := r.authored(ctx, d, s)
		if err != nil {
			return nil, err
		}
		if d.Kind == gatepolicy.KindList {
			effective, err := r.resolveList(ctx, d.Parameter, list, s)
			if err != nil {
				return nil, err
			}
			all = append(all, effective)
			continue
		}
		effective, err := r.resolve(ctx, d.Parameter, authored, s)
		if err != nil {
			return nil, err
		}
		all = append(all, effective)
	}
	return all, nil
}

// authored reads what an owner authored for one parameter from the record its
// scope names. A record the subjects do not name reads as unauthored, which is
// the same answer as a field nobody wrote.
func (r *Reader) authored(ctx context.Context, d gatepolicy.Definition, s Subjects) (gatepolicy.Authored, []string, error) {
	switch d.Parameter {
	case gatepolicy.RiskThreshold:
		if s.EnvironmentID == "" || s.GateRow == "" {
			return gatepolicy.Authored{}, nil, nil
		}
		authored, err := environment.GateThreshold(ctx, r.pool, s.EnvironmentID, s.GateRow)
		return authored, nil, err
	case gatepolicy.AttemptBound:
		policyRecord, err := factorypolicy.Get(ctx, r.pool)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		authored, err := factorypolicy.AttemptBound(ctx, r.pool, policyRecord.ID, s.Stage)
		return authored, nil, err
	case gatepolicy.PredicateCatalog:
		policyRecord, err := factorypolicy.Get(ctx, r.pool)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		return gatepolicy.Authored{}, policyRecord.PredicateCatalog, nil
	case gatepolicy.ItemSizeTarget:
		if s.AreaID == "" {
			return gatepolicy.Authored{}, nil, nil
		}
		a, err := area.Get(ctx, r.pool, s.AreaID)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		return a.ItemSizeTarget, nil, nil
	default:
		if s.ServiceID == "" {
			return gatepolicy.Authored{}, nil, nil
		}
		svc, err := service.Get(ctx, r.pool, s.ServiceID)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		switch d.Parameter {
		case gatepolicy.WindowSize:
			return svc.Parameters.WindowSize, nil, nil
		case gatepolicy.WindowConfidence:
			return svc.Parameters.WindowConfidence, nil, nil
		case gatepolicy.WindowCap:
			return svc.Parameters.WindowCapSeconds, nil, nil
		case gatepolicy.K:
			return svc.Parameters.K, nil, nil
		}
		return gatepolicy.Authored{}, nil, fmt.Errorf("policy: nothing reads an authored %s", d.Parameter)
	}
}

// resolve is the three reads for a numeric parameter: what an owner authored,
// what the score supplies where they authored nothing, and the clamp each pin
// applies.
func (r *Reader) resolve(ctx context.Context, parameter gatepolicy.Parameter,
	authored gatepolicy.Authored, s Subjects) (Effective, error) {
	definition, err := gatepolicy.Define(parameter)
	if err != nil {
		return Effective{}, err
	}
	supplied, hasSupplied := score.Supplied(parameter)
	effective := Effective{
		Parameter: parameter,
		Row:       definition.Row,
		Source:    sourceOf(authored),
		Number:    authored.Or(supplied),
		ReadBy:    definition.ReaderAtThisMilestone,
	}
	if !authored.Present && !hasSupplied {
		effective.Source = FromNothing
	}

	pins, err := r.pinsOn(ctx, parameter, s)
	if err != nil {
		return Effective{}, err
	}
	for _, p := range pins {
		effective.Pins = append(effective.Pins, p.ID)
		if p.Direction == gatepolicy.DirectionAddsAHuman {
			effective.HumanPinned = true
			continue
		}
		clamped := gatepolicy.Clamp(p.Direction, p.Bound, effective.Number)
		if clamped != effective.Number {
			effective.Clamped = true
			effective.Number = clamped
		}
	}
	return effective, nil
}

// resolveList is the same three reads for the one parameter whose value is a
// list. The score supplies no catalog, so an unauthored one is empty, and a pin
// may only extend whatever it is.
func (r *Reader) resolveList(ctx context.Context, parameter gatepolicy.Parameter,
	authored []string, s Subjects) (Effective, error) {
	definition, err := gatepolicy.Define(parameter)
	if err != nil {
		return Effective{}, err
	}
	effective := Effective{
		Parameter: parameter,
		Row:       definition.Row,
		Source:    FromNothing,
		List:      authored,
		ReadBy:    definition.ReaderAtThisMilestone,
	}
	if len(authored) > 0 {
		effective.Source = FromAuthored
	}

	pins, err := r.pinsOn(ctx, parameter, s)
	if err != nil {
		return Effective{}, err
	}
	for _, p := range pins {
		effective.Pins = append(effective.Pins, p.ID)
		extended := gatepolicy.ClampList(p.BoundList, effective.List)
		if len(extended) != len(effective.List) {
			effective.Clamped = true
		}
		effective.List = extended
	}
	return effective, nil
}

// pinsOn is every pin in force on one parameter across every subject these
// subjects reach: the gate row, the factory policy record, the service, and each
// area in the chain. Which of them can hold a pin for which parameter is not
// enumerated — a pin drawn on a subject a parameter's mechanism never reads
// applies to nothing, which is the dangling pin the design already accounts for,
// and enumerating it here would be a second table able to disagree with
// gatepolicy's.
func (r *Reader) pinsOn(ctx context.Context, parameter gatepolicy.Parameter, s Subjects) ([]pin.Pin, error) {
	var subjects []pin.Subject
	if s.GateRow != "" {
		subjects = append(subjects, pin.Subject{Kind: pin.SubjectGateRow, ID: s.GateRow})
	}
	if s.ServiceID != "" {
		subjects = append(subjects, pin.Subject{Kind: pin.SubjectService, ID: s.ServiceID})
	}
	if s.AreaID != "" {
		chain, err := area.Chain(ctx, r.pool, s.AreaID)
		if err != nil {
			return nil, err
		}
		for _, a := range chain {
			subjects = append(subjects, pin.Subject{Kind: pin.SubjectArea, ID: a.ID})
		}
	}
	policyRecord, err := factorypolicy.Get(ctx, r.pool)
	if err != nil {
		return nil, err
	}
	subjects = append(subjects, pin.Subject{Kind: pin.SubjectFactoryPolicy, ID: policyRecord.ID})

	return pin.BySubjects(ctx, r.pool, parameter, subjects)
}

func sourceOf(authored gatepolicy.Authored) Source {
	if authored.Present {
		return FromAuthored
	}
	return FromSupplied
}
