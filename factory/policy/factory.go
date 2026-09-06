package policy

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
)

// ErrNotAnOwner is returned by every authoring call for an actor that is not a
// human. Gate policy is duty 8 — everything an owner authors — so a component
// authoring a parameter would be the factory setting its own bounds, which is
// the one thing this record type exists to keep apart from what the score
// supplies.
var ErrNotAnOwner = errors.New("policy: gate policy is authored by a human")

// ErrNoDeployer is returned by [Factory.RetireService] where nothing composed
// [Factory.Removal]. Retiring a service is an owner's write that calls the
// deployer, so a factory that cannot reach one refuses the write rather than
// recording a retirement nothing performed.
var ErrNoDeployer = errors.New("policy: retiring a service calls the deployer, and none is composed")

// Factory is the writer of everything an owner authors: it calls into the
// package that owns each record and appends the policy version, as a row of the
// decision log, in the same transaction.
//
// At the milestone that builds the four screens, Factory the screen is what
// calls this; until then the command-line interface does.
type Factory struct {
	pool  *pgxpool.Pool
	token lease.Token
	log   *decisionlog.Writer

	// Declaration is the People declaration in force, supplied by whatever
	// composes the factory: every version names it by per-person key, and this
	// package may not read it — the direction between the two is People to
	// here. A nil value carries the declaration the version in force names
	// forward unchanged, which is what a factory whose People screen is not
	// built does.
	Declaration func(ctx context.Context) (DeclarationSnapshot, error)

	// Removal is what the deployer performs when a service is retired: it ends
	// every instance of the service on every target of every persistent
	// environment and writes a deploy record per environment naming no release.
	// environmentID bounds it to one environment, which is the same removal
	// performed for that one and what an owner has done before an environment
	// may be withdrawn; empty is every persistent environment, which is what a
	// retirement reaches.
	// It is supplied by whatever composes the factory, this package reaching no
	// deploy target and importing nothing that does. A nil value refuses the
	// retirement with [ErrNoDeployer]: the design has the write call the
	// deployer, so retiring through a factory with none composed would write
	// retired and leave the service running.
	Removal func(ctx context.Context, serviceID, environmentID string) error

	// AutoPassRates is the realized auto-pass rate at a threshold, one per
	// factor set, computed in the same call that appends the version and frozen
	// there. It is supplied rather than computed here because what it is
	// computed from is the score's own decisions. A nil value freezes no rate,
	// which is what a factory with no such reader composed does, and
	// [Reader.AuthoredAutoPassRate] then finds none.
	AutoPassRates func(ctx context.Context, scope Scope, gateRow string, threshold float64) ([]AutoPassRate, error)
}

// NewFactory returns the writer over pool, fencing every write with token.
func NewFactory(pool *pgxpool.Pool, token lease.Token) *Factory {
	return &Factory{pool: pool, token: token, log: decisionlog.NewWriter(pool, token)}
}

// Created is what a write that creates a record hands back to the version that
// names it: the scope the record is, and the id where the record is a
// safeguard, a halt, a legal hold or a withdrawal of one.
type Created struct {
	Scope        Scope
	SafeguardID  string
	HaltID       string
	LegalHoldID  string
	WithdrawalID string
}

// write is one owner write: what the version says, what it does to the state
// the version carries, and the record write itself.
type write struct {
	caller    Caller
	actor     record.Actor
	action    Action
	parameter gatepolicy.Parameter
	scope     Scope
	number    float64
	list      []string

	// authored is whether this write puts a value on the version's authored
	// state. A creation, a safeguard, a halt and a hold each author no
	// parameter and set it false.
	authored bool

	// mint runs before the version is appended, for a write whose version names
	// a record its own writer mints the id of. apply runs after the version, for
	// every write that authors a field on a record that already exists: the log
	// appends the version first and Factory writes the field second. Both run in
	// the one transaction, so neither can be left without the other.
	mint  func(ctx context.Context, tx pgx.Tx) (Created, error)
	apply func(ctx context.Context, tx pgx.Tx) error

	// dropSafeguard, dropHalt and dropLegalHold are what an approved withdrawal
	// takes out of force.
	dropSafeguard string
	dropHalt      string
	dropLegalHold string

	// decision is the close event of the gate row that decided this write, on the
	// four writes a row decides and empty on every other.
	decision string

	// declaration replaces what the version in force names, for a write at
	// People. Every other write carries it forward.
	declaration *DeclarationSnapshot

	// rates is the realized auto-pass rate frozen on a write that set a risk
	// threshold, and empty on every other.
	rates []AutoPassRate

	// keyExtra is what a write whose value is not a number or a list adds to
	// its own key, so that two such writes differing in what they carry derive
	// two keys: the People declaration is the one, and its key is a digest of
	// what the version will name.
	keyExtra string

	// confirmsScoreVersion is the score version a threshold write confirms or
	// re-authors against, and is empty on every other write. It is part of the
	// key: confirming the same version twice writes nothing, and confirming the
	// one appended since is a write of its own.
	confirmsScoreVersion string
}

