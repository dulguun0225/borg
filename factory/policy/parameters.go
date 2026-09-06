package policy

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/service"
)

// AuthorGateThreshold authors the number one gate row compares against on one
// environment. Where a rate reader is composed, the realized auto-pass rate at
// that threshold is computed in this same call, one per factor set, and frozen
// on the version.
//
// The version names the score version in force at the write, which is what the
// owner authored against: under a changed formula the same change gets a
// different number, so a threshold authored under one version is not a threshold
// authored under the next.
func (f *Factory) AuthorGateThreshold(ctx context.Context, actor record.Actor,
	environmentID, gateRow string, threshold float64) (Version, error) {
	scope := Scope{Kind: ScopeEnvironment, ID: environmentID, Key: gateRow}
	rates, err := f.ratesAt(ctx, scope, gateRow, threshold)
	if err != nil {
		return Version{}, err
	}
	confirms, err := f.scoreVersionInForce(ctx)
	if err != nil {
		return Version{}, err
	}
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionAuthored,
		parameter: gatepolicy.RiskThreshold, scope: scope, number: threshold, authored: true, rates: rates,
		confirmsScoreVersion: confirms,
		apply: func(ctx context.Context, tx pgx.Tx) error {
			return environment.SetGateThreshold(ctx, tx, f.token, actor, environmentID, gateRow, threshold)
		},
	})
}

// ConfirmGateThreshold is the owner's confirmation that the threshold standing
// on this row and this environment is the threshold they mean under the score
// version in force now. It authors no value and writes no field: what it appends
// is a version naming the scope and that score version, which is what puts a
// version that changed the published formula, the factor set or the weights into
// force at a gate an authored threshold binds.
//
// An owner who disagrees re-authors the number instead, through
// [Factory.AuthorGateThreshold], which confirms the same way. Confirming the
// same score version twice appends nothing, the key being the same.
//
// Nothing in the factory calls it yet: the screen the threshold is authored
// from is not built, so the caller is the command-line interface.
func (f *Factory) ConfirmGateThreshold(ctx context.Context, actor record.Actor,
	environmentID, gateRow string) (Version, error) {
	confirms, err := f.scoreVersionInForce(ctx)
	if err != nil {
		return Version{}, err
	}
	if confirms == "" {
		return Version{}, fmt.Errorf("%w: there is no score version to confirm the threshold on %s against",
			score.ErrNoVersion, gateRow)
	}
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionConfirmed,
		parameter:            gatepolicy.RiskThreshold,
		scope:                Scope{Kind: ScopeEnvironment, ID: environmentID, Key: gateRow},
		confirmsScoreVersion: confirms,
	})
}

// AuthorRolePromptOrSkillThreshold authors the threshold the gate row that
// decides a version of what an agent is told reads. It is the same parameter as
// the one above on a different record, that row having no project and so no
// production environment to read.
func (f *Factory) AuthorRolePromptOrSkillThreshold(ctx context.Context, actor record.Actor,
	threshold float64) (Version, error) {
	settings, err := factorysettings.Get(ctx, f.pool)
	if err != nil {
		return Version{}, err
	}
	scope := Scope{Kind: ScopeFactorySettings, ID: settings.ID, Key: RolePromptOrSkillRow}
	rates, err := f.ratesAt(ctx, scope, RolePromptOrSkillRow, threshold)
	if err != nil {
		return Version{}, err
	}
	confirms, err := f.scoreVersionInForce(ctx)
	if err != nil {
		return Version{}, err
	}
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionAuthored,
		parameter: gatepolicy.RiskThreshold, scope: scope, number: threshold, authored: true, rates: rates,
		confirmsScoreVersion: confirms,
		apply: func(ctx context.Context, tx pgx.Tx) error {
			return factorysettings.SetRolePromptOrSkillThreshold(ctx, tx, settings.ID, threshold)
		},
	})
}

// ratesAt is the realized auto-pass rate at a threshold, from whatever the
// composition supplied, and nothing where it supplied none.
func (f *Factory) ratesAt(ctx context.Context, scope Scope, gateRow string, threshold float64) ([]AutoPassRate, error) {
	if f.AutoPassRates == nil {
		return nil, nil
	}
	return f.AutoPassRates(ctx, scope, gateRow, threshold)
}

