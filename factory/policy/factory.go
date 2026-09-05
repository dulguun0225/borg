package policy

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/safeguard"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
)

// ErrNotAnOwner is returned by every authoring call for an actor that is not a
// human. Gate policy is duty 8 — everything an owner authors — so a component
// authoring a parameter would be the factory setting its own bounds, which is
// the one thing this record type exists to keep apart from what the score
// supplies.
var ErrNotAnOwner = errors.New("policy: gate policy is authored by a human")

// Factory is the writer of everything an owner authors: it calls into the
// package that owns each record and appends the policy version in the same
// transaction. At the milestone that builds the four screens, Factory the
// screen is what calls this; until then the crude interface does.
type Factory struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewFactory returns the writer over pool, fencing every write with token.
func NewFactory(pool *pgxpool.Pool, token lease.Token) *Factory { return &Factory{pool: pool, token: token} }

// Installed is what [Factory.Install] found or created.
type Installed struct {
	Settings   factorysettings.Settings
	Production environment.Environment
	Version    Version
}

// Install is the two records that exist before any parameter is authored: the
// factory-wide settings record, which exists before any project does, and production's
// environment record, which an owner does not choose because production exists
// everywhere. Each creation appends a policy version, so a factory that has been
// installed has a version in force with nothing authored — which is what a gate
// names when an owner has authored nothing at all.
//
// It is idempotent. Running it against a factory that has both records appends
// no version and returns what is there, so the crude interface may call it at
// every start.
func (f *Factory) Install(ctx context.Context, actor record.Actor, targets []string, credential secretref.Ref) (Installed, error) {
	if err := ownerOnly(actor); err != nil {
		return Installed{}, err
	}

	settings, err := factorysettings.NewWriter(f.pool, f.token).Ensure(ctx, actor)
	if err != nil {
		return Installed{}, err
	}
	if err := f.versionForCreation(ctx, actor, Subject{Kind: "factory_settings", ID: settings.ID}); err != nil {
		return Installed{}, err
	}

	production, found, err := environment.ByName(ctx, f.pool, environment.ProductionName)
	if err != nil {
		return Installed{}, err
	}
	if !found {
		production, err = environment.NewWriter(f.pool, f.token).Create(ctx, actor,
			environment.KindProduction, environment.ProductionName, targets, credential)
		if err != nil {
			return Installed{}, err
		}
	}
	if err := f.versionForCreation(ctx, actor, Subject{Kind: "environment", ID: production.ID}); err != nil {
		return Installed{}, err
	}

	version, err := InForce(ctx, f.pool)
	if err != nil {
		return Installed{}, err
	}
	return Installed{Settings: settings, Production: production, Version: version}, nil
}

// versionForCreation appends a created version for a record, and appends nothing
// where one already names it — which is what makes Install idempotent.
func (f *Factory) versionForCreation(ctx context.Context, actor record.Actor, subject Subject) error {
	var existing string
	err := f.pool.QueryRow(ctx, `select id from `+Table+`
		where action = 'created' and subject_kind = $1 and subject_id = $2`,
		subject.Kind, subject.ID).Scan(&existing)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("policy: reading the creation of %s: %w", subject, err)
	}
	_, err = f.write(ctx, actor, ActionCreated, "", subject, "", func(pgx.Tx) error { return nil })
	return err
}

// AuthorGateThreshold authors the number one gate row compares against on one
// environment. It is the one parameter this milestone's gates read.
func (f *Factory) AuthorGateThreshold(ctx context.Context, actor record.Actor, environmentID, gateRow string, threshold float64) (Version, error) {
	subject := Subject{Kind: "environment", ID: environmentID, Qualifier: gateRow}
	return f.author(ctx, actor, gatepolicy.RiskThreshold, subject, func(tx pgx.Tx) error {
		return environment.SetGateThreshold(ctx, tx, f.token, actor, environmentID, gateRow, threshold)
	})
}

// AuthorRolePromptOrSkillThreshold authors the threshold the gate row that decides a
// version of what an agent is told reads. It is the same parameter as the one
// above on a different record, that row having no project and so no production
// environment to read. Nothing reads it until that row is built.
func (f *Factory) AuthorRolePromptOrSkillThreshold(ctx context.Context, actor record.Actor, threshold float64) (Version, error) {
	settings, err := factorysettings.Get(ctx, f.pool)
	if err != nil {
		return Version{}, err
	}
	subject := Subject{Kind: "factory_settings", ID: settings.ID, Qualifier: "role_prompt_or_skill"}
	return f.author(ctx, actor, gatepolicy.RiskThreshold, subject, func(tx pgx.Tx) error {
		return factorysettings.SetRolePromptOrSkillThreshold(ctx, tx, settings.ID, threshold)
	})
}

