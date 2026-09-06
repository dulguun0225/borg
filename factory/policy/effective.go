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
		// Two scopes and no third, which [Reader.authoredThreshold] answers
		// over: the environment record per row, and the factory-wide settings
		// record for the one row with no environment.
		authored, _, err := r.authoredThreshold(ctx, s)
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
		if d.Scope == gatepolicy.ScopeFactorySettings {
			return r.authoredOnSettings(ctx, d, s)
		}
		if d.Scope == gatepolicy.ScopeEnvironment {
			if s.EnvironmentID == "" {
				return gatepolicy.Authored{}, nil, nil
			}
			e, err := environment.Get(ctx, r.pool, s.EnvironmentID)
			if err != nil {
				return gatepolicy.Authored{}, nil, err
			}
			if d.Parameter == gatepolicy.StrategyDefault {
				if e.StrategyDefault == "" {
					return gatepolicy.Authored{}, nil, nil
				}
				return gatepolicy.Authored{}, []string{string(e.StrategyDefault)}, nil
			}
			return gatepolicy.Authored{
				Number:  float64(e.MaxConcurrentCandidateEnvironments),
				Present: e.MaxConcurrentCandidateEnvironments > 0,
			}, nil, nil
		}
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
		case gatepolicy.ChangeFreeze:
			// The freeze is periods, plural, and each is a row rather than a
			// field, so it is read here where the pool is rather than off the
			// record already in hand.
			periods, err := service.FreezePeriods(ctx, r.pool, s.ServiceID)
			if err != nil {
				return gatepolicy.Authored{}, nil, err
			}
			named := make([]string, 0, len(periods))
			for _, p := range periods {
				named = append(named, p.StartsAt+" "+p.EndsAt)
			}
			return gatepolicy.Authored{}, named, nil
		}
		return authoredBesideTheEleven(d, s, svc)
	}
}

// authoredBesideTheEleven is what an owner authored on the service record that
// is not one of gate policy's eleven rows: the twelve the design names there and
// the values authored beside them. Each is a field of the record already read,
// the paging hours being read as a list rather than a number; the change
// freeze's periods are rows and are read where the pool is.
func authoredBesideTheEleven(d gatepolicy.Definition, s Subjects,
	svc service.Service) (gatepolicy.Authored, []string, error) {
	switch d.Parameter {
	case gatepolicy.BakeVolume:
		return svc.BakeVolume, nil, nil
	case gatepolicy.BacklogCap:
		return svc.BacklogCap, nil, nil
	case gatepolicy.MutationFloor:
		return svc.MutationFloor, nil, nil
	case gatepolicy.SearchBudget:
		return svc.SearchBudgetBuilds, nil, nil
	case gatepolicy.KeptFraction:
		return svc.KeptFraction, nil, nil
	case gatepolicy.MaxConcurrentKeptFleets:
		return svc.MaxConcurrentKeptFleets, nil, nil
	case gatepolicy.RecentHistorySize:
		return svc.RecentHistorySize[gatepolicy.Quantity(s.Quantity)], nil, nil
	case gatepolicy.RecentHistoryRunLength:
		return svc.RecentHistoryRunLength, nil, nil
	case gatepolicy.Objective:
		return svc.Objective.Target, nil, nil
	case gatepolicy.PagingHours:
		if !svc.PagingHours.Authored() {
			return gatepolicy.Authored{}, nil, nil
		}
		return gatepolicy.Authored{}, []string{svc.PagingHours.Start, svc.PagingHours.End, svc.PagingHours.Zone}, nil
	case gatepolicy.ProofTestRate:
		return svc.ProofTestRate, nil, nil
	case gatepolicy.InstanceHourRate:
		return svc.InstanceHourRate, nil, nil
	case gatepolicy.EnvironmentHourRate:
		return svc.EnvironmentHourRate, nil, nil
	case gatepolicy.OperationCap:
		return svc.OperationCap, nil, nil
	case gatepolicy.MutantCap:
		return svc.MutantCap, nil, nil
	case gatepolicy.FailureRecordKeyCap:
		return svc.FailureRecordKeyCap, nil, nil
	case gatepolicy.UnreliableBound:
		return svc.UnreliableBound, nil, nil
	case gatepolicy.IncidentItemBound:
		return svc.IncidentItemBoundSeconds, nil, nil
	case gatepolicy.SnapshotRetention:
		return svc.SnapshotRetentionSeconds, nil, nil
	case gatepolicy.ExplicitThreshold:
		threshold, held := svc.ExplicitThreshold[gatepolicy.Quantity(s.Quantity)]
		return gatepolicy.Authored{Number: threshold.Number, Present: held}, nil, nil
	case gatepolicy.ExplicitThresholdSize:
		threshold, held := svc.ExplicitThreshold[gatepolicy.Quantity(s.Quantity)]
		return gatepolicy.Authored{Number: threshold.Size, Present: held}, nil, nil
	}
	return gatepolicy.Authored{}, nil, fmt.Errorf("policy: nothing reads an authored %s", d.Parameter)
}

