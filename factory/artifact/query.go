package artifact

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

const selectArtifact = `select id, actor_kind, actor_key, actor_key_basis, at, item_id, role, subject, kind,
	version, supersedes, authorship, author, content, content_digest, shipped_bundle_identity, input_manifest_id
	from ` + Table

// Get is the artifact with the given id. It takes the pool and not a
// [Store], because reading a version is not a reason to be handed the thing
// that writes them.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Artifact, error) {
	var a Artifact
	var kind, authorship, actorKind, actorBasis string
	err := pool.QueryRow(ctx, selectArtifact+` where id = $1`, id).Scan(
		&a.ID, &actorKind, &a.Actor.Key, &actorBasis, &a.At, &a.ItemID, &a.Role, &a.Subject, &kind,
		&a.Version, &a.Supersedes, &authorship, &a.Author, &a.Content, &a.ContentDigest,
		&a.ShippedBundleIdentity, &a.InputManifestID)
	if err != nil {
		return Artifact{}, fmt.Errorf("artifact: reading %s: %w", id, err)
	}
	a.Actor.Kind = record.Kind(actorKind)
	a.Actor.Basis = record.Basis(actorBasis)
	a.Kind = Kind(kind)
	a.Authorship = Authorship(authorship)
	return a, nil
}

// InForce is the newest version of one chain — kind, and the role or subject
// naming it, empty for the one selection rule — whose id is in
// approvedVersionIDs. Approval and withdrawal are the log's facts, which this
// package does not import, so the caller supplies them already combined:
// approvedVersionIDs names only versions that were approved and that nothing
// has withdrawn since. False where none of the chain's versions is in that
// set — nothing in force, which is the reading a chain an upgrade just started
// gets until a human decides its first version.
func InForce(ctx context.Context, pool *pgxpool.Pool, kind Kind, role, subject string, approvedVersionIDs []string) (Artifact, bool, error) {
	if len(approvedVersionIDs) == 0 {
		return Artifact{}, false, nil
	}
	var a Artifact
	var storedKind, authorship, actorKind, actorBasis string
	err := pool.QueryRow(ctx, selectArtifact+`
		where kind = $1 and role = $2 and subject = $3 and id = any($4)
		order by version desc limit 1`, string(kind), role, subject, approvedVersionIDs).
		Scan(&a.ID, &actorKind, &a.Actor.Key, &actorBasis, &a.At, &a.ItemID, &a.Role, &a.Subject, &storedKind,
			&a.Version, &a.Supersedes, &authorship, &a.Author, &a.Content, &a.ContentDigest,
			&a.ShippedBundleIdentity, &a.InputManifestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Artifact{}, false, nil
	} else if err != nil {
		return Artifact{}, false, fmt.Errorf("artifact: reading what is in force for %s: %w", kind, err)
	}
	a.Actor.Kind = record.Kind(actorKind)
	a.Actor.Basis = record.Basis(actorBasis)
	a.Kind = Kind(storedKind)
	a.Authorship = Authorship(authorship)
	return a, true, nil
}