// AuthorAttemptLimit authors how many attempts one stage gets.
func (f *Factory) AuthorAttemptLimit(ctx context.Context, actor record.Actor, stage item.Stage, limit int) (Version, error) {
	settings, err := factorysettings.Get(ctx, f.pool)
	if err != nil {
		return Version{}, err
	}
	subject := Subject{Kind: "factory_settings", ID: settings.ID, Qualifier: string(stage)}
	return f.author(ctx, actor, gatepolicy.AttemptLimit, subject, func(tx pgx.Tx) error {
		return factorysettings.SetAttemptLimit(ctx, tx, actor, settings.ID, stage, limit)
	})
}

// AuthorAllowedPredicateKinds authors what kinds of assertion a consumer
// contract may draw from. Nothing reads it until contracts are built.
func (f *Factory) AuthorAllowedPredicateKinds(ctx context.Context, actor record.Actor, allowed []string) (Version, error) {
	settings, err := factorysettings.Get(ctx, f.pool)
	if err != nil {
		return Version{}, err
	}
	subject := Subject{Kind: "factory_settings", ID: settings.ID}
	return f.author(ctx, actor, gatepolicy.AllowedPredicateKinds, subject, func(tx pgx.Tx) error {
		return factorysettings.SetAllowedPredicateKinds(ctx, tx, settings.ID, allowed)
	})
}

// AuthorItemSizeTarget authors how large an item in one area is meant to be.
// Nothing reads it until a decomposition sizes anything.
func (f *Factory) AuthorItemSizeTarget(ctx context.Context, actor record.Actor, areaID string, target float64) (Version, error) {
	subject := Subject{Kind: "area", ID: areaID}
	return f.author(ctx, actor, gatepolicy.ItemSizeTarget, subject, func(tx pgx.Tx) error {
		return area.SetItemSizeTarget(ctx, tx, areaID, target)
	})
}

// The four an owner authors on a service. Nothing reads any of them until the
// analysis window is built, which is stated once here and again in service's
// parameters.go.

// AuthorWindowSize authors the smallest regression a comparison must rule out.
func (f *Factory) AuthorWindowSize(ctx context.Context, actor record.Actor, serviceID string, size float64) (Version, error) {
	return f.authorOnService(ctx, actor, gatepolicy.WindowSize, serviceID, size, service.SetWindowSize)
}

// AuthorWindowConfidence authors how sure that comparison must be.
func (f *Factory) AuthorWindowConfidence(ctx context.Context, actor record.Actor, serviceID string, confidence float64) (Version, error) {
	return f.authorOnService(ctx, actor, gatepolicy.WindowConfidence, serviceID, confidence, service.SetWindowConfidence)
}

// AuthorWindowCap authors the elapsed time in seconds that ends a window which
// will never reach its volume.
func (f *Factory) AuthorWindowCap(ctx context.Context, actor record.Actor, serviceID string, seconds float64) (Version, error) {
	return f.authorOnService(ctx, actor, gatepolicy.WindowCap, serviceID, seconds, service.SetWindowCap)
}

// AuthorWindowLimit authors how many analysis windows one service may hold open at once.
func (f *Factory) AuthorWindowLimit(ctx context.Context, actor record.Actor, serviceID string, limit float64) (Version, error) {
	return f.authorOnService(ctx, actor, gatepolicy.WindowLimit, serviceID, limit, service.SetWindowLimit)
}

func (f *Factory) authorOnService(ctx context.Context, actor record.Actor, parameter gatepolicy.Parameter,
	serviceID string, value float64, set func(context.Context, pgx.Tx, string, float64) error) (Version, error) {
	subject := Subject{Kind: "service", ID: serviceID}
	return f.author(ctx, actor, parameter, subject, func(tx pgx.Tx) error {
		return set(ctx, tx, serviceID, value)
	})
}