// AuthorExposureBound authors where the exposure factor stops being weighed and
// puts a human at Implementation instead.
func (f *Factory) AuthorExposureBound(ctx context.Context, actor record.Actor,
	serviceID string, bound float64) (Version, error) {
	return f.authorOnService(ctx, actor, gatepolicy.ExposureBound, serviceID, bound, service.SetExposureBound)
}

// AuthorAdvisorySeverity authors the bound at or above which a matching
// advisory rejects at Implementation and holds at Deploy to production.
func (f *Factory) AuthorAdvisorySeverity(ctx context.Context, actor record.Actor, severity float64) (Version, error) {
	return f.authorOnSettings(ctx, actor, gatepolicy.AdvisorySeverity, "", severity,
		func(ctx context.Context, tx pgx.Tx, settingsID string) error {
			return factorysettings.SetAdvisorySeverity(ctx, tx, settingsID, severity)
		})
}

// AuthorAttemptLimit authors how many attempts one stage gets. The interview's
// rounds and decomposition's re-decompositions are two more subjects of the
// same parameter; a caller wanting those calls [Factory.AuthorAttemptLimitOf].
func (f *Factory) AuthorAttemptLimit(ctx context.Context, actor record.Actor,
	stage item.Stage, limit int) (Version, error) {
	subject, err := factorysettings.OfStage(stage)
	if err != nil {
		return Version{}, err
	}
	return f.AuthorAttemptLimitOf(ctx, actor, subject, limit)
}

// AuthorAttemptLimitOf authors the limit for one of the six subjects it is
// counted against.
func (f *Factory) AuthorAttemptLimitOf(ctx context.Context, actor record.Actor,
	subject factorysettings.AttemptLimitSubject, limit int) (Version, error) {
	return f.authorOnSettings(ctx, actor, gatepolicy.AttemptLimit, string(subject), float64(limit),
		func(ctx context.Context, tx pgx.Tx, settingsID string) error {
			return factorysettings.SetAttemptLimit(ctx, tx, actor, settingsID, subject, limit)
		})
}

// AuthorItemSizeTarget authors how large an item in one area is meant to be, in
// the count of its intent's requirements an item answers.
func (f *Factory) AuthorItemSizeTarget(ctx context.Context, actor record.Actor,
	areaID string, target float64) (Version, error) {
	return f.author(ctx, actor, gatepolicy.ItemSizeTarget, Scope{Kind: ScopeArea, ID: areaID}, target,
		func(ctx context.Context, tx pgx.Tx) error {
			return area.SetItemSizeTarget(ctx, tx, areaID, target)
		})
}

// AuthorAllowedPredicateKinds authors what kinds of assertion a consumer
// contract may draw from. It is the one parameter whose value is a list, and an
// owner extends the factory's own rather than replacing it.
func (f *Factory) AuthorAllowedPredicateKinds(ctx context.Context, actor record.Actor,
	allowed []string) (Version, error) {
	settings, err := factorysettings.Get(ctx, f.pool)
	if err != nil {
		return Version{}, err
	}
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionAuthored,
		parameter: gatepolicy.AllowedPredicateKinds,
		scope:     Scope{Kind: ScopeFactorySettings, ID: settings.ID},
		list:      allowed, authored: true,
		apply: func(ctx context.Context, tx pgx.Tx) error {
			return factorysettings.SetAllowedPredicateKinds(ctx, tx, settings.ID, allowed)
		},
	})
}

// AuthorWindowSize authors the smallest regression a comparison must rule out
// on one quantity. It is per quantity because a detectable change in an error
// rate and one in a latency quantile are not one number.
func (f *Factory) AuthorWindowSize(ctx context.Context, actor record.Actor, serviceID string,
	quantity gatepolicy.Quantity, size float64) (Version, error) {
	return f.author(ctx, actor, gatepolicy.WindowSize,
		Scope{Kind: ScopeService, ID: serviceID, Key: string(quantity)}, size,
		func(ctx context.Context, tx pgx.Tx) error {
			return service.SetWindowSize(ctx, tx, f.token, actor, serviceID, quantity, size)
		})
}

