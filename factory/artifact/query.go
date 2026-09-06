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
	version, supersedes, authorship, author, content, content_digest, shipped_bundle_identity, entered_by,
	input_manifest_id
	from ` + Table

// Get is the artifact with the given id. It takes the pool and not a
// [Store], because reading a version is not a reason to be handed the thing
// that writes them.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Artifact, error) {
	var a Artifact
	var kind, authorship, actorKind, actorBasis, enteredBy string
	err := pool.QueryRow(ctx, selectArtifact+` where id = $1`, id).Scan(
		&a.ID, &actorKind, &a.Actor.Key, &actorBasis, &a.At, &a.ItemID, &a.Role, &a.Subject, &kind,
		&a.Version, &a.Supersedes, &authorship, &a.Author, &a.Content, &a.ContentDigest,
		&a.ShippedBundleIdentity, &enteredBy, &a.InputManifestID)
	if err != nil {
		return Artifact{}, fmt.Errorf("artifact: reading %s: %w", id, err)
	}
	a.Actor.Kind = record.Kind(actorKind)
	a.Actor.Basis = record.Basis(actorBasis)
	a.Kind = Kind(kind)
	a.Authorship = Authorship(authorship)
	a.EnteredBy = EnteredBy(enteredBy)
	return a, nil
}

// NewestShipped is the newest version of one fleet chain that a start entered
// rather than anybody authored — [EnteredBys] naming the two events — and false
// where no start has entered one. It is what the first-start step reads: the
// entry carries the shipped-bundle identity it entered under, so a start whose
// bundle is already on it is not a first start on that version and enters
// nothing, however many versions have been authored over it since.
func NewestShipped(ctx context.Context, pool *pgxpool.Pool, kind Kind, role, subject string) (Artifact, bool, error) {
	var a Artifact
	var storedKind, authorship, actorKind, actorBasis, enteredBy string
	err := pool.QueryRow(ctx, selectArtifact+`
		where kind = $1 and role = $2 and subject = $3 and entered_by <> ''
		order by version desc limit 1`, string(kind), role, subject).
		Scan(&a.ID, &actorKind, &a.Actor.Key, &actorBasis, &a.At, &a.ItemID, &a.Role, &a.Subject, &storedKind,
			&a.Version, &a.Supersedes, &authorship, &a.Author, &a.Content, &a.ContentDigest,
			&a.ShippedBundleIdentity, &enteredBy, &a.InputManifestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Artifact{}, false, nil
	} else if err != nil {
		return Artifact{}, false, fmt.Errorf("artifact: reading the newest entered %s: %w", kind, err)
	}
	a.Actor.Kind = record.Kind(actorKind)
	a.Actor.Basis = record.Basis(actorBasis)
	a.Kind = Kind(storedKind)
	a.Authorship = Authorship(authorship)
	a.EnteredBy = EnteredBy(enteredBy)
	return a, true, nil
}

// Newest is the newest version of one fleet chain — kind, and the role or
// subject naming it — whatever decided it, and false where the chain is empty.
// It is not "in force": a version not approved is not in force with the one
// below it still standing, which is [InForce]'s question, and it is not
// [NewestShipped], which answers what the first-start step reads.
func Newest(ctx context.Context, pool *pgxpool.Pool, kind Kind, role, subject string) (Artifact, bool, error) {
	var a Artifact
	var storedKind, authorship, actorKind, actorBasis, enteredBy string
	err := pool.QueryRow(ctx, selectArtifact+`
		where kind = $1 and role = $2 and subject = $3
		order by version desc limit 1`, string(kind), role, subject).
		Scan(&a.ID, &actorKind, &a.Actor.Key, &actorBasis, &a.At, &a.ItemID, &a.Role, &a.Subject, &storedKind,
			&a.Version, &a.Supersedes, &authorship, &a.Author, &a.Content, &a.ContentDigest,
			&a.ShippedBundleIdentity, &enteredBy, &a.InputManifestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Artifact{}, false, nil
	} else if err != nil {
		return Artifact{}, false, fmt.Errorf("artifact: reading the newest %s: %w", kind, err)
	}
	a.Actor.Kind = record.Kind(actorKind)
	a.Actor.Basis = record.Basis(actorBasis)
	a.Kind = Kind(storedKind)
	a.Authorship = Authorship(authorship)
	a.EnteredBy = EnteredBy(enteredBy)
	return a, true, nil
}

// InForce is the newest version of one chain — kind, and the role or subject
// naming it, empty for the one selection rule — that is either in
// approvedVersionIDs or an entry the install wrote. Approval and withdrawal
// are the log's facts, which this package does not import, so the caller
// supplies them already combined: approvedVersionIDs names only versions that
// were approved and that nothing has withdrawn since.
//
// The install's entries alone stand without an approval, a factory with
// nothing decided in it having to run, and entered_by is what says which
// entries those are: an upgrade's first start enters a version awaiting the
// gate every version fires, so it is in force only once it is among the
// approved ids. False where the chain has neither — nothing in force, which is
// the reading a chain an upgrade just started gets until a human decides its
// first version.
func InForce(ctx context.Context, pool *pgxpool.Pool, kind Kind, role, subject string, approvedVersionIDs []string) (Artifact, bool, error) {
	if approvedVersionIDs == nil {
		approvedVersionIDs = []string{}
	}
	var a Artifact
	var storedKind, authorship, actorKind, actorBasis, enteredBy string
	err := pool.QueryRow(ctx, selectArtifact+`
		where kind = $1 and role = $2 and subject = $3 and (id = any($4) or entered_by = $5)
		order by version desc limit 1`, string(kind), role, subject, approvedVersionIDs, string(EnteredByInstall)).
		Scan(&a.ID, &actorKind, &a.Actor.Key, &actorBasis, &a.At, &a.ItemID, &a.Role, &a.Subject, &storedKind,
			&a.Version, &a.Supersedes, &authorship, &a.Author, &a.Content, &a.ContentDigest,
			&a.ShippedBundleIdentity, &enteredBy, &a.InputManifestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Artifact{}, false, nil
	} else if err != nil {
		return Artifact{}, false, fmt.Errorf("artifact: reading what is in force for %s: %w", kind, err)
	}
	a.Actor.Kind = record.Kind(actorKind)
	a.Actor.Basis = record.Basis(actorBasis)
	a.Kind = Kind(storedKind)
	a.Authorship = Authorship(authorship)
	a.EnteredBy = EnteredBy(enteredBy)
	return a, true, nil
}
