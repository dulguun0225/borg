package policy

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorypolicy"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/pin"
	"github.com/dulguun0225/borg/factory/record"
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
// transaction. At the milestone that builds the four surfaces, Factory the
// surface is what calls this; until then the crude interface does.
type Factory struct {
	pool *pgxpool.Pool
}

// NewFactory returns the writer over pool.
func NewFactory(pool *pgxpool.Pool) *Factory { return &Factory{pool: pool} }

// Installed is what [Factory.Install] found or created.
type Installed struct {
	Policy     factorypolicy.Policy
	Production environment.Environment
	Version    Version
}

// Install is the two records that exist before any parameter is authored: the
// factory policy record, which exists before any project does, and production's
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

	policyRecord, err := factorypolicy.NewWriter(f.pool).Ensure(ctx, actor)
	if err != nil {
		return Installed{}, err
	}
	if err := f.versionForCreation(ctx, actor, Subject{Kind: "factory_policy", ID: policyRecord.ID}); err != nil {
		return Installed{}, err
	}

	production, found, err := environment.ByName(ctx, f.pool, environment.ProductionName)
	if err != nil {
		return Installed{}, err
	}
	if !found {
		production, err = environment.NewWriter(f.pool).Create(ctx, actor,
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
	return Installed{Policy: policyRecord, Production: production, Version: version}, nil
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
		return environment.SetGateThreshold(ctx, tx, actor, environmentID, gateRow, threshold)
	})
}

// AuthorBriefOrSkillThreshold authors the threshold the gate row that decides a
// version of what an agent is told reads. It is the same parameter as the one
// above on a different record, that row having no project and so no production
// environment to read. Nothing reads it until that row is built.
func (f *Factory) AuthorBriefOrSkillThreshold(ctx context.Context, actor record.Actor, threshold float64) (Version, error) {
	policyRecord, err := factorypolicy.Get(ctx, f.pool)
	if err != nil {
		return Version{}, err
	}
	subject := Subject{Kind: "factory_policy", ID: policyRecord.ID, Qualifier: "brief_or_skill"}
	return f.author(ctx, actor, gatepolicy.RiskThreshold, subject, func(tx pgx.Tx) error {
		return factorypolicy.SetBriefOrSkillThreshold(ctx, tx, policyRecord.ID, threshold)
	})
}

// AuthorAttemptBound authors how many attempts one stage gets.
func (f *Factory) AuthorAttemptBound(ctx context.Context, actor record.Actor, stage item.Stage, bound int) (Version, error) {
	policyRecord, err := factorypolicy.Get(ctx, f.pool)
	if err != nil {
		return Version{}, err
	}
	subject := Subject{Kind: "factory_policy", ID: policyRecord.ID, Qualifier: string(stage)}
	return f.author(ctx, actor, gatepolicy.AttemptBound, subject, func(tx pgx.Tx) error {
		return factorypolicy.SetAttemptBound(ctx, tx, actor, policyRecord.ID, stage, bound)
	})
}

// AuthorPredicateCatalog authors what kinds of assertion a consumer's
// declaration may draw from. Nothing reads it until contracts are built.
func (f *Factory) AuthorPredicateCatalog(ctx context.Context, actor record.Actor, catalog []string) (Version, error) {
	policyRecord, err := factorypolicy.Get(ctx, f.pool)
	if err != nil {
		return Version{}, err
	}
	subject := Subject{Kind: "factory_policy", ID: policyRecord.ID}
	return f.author(ctx, actor, gatepolicy.PredicateCatalog, subject, func(tx pgx.Tx) error {
		return factorypolicy.SetPredicateCatalog(ctx, tx, policyRecord.ID, catalog)
	})
}

