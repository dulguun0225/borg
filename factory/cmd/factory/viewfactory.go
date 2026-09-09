package main

import (
	"context"
	"errors"
	"time"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/constraint"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/halt"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/legalhold"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/project"
	"github.com/dulguun0225/borg/factory/safeguard"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/screens"
)

// Factory: the machine itself. The policy in force, the fleet, the permanent
// constraints, the projects and areas, and the numbers computed at read time
// over the one factory-owned span.

// Factory is the whole of what that screen holds. What it leaves out is listed
// in package screens' own doc.go: a field the design names and no view carries
// is absent on purpose and not forgotten.
func (v *views) Factory(ctx context.Context, who principal.Principal) (screens.Factory, error) {
	var view screens.Factory
	var err error

	if view.Parameters, err = v.parameters(ctx); err != nil {
		return screens.Factory{}, err
	}
	if view.Safeguards, err = v.safeguards(ctx); err != nil {
		return screens.Factory{}, err
	}
	if view.Halts, err = v.halts(ctx); err != nil {
		return screens.Factory{}, err
	}
	if view.LegalHolds, err = v.legalHolds(ctx); err != nil {
		return screens.Factory{}, err
	}
	if view.Environments, err = v.environments(ctx); err != nil {
		return screens.Factory{}, err
	}
	if view.FleetEntries, view.LentCredentials, err = v.fleet(ctx); err != nil {
		return screens.Factory{}, err
	}
	if view.RolePrompts, view.RolePromptGateRow, err = v.rolePrompts(ctx); err != nil {
		return screens.Factory{}, err
	}
	if view.Constraints, err = v.permanentConstraints(ctx); err != nil {
		return screens.Factory{}, err
	}
	if view.Projects, view.Areas, err = v.projectsAndAreas(ctx); err != nil {
		return screens.Factory{}, err
	}
	if view.Numbers, err = v.numbers(ctx, who); err != nil {
		return screens.Factory{}, err
	}
	if view.ReportChannel, err = v.reportChannelNumbers(ctx); err != nil {
		return screens.Factory{}, err
	}
	if view.StoppedAtDispatch, err = v.stoppedAtDispatch(ctx); err != nil {
		return screens.Factory{}, err
	}
	if view.SpendCeilings, err = v.spendCeilings(ctx); err != nil {
		return screens.Factory{}, err
	}
	if view.AutoPassRates, view.HeldOutBands, err = v.thresholdReadings(ctx, who); err != nil {
		return screens.Factory{}, err
	}
	if view.RecordDecidingRows, err = v.recordDecidingRows(ctx); err != nil {
		return screens.Factory{}, err
	}
	// A factory with no settings record enforces nothing: the field is off at
	// install and turned on once, so an install that has not written the record
	// has not turned it on — the same reading dispatch makes of it.
	settings, err := factorysettings.Get(ctx, v.p.d.pool)
	if err != nil && !errors.Is(err, factorysettings.ErrNotFound) {
		return screens.Factory{}, err
	}
	view.Seam5Enforced = settings.Seam5Enforced
	if err := v.humanReadings(ctx, who, &view); err != nil {
		return screens.Factory{}, err
	}
	return view, nil
}

// parameters is every authored value in force, with which of the three
// sources — authored, supplied, or clamped by a safeguard — the effective value
// came from.
//
// The read is made over one set of subjects and not over every combination of
// them: a parameter is a field of one record, and the reader answers each
// against the subject it is a field of. The subjects named here are the
// factory's own — production's environment, the merge row, the implementation
// stage, and the quantity every emission carries — so what the screen shows is
// the value in force over the factory rather than over a service. What it costs
// is that a value authored per service is read here at the factory's, and the
// service's own is on Ops beside its windows.
func (v *views) parameters(ctx context.Context) ([]screens.Parameter, error) {
	effective, err := v.p.policy.All(ctx, policy.Subjects{
		EnvironmentID: v.p.production.ID,
		ProjectID:     v.p.projectID,
		AreaID:        v.p.areaID,
		GateRow:       gate.MergeToMaster.String(),
		Stage:         item.StageImplementation,
		Quantity:      string(gatepolicy.QuantityErrorRate),
	})
	if err != nil {
		return nil, err
	}
	rows := make([]screens.Parameter, 0, len(effective))
	for _, one := range effective {
		rows = append(rows, screens.Parameter{
			Name:    string(one.Parameter),
			Subject: one.Row,
			Value:   formatNumber(one.Number),
			Source:  string(one.Source),
		})
	}
	return rows, nil
}

func (v *views) safeguards(ctx context.Context) ([]screens.Safeguard, error) {
	placed, err := safeguard.All(ctx, v.p.d.pool)
	if err != nil {
		return nil, err
	}
	rows := make([]screens.Safeguard, 0, len(placed))
	for _, one := range placed {
		if one.Withdrawn {
			continue
		}
		rows = append(rows, screens.Safeguard{
			ID: one.ID, Parameter: string(one.Parameter), Subject: one.Subject.String(),
			Direction: string(one.Direction), Bound: formatNumber(one.Bound.Number),
			RoutedToDuty: int64(one.Routing.Duty), RoutedToHuman: v.nameOf(ctx, one.Routing.HumanKey),
		})
	}
	return rows, nil
}