// append is every owner write: read the version in force, derive the write's
// key, and where that key is not the one the version in force already carries,
// open one fenced transaction and put the version and the record write in it.
//
// A step taken again derives the same key as the version in force and returns
// that version, having written nothing.
func (f *Factory) append(ctx context.Context, w write) (Version, error) {
	if err := ownerOnly(w.actor); err != nil {
		return Version{}, err
	}

	previous, err := f.newest(ctx, w.actor)
	if err != nil && !errors.Is(err, ErrNoVersion) {
		return Version{}, err
	}

	key := writeKey(w.caller, w.actor, w.action, w.parameter, w.scope, w.number, w.list,
		w.keyExtra+w.dropSafeguard+w.dropHalt+w.dropLegalHold+w.confirmsScoreVersion+w.decision)
	if w.mint == nil && previous.Key != "" && previous.Key == key {
		return previous, nil
	}

	declaration := previous.Declaration
	switch {
	case w.declaration != nil:
		declaration = *w.declaration
	case f.Declaration != nil:
		declaration, err = f.Declaration(ctx)
		if err != nil {
			return Version{}, fmt.Errorf("policy: reading the People declaration in force: %w", err)
		}
	}

	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return Version{}, fmt.Errorf("policy: beginning the write of %s: %w", w.scope, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lease.Fence(ctx, tx, f.token); err != nil {
		return Version{}, err
	}
	// The lock covers the read of the version in force above and the append
	// below. Two owner writes at once would otherwise each carry forward the
	// state the other was about to change, and the newer version would name an
	// authored value the older one had already moved.
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, AdvisoryLockKey()); err != nil {
		return Version{}, fmt.Errorf("policy: taking the version lock: %w", err)
	}

	version := Version{
		Actor: w.actor, Caller: w.caller, Action: w.action, Parameter: w.parameter,
		Scope: w.scope, Number: w.number, List: w.list, Key: key,
		Authored: slices.Clone(previous.Authored), Safeguards: slices.Clone(previous.Safeguards),
		Halts: slices.Clone(previous.Halts), LegalHolds: slices.Clone(previous.LegalHolds),
		Declaration: declaration, AutoPassRates: w.rates,
		ConfirmsScoreVersion: w.confirmsScoreVersion, Decision: w.decision,
	}
	if w.mint != nil {
		created, err := w.mint(ctx, tx)
		if err != nil {
			return Version{}, err
		}
		if created.Scope.Kind != "" {
			version.Scope = created.Scope
		}
		version.SafeguardID, version.HaltID = created.SafeguardID, created.HaltID
		version.LegalHoldID, version.WithdrawalID = created.LegalHoldID, created.WithdrawalID
		version.Safeguards = withID(version.Safeguards, created.SafeguardID)
		version.Halts = withID(version.Halts, created.HaltID)
		version.LegalHolds = withID(version.LegalHolds, created.LegalHoldID)
	}
	if w.authored {
		version.Authored = withAuthored(version.Authored, AuthoredValue{
			Parameter: w.parameter, Scope: w.scope, Number: w.number, List: w.list,
		})
	}
	version.SafeguardID = firstOf(version.SafeguardID, w.dropSafeguard)
	version.HaltID = firstOf(version.HaltID, w.dropHalt)
	version.LegalHoldID = firstOf(version.LegalHoldID, w.dropLegalHold)
	version.Safeguards = withoutID(version.Safeguards, w.dropSafeguard)
	version.Halts = withoutID(version.Halts, w.dropHalt)
	version.LegalHolds = withoutID(version.LegalHolds, w.dropLegalHold)

	body, err := version.marshal()
	if err != nil {
		return Version{}, err
	}
	row, err := f.log.AppendPolicyVersionInTx(ctx, tx, decisionlog.Entry{
		Actor: w.actor, Payload: body, FormatVersion: FormatVersion,
	})
	if err != nil {
		return Version{}, err
	}
	version.ID, version.At = row.ID, row.At

	if w.apply != nil {
		if err := w.apply(ctx, tx); err != nil {
			return Version{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Version{}, fmt.Errorf("policy: committing the write of %s: %w", version.Scope, err)
	}
	return version, nil
}

// author is an owner authoring one number on the record its scope names.
func (f *Factory) author(ctx context.Context, actor record.Actor, parameter gatepolicy.Parameter,
	scope Scope, number float64, apply func(context.Context, pgx.Tx) error) (Version, error) {
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionAuthored,
		parameter: parameter, scope: scope, number: number, authored: true, apply: apply,
	})
}

// newest is the version in force, read as the actor making the write, calling
// as itself. Reading the log appends a read event, so an owner write is a read
// event, a version, and the field it authored. No dispatch and no scope travel
// with it: what this package writes is authored by a human and by nothing that
// was dispatched to a stage.
func (f *Factory) newest(ctx context.Context, actor record.Actor) (Version, error) {
	return NewReader(f.pool, f.token, score.Version{}).Newest(ctx, principal.Principal{Actor: actor})
}

// scoreVersionInForce is the newest score version, which is what an owner
// authoring or confirming a threshold authors against: what they compare a
// number to is what the published formula returns now, so the write names the
// version that returns it. A factory with no score version yet names none, and
// nothing at a gate then waits on a confirmation.
func (f *Factory) scoreVersionInForce(ctx context.Context) (string, error) {
	version, found, err := score.Newest(ctx, f.pool, f.token)
	if err != nil {
		return "", fmt.Errorf("policy: reading the score version in force: %w", err)
	}
	if !found {
		return "", nil
	}
	return version.ID, nil
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

// withAuthored replaces the value for one parameter and scope, or adds it. The
// version names one value per parameter and scope, so re-authoring one moves it
// rather than listing it twice.
func withAuthored(values []AuthoredValue, value AuthoredValue) []AuthoredValue {
	for n, held := range values {
		if held.Parameter == value.Parameter && held.Scope == value.Scope {
			values[n] = value
			return values
		}
	}
	return append(values, value)
}

func withID(ids []string, id string) []string {
	if id == "" || slices.Contains(ids, id) {
		return ids
	}
	return append(ids, id)
}

func withoutID(ids []string, id string) []string {
	if id == "" {
		return ids
	}
	return slices.DeleteFunc(ids, func(held string) bool { return held == id })
}

func firstOf(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
