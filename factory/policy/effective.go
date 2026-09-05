package policy

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/safeguard"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/service"
)

// Effective is one parameter as it is in force: where the value came from, the
// value, the safeguards that clamped it, and what reads it.
type Effective struct {
	Parameter gatepolicy.Parameter
	Row       string
	Source    Source
	Number    float64
	List      []string
	// Safeguards are the ids of the safeguards in force on this parameter for
	// these subjects, whether or not they moved the value: a safeguard that
	// clamped nothing is still a safeguard an owner placed.
	Safeguards []string
	// Clamped is whether a safeguard actually moved the value. A safeguard is a
	// bound and not a precedence, so a safeguard narrower than the value in force
	// moves it and one wider than it does not.
	Clamped bool
	// HumanBySafeguard is whether a safeguard on this parameter adds a human,
	// which only the risk threshold's does.
	HumanBySafeguard bool
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
	case gatepolicy.AttemptLimit:
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
	case gatepolicy.AttemptLimit:
		settings, err := factorysettings.Get(ctx, r.pool)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		subject, err := factorysettings.OfStage(s.Stage)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		authored, err := factorysettings.AttemptLimit(ctx, r.pool, settings.ID, subject)
		return authored, nil, err
	case gatepolicy.AllowedPredicateKinds:
		settings, err := factorysettings.Get(ctx, r.pool)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		return gatepolicy.Authored{}, settings.AllowedPredicateKinds, nil
	case gatepolicy.ItemSizeTarget:
		if s.AreaID == "" {
			return gatepolicy.Authored{}, nil, nil
		}
		a, err := area.Get(ctx, r.pool, s.AreaID)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		return a.ItemSizeTarget, nil, nil
	case gatepolicy.AdvisorySeverity:
		settings, err := factorysettings.Get(ctx, r.pool)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		return settings.AdvisorySeverity, nil, nil
	case gatepolicy.HeldOutSampleRate:
		settings, err := factorysettings.Get(ctx, r.pool)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		return settings.HeldOutSampleRate, nil, nil
	case gatepolicy.ReviewSampleRate:
		if s.Duty == 0 {
			return gatepolicy.Authored{}, nil, nil
		}
		settings, err := factorysettings.Get(ctx, r.pool)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		authored, err := factorysettings.ReviewSampleRate(ctx, r.pool, settings.ID, s.Duty)
		return authored, nil, err
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
			return svc.Parameters.WindowSizeFor(gatepolicy.Quantity(s.Quantity)), nil, nil
		case gatepolicy.WindowConfidence:
			return svc.Parameters.WindowConfidence, nil, nil
		case gatepolicy.WindowPower:
			return svc.Parameters.WindowPowerFor(gatepolicy.Quantity(s.Quantity)), nil, nil
		case gatepolicy.WindowCap:
			return svc.Parameters.WindowCapSeconds, nil, nil
		case gatepolicy.WindowLimit:
			return svc.Parameters.WindowLimit, nil, nil
		case gatepolicy.ExposureBound:
			return svc.Parameters.ExposureBound, nil, nil
		}
		return gatepolicy.Authored{}, nil, fmt.Errorf("policy: nothing reads an authored %s", d.Parameter)
	}
}

// resolve is the three reads for a numeric parameter: what an owner authored,
// what the score supplies where they authored nothing, and the clamp each
// safeguard applies.
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

	safeguards, err := r.safeguardsOn(ctx, parameter, s)
	if err != nil {
		return Effective{}, err
	}
	for _, p := range safeguards {
		effective.Safeguards = append(effective.Safeguards, p.ID)
		if p.Direction == gatepolicy.DirectionAddsAHuman {
			effective.HumanBySafeguard = true
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
// list, with the factory's own value under all of them. The list of allowed
// predicate kinds is that parameter: the kinds this factory can decide are the
// floor, what an owner authored extends it, and a safeguard extends it again —
// a union at every step, because a kind of assertion added is coverage added
// and one removed would invalidate consumer contracts already ratified at a
// gate.
//
// The source says which of the three the value came from, and the factory's own is
// the answer where an owner authored nothing. It is not [FromNothing]: an
// unauthored list is the five kinds and not an empty one, so a consumer contract
// can be derived on a factory where nobody has opened gate policy.
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

	safeguards, err := r.safeguardsOn(ctx, parameter, s)
	if err != nil {
		return Effective{}, err
	}
	for _, p := range safeguards {
		effective.Safeguards = append(effective.Safeguards, p.ID)
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
	if parameter == gatepolicy.AllowedPredicateKinds {
		return gatepolicy.AllowedPredicateKindNames()
	}
	return nil
}

// safeguardsOn is every safeguard in force on one parameter across every subject these
// subjects reach: the gate row, the factory-wide settings record, the service, and each
// area in the chain. Which of them can hold a safeguard for which parameter is not
// enumerated — a safeguard drawn on a subject a parameter's mechanism never reads
// applies to nothing, which is the dangling safeguard the design already accounts for,
// and enumerating it here would be a second table able to disagree with gatepolicy's.
func (r *Reader) safeguardsOn(ctx context.Context, parameter gatepolicy.Parameter, s Subjects) ([]safeguard.Safeguard, error) {
	var subjects []safeguard.Subject
	// The design keeps a gate row out of the safeguard subject kinds
	// themselves: it is carried as the parameter's own key on the service
	// subject rather than as a subject of its own, so a row-scoped safeguard
	// needs a service to be keyed on.
	if s.ServiceID != "" {
		subjects = append(subjects, safeguard.Subject{Kind: safeguard.SubjectService, ID: s.ServiceID})
		if s.GateRow != "" {
			subjects = append(subjects, safeguard.Subject{Kind: safeguard.SubjectService, ID: s.ServiceID, Key: s.GateRow})
		}
	}
	// The attempt limit is per stage, one of the factory's own subjects the
	// design lists directly, so a ceiling over it is drawn on the stage
	// itself: the subject and the parameter's own key name the same thing.
	if s.Stage != "" {
		subjects = append(subjects,
			safeguard.Subject{Kind: safeguard.SubjectStage, ID: string(s.Stage), Key: string(s.Stage)})
	}
	if s.AreaID != "" {
		chain, _, err := area.Chain(ctx, r.pool, s.AreaID)
		if err != nil {
			return nil, err
		}
		for _, a := range chain {
			subjects = append(subjects, safeguard.Subject{Kind: safeguard.SubjectArea, ID: a.ID})
			if s.GateRow != "" {
				subjects = append(subjects, safeguard.Subject{Kind: safeguard.SubjectArea, ID: a.ID, Key: s.GateRow})
			}
		}
	}
	// The list of allowed predicate kinds is "this section's own list", the
	// one subject the design names in its own right rather than as a
	// narrowing of a service, a project or an area; every other factory-wide
	// parameter is narrowed through the subjects above instead.
	if parameter == gatepolicy.AllowedPredicateKinds {
		settings, err := factorysettings.Get(ctx, r.pool)
		if err != nil {
			return nil, err
		}
		subjects = append(subjects, safeguard.Subject{Kind: safeguard.SubjectPredicateKindsList, ID: settings.ID})
	}

	return safeguard.BySubjects(ctx, r.pool, parameter, subjects)
}