// Safeguard places one, in the direction the parameter's definition gives it, and
// appends the version in the same transaction. The bound is one value of three
// shapes — a number, a list, or a predicate — which is package safeguard's
// [safeguard.Bound], so this signature does not grow an argument each time a
// shape arrives.
func (f *Factory) AddSafeguard(ctx context.Context, actor record.Actor, parameter gatepolicy.Parameter,
	subject safeguard.Subject, bound safeguard.Bound) (safeguard.Safeguard, Version, error) {
	if err := ownerOnly(actor); err != nil {
		return safeguard.Safeguard{}, Version{}, err
	}

	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return safeguard.Safeguard{}, Version{}, fmt.Errorf("policy: beginning the safeguard: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lease.Fence(ctx, tx, f.token); err != nil {
		return safeguard.Safeguard{}, Version{}, err
	}

	placed, err := safeguard.Insert(ctx, tx, f.token, actor, parameter, subject, bound)
	if err != nil {
		return safeguard.Safeguard{}, Version{}, err
	}
	version, err := appendVersion(ctx, tx, actor, ActionSafeguardAdded, parameter,
		Subject{Kind: string(subject.Kind), ID: subject.ID}, placed.ID)
	if err != nil {
		return safeguard.Safeguard{}, Version{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return safeguard.Safeguard{}, Version{}, fmt.Errorf("policy: committing the safeguard on %s: %w", subject, err)
	}
	return placed, version, nil
}

// WithdrawSafeguard marks one withdrawn, which is what stops a mechanism
// reading it. The safeguard's row stays, so a safeguard that was in force when
// a decision was taken is still readable beside it.
func (f *Factory) WithdrawSafeguard(ctx context.Context, actor record.Actor, safeguardID string) (Version, error) {
	if err := ownerOnly(actor); err != nil {
		return Version{}, err
	}
	safeguards, err := safeguard.All(ctx, f.pool)
	if err != nil {
		return Version{}, err
	}
	var withdrawing safeguard.Safeguard
	for _, p := range safeguards {
		if p.ID == safeguardID {
			withdrawing = p
		}
	}
	if withdrawing.ID == "" {
		return Version{}, fmt.Errorf("%w: %s", safeguard.ErrNotFound, safeguardID)
	}
	return f.write(ctx, actor, ActionWithdrawn, withdrawing.Parameter,
		Subject{Kind: string(withdrawing.Subject.Kind), ID: withdrawing.Subject.ID}, safeguardID,
		func(tx pgx.Tx) error { return safeguard.Withdraw(ctx, tx, f.token, safeguardID) })
}

func (f *Factory) author(ctx context.Context, actor record.Actor, parameter gatepolicy.Parameter,
	subject Subject, write func(pgx.Tx) error) (Version, error) {
	if err := ownerOnly(actor); err != nil {
		return Version{}, err
	}
	return f.write(ctx, actor, ActionAuthored, parameter, subject, "", write)
}

// write is every authoring write: one transaction, the record's own writer
// called inside it, and the policy version appended before it commits. So a
// value that moved without the version moving is not a state the store can be
// left in.
func (f *Factory) write(ctx context.Context, actor record.Actor, action Action,
	parameter gatepolicy.Parameter, subject Subject, safeguardID string, apply func(pgx.Tx) error) (Version, error) {
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return Version{}, fmt.Errorf("policy: beginning the write of %s: %w", subject, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lease.Fence(ctx, tx, f.token); err != nil {
		return Version{}, err
	}

	// The lock covers the write and the append together. What it serialises is
	// reading the version in force and appending the one that supersedes it: two
	// writers doing that at once would each supersede the same version, and the
	// sequence an auditor walks would fork. The unique constraint refuses the fork;
	// this is what means a second owner authoring at the same moment waits for the
	// first rather than being refused.
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, AdvisoryLockKey()); err != nil {
		return Version{}, fmt.Errorf("policy: taking the version lock: %w", err)
	}

	if err := apply(tx); err != nil {
		return Version{}, err
	}
	version, err := appendVersion(ctx, tx, actor, action, parameter, subject, safeguardID)
	if err != nil {
		return Version{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Version{}, fmt.Errorf("policy: committing the write of %s: %w", subject, err)
	}
	return version, nil
}

func ownerOnly(actor record.Actor) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if actor.Kind != record.KindHuman {
		return fmt.Errorf("%w: %s %q", ErrNotAnOwner, actor.Kind, actor.Key)
	}
	return nil
}