// AuthorWindowPower authors how reliably a regression of the size in force is
// caught rather than reaching passed, per quantity as the size is.
func (f *Factory) AuthorWindowPower(ctx context.Context, actor record.Actor, serviceID string,
	quantity gatepolicy.Quantity, power float64) (Version, error) {
	return f.author(ctx, actor, gatepolicy.WindowPower,
		Scope{Kind: ScopeService, ID: serviceID, Key: string(quantity)}, power,
		func(ctx context.Context, tx pgx.Tx) error {
			return service.SetWindowPower(ctx, tx, f.token, actor, serviceID, quantity, power)
		})
}

// AuthorWindowConfidence authors how sure that comparison must be.
func (f *Factory) AuthorWindowConfidence(ctx context.Context, actor record.Actor,
	serviceID string, confidence float64) (Version, error) {
	return f.authorOnService(ctx, actor, gatepolicy.WindowConfidence, serviceID, confidence, service.SetWindowConfidence)
}

// AuthorWindowCap authors the elapsed time in seconds that ends a window which
// will never reach its volume.
func (f *Factory) AuthorWindowCap(ctx context.Context, actor record.Actor,
	serviceID string, seconds float64) (Version, error) {
	return f.authorOnService(ctx, actor, gatepolicy.WindowCap, serviceID, seconds, service.SetWindowCap)
}

// AuthorWindowLimit authors how many analysis windows one service may hold open
// at once.
func (f *Factory) AuthorWindowLimit(ctx context.Context, actor record.Actor,
	serviceID string, limit float64) (Version, error) {
	return f.authorOnService(ctx, actor, gatepolicy.WindowLimit, serviceID, limit, service.SetWindowLimit)
}

// AuthorHeldOutSampleRate authors how often the score auto-passes a change it
// would have gated. It is a field of the factory-wide settings record, the
// sample being one formula's and no service's.
func (f *Factory) AuthorHeldOutSampleRate(ctx context.Context, actor record.Actor, rate float64) (Version, error) {
	return f.authorOnSettings(ctx, actor, gatepolicy.HeldOutSampleRate, "", rate,
		func(ctx context.Context, tx pgx.Tx, settingsID string) error {
			return factorysettings.SetHeldOutSampleRate(ctx, tx, settingsID, rate)
		})
}

// AuthorReviewSampleRate authors how often a change the score would have
// auto-passed is put in front of one duty's human anyway.
func (f *Factory) AuthorReviewSampleRate(ctx context.Context, actor record.Actor,
	duty int, rate float64) (Version, error) {
	return f.authorOnSettings(ctx, actor, gatepolicy.ReviewSampleRate, dutyKey(duty), rate,
		func(ctx context.Context, tx pgx.Tx, settingsID string) error {
			return factorysettings.SetReviewSampleRate(ctx, tx, actor, settingsID, duty, rate)
		})
}

// authorOnService is one number authored on the service record, the scope five
// of the eleven rows name.
func (f *Factory) authorOnService(ctx context.Context, actor record.Actor, parameter gatepolicy.Parameter,
	serviceID string, value float64, set func(context.Context, pgx.Tx, string, float64) error) (Version, error) {
	return f.author(ctx, actor, parameter, Scope{Kind: ScopeService, ID: serviceID}, value,
		func(ctx context.Context, tx pgx.Tx) error {
			return set(ctx, tx, serviceID, value)
		})
}

// authorOnSettings is one number authored on the factory-wide settings record,
// which the write reads first because every such value is keyed on its id.
func (f *Factory) authorOnSettings(ctx context.Context, actor record.Actor, parameter gatepolicy.Parameter,
	key string, value float64, set func(context.Context, pgx.Tx, string) error) (Version, error) {
	settings, err := factorysettings.Get(ctx, f.pool)
	if err != nil {
		return Version{}, err
	}
	return f.author(ctx, actor, parameter, Scope{Kind: ScopeFactorySettings, ID: settings.ID, Key: key}, value,
		func(ctx context.Context, tx pgx.Tx) error {
			return set(ctx, tx, settings.ID)
		})
}

// dutyKey is a duty as a scope's key: the review sample rate is one value per
// duty, and the version and a safeguard on it name which by the same spelling.
func dutyKey(duty int) string { return strconv.Itoa(duty) }
