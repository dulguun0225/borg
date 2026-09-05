package policy

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/lease"
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
// What it re-derives is every value whose parameter package gatepolicy names —
// gate policy's eleven rows and what is authored beside them on the
// factory-wide settings record. The fields a version names by key and no
// parameter, on a service record beside the eleven, are left as they stand: the
// value in force for each is the field itself and no mechanism reads the
// version for it.
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
		if value.Parameter == "" {
			continue
		}
		if _, err := gatepolicy.Define(value.Parameter); err != nil {
			continue
		}
		held, heldList, err := f.fieldInForce(ctx, value)
		if err != nil {
			return nil, err
		}
		if agrees(value, held, heldList) {
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
func agrees(value AuthoredValue, held gatepolicy.Authored, heldList []string) bool {
	if len(value.List) > 0 {
		return slices.Equal(value.List, heldList)
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
	case gatepolicy.AdvisorySeverity, gatepolicy.HeldOutSampleRate,
		gatepolicy.DecisionLogRetention, gatepolicy.ReportRetention,
		gatepolicy.BackupRetention, gatepolicy.RetentionFloor:
		settings, err := factorysettings.Get(ctx, f.pool)
		if err != nil {
			return gatepolicy.Authored{}, nil, err
		}
		return settingsField(settings, value.Parameter), nil, nil
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
	}
	return gatepolicy.Authored{}
}

// rewrite writes one field back to what the version names, in a transaction of
// its own, fenced. It appends no version, so the write here is the same write
// the version already records.
func (f *Factory) rewrite(ctx context.Context, actor record.Actor, value AuthoredValue) error {
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("policy: beginning the re-derivation of %s: %w", value.Scope, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, f.token); err != nil {
		return err
	}
	if err := f.rewriteIn(ctx, tx, actor, value); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("policy: committing the re-derivation of %s: %w", value.Scope, err)
	}
	return nil
}

func (f *Factory) rewriteIn(ctx context.Context, tx pgx.Tx, actor record.Actor, value AuthoredValue) error {
	settingsID := value.Scope.ID
	switch value.Parameter {
	case gatepolicy.RiskThreshold:
		if value.Scope.Kind == ScopeFactorySettings {
			return factorysettings.SetRolePromptOrSkillThreshold(ctx, tx, settingsID, value.Number)
		}
		return environment.SetGateThreshold(ctx, tx, f.token, actor, value.Scope.ID, value.Scope.Key, value.Number)
	case gatepolicy.ItemSizeTarget:
		return area.SetItemSizeTarget(ctx, tx, value.Scope.ID, value.Number)
	case gatepolicy.AttemptLimit:
		return factorysettings.SetAttemptLimit(ctx, tx, actor, settingsID,
			factorysettings.AttemptLimitSubject(value.Scope.Key), int(value.Number))
	case gatepolicy.ReviewSampleRate:
		duty, err := dutyOf(value.Scope.Key)
		if err != nil {
			return err
		}
		return factorysettings.SetReviewSampleRate(ctx, tx, actor, settingsID, duty, value.Number)
	case gatepolicy.AllowedPredicateKinds:
		return factorysettings.SetAllowedPredicateKinds(ctx, tx, settingsID, value.List)
	case gatepolicy.AdvisorySeverity:
		return factorysettings.SetAdvisorySeverity(ctx, tx, settingsID, value.Number)
	case gatepolicy.HeldOutSampleRate:
		return factorysettings.SetHeldOutSampleRate(ctx, tx, settingsID, value.Number)
	case gatepolicy.DecisionLogRetention:
		return factorysettings.SetDecisionLogRetention(ctx, tx, settingsID, int64(value.Number))
	case gatepolicy.ReportRetention:
		return factorysettings.SetReportRetention(ctx, tx, settingsID, int64(value.Number))
	case gatepolicy.BackupRetention:
		return factorysettings.SetBackupRetention(ctx, tx, settingsID, int64(value.Number))
	case gatepolicy.RetentionFloor:
		return factorysettings.SetRetentionFloor(ctx, tx, settingsID, int64(value.Number))
	case gatepolicy.WindowSize:
		return service.SetWindowSize(ctx, tx, f.token, actor, value.Scope.ID,
			gatepolicy.Quantity(value.Scope.Key), value.Number)
	case gatepolicy.WindowPower:
		return service.SetWindowPower(ctx, tx, f.token, actor, value.Scope.ID,
			gatepolicy.Quantity(value.Scope.Key), value.Number)
	case gatepolicy.WindowConfidence:
		return service.SetWindowConfidence(ctx, tx, value.Scope.ID, value.Number)
	case gatepolicy.WindowCap:
		return service.SetWindowCap(ctx, tx, value.Scope.ID, value.Number)
	case gatepolicy.WindowLimit:
		return service.SetWindowLimit(ctx, tx, value.Scope.ID, value.Number)
	case gatepolicy.ExposureBound:
		return service.SetExposureBound(ctx, tx, value.Scope.ID, value.Number)
	}
	return fmt.Errorf("policy: nothing re-derives %s", value.Parameter)
}

// dutyOf reads the duty back out of a scope's key, which [dutyKey] wrote.
func dutyOf(key string) (int, error) {
	duty, err := strconv.Atoi(key)
	if err != nil {
		return 0, fmt.Errorf("policy: %q names no duty: %w", key, err)
	}
	return duty, nil
}
