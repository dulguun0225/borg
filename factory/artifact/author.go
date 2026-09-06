package artifact

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

// NewestOfKind is the newest version of one kind on one item, and false where
// the item has none. It is what the score follows to the author of the change
// under decision: the build was made from the newest implementation version, so
// its author is the author the prior is read on.
func NewestOfKind(ctx context.Context, pool *pgxpool.Pool, itemID string, kind Kind) (Artifact, bool, error) {
	var a Artifact
	var storedKind, authorship, actorKind, actorBasis, enteredBy string
	err := pool.QueryRow(ctx, selectArtifact+`
		where item_id = $1 and kind = $2 order by version desc limit 1`, itemID, string(kind)).
		Scan(&a.ID, &actorKind, &a.Actor.Key, &actorBasis, &a.At, &a.ItemID, &a.Role, &a.Subject, &storedKind,
			&a.Version, &a.Supersedes, &authorship, &a.Author, &a.Content, &a.ContentDigest,
			&a.ShippedBundleIdentity, &enteredBy, &a.InputManifestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Artifact{}, false, nil
	} else if err != nil {
		return Artifact{}, false, fmt.Errorf("artifact: reading the newest %s version of %s: %w", kind, itemID, err)
	}
	a.Actor.Kind = record.Kind(actorKind)
	a.Actor.Basis = record.Basis(actorBasis)
	a.Kind = Kind(storedKind)
	a.Authorship = Authorship(authorship)
	a.EnteredBy = EnteredBy(enteredBy)
	return a, true, nil
}

// IDsByAuthor is every version that author wrote, of any kind and any item. It
// is the set a per-author prior is computed over: every outcome on that
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

// ItemsByAuthor is every item this author wrote a version of, once each, in
// the order the versions were written: an item stands where the author's first
// version of it stands, and a second version of the same item moves it
// nowhere. The prior needs both this and [IDsByAuthor] and they are not the
// same question: a human's verdict is given on an artifact version, and an
// analysis window's exit is an outcome on an item — the release, the deploy,
// and the window all hang off the item and none of them names a version.
//
// A version of a fleet kind names no item and is not one of these, its chain
// being a role, a subject, or the factory as a whole.
//
// An empty author is no items and no error, for the reason [IDsByAuthor] gives.
func ItemsByAuthor(ctx context.Context, pool *pgxpool.Pool, author string) ([]string, error) {
	if author == "" {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `select item_id, min(at) as first_at from `+Table+`
		where author = $1 and item_id <> '' group by item_id order by first_at, item_id`, author)
	if err != nil {
		return nil, fmt.Errorf("artifact: reading the items %s wrote a version of: %w", author, err)
	}
	defer rows.Close()

	var items []string
	for rows.Next() {
		var id, firstAt string
		if err := rows.Scan(&id, &firstAt); err != nil {
			return nil, fmt.Errorf("artifact: reading an item %s wrote a version of: %w", author, err)
		}
		items = append(items, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("artifact: reading the items %s wrote a version of: %w", author, err)
	}
	return items, nil
}
