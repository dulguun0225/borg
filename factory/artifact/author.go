package artifact

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

// By is who authored a version: which of the store's three callers it came
// through, and the identity an authorship prior is kept on. The two are
// separate facts and both are stored — the authorship says whether an agent, a
// human at the stage, or a human at a gate wrote it, and the author says which
// one, so a prior can be computed from that author's own work.
//
// It is a struct and not two arguments so that a caller cannot pass the
// authorship where the author belongs, both being strings.
type By struct {
	Authorship Authorship
	// Author is the model version for a version an agent wrote, and the
	// person's name for one a human wrote. It is kept per model version and
	// not per family: a new version accumulates a prior of its own, and keeping
	// the old name for a new version would read the old version's outcomes as
	// the new one's.
	Author string
}

// NewestOfKind is the newest version of one kind on one item, and false where
// the item has none. It is what the score follows to the author of the change
// under decision: the build was made from the newest implementation version, so
// its author is the author the prior is read on.
func NewestOfKind(ctx context.Context, pool *pgxpool.Pool, itemID string, kind Kind) (Artifact, bool, error) {
	var a Artifact
	var storedKind, authorship, actorKind string
	err := pool.QueryRow(ctx, selectArtifact+`
		where item_id = $1 and kind = $2 order by version desc limit 1`, itemID, string(kind)).
		Scan(&a.ID, &actorKind, &a.Actor.Name, &a.At, &a.ItemID, &storedKind,
			&a.Version, &a.Supersedes, &authorship, &a.Author, &a.Content)
	if errors.Is(err, pgx.ErrNoRows) {
		return Artifact{}, false, nil
	} else if err != nil {
		return Artifact{}, false, fmt.Errorf("artifact: reading the newest %s version of %s: %w", kind, itemID, err)
	}
	a.Actor.Kind = record.Kind(actorKind)
	a.Kind = Kind(storedKind)
	a.Authorship = Authorship(authorship)
	return a, true, nil
}

// IDsByAuthor is every version that author wrote, of any kind and any item. It
// is the set an authorship prior is computed over: every outcome on that
// author's artifact moves the prior, so what the score needs is the artifacts
// and not the items.
//
// An empty author is no versions and no error, rather than every version whose
// author column happens to be empty — the store refuses those anyway.
func IDsByAuthor(ctx context.Context, pool *pgxpool.Pool, author string) ([]string, error) {
	if author == "" {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `select id from `+Table+` where author = $1 order by at, id`, author)
	if err != nil {
		return nil, fmt.Errorf("artifact: reading the versions %s wrote: %w", author, err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("artifact: reading a version %s wrote: %w", author, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("artifact: reading the versions %s wrote: %w", author, err)
	}
	return ids, nil
}
