package policy

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/service"
)

// Rederived is one field the re-derivation wrote back: what the newest version
// names, and what the field held before — a number for every parameter but the
// list of allowed predicate kinds, whose field is [Rederived.HeldList].
type Rederived struct {
	Value    AuthoredValue
	Held     gatepolicy.Authored
	HeldList []string
}

// Rederive rewrites every authored field the newest policy version names that
// does not hold what it names. The factory's start calls it, which is what
// finishes a write a stop interrupted, and a re-derivation that finds the two
// already agreeing writes nothing.
//
// It appends no version: it writes no value an owner did not already author,
// and the version it reads is the one that already names them.
//
// What it re-derives is every field the newest version names, the pairs
// included: a write that sets a second value beside the first names both, so
// the pair is written together and neither number is written alone into a state
// the record's own CHECK refuses. The People declaration the version names is
// re-derived by package people, the direction between the two being People to
// here.
func (f *Factory) Rederive(ctx context.Context, actor record.Actor) ([]Rederived, error) {
	if err := ownerOnly(actor); err != nil {
		return nil, err
	}
	newest, err := f.newest(ctx, actor)
	if err != nil {
		return nil, err
	}

	var rewritten []Rederived
	for _, value := range newest.Authored {
		definition, err := gatepolicy.Define(value.Parameter)
		if provisioningValue(value) {
			definition, err = gatepolicy.Definition{Kind: gatepolicy.KindList}, nil
		}
		if err != nil {
			return nil, fmt.Errorf("policy: the version names %s and nothing defines it: %w",
				value.Parameter, err)
		}
		held, heldList, err := f.fieldInForce(ctx, value)
		if err != nil {
			return nil, err
		}
		if agrees(definition, value, held, heldList) {
			continue
		}
		if err := f.rewrite(ctx, actor, value); err != nil {
			return nil, err
		}
		rewritten = append(rewritten, Rederived{Value: value, Held: held, HeldList: heldList})
	}
	return rewritten, nil
}

// agrees reports whether the field holds what the version names. An absent
// field never agrees with a version naming a value, which is the state a stop
// between the two writes leaves.
//
// A write that set two values names both, the first as the number and the rest
// as the list, so agreement is over both: a record holding the objective and
// not its period agrees with neither half.
func agrees(d gatepolicy.Definition, value AuthoredValue,
	held gatepolicy.Authored, heldList []string) bool {
	if (len(value.List) > 0 || len(heldList) > 0) && !slices.Equal(value.List, heldList) {
		return false
	}
	if d.Kind == gatepolicy.KindList || d.Kind == gatepolicy.KindStrategy {
		return true
	}
	return held.Present && held.Number == value.Number
}

