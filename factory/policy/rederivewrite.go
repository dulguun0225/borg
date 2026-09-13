package policy

import (
	"context"
	"fmt"
	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/jackc/pgx/v5"
	"strconv"
	"strings"
)

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
	if provisioningValue(value) {
		if len(value.List) != 3 {
			return fmt.Errorf("policy: the version names no credential shape and pair on %s", value.Scope.ID)
		}
		branch, err := secretref.New(value.List[1])
		if err != nil {
			return err
		}
		var master secretref.Ref
		if value.List[2] != "" {
			master, err = secretref.New(value.List[2])
			if err != nil {
				return err
			}
		}
		return service.SetProvisioned(ctx, tx, value.Scope.ID, service.CredentialShape(value.List[0]), branch, master)
	}
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
	case gatepolicy.BakeVolume:
		return service.SetBakeVolume(ctx, tx, actor, value.Scope.ID, value.Number)
	case gatepolicy.BacklogCap:
		return service.SetBacklogCap(ctx, tx, actor, value.Scope.ID, value.Number)
	case gatepolicy.MutationFloor:
		return service.SetMutationFloor(ctx, tx, actor, value.Scope.ID, value.Number)
	case gatepolicy.KeptFraction:
		return service.SetKeptFraction(ctx, tx, value.Scope.ID, value.Number)
	case gatepolicy.MaxConcurrentKeptFleets:
		return service.SetMaxConcurrentKeptFleets(ctx, tx, value.Scope.ID, value.Number)
	case gatepolicy.RecentHistorySize:
		return service.SetRecentHistorySize(ctx, tx, f.token, actor, value.Scope.ID,
			gatepolicy.Quantity(value.Scope.Key), value.Number)
	case gatepolicy.RecentHistoryRunLength:
		return service.SetRecentHistoryRunLength(ctx, tx, actor, value.Scope.ID, value.Number)
	case gatepolicy.ProofTestRate:
		return service.SetProofTestRate(ctx, tx, value.Scope.ID, value.Number)
	case gatepolicy.InstanceHourRate:
		return service.SetInstanceHourRate(ctx, tx, value.Scope.ID, value.Number)
	case gatepolicy.EnvironmentHourRate:
		return service.SetEnvironmentHourRate(ctx, tx, value.Scope.ID, value.Number)
	case gatepolicy.MutantCap:
		return service.SetMutantCap(ctx, tx, value.Scope.ID, value.Number)
	case gatepolicy.FailureRecordKeyCap:
		if len(value.List) != 1 || value.List[0] == "" {
			return fmt.Errorf("policy: the version names no overflow failure-record bucket on %s", value.Scope.ID)
		}
		return service.SetFailureRecordKeyCap(ctx, tx, actor, value.Scope.ID, value.Number, value.List[0])
	case gatepolicy.UnreliableBound:
		return service.SetUnreliableBound(ctx, tx, actor, value.Scope.ID, value.Number)
	case gatepolicy.IncidentItemBound:
		return service.SetIncidentItemBound(ctx, tx, value.Scope.ID, value.Number)
	case gatepolicy.SnapshotRetention:
		return service.SetSnapshotRetention(ctx, tx, value.Scope.ID, value.Number)
	case gatepolicy.SearchBudget:
		seconds, err := secondsOf(value.List)
		if err != nil {
			return err
		}
		return service.SetSearchBudget(ctx, tx, actor, value.Scope.ID, value.Number, seconds)
	case gatepolicy.Objective:
		period, err := secondsOf(value.List)
		if err != nil {
			return err
		}
		return service.SetObjective(ctx, tx, value.Scope.ID, value.Number, period)
	case gatepolicy.OperationCap:
		if len(value.List) == 0 {
			return fmt.Errorf("policy: the version names no overflow operation on %s", value.Scope.ID)
		}
		return service.SetOperationCap(ctx, tx, value.Scope.ID, value.Number, value.List[0])
	case gatepolicy.PagingHours:
		if len(value.List) != 3 {
			return fmt.Errorf("policy: the version names %d paging hours on %s, and there are three",
				len(value.List), value.Scope.ID)
		}
		return service.SetPagingHours(ctx, tx, value.Scope.ID, service.PagingHours{
			Start: value.List[0], End: value.List[1], Zone: value.List[2],
		})
	case gatepolicy.ProductLicence:
		if len(value.List) == 0 {
			return fmt.Errorf("policy: the version names no product licence on %s", value.Scope.ID)
		}
		return service.SetProductLicence(ctx, tx, value.Scope.ID, value.List[0])
	case gatepolicy.ServiceTargets:
		// The environment's own list is not read here: what the version names
		// is what package service already checked each target against when the
		// owner authored it, so the re-derivation restores that same list.
		return service.SetTargets(ctx, tx, value.Scope.ID, value.List, value.List)
	case gatepolicy.ChangeFreeze:
		// A period is added and never edited, so each the version names that
		// the record does not hold is added; the insert conflicts on the period
		// itself, which is what makes one already held write nothing.
		for _, period := range value.List {
			startsAt, endsAt, found := strings.Cut(period, " ")
			if !found {
				return fmt.Errorf("policy: %q is no change-freeze period", period)
			}
			if err := service.AddFreezePeriod(ctx, tx, f.token, actor, value.Scope.ID, startsAt, endsAt); err != nil {
				return err
			}
		}
		return nil
	case gatepolicy.RemediationPeriod:
		severity, err := severityOf(value.Scope.Key)
		if err != nil {
			return err
		}
		return factorysettings.SetRemediationPeriod(ctx, tx, actor, settingsID, severity, int64(value.Number))
	case gatepolicy.ReportChannelRate:
		return factorysettings.SetReportChannelRate(ctx, tx, settingsID, int64(value.Number))
	case gatepolicy.ServiceReportChannelRate:
		return factorysettings.SetServiceReportChannelRate(ctx, tx, actor, settingsID,
			value.Scope.Key, int64(value.Number))
	case gatepolicy.HarmMarkPageCap:
		if value.Scope.Key == "pages" {
			return factorysettings.SetHarmMarkPages(ctx, tx, settingsID, value.Number != 0)
		}
		interval, err := secondsOf(value.List)
		if err != nil {
			return err
		}
		return factorysettings.SetHarmMarkPageCap(ctx, tx, actor, settingsID, value.Scope.Key,
			int(value.Number), int64(interval))
	case gatepolicy.Seam5Enforced:
		return factorysettings.SetSeam5Enforced(ctx, tx, settingsID, true)
	case gatepolicy.MaxConcurrentCandidateEnvironments:
		return environment.SetMaxConcurrentCandidateEnvironments(ctx, tx, f.token, actor,
			value.Scope.ID, int(value.Number))
	case gatepolicy.StrategyDefault:
		if len(value.List) == 0 {
			return fmt.Errorf("policy: the version names no strategy default on %s", value.Scope.ID)
		}
		strategy, err := gatepolicy.DecidableStrategy(value.List[0])
		if err != nil {
			return err
		}
		return environment.SetStrategyDefault(ctx, tx, f.token, actor, value.Scope.ID, strategy)
	}
	return fmt.Errorf("policy: nothing re-derives %s", value.Parameter)
}

// severityOf reads the advisory severity back out of a scope's key, which
// [severityKey] wrote.
func severityOf(key string) (float64, error) {
	severity, err := strconv.ParseFloat(key, 64)
	if err != nil {
		return 0, fmt.Errorf("policy: %q names no advisory severity: %w", key, err)
	}
	return severity, nil
}

// dutyOf reads the duty back out of a scope's key, which [dutyKey] wrote.
func dutyOf(key string) (int, error) {
	duty, err := strconv.Atoi(key)
	if err != nil {
		return 0, fmt.Errorf("policy: %q names no duty: %w", key, err)
	}
	return duty, nil
}
