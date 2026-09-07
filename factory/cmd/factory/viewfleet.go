package main

import (
	"context"
	"time"

	"github.com/dulguun0225/borg/factory/agentrun"
	"github.com/dulguun0225/borg/factory/fleetentry"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/screens"
)

// Factory's fleet and what it spends: the entries in force with the credentials
// they may run on, and the spend ceiling authored on each credential. Split
// from viewfactory.go by subject at the 500-line bound.

// fleet is the entries in force and the lent credentials a new entry may run
// on, so the entry form lists them without reading People.
func (v *views) fleet(ctx context.Context) ([]screens.FleetEntry, []screens.LentCredentialSummary, error) {
	entries, err := fleetentry.InForce(ctx, v.p.d.pool)
	if err != nil {
		return nil, nil, err
	}
	rows := make([]screens.FleetEntry, 0, len(entries))
	for _, one := range entries {
		rows = append(rows, screens.FleetEntry{
			ID: one.ID, ModelVersion: one.ModelVersion, Effort: one.Effort, Role: one.Role,
			ScopeProjectID: one.Scope.ProjectID, ScopeServiceID: one.Scope.ServiceID,
			ScopeAreaID: one.Scope.AreaID, Credential: one.CredentialName,
			ProcessingLocation: one.ProcessingLocation, MaterialClasses: one.MaterialClasses,
			ReadAtOnceBound: one.ReadsAtOnce, DispatchesBetweenEvalRuns: one.DispatchesBetweenEvaluationRuns,
		})
	}
	credentials, err := people.Credentials(ctx, v.p.d.pool)
	if err != nil {
		return nil, nil, err
	}
	lent := make([]screens.LentCredentialSummary, 0, len(credentials))
	for _, one := range credentials {
		lent = append(lent, screens.LentCredentialSummary{
			Name: one.Name, LenderKey: one.Key, LenderName: v.nameOf(ctx, one.Key),
			Kind: string(one.Kind), TakenBack: !one.Lent(),
		})
	}
	return rows, lent, nil
}

// spendCeilings is the ceiling on each lent credential, the burn rate — units
// spent so far against the period in force — and when spending at that rate
// projects to exhaust it. A credential with no ceiling authored is reported
// unbounded, the provider account's own quota being what limits it then.
//
// The unpriced runs of the period go on the row beside the burn rate: their
// units convert to nothing, so a rate read without them reads lower than what
// was spent, and the count is what says the reading is a lower bound.
func (v *views) spendCeilings(ctx context.Context) ([]screens.SpendCeiling, error) {
	credentials, err := people.Credentials(ctx, v.p.d.pool)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	rows := make([]screens.SpendCeiling, 0, len(credentials))
	for _, one := range credentials {
		row := screens.SpendCeiling{Credential: one.Name}
		if !one.Ceiling.Authored() {
			row.Unbounded = true
			rows = append(rows, row)
			continue
		}
		start, err := one.Ceiling.PeriodStartAt(now)
		if err != nil {
			return nil, err
		}
		spend, err := agentrun.SpendByCredentialSince(ctx, v.p.d.pool, one.Name, start)
		if err != nil {
			return nil, err
		}
		row.Ceiling, row.Currency = one.Ceiling.Amount, one.Ceiling.Currency
		row.BurnRate = spend.Amount
		row.UnpricedRuns = int64(len(spend.Unpriced))
		if began, err := record.ParseTime(start); err == nil && spend.Amount > 0 {
			elapsed := now.Sub(began)
			if elapsed > 0 {
				perSecond := spend.Amount / elapsed.Seconds()
				left := one.Ceiling.Amount - spend.Amount
				if perSecond > 0 && left > 0 {
					row.ProjectedExhaustion = record.FormatTime(
						now.Add(time.Duration(left / perSecond * float64(time.Second))))
				}
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}
