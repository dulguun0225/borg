package environment

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
)

var (
	// ErrItemIDEmpty is returned by [Candidates.Compose] for a candidate
	// environment naming no item. record's doc.go states what a link is checked
	// for; this one is what the record is for.
	ErrItemIDEmpty = errors.New("environment: a candidate's environment names the item it belongs to")
	// ErrNotACandidate is returned by every write of [Candidates] for an
	// environment that is not a candidate's. Only a candidate's is recomposed and
	// only a candidate's is torn down.
	ErrNotACandidate = errors.New("environment: only a candidate's environment is recomposed or torn down")
	// ErrAlreadyTornDown is returned for an environment torn down for good.
	// Teardown for good does not run twice, and nothing here puts one back: what
	// is composed again is one that was reclaimed.
	ErrAlreadyTornDown = errors.New("environment: the environment is torn down for good")
	// ErrNoOpenCycle is returned by [Candidates.RunCouldStart] and
	// [Candidates.TearDown] for an environment with no cycle in progress — one
	// already reclaimed, which is composed again by [Candidates.Recompose].
	ErrNoOpenCycle = errors.New("environment: the environment has no compose-and-reclaim cycle in progress")
	// ErrCompositionIncomplete is returned for a composition entry naming a
	// service and no release, or a release and no service.
	ErrCompositionIncomplete = errors.New("environment: a composition entry names a service and a release")
)

// Composed is one dependency the deployer put in place beside the candidate: the
// service it is a release of, and the release of it that was current when the
// environment was composed. It is what was current then and not what is current
// now, which is the whole point of storing it.
type Composed struct {
	ServiceID string
	ReleaseID string
}

// Composition is the whole of what a candidate's environment was composed from:
// each dependency and the release of it that was current then, and the versions
// of the seed and of the non-production value set the store and the
// configuration were built from. A version is empty where the service's owner
// authored none.
//
// All three are compared together, which is what the merge queue does between a
// run and the run it re-verifies: a seed or a value set replaced between two runs
// is a composition that differs, read the way a moved release is.
type Composition struct {
	From            []Composed
	SeedVersion     string
	ValueSetVersion string
}

// Equal is whether two compositions are the same one: the same dependencies at
// the same releases in the same order, the same seed version, and the same
// value-set version.
func (c Composition) Equal(other Composition) bool {
	return c.SeedVersion == other.SeedVersion &&
		c.ValueSetVersion == other.ValueSetVersion &&
		slices.Equal(c.From, other.From)
}

// Empty is whether nothing was put in place and no version was named, which is
// what a persistent environment's composition reads as.
func (c Composition) Empty() bool {
	return len(c.From) == 0 && c.SeedVersion == "" && c.ValueSetVersion == ""
}

// NameForItem is the name a candidate's environment carries. It is derived from
// the item rather than chosen, because the name column is unique within the
// project and two candidates of one service would otherwise have to agree on a
// name between them.
func NameForItem(itemID string) string { return "candidate/" + itemID }

// Candidates is the candidate kind's one writer: the deployer, which is the only
// component that reaches a deploy target at all. It composes an environment at
// the approval of Deploy to candidate environment, reclaims one from an item
// running nothing, composes it again when that item next reaches the gate, and
// tears it down for good when the item merges, is dropped, or is superseded by a
// re-decomposition.
type Candidates struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewCandidates returns the writer over pool, fencing every write with token.
func NewCandidates(pool *pgxpool.Pool, token lease.Token) *Candidates {
	return &Candidates{pool: pool, token: token}
}