// fieldInForce reads what the record its scope names holds for one parameter.
// It is the field alone: no safeguard and nothing the score supplies, because
// what a version records is what an owner authored.
func (f *Factory) fieldInForce(ctx context.Context, value AuthoredValue) (gatepolicy.Authored, []string, error) {
	switch value.Parameter {
	case gatepolicy.RiskThreshold:
		if value.Scope.Kind == ScopeFactorySettings {
			settings, err := factorysettings.Get(ctx, f.pool)
			return settings.RolePromptOrSkillThreshold, nil, err
		}
		authored, err := environment.GateThreshold(ctx, f.pool, value.Scope.ID, value.Scope.Key)
		return authored, nil, err
	case gatepolicy.ItemSizeTarget:
		a, err := area.Get(ctx, f.pool, value.Scope.ID)
		return a.ItemSizeTarget, nil, err
	case gatepolicy.AttemptLimit:
		settings, err := factorysettings.Get(ctx, f.pool)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		authored, err := factorysettings.AttemptLimit(ctx, f.pool, settings.ID,
			factorysettings.AttemptLimitSubject(value.Scope.Key))
		return authored, nil, err
	case gatepolicy.ReviewSampleRate:
		settings, err := factorysettings.Get(ctx, f.pool)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		duty, err := dutyOf(value.Scope.Key)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		authored, err := factorysettings.ReviewSampleRate(ctx, f.pool, settings.ID, duty)
		return authored, nil, err
	case gatepolicy.AllowedPredicateKinds:
		settings, err := factorysettings.Get(ctx, f.pool)
		return gatepolicy.Authored{}, settings.AllowedPredicateKinds, err
	case gatepolicy.StrategyDefault:
		e, err := environment.Get(ctx, f.pool, value.Scope.ID)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		if e.StrategyDefault == "" {
			return gatepolicy.Authored{}, nil, nil
		}
		return gatepolicy.Authored{}, []string{string(e.StrategyDefault)}, nil
	case gatepolicy.RemediationPeriod:
		settings, err := factorysettings.Get(ctx, f.pool)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		severity, err := severityOf(value.Scope.Key)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		authored, err := factorysettings.RemediationPeriod(ctx, f.pool, settings.ID, severity)
		return authored, nil, err
	case gatepolicy.ServiceReportChannelRate:
		settings, err := factorysettings.Get(ctx, f.pool)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		authored, err := factorysettings.ReportChannelRate(ctx, f.pool, settings.ID, value.Scope.Key)
		return authored, nil, err
	case gatepolicy.HarmMarkPageCap:
		settings, err := factorysettings.Get(ctx, f.pool)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		if value.Scope.Key == "pages" {
			return gatepolicy.Authored{Number: boolValue(settings.HarmMarkPages), Present: true}, nil, nil
		}
		cap, err := factorysettings.HarmMarkPageCap(ctx, f.pool, settings.ID, value.Scope.Key)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		return gatepolicy.Authored{Number: float64(cap.Cap), Present: cap.Authored},
			[]string{secondsKey(float64(cap.IntervalSeconds))}, nil
	case gatepolicy.MaxConcurrentCandidateEnvironments:
		e, err := environment.Get(ctx, f.pool, value.Scope.ID)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		return gatepolicy.Authored{
			Number:  float64(e.MaxConcurrentCandidateEnvironments),
			Present: e.MaxConcurrentCandidateEnvironments > 0,
		}, nil, nil
	case gatepolicy.AdvisorySeverity, gatepolicy.HeldOutSampleRate,
		gatepolicy.DecisionLogRetention, gatepolicy.ReportRetention,
		gatepolicy.BackupRetention, gatepolicy.RetentionFloor,
		gatepolicy.ReportChannelRate, gatepolicy.Seam5Enforced:
		settings, err := factorysettings.Get(ctx, f.pool)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		return settingsField(settings, value.Parameter), nil, nil
	}
	if provisioningValue(value) {
		svc, err := service.Get(ctx, f.pool, value.Scope.ID)
		if err != nil || !svc.Provisioned.Written() {
			return gatepolicy.Authored{}, nil, err
		}
		return gatepolicy.Authored{}, []string{string(svc.Provisioned.Shape),
			svc.Provisioned.BranchCredential.Name(), svc.Provisioned.MasterCredential.Name()}, nil
	}
	svc, err := service.Get(ctx, f.pool, value.Scope.ID)
	if err != nil {
		return gatepolicy.Authored{}, nil, err
	}
	switch value.Parameter {
	case gatepolicy.WindowSize:
		return svc.Parameters.WindowSizeFor(gatepolicy.Quantity(value.Scope.Key)), nil, nil
	case gatepolicy.WindowPower:
		return svc.Parameters.WindowPowerFor(gatepolicy.Quantity(value.Scope.Key)), nil, nil
	case gatepolicy.WindowConfidence:
		return svc.Parameters.WindowConfidence, nil, nil
	case gatepolicy.WindowCap:
		return svc.Parameters.WindowCapSeconds, nil, nil
	case gatepolicy.WindowLimit:
		return svc.Parameters.WindowLimit, nil, nil
	case gatepolicy.ExposureBound:
		return svc.Parameters.ExposureBound, nil, nil
	case gatepolicy.BakeVolume:
		return svc.BakeVolume, nil, nil
	case gatepolicy.BacklogCap:
		return svc.BacklogCap, nil, nil
	case gatepolicy.MutationFloor:
		return svc.MutationFloor, nil, nil
	case gatepolicy.KeptFraction:
		return svc.KeptFraction, nil, nil
	case gatepolicy.MaxConcurrentKeptFleets:
		return svc.MaxConcurrentKeptFleets, nil, nil
	case gatepolicy.RecentHistorySize:
		return svc.RecentHistorySize[gatepolicy.Quantity(value.Scope.Key)], nil, nil
	case gatepolicy.RecentHistoryRunLength:
		return svc.RecentHistoryRunLength, nil, nil
	case gatepolicy.ProofTestRate:
		return svc.ProofTestRate, nil, nil
	case gatepolicy.InstanceHourRate:
		return svc.InstanceHourRate, nil, nil
	case gatepolicy.EnvironmentHourRate:
		return svc.EnvironmentHourRate, nil, nil
	case gatepolicy.MutantCap:
		return svc.MutantCap, nil, nil
	case gatepolicy.FailureRecordKeyCap:
		if !svc.FailureRecordKeyCap.Present {
			return svc.FailureRecordKeyCap, nil, nil
		}
		return svc.FailureRecordKeyCap, []string{svc.OverflowFailureRecordBucket}, nil
	case gatepolicy.UnreliableBound:
		return svc.UnreliableBound, nil, nil
	case gatepolicy.IncidentItemBound:
		return svc.IncidentItemBoundSeconds, nil, nil
	case gatepolicy.SnapshotRetention:
		return svc.SnapshotRetentionSeconds, nil, nil
	case gatepolicy.SearchBudget:
		return svc.SearchBudgetBuilds, secondsList(svc.SearchBudgetSeconds), nil
	case gatepolicy.Objective:
		return svc.Objective.Target, secondsList(svc.Objective.PeriodSeconds), nil
	case gatepolicy.OperationCap:
		if !svc.OperationCap.Present {
			return svc.OperationCap, nil, nil
		}
		return svc.OperationCap, []string{svc.OverflowOperation}, nil
	case gatepolicy.PagingHours:
		if !svc.PagingHours.Authored() {
			return gatepolicy.Authored{}, nil, nil
		}
		return gatepolicy.Authored{}, []string{svc.PagingHours.Start, svc.PagingHours.End, svc.PagingHours.Zone}, nil
	case gatepolicy.ProductLicence:
		if svc.ProductLicence == "" {
			return gatepolicy.Authored{}, nil, nil
		}
		return gatepolicy.Authored{}, []string{svc.ProductLicence}, nil
	case gatepolicy.ServiceTargets:
		return gatepolicy.Authored{}, svc.Targets, nil
	case gatepolicy.ChangeFreeze:
		periods, err := service.FreezePeriods(ctx, f.pool, value.Scope.ID)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		held := make([]string, 0, len(periods))
		for _, p := range periods {
			held = append(held, freezeKey(p.StartsAt, p.EndsAt))
		}
		slices.Sort(held)
		return gatepolicy.Authored{}, held, nil
	}
	return gatepolicy.Authored{}, nil, fmt.Errorf("policy: nothing re-derives %s", value.Parameter)
}