func (v *views) halts(ctx context.Context) ([]screens.Halt, error) {
	standing, err := halt.Standing(ctx, v.p.d.pool)
	if err != nil {
		return nil, err
	}
	rows := make([]screens.Halt, 0, len(standing))
	for _, one := range standing {
		rows = append(rows, screens.Halt{ID: one.ID, Reason: one.Reason, At: one.At})
	}
	return rows, nil
}

func (v *views) legalHolds(ctx context.Context) ([]screens.LegalHold, error) {
	standing, err := legalhold.Standing(ctx, v.p.d.pool)
	if err != nil {
		return nil, err
	}
	rows := make([]screens.LegalHold, 0, len(standing))
	for _, one := range standing {
		rows = append(rows, screens.LegalHold{
			ID: one.ID, Subject: one.Subject.String(), Reason: one.Reason, At: one.At,
		})
	}
	return rows, nil
}

// environments is production and every live candidate environment: a candidate
// torn down is a place that no longer exists, and one still standing is a place
// a service is running on.
func (v *views) environments(ctx context.Context) ([]screens.Environment, error) {
	rows := []screens.Environment{environmentView(v.p.production)}
	items, err := item.All(ctx, v.p.d.pool)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		env, found, err := environment.ForItem(ctx, v.p.d.pool, it.ID)
		if err != nil {
			return nil, err
		}
		if !found || !env.Live() {
			continue
		}
		rows = append(rows, environmentView(env))
	}
	return rows, nil
}

func environmentView(env environment.Environment) screens.Environment {
	view := screens.Environment{ID: env.ID, Kind: string(env.Kind)}
	for _, target := range env.Targets {
		view.Targets = append(view.Targets, target.Address)
	}
	return view
}