// Compose creates the environment for one item and opens its first
// compose-and-reclaim cycle, naming the targets a deploy into it is performed
// against, the credential it is performed with, and what it was composed from.
// The project is the item's service's, and it is what [CountLiveCandidates]
// counts this environment against.
//
// A second call for one item is refused by the unique constraint on the name,
// which is derived from the item: the environment is the item's and persists
// across a rebuild and across a reclamation, so a rebuild recomposes and a
// reclaimed environment is composed again through [Candidates.Recompose].
//
// The externals the candidate reaches are not stored here. An external is reached
// from a candidate through the non-production value set alone, so what says which
// address it reached is that set, and what this record holds about it is the
// version of the set — which is what a run's results carry.
func (c *Candidates) Compose(ctx context.Context, actor record.Actor, itemID, projectID string,
	targets []Target, credential secretref.Ref, composition Composition) (Environment, error) {
	if err := actor.Validate(); err != nil {
		return Environment{}, err
	}
	if itemID == "" {
		return Environment{}, ErrItemIDEmpty
	}
	if projectID == "" {
		return Environment{}, fmt.Errorf("%w: the candidate environment of %s", ErrProjectIDEmpty, itemID)
	}
	if err := validTargets(targets); err != nil {
		return Environment{}, err
	}
	if credential.Name() == "" {
		return Environment{}, fmt.Errorf("environment: the candidate environment of %s names no credential", itemID)
	}
	if err := validComposition(composition); err != nil {
		return Environment{}, err
	}

	e := Environment{
		ID:          record.NewID(IDPrefix),
		Actor:       actor,
		At:          record.Now(),
		Kind:        KindCandidate,
		ProjectID:   projectID,
		Name:        NameForItem(itemID),
		Targets:     targets,
		Credential:  credential,
		ItemID:      itemID,
		Composition: composition,
	}

	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return Environment{}, fmt.Errorf("environment: beginning the composition of %q: %w", e.Name, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, c.token); err != nil {
		return Environment{}, err
	}
	if err := insert(ctx, tx, e); err != nil {
		return Environment{}, err
	}
	if err := openCycle(ctx, tx, actor, e.ID); err != nil {
		return Environment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Environment{}, fmt.Errorf("environment: committing the composition of %q: %w", e.Name, err)
	}
	return e, nil
}

// Recompose rewrites what the environment was composed from, which the deployer
// does at a re-verification and when it composes a reclaimed environment again.
// Where the environment's last cycle was reclaimed it opens a new one, the span
// this composition is counted over being its own; where a cycle is in progress it
// rewrites the composition and leaves that cycle standing.
//
// The composition is the one field of this record that is written twice. The
// alternative was a row per composition, which is a history of what a component
// put on an environment that nothing in the design reads — and what the field can
// already disagree with is what is actually on that environment, which the design
// states and nothing checks. What a run ran against survives on the build, which
// the composition is copied onto at the run.
func (c *Candidates) Recompose(ctx context.Context, actor record.Actor, id string, composition Composition) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if err := validComposition(composition); err != nil {
		return err
	}
	return c.write(ctx, id, "recomposing", func(tx pgx.Tx, e Environment) error {
		if _, err := tx.Exec(ctx, `update `+Table+`
			set composed_from = $1, seed_version = $2, value_set_version = $3 where id = $4`,
			joinComposed(composition.From), composition.SeedVersion, composition.ValueSetVersion, id); err != nil {
			return fmt.Errorf("environment: recomposing %s: %w", id, err)
		}
		_, err := openCycleOf(ctx, tx, id)
		if errors.Is(err, ErrNoOpenCycle) {
			return openCycle(ctx, tx, actor, id)
		}
		return err
	})
}

// RunCouldStart writes the moment the run could start on the cycle in progress,
// which the deployer writes as it does the work. Composition time is that moment
// against the moment composing began, and it is written rather than estimated.
func (c *Candidates) RunCouldStart(ctx context.Context, id string) error {
	return c.write(ctx, id, "recording when the run could start", func(tx pgx.Tx, e Environment) error {
		cycle, err := openCycleOf(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `update `+CycleTable+` set run_could_start_at = $1 where id = $2`,
			record.Now(), cycle.ID); err != nil {
			return fmt.Errorf("environment: recording when the run could start on %s: %w", id, err)
		}
		return nil
	})
}

// write is every write this writer makes after creation: the row is locked while
// its kind and its teardown are read, so a teardown racing a recompose is one of
// the two and an error.
func (c *Candidates) write(ctx context.Context, id, doing string, write func(pgx.Tx, Environment) error) error {
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("environment: beginning %s %s: %w", doing, id, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, c.token); err != nil {
		return err
	}

	e, err := scan(tx.QueryRow(ctx, selectEnvironment+` where id = $1 for update`, id), id)
	if err != nil {
		return err
	}
	if e.Kind != KindCandidate {
		return fmt.Errorf("%w: %s is %s", ErrNotACandidate, id, e.Kind)
	}
	if e.TornDownAt != "" {
		return fmt.Errorf("%w: %s at %s", ErrAlreadyTornDown, id, e.TornDownAt)
	}
	if err := write(tx, e); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("environment: committing %s %s: %w", doing, id, err)
	}
	return nil
}

func validComposition(composition Composition) error {
	for _, d := range composition.From {
		if d.ServiceID == "" || d.ReleaseID == "" {
			return fmt.Errorf("%w, not %q and %q", ErrCompositionIncomplete, d.ServiceID, d.ReleaseID)
		}
	}
	return nil
}