// authoredOnSettings is what an owner authored on the factory-wide settings
// record that is not one of the rows the switch above answers: the retentions,
// the retention floor, the remediation period per severity, the report
// channel's rate per service and factory-wide, and the harm mark's page cap.
// Each is a field of that record, and the keyed ones are read at the key these
// subjects name.
func (r *Reader) authoredOnSettings(ctx context.Context, d gatepolicy.Definition,
	s Subjects) (gatepolicy.Authored, []string, error) {
	settings, err := factorysettings.Get(ctx, r.pool)
	if err != nil {
		return gatepolicy.Authored{}, nil, err
	}
	switch d.Parameter {
	case gatepolicy.DecisionLogRetention:
		return settings.DecisionLogRetentionSeconds, nil, nil
	case gatepolicy.ReportRetention:
		return settings.ReportRetentionSeconds, nil, nil
	case gatepolicy.BackupRetention:
		return settings.BackupRetentionSeconds, nil, nil
	case gatepolicy.RetentionFloor:
		return settings.RetentionFloorSeconds, nil, nil
	case gatepolicy.ReportChannelRate:
		if s.ServiceID == "" {
			return settings.ReportChannelRate, nil, nil
		}
		authored, err := factorysettings.ReportChannelRate(ctx, r.pool, settings.ID, s.ServiceID)
		return authored, nil, err
	case gatepolicy.RemediationPeriod:
		// One value per advisory severity, read at the severity these subjects
		// name; a read naming none finds nothing authored, the way a read
		// naming no quantity does.
		if !s.SeverityNamed {
			return gatepolicy.Authored{}, nil, nil
		}
		authored, err := factorysettings.RemediationPeriod(ctx, r.pool, settings.ID, s.Severity)
		return authored, nil, err
	case gatepolicy.HarmMarkPageCap:
		if s.ServiceID == "" {
			return gatepolicy.Authored{}, nil, nil
		}
		cap, err := factorysettings.HarmMarkPageCap(ctx, r.pool, settings.ID, s.ServiceID)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		return gatepolicy.Authored{Number: float64(cap.Cap), Present: cap.Authored}, nil, nil
	}
	return gatepolicy.Authored{}, nil, fmt.Errorf("policy: nothing reads an authored %s", d.Parameter)
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
// unauthored list is the nine kinds and not an empty one, so a consumer contract
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
	definition, err := gatepolicy.Define(parameter)
	if err != nil {
		return nil, err
	}
	// A safeguard on a keyed parameter names the value of that key, and one on
	// an unkeyed parameter names none: package safeguard refuses either the
	// other way round, so a read that asked for both would ask for a safeguard
	// that cannot exist.
	key := ""
	if definition.Key != gatepolicy.KeyNone {
		key = keyOf(definition, s)
	}
	if s.ServiceID != "" {
		subjects = append(subjects, safeguard.Subject{Kind: safeguard.SubjectService, ID: s.ServiceID, Key: key})
	}
	// A project is a subject of its own: a safeguard on one reaches its
	// persistent environment and every service in it.
	if s.ProjectID != "" {
		subjects = append(subjects, safeguard.Subject{Kind: safeguard.SubjectProject, ID: s.ProjectID, Key: key})
	}
	// The attempt limit is per stage, one of the factory's own subjects the
	// design lists directly, so a ceiling over it is drawn on the stage
	// itself: the subject and the parameter's own key name the same thing.
	if s.Stage != "" {
		subjects = append(subjects, safeguard.Subject{Kind: safeguard.SubjectStage, ID: string(s.Stage), Key: key})
	}
	if s.AreaID != "" {
		chain, _, err := area.Chain(ctx, r.pool, s.AreaID)
		if err != nil {
			return nil, err
		}
		for _, a := range chain {
			subjects = append(subjects, safeguard.Subject{Kind: safeguard.SubjectArea, ID: a.ID, Key: key})
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

// keyOf is the value of a parameter's own key for these subjects: the gate row
// for the risk threshold, the stage for the attempt limit, the quantity for the
// window's size and power, the duty for the review sample rate, the severity for
// the remediation period, the service for the report channel's per-service rate
// and the harm mark's page cap.
func keyOf(d gatepolicy.Definition, s Subjects) string {
	switch d.Key {
	case gatepolicy.KeyGateRow:
		return s.GateRow
	case gatepolicy.KeyStage:
		return string(s.Stage)
	case gatepolicy.KeyQuantity:
		return s.Quantity
	case gatepolicy.KeyDuty:
		return dutyKey(s.Duty)
	case gatepolicy.KeySeverity:
		if !s.SeverityNamed {
			return ""
		}
		return severityKey(s.Severity)
	case gatepolicy.KeyService:
		return s.ServiceID
	}
	return ""
}