// AuthorItemSizeTarget authors how large an item in one area is meant to be.
// Nothing reads it until a cut sizes anything.
func (f *Factory) AuthorItemSizeTarget(ctx context.Context, actor record.Actor, areaID string, target float64) (Version, error) {
	subject := Subject{Kind: "area", ID: areaID}
	return f.author(ctx, actor, gatepolicy.ItemSizeTarget, subject, func(tx pgx.Tx) error {
		return area.SetItemSizeTarget(ctx, tx, areaID, target)
	})
}

// The four an owner authors on a service. Nothing reads any of them until the
// watch window is built, which is stated once here and again in service's
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

// AuthorK authors how many watch windows one service may hold open at once.
func (f *Factory) AuthorK(ctx context.Context, actor record.Actor, serviceID string, k float64) (Version, error) {
	return f.authorOnService(ctx, actor, gatepolicy.K, serviceID, k, service.SetK)
}

func (f *Factory) authorOnService(ctx context.Context, actor record.Actor, parameter gatepolicy.Parameter,
	serviceID string, value float64, set func(context.Context, pgx.Tx, string, float64) error) (Version, error) {
	subject := Subject{Kind: "service", ID: serviceID}
	return f.author(ctx, actor, parameter, subject, func(tx pgx.Tx) error {
		return set(ctx, tx, serviceID, value)
	})
}

// Pin places one, in the direction the parameter's definition gives it, and
// appends the version in the same transaction. The bound is one value of three
// shapes — a number, a list, or a predicate — which is package pin's [pin.Bound],
// so this signature does not grow an argument each time a shape arrives.
func (f *Factory) Pin(ctx context.Context, actor record.Actor, parameter gatepolicy.Parameter,
	subject pin.Subject, bound pin.Bound) (pin.Pin, Version, error) {
	if err := ownerOnly(actor); err != nil {
		return pin.Pin{}, Version{}, err
	}

	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return pin.Pin{}, Version{}, fmt.Errorf("policy: beginning the pin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	placed, err := pin.Insert(ctx, tx, actor, parameter, subject, bound)
	if err != nil {
		return pin.Pin{}, Version{}, err
	}
	version, err := appendVersion(ctx, tx, actor, ActionPinned, parameter,
		Subject{Kind: string(subject.Kind), ID: subject.ID}, placed.ID)
	if err != nil {
		return pin.Pin{}, Version{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return pin.Pin{}, Version{}, fmt.Errorf("policy: committing the pin on %s: %w", subject, err)
	}
	return placed, version, nil
}

// WithdrawPin marks one withdrawn, which is what stops a mechanism reading it.
// The pin's row stays, so a pin that was in force when a decision was taken is
// still readable beside it.
func (f *Factory) WithdrawPin(ctx context.Context, actor record.Actor, pinID string) (Version, error) {
	if err := ownerOnly(actor); err != nil {
		return Version{}, err
	}
	pins, err := pin.All(ctx, f.pool)
	if err != nil {
		return Version{}, err
	}
	var withdrawing pin.Pin
	for _, p := range pins {
		if p.ID == pinID {
			withdrawing = p
		}
	}
	if withdrawing.ID == "" {
		return Version{}, fmt.Errorf("%w: %s", pin.ErrNotFound, pinID)
	}
	return f.write(ctx, actor, ActionWithdrawn, withdrawing.Parameter,
		Subject{Kind: string(withdrawing.Subject.Kind), ID: withdrawing.Subject.ID}, pinID,
		func(tx pgx.Tx) error { return pin.Withdraw(ctx, tx, pinID) })
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
	parameter gatepolicy.Parameter, subject Subject, pinID string, apply func(pgx.Tx) error) (Version, error) {
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return Version{}, fmt.Errorf("policy: beginning the write of %s: %w", subject, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := apply(tx); err != nil {
		return Version{}, err
	}
	version, err := appendVersion(ctx, tx, actor, action, parameter, subject, pinID)
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
		return fmt.Errorf("%w: %s %q", ErrNotAnOwner, actor.Kind, actor.Name)
	}
	return nil
}