// rolePrompts is the version in force per role and any version awaiting the
// gate every version of what an agent is told fires. A chain whose head an
// upgrade entered reads as the version below it in force until that row is
// decided here.
func (v *views) rolePrompts(ctx context.Context) ([]screens.RolePrompt, *screens.RolePromptGateRow, error) {
	rows := make([]screens.RolePrompt, 0, len(dispatch.Roles))
	var awaiting *screens.RolePromptGateRow
	pending, err := v.p.gate.Pending(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, role := range dispatch.Roles {
		inForce, found, err := v.p.prompts.InForce(ctx, role)
		if err != nil {
			return nil, nil, err
		}
		row := screens.RolePrompt{Role: string(role)}
		if found {
			row.VersionInForce = inForce.ID
		}
		head, headFound, err := artifact.Newest(ctx, v.p.d.pool, artifact.KindRolePrompt, string(role), "")
		if err != nil {
			return nil, nil, err
		}
		if headFound && (!found || head.ID != inForce.ID) {
			row.AwaitingGateVersion = head.ID
			if awaiting == nil {
				awaiting = &screens.RolePromptGateRow{Role: string(role), VersionID: head.ID}
				for _, opened := range pending {
					if opened.Gate.Kind == gate.KindRolePromptOrSkill && opened.ArtifactID == head.ID {
						awaiting.OpenedAt = opened.Row.At
					}
				}
			}
		}
		rows = append(rows, row)
	}
	return rows, awaiting, nil
}

// permanentConstraints is the constraints in force whose reach is the factory,
// a project, or an area, listed by reach. A constraint that arrived with a
// single intent is read at Work, on its intent, and is not here.
func (v *views) permanentConstraints(ctx context.Context) ([]screens.Constraint, error) {
	standing, err := constraint.Permanent(ctx, v.p.d.pool, time.Now())
	if err != nil {
		return nil, err
	}
	rows := make([]screens.Constraint, 0, len(standing))
	for _, one := range standing {
		rows = append(rows, constraintView(one))
	}
	return rows, nil
}

// projectsAndAreas is every project and every area declared, each area with
// what it lies inside, the project its chain ends at, and the severity in
// force on it — which is the chain's highest grade and not the area's own.
func (v *views) projectsAndAreas(ctx context.Context) ([]screens.Project, []screens.Area, error) {
	projects, err := project.All(ctx, v.p.d.pool)
	if err != nil {
		return nil, nil, err
	}
	rows := make([]screens.Project, 0, len(projects))
	for _, one := range projects {
		rows = append(rows, screens.Project{ID: one.ID, Name: one.Name})
	}
	declared, err := area.All(ctx, v.p.d.pool)
	if err != nil {
		return nil, nil, err
	}
	areas := make([]screens.Area, 0, len(declared))
	for _, one := range declared {
		inside := one.Inside.AreaID
		if inside == "" {
			inside = one.Inside.ProjectID
		}
		// The chain is walked for the project rather than the project read off
		// the area, because an area inside another area names no project of its
		// own and the chain is where the one it belongs to is.
		_, projectID, err := area.Chain(ctx, v.p.d.pool, one.ID)
		if err != nil {
			return nil, nil, err
		}
		severity, err := area.SeverityInForce(ctx, v.p.d.pool, one.ID)
		if err != nil {
			return nil, nil, err
		}
		areas = append(areas, screens.Area{
			ID: one.ID, Name: one.Name, Inside: inside,
			ProjectID: projectID, Severity: string(severity),
		})
	}
	return rows, areas, nil
}

// stoppedAtDispatch is how many items are stopped at dispatch now, grouped by
// the cause the hold's own first row names.
func (v *views) stoppedAtDispatch(ctx context.Context) ([]screens.DispatchCause, error) {
	held, _, err := v.p.dispatch.Open(ctx)
	if err != nil {
		return nil, err
	}
	counted := map[string]int64{}
	var order []string
	for _, one := range held {
		if _, seen := counted[one.Condition]; !seen {
			order = append(order, one.Condition)
		}
		counted[one.Condition]++
	}
	rows := make([]screens.DispatchCause, 0, len(order))
	for _, cause := range order {
		rows = append(rows, screens.DispatchCause{Cause: cause, Count: counted[cause]})
	}
	return rows, nil
}

// thresholdReadings is the pair the risk threshold is read against: the
// realized auto-pass rate at each threshold in force against the rate the
// policy version that set it recorded, and the held-out windows' outcomes by
// band of the number.
func (v *views) thresholdReadings(ctx context.Context, who principal.Principal) ([]screens.AutoPassRate, []screens.HeldOutBand, error) {
	realized, err := score.RealizedAutoPass(ctx, v.p.d.pool, v.p.d.token, who, sinceTheInstall)
	if err != nil {
		return nil, nil, err
	}
	rates := make([]screens.AutoPassRate, 0, len(realized))
	for _, one := range realized {
		recorded, found, err := v.p.policy.AuthoredAutoPassRate(ctx, who,
			policy.Scope{Kind: policy.ScopeEnvironment, ID: v.p.production.ID}, one.Subject)
		if err != nil {
			return nil, nil, err
		}
		row := screens.AutoPassRate{
			FactorSet: string(one.FactorSet), Threshold: one.Threshold, Realized: one.RealizedRate,
		}
		if found {
			for _, rate := range recorded {
				if rate.FactorSet == string(one.FactorSet) {
					row.Recorded = rate.Rate
				}
			}
		}
		rates = append(rates, row)
	}

	outcomes, err := score.HeldOutByBand(ctx, v.p.d.pool, v.p.d.token, who, sinceTheInstall, v.p.scoreBand)
	if err != nil {
		return nil, nil, err
	}
	bands := make([]screens.HeldOutBand, 0, len(outcomes))
	for _, one := range outcomes {
		bands = append(bands, screens.HeldOutBand{
			FactorSet:     string(one.FactorSet),
			Band:          formatNumber(one.From) + "–" + formatNumber(one.To),
			FailedShare:   one.FailedShare,
			ResolvedCount: int64(one.Windows),
		})
	}
	return rates, bands, nil
}

// recordDecidingRows is the four rows outside every item awaiting a
// disposition here: a safeguard's withdrawal, a halt's withdrawal, a legal
// hold's ending, and a shortening of decision-log retention.
//
// What is read is the records and not the log's open events. Each of the four
// is fired and closed in the one call that takes the verdict — a row left open
// with the record standing would be a protection removed with no decision on
// it — so what waits is a withdrawal or a shortening standing unapproved, and
// each of the four packages answers its own.
//
// RecordID is that record's own id, which is what [screens.DecideRecordRowArgs]
// names: the withdrawal is what an owner decides, and the record it removes a
// protection from is read off it.
func (v *views) recordDecidingRows(ctx context.Context) ([]screens.RecordDecidingRow, error) {
	var rows []screens.RecordDecidingRow
	safeguards, err := safeguard.WithdrawalsAwaitingADecision(ctx, v.p.d.pool)
	if err != nil {
		return nil, err
	}
	for _, one := range safeguards {
		rows = append(rows, screens.RecordDecidingRow{
			Kind: gate.SafeguardWithdrawal.String(), RecordID: one.ID, OpenedAt: one.At,
		})
	}
	halts, err := halt.WithdrawalsAwaitingADecision(ctx, v.p.d.pool)
	if err != nil {
		return nil, err
	}
	for _, one := range halts {
		rows = append(rows, screens.RecordDecidingRow{
			Kind: gate.HaltWithdrawal.String(), RecordID: one.ID, OpenedAt: one.At,
		})
	}
	holds, err := legalhold.WithdrawalsAwaitingADecision(ctx, v.p.d.pool)
	if err != nil {
		return nil, err
	}
	for _, one := range holds {
		rows = append(rows, screens.RecordDecidingRow{
			Kind: gate.LegalHoldWithdrawal.String(), RecordID: one.ID, OpenedAt: one.At,
		})
	}
	shortenings, err := factorysettings.ShorteningsAwaitingADecision(ctx, v.p.d.pool)
	if err != nil {
		return nil, err
	}
	for _, one := range shortenings {
		rows = append(rows, screens.RecordDecidingRow{
			Kind: gate.DecisionLogRetentionShortening.String(), RecordID: one.ID, OpenedAt: one.At,
		})
	}
	return rows, nil
}
