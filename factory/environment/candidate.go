package environment

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
)

var (
	// ErrItemIDEmpty is returned by [Candidates.Compose] for a candidate
	// environment naming no item. record's doc.go states what a link is checked
	// for; this one is what the record is for.
	ErrItemIDEmpty = errors.New("environment: a candidate's environment names the item it belongs to")
	// ErrNotACandidate is returned by [Candidates.Recompose] and
	// [Candidates.TearDown] for an environment that is not a candidate's. Only
	// a candidate's is recomposed and only a candidate's is torn down.
	ErrNotACandidate = errors.New("environment: only a candidate's environment is recomposed or torn down")
	// ErrAlreadyTornDown is returned by [Candidates.Recompose] and
	// [Candidates.TearDown] for an environment already torn down. Teardown does
	// not run twice, and nothing here puts one back.
	ErrAlreadyTornDown = errors.New("environment: the environment is already torn down")
)

// Composed is one dependency the deploy agent put in place beside the
// candidate: the service it is a release of, and the release of it that was
// current when the environment was composed. It is what was current then and
// not what is current now, which is the whole point of storing it.
type Composed struct {
	ServiceID string
	ReleaseID string
}

// NameForItem is the name a candidate's environment carries. It is derived from
// the item rather than chosen, because the name column is unique and two
// candidates of one service would otherwise have to agree on a name between
// them.
func NameForItem(itemID string) string { return "candidate/" + itemID }

// Candidates is the candidate kind's one writer: the deploy agent, which is the
// only component that reaches a deploy target at all. It creates an environment
// at the approval of Deploy to candidate environment and tears it down when the
// item merges, is dropped, or is superseded by a re-decomposition.
type Candidates struct {
	pool *pgxpool.Pool
}

// NewCandidates returns the writer over pool.
func NewCandidates(pool *pgxpool.Pool) *Candidates { return &Candidates{pool: pool} }

// Compose creates the environment for one item, naming the targets a deploy into
// it is performed against, the credential it is performed with, and what it was
// composed from — the current release of each of the candidate's dependencies,
// which is empty where the item declared none.
//
// A second call for one item is refused by the unique constraint on the name,
// which is derived from the item: the environment is the item's and persists
// across a rebuild, so a rebuild recomposes rather than composing again.
func (c *Candidates) Compose(ctx context.Context, actor record.Actor, itemID string,
	targets []string, credential secretref.Ref, composedFrom []Composed) (Environment, error) {
	if err := actor.Validate(); err != nil {
		return Environment{}, err
	}
	if itemID == "" {
		return Environment{}, ErrItemIDEmpty
	}
	if len(targets) == 0 {
		return Environment{}, ErrTargetsEmpty
	}
	if credential.Name() == "" {
		return Environment{}, fmt.Errorf("environment: the candidate environment of %s names no credential", itemID)
	}
	if err := validComposition(composedFrom); err != nil {
		return Environment{}, err
	}

	e := Environment{
		ID:           record.NewID(IDPrefix),
		Actor:        actor,
		At:           record.Now(),
		Kind:         KindCandidate,
		Name:         NameForItem(itemID),
		Targets:      targets,
		Credential:   credential,
		ItemID:       itemID,
		ComposedFrom: composedFrom,
	}
	if err := insert(ctx, c.pool, e); err != nil {
		return Environment{}, err
	}
	return e, nil
}

// Recompose rewrites what the environment was composed from, which the deploy
// agent does at a re-verification: the dependencies' current releases have moved
// since the environment was created, and what is stored is what was put there.
//
// It is the one field of this record that is written twice. The alternative was
// a row per composition, which is a history of what a component put on an
// environment that nothing in the design reads — and what the field can already
// disagree with is what is actually on that environment, which the design states
// and nothing checks.
func (c *Candidates) Recompose(ctx context.Context, id string, composedFrom []Composed) error {
	if err := validComposition(composedFrom); err != nil {
		return err
	}
	return c.update(ctx, id, `update `+Table+` set composed_from = $1 where id = $2`,
		joinComposed(composedFrom), "recomposing")
}

// TearDown writes the time the environment was torn down and keeps the row. Its
// caller stops the software first: the record and the process are two facts,
// and a record saying torn down over a process still running is the
// disagreement the drift detector exists to find.
func (c *Candidates) TearDown(ctx context.Context, id string) error {
	return c.update(ctx, id, `update `+Table+` set torn_down_at = $1 where id = $2`,
		record.Now(), "tearing down")
}

// update is the two writes this writer makes after creation: the row is locked
// while its kind and its teardown are read, so a teardown racing a recompose is
// one of the two and an error.
func (c *Candidates) update(ctx context.Context, id, statement string, value, doing string) error {
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("environment: beginning %s %s: %w", doing, id, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var kind, tornDown string
	err = tx.QueryRow(ctx, `select kind, torn_down_at from `+Table+` where id = $1 for update`, id).
		Scan(&kind, &tornDown)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return fmt.Errorf("environment: reading %s: %w", id, err)
	}
	if Kind(kind) != KindCandidate {
		return fmt.Errorf("%w: %s is %s", ErrNotACandidate, id, kind)
	}
	if tornDown != "" {
		return fmt.Errorf("%w: %s at %s", ErrAlreadyTornDown, id, tornDown)
	}

	if _, err := tx.Exec(ctx, statement, value, id); err != nil {
		return fmt.Errorf("environment: %s %s: %w", doing, id, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("environment: committing %s %s: %w", doing, id, err)
	}
	return nil
}

func validComposition(composedFrom []Composed) error {
	for _, d := range composedFrom {
		if d.ServiceID == "" || d.ReleaseID == "" {
			return fmt.Errorf("environment: a composition entry names a service and a release, not %q and %q",
				d.ServiceID, d.ReleaseID)
		}
	}
	return nil
}