// settingsField is one authored field of the factory-wide settings record that
// has one value per record.
func settingsField(settings factorysettings.Settings, parameter gatepolicy.Parameter) gatepolicy.Authored {
	switch parameter {
	case gatepolicy.AdvisorySeverity:
		return settings.AdvisorySeverity
	case gatepolicy.HeldOutSampleRate:
		return settings.HeldOutSampleRate
	case gatepolicy.DecisionLogRetention:
		return settings.DecisionLogRetentionSeconds
	case gatepolicy.ReportRetention:
		return settings.ReportRetentionSeconds
	case gatepolicy.BackupRetention:
		return settings.BackupRetentionSeconds
	case gatepolicy.RetentionFloor:
		return settings.RetentionFloorSeconds
	case gatepolicy.ReportChannelRate:
		return settings.ReportChannelRate
	case gatepolicy.Seam5Enforced:
		// Off is not authored: an owner turns it on once and nothing turns it
		// off again, so the field holds a value only where they have.
		return gatepolicy.Authored{Number: 1, Present: settings.Seam5Enforced}
	}
	return gatepolicy.Authored{}
}

// secondsList is the second number of a pair as the version names it, and
// nothing where the owner authored none.
func secondsList(authored gatepolicy.Authored) []string {
	if !authored.Present {
		return nil
	}
	return []string{secondsKey(authored.Number)}
}

// secondsOf reads a number back out of the list a version names a pair in.
func secondsOf(list []string) (float64, error) {
	if len(list) == 0 {
		return 0, fmt.Errorf("policy: the version names no second value beside the first")
	}
	return strconv.ParseFloat(list[0], 64)
}

// provisioningValue recognizes the owner-authored credential references. These
// are a service field, not a numeric gate-policy parameter.
func provisioningValue(value AuthoredValue) bool {
	return value.Parameter == "" && value.Scope.Kind == ScopeService && value.Scope.Key == "provisioned"
}
