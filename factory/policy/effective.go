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
	"github.com/dulguun0225/borg/factory/record"
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
	// FromNothing is neither an authored value nor a supplied one, which is a
	// numeric parameter the score supplies nothing for. Nothing reaches it today.
	FromNothing Source = "neither"
	// FromFactory is the factory's own value, which is what an owner extends
	// rather than replaces. The predicate catalog is the one parameter with this
	// source: gate policy has an owner extend the catalog and a pin only add to
	// it, which presupposes something to extend, and the score supplies none —
	// no outcome teaches a kind of assertion. So the unauthored value is the kinds
	// this factory can decide.
	FromFactory Source = "the factory's own"
)

// Reader answers what is in force. It is a type rather than a set of functions
// so that a gate can hold one behind an interface and a test can hold a fake.
type Reader struct {
	pool *pgxpool.Pool
	// score is the score version the supplied half of every answer is read out
	// of. It is held rather than read per answer because a supplied value moves
	// as outcomes arrive: a reader that read the newest version at each resolve
	// could give one gate firing a threshold from one version and a decision row
	// naming another, and the row would not be readable against the policy it was
	// decided under.
	score score.Version
}

// NewReader returns the reader over pool, reading what the score supplies out of
// version. The zero version is the starting values — the numbers the formula was
// calibrated at — which is what a factory that has appended no version yet
// supplies, so a reader composed before the first ensure answers with those and
// not with nothing.
func NewReader(pool *pgxpool.Pool, version score.Version) *Reader {
	return &Reader{pool: pool, score: version}
}

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
	// Supplied is the score's own row behind the threshold where the score
	// supplied it, and is empty where an owner authored one. It is what a firing
	// prints beside the number so that an owner reading a gate can see which
	// outcomes moved the threshold it was compared against.
	Supplied score.Supplied
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
	supplied, _ := r.score.Value(gatepolicy.RiskThreshold, s.GateRow)

	pins, err := r.pinsOn(ctx, gatepolicy.RiskThreshold, s)
	if err != nil {
		return Applied{}, err
	}

	applied := Applied{
		PolicyVersion: version.ID,
		Threshold:     authored.Or(supplied.Value),
		ThresholdFrom: sourceOf(authored),
	}
	if !authored.Present {
		applied.Supplied = supplied
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

// Window is the four parameters the watch window reads, each in force against one
// service: what an owner authored where they authored one, what the score supplies
// where they did not, and a pin clamping either. The four are read together because
// a window resolves all of them at the open and copies them onto its record — a
// read per parameter would let an owner's write land between two of them and give
// one window a size and a confidence that were never in force at the same moment.
type Window struct {
	Size       Effective
	Confidence Effective
	CapSeconds Effective
	K          Effective
}

// WindowParameters is those four for one service. It is a read of its own rather
// than a filter over [Reader.All], because All is a printer's answer over every
// subject a firing names and this is the comparison's over one service.
func (r *Reader) WindowParameters(ctx context.Context, serviceID string) (Window, error) {
	if serviceID == "" {
		return Window{}, fmt.Errorf("policy: the watch window's parameters are per service, and none is named")
	}
	s := Subjects{ServiceID: serviceID}
	var w Window
	for _, of := range []struct {
		parameter gatepolicy.Parameter
		into      *Effective
	}{
		{gatepolicy.WindowSize, &w.Size},
		{gatepolicy.WindowConfidence, &w.Confidence},
		{gatepolicy.WindowCap, &w.CapSeconds},
		{gatepolicy.K, &w.K},
	} {
		definition, err := gatepolicy.Define(of.parameter)
		if err != nil {
			return Window{}, err
		}
		authored, _, err := r.authored(ctx, definition, s)
		if err != nil {
			return Window{}, err
		}
		effective, err := r.resolve(ctx, of.parameter, authored, s)
		if err != nil {
			return Window{}, err
		}
		*of.into = effective
	}
	return w, nil
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
	// Supplied is the score's own row behind a value whose source is the score:
	// the subject it was supplied for and why that number. It is empty where an
	// owner authored the value, because then nothing the score supplies is in
	// force, and it is what lets a printer say which outcomes moved a number
	// rather than only that the score set it.
	Supplied score.Supplied
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

// suppliedSubject is the subject the score supplies a value for, which is the
// same key the authored value has and is read off the same [Subjects]: the
// service, the area, the stage. The risk threshold is the one that differs — its
// authored value is a field of an environment record per gate row and what the
// score supplies is per row alone, because what an outcome teaches is about the
// row and every row of the default path reads production's environment anyway.
//
// A subject the caller named nothing for is empty, and an empty subject reads the
// starting value: a firing that named no service gets the number the formula was
// calibrated at rather than one learned about some other service.
func suppliedSubject(d gatepolicy.Definition, s Subjects) string {
	switch d.Parameter {
	case gatepolicy.RiskThreshold:
		return s.GateRow
	case gatepolicy.AttemptBound:
		return string(s.Stage)
	case gatepolicy.ItemSizeTarget:
		return s.AreaID
	default:
		if d.Scope == gatepolicy.ScopeService {
			return s.ServiceID
		}
		return ""
	}
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
	supplied, hasSupplied := r.score.Value(parameter, suppliedSubject(definition, s))
	effective := Effective{
		Parameter: parameter,
		Row:       definition.Row,
		Source:    sourceOf(authored),
		Number:    authored.Or(supplied.Value),
		ReadBy:    definition.ReaderAtThisMilestone,
	}
	if !authored.Present && hasSupplied {
		effective.Supplied = supplied
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
		clamped := gatepolicy.Clamp(p.Direction, p.Bound.Number, effective.Number)
		if clamped != effective.Number {
			effective.Clamped = true
			effective.Number = clamped
		}
	}
	return effective, nil
}

// resolveList is the same three reads for the one parameter whose value is a
// list, with the factory's own value under all of them. The predicate catalog is
// that parameter: the kinds this factory can decide are the floor, what an owner
// authored extends it, and a pin extends it again — a union at every step, because
// a kind of assertion added is coverage added and one removed would invalidate
// declarations already ratified at a gate.
//
// The source says which of the three the value came from, and the factory's own is
// the answer where an owner authored nothing. It is not [FromNothing]: an
// unauthored catalog is the five kinds and not an empty list, so a declaration can
// be derived on a factory where nobody has opened gate policy.
func (r *Reader) resolveList(ctx context.Context, parameter gatepolicy.Parameter,
	authored []string, s Subjects) (Effective, error) {
	definition, err := gatepolicy.Define(parameter)
	if err != nil {
		return Effective{}, err
	}
	own := factoryOwn(parameter)
	effective := Effective{
		Parameter: parameter,
		Row:       definition.Row,
		Source:    FromFactory,
		List:      gatepolicy.ClampList(authored, own),
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
		extended := gatepolicy.ClampList(p.Bound.List, effective.List)
		if len(extended) != len(effective.List) {
			effective.Clamped = true
		}
		effective.List = extended
	}
	return effective, nil
}

// factoryOwn is the value the factory itself provides for a list-valued
// parameter, under whatever an owner authored. There is one such parameter and one
// such value: the predicate kinds package gatepolicy names, which are the ones
// enforcement can decide.
func factoryOwn(parameter gatepolicy.Parameter) []string {
	if parameter == gatepolicy.PredicateCatalog {
		return gatepolicy.PredicateCatalogNames()
	}
	return nil
}

// PredicateCatalog is the catalog in force, which is the one read package
// declaration's derivation performs against gate policy: the kinds a consumer may
// draw from. It is a read of its own rather than a filter over [Reader.All] for the
// reason [Reader.WindowParameters] is — All is a printer's answer over every
// parameter and this is one mechanism's over one.
//
// The subjects are the factory policy record's and nothing else, which [pinsOn]
// adds on its own: the catalog is one list the factory owns, so a pin on a service
// or an area is a pin on a subject this parameter's mechanism never reads — the
// dangling pin the design already accounts for.
func (r *Reader) PredicateCatalog(ctx context.Context) (Effective, error) {
	definition, err := gatepolicy.Define(gatepolicy.PredicateCatalog)
	if err != nil {
		return Effective{}, err
	}
	_, authored, err := r.authored(ctx, definition, Subjects{})
	if err != nil {
		return Effective{}, err
	}
	return r.resolveList(ctx, gatepolicy.PredicateCatalog, authored, Subjects{})
}

// PinnedPredicate is one pinned predicate as a mechanism reads it: the pin, who
// placed it, the element it is about, and the assertion. The author is here because
// a removal item blocked on a pin appears as an escalation naming the pin and its
// author, and a reader of that escalation has to know whom to ask.
type PinnedPredicate struct {
	PinID   string
	Actor   record.Actor
	Subject string
	Kind    gatepolicy.PredicateKind
	// Argument is the unit, the domain, or the range, and is empty for a kind
	// that takes none.
	Argument string
}

// PinnedPredicatesOn is every pinned predicate in force on any of these contract
// elements, each named the way [pin.SubjectContractElement] names one. It is the
// read enforcement and the deprecation list both perform, and it is here rather
// than in either of them because package pin has one reader and this is it.
//
// A withdrawn pin is not in force and is not returned, which is what makes
// withdrawing one the way an owner takes an invented read back.
func (r *Reader) PinnedPredicatesOn(ctx context.Context, subjects []string) ([]PinnedPredicate, error) {
	if len(subjects) == 0 {
		return nil, nil
	}
	on := make([]pin.Subject, 0, len(subjects))
	for _, s := range subjects {
		if s != "" {
			on = append(on, pin.Subject{Kind: pin.SubjectContractElement, ID: s})
		}
	}
	pins, err := pin.BySubjects(ctx, r.pool, gatepolicy.PinnedPredicate, on)
	if err != nil {
		return nil, err
	}
	pinned := make([]PinnedPredicate, 0, len(pins))
	for _, p := range pins {
		pinned = append(pinned, PinnedPredicate{
			PinID:    p.ID,
			Actor:    p.Actor,
			Subject:  p.Subject.ID,
			Kind:     p.Bound.Predicate.Kind,
			Argument: p.Bound.Predicate.Argument,
		})
	}
	return pinned, nil
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
