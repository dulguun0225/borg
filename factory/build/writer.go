package build

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/exposure"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrCommitHashEmpty is returned by [Writer.Create] for a build naming no
	// commit.
	ErrCommitHashEmpty = errors.New("build: the commit hash is empty")
	// ErrServiceIDEmpty is returned by [Writer.Create] for a build naming no
	// service. record's doc.go states what a link is checked for.
	ErrServiceIDEmpty = errors.New("build: the service id is empty")
	// ErrArtifactDigestEmpty is returned by [Writer.Create] for a build
	// naming no artifact digest.
	ErrArtifactDigestEmpty = errors.New("build: the artifact digest is empty")
	// ErrShippedBundleIdentityEmpty is returned by [Writer.Create] for a build
	// naming no release of the product. Every build names one, which is what
	// says which way in the build carries.
	ErrShippedBundleIdentityEmpty = errors.New("build: the shipped bundle identity is empty")
	// ErrNotFound is returned where the named build does not exist.
	ErrNotFound = errors.New("build: no build has that id")
)

// ResolvedEntry is one package a build resolved: the ecosystem, the source it
// was resolved from, the package and version, the digest of the content
// resolved, the declared licence, and what required it. Digest, Licence and
// RequiredBy are empty where the resolver could not produce them, which is
// that entry's own coverage and not an error.
type ResolvedEntry struct {
	Ecosystem  string
	Source     string
	Package    string
	Version    string
	Digest     string
	Licence    string
	RequiredBy string
}

// Draft is one build as a caller of [Writer.Create] hands it in.
type Draft struct {
	// ItemID is empty on a search build, which names a service and no item.
	ItemID     string
	ServiceID  string
	CommitHash string
	// ArtifactDigest is the digest of the artifact the build runner
	// produced.
	ArtifactDigest string
	Resolved       []ResolvedEntry
	// ResolvedSetCoverage is what the resolver read, keyed by ecosystem, and
	// is ignored where ResolvedSetCouldNotDerive is set.
	ResolvedSetCoverage map[string]string
	// ResolvedSetCouldNotDerive is the reason where resolution could not be
	// performed at all, and empty otherwise.
	ResolvedSetCouldNotDerive string
	// NoticeFile is the notice text produced from the resolved set in this
	// same write, or "could not derive" where the set is.
	NoticeFile string
	// DesignSystemConstraintID is empty on a build in a project with no user
	// interface.
	DesignSystemConstraintID string
	// ShippedBundleIdentity names the release of the product that made this
	// build, on every build and never empty.
	ShippedBundleIdentity string
	// Exposure is the exposure list the build runner derived from the diff
	// between the base and this build's commit, and nil where no extractor ran
	// for the toolchain. Nil and an empty list are different readings: nil is a
	// build nobody read, which resolves the score's exposure factor, and an
	// empty list is a diff that reached nothing new, which lowers the number.
	Exposure *exposure.Evidence
	// DeclaresSchemaChange is whether the checkout this build was made from
	// ships a schema change. It is the build's own reading, and it is what the
	// store rule asks before it requires the candidate environment to have
	// applied the change twice.
	DeclaresSchemaChange bool
	// Results is what the build's own process decided: every criterion whose
	// encoding declares the build, decided as the build runner performed it.
	// [Writer.Create] records these as run 0 through [criterion.InsertResults],
	// inside the same transaction as the build row.
	Results map[string]criterion.Outcome
}

// Build is one build record as it is stored.
type Build struct {
	ID                        string
	Actor                     record.Actor
	At                        string
	ItemID                    string
	ServiceID                 string
	CommitHash                string
	ArtifactDigest            string
	ResolvedSetCoverage       map[string]string
	ResolvedSetCouldNotDerive string
	NoticeFile                string
	DesignSystemConstraintID  string
	ShippedBundleIdentity     string
	DeclaresSchemaChange      bool
}

// Writer is the one writer of build records.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewWriter returns the writer over pool, fencing every write with token.
func NewWriter(pool *pgxpool.Pool, token lease.Token) *Writer {
	return &Writer{pool: pool, token: token}
}

const insertBuild = `insert into ` + Table + `
	(id, format_version, actor_kind, actor_key, actor_key_basis, at, item_id, service_id, commit_hash,
	artifact_digest, resolved_set_coverage, resolved_set_could_not_derive, notice_file,
	design_system_constraint_id, shipped_bundle_identity, exposure, declares_schema_change)
	values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`

const insertResolvedEntry = `insert into ` + ResolvedTable + `
	(id, format_version, actor_kind, actor_key, actor_key_basis, at, build_id, ecosystem, source, package,
	version, digest, licence, required_by)
	values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`

// Create writes the build record, its resolved entries, and — through
// [criterion.InsertResults] — what the build's own process decided, all in
// one transaction. The record is never written again — there is no update
// method — and a rebuild of the same commit for the same item and service is
// refused by the store's unique constraint rather than given a second record:
// a rebuild is a new build, and the caller asks [ForCommit] first if it wants
// to know which record is already there.
func (w *Writer) Create(ctx context.Context, actor record.Actor, draft Draft) (Build, error) {
	if err := actor.Validate(); err != nil {
		return Build{}, err
	}
	if draft.ServiceID == "" {
		return Build{}, ErrServiceIDEmpty
	}
	if draft.CommitHash == "" {
		return Build{}, ErrCommitHashEmpty
	}
	if draft.ArtifactDigest == "" {
		return Build{}, ErrArtifactDigestEmpty
	}
	if draft.ShippedBundleIdentity == "" {
		return Build{}, ErrShippedBundleIdentityEmpty
	}

	coverage, err := json.Marshal(draft.ResolvedSetCoverage)
	if err != nil {
		return Build{}, fmt.Errorf("build: encoding the resolved set coverage: %w", err)
	}
	var reached *string
	if draft.Exposure != nil {
		encoded, err := json.Marshal(draft.Exposure)
		if err != nil {
			return Build{}, fmt.Errorf("build: encoding the exposure list: %w", err)
		}
		text := string(encoded)
		reached = &text
	}

	b := Build{
		ID:                        record.NewID(IDPrefix),
		Actor:                     actor,
		At:                        record.Now(),
		ItemID:                    draft.ItemID,
		ServiceID:                 draft.ServiceID,
		CommitHash:                draft.CommitHash,
		ArtifactDigest:            draft.ArtifactDigest,
		ResolvedSetCoverage:       draft.ResolvedSetCoverage,
		ResolvedSetCouldNotDerive: draft.ResolvedSetCouldNotDerive,
		NoticeFile:                draft.NoticeFile,
		DesignSystemConstraintID:  draft.DesignSystemConstraintID,
		ShippedBundleIdentity:     draft.ShippedBundleIdentity,
		DeclaresSchemaChange:      draft.DeclaresSchemaChange,
	}

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Build{}, fmt.Errorf("build: beginning the creation of %s: %w", b.ID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return Build{}, err
	}

	if _, err := tx.Exec(ctx, insertBuild,
		b.ID, FormatVersion, string(b.Actor.Kind), b.Actor.Key, string(b.Actor.Basis), b.At,
		b.ItemID, b.ServiceID, b.CommitHash, b.ArtifactDigest, string(coverage),
		b.ResolvedSetCouldNotDerive, b.NoticeFile, b.DesignSystemConstraintID, b.ShippedBundleIdentity,
		reached, b.DeclaresSchemaChange,
	); err != nil {
		return Build{}, fmt.Errorf("build: creating %s: %w", b.ID, err)
	}

	for _, entry := range draft.Resolved {
		if _, err := tx.Exec(ctx, insertResolvedEntry,
			record.NewID(ResolvedIDPrefix), FormatVersionResolved,
			string(actor.Kind), actor.Key, string(actor.Basis), record.Now(),
			b.ID, entry.Ecosystem, entry.Source, entry.Package, entry.Version,
			entry.Digest, entry.Licence, entry.RequiredBy,
		); err != nil {
			return Build{}, fmt.Errorf("build: recording what %s resolved: %w", b.ID, err)
		}
	}

	if len(draft.Results) > 0 {
		run := criterion.Run{BuildID: b.ID, Number: 0, Place: criterion.PlaceBuild}
		if err := criterion.InsertResults(ctx, tx, actor, run, draft.Results); err != nil {
			return Build{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Build{}, fmt.Errorf("build: committing %s: %w", b.ID, err)
	}
	return b, nil
}

const selectBuild = `select id, actor_kind, actor_key, actor_key_basis, at, item_id, service_id, commit_hash,
	artifact_digest, resolved_set_coverage, resolved_set_could_not_derive, notice_file,
	design_system_constraint_id, shipped_bundle_identity, declares_schema_change
	from ` + Table

// Get is one build by id. It takes the pool and not a [Writer], because
// reading a build is not a reason to be handed the thing that writes them.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Build, error) {
	b, err := scan(pool.QueryRow(ctx, selectBuild+` where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Build{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return Build{}, fmt.Errorf("build: reading %s: %w", id, err)
	}
	return b, nil
}

// ForCommit is the build of one item at one commit, and false where there is
// none. It is what a caller asks before writing one: a rebuild is a new
// build, so a re-verification that produced the commit already built
// produced no build, and [Writer.Create] would be refused by the unique
// constraint rather than answer which record is already there.
func ForCommit(ctx context.Context, pool *pgxpool.Pool, itemID, serviceID, commitHash string) (Build, bool, error) {
	b, err := scan(pool.QueryRow(ctx, selectBuild+` where item_id = $1 and service_id = $2 and commit_hash = $3`,
		itemID, serviceID, commitHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return Build{}, false, nil
	} else if err != nil {
		return Build{}, false, fmt.Errorf("build: reading the build of %s at %s: %w", itemID, commitHash, err)
	}
	return b, true, nil
}

// scanner is what [pgx.Row] and [pgx.Rows] share, so one scan reads either.
type scanner interface {
	Scan(dest ...any) error
}

func scan(row scanner) (Build, error) {
	var b Build
	var kind, basis, coverage string
	if err := row.Scan(&b.ID, &kind, &b.Actor.Key, &basis, &b.At, &b.ItemID, &b.ServiceID, &b.CommitHash,
		&b.ArtifactDigest, &coverage, &b.ResolvedSetCouldNotDerive, &b.NoticeFile,
		&b.DesignSystemConstraintID, &b.ShippedBundleIdentity, &b.DeclaresSchemaChange); err != nil {
		return Build{}, err
	}
	b.Actor.Kind = record.Kind(kind)
	b.Actor.Basis = record.Basis(basis)
	if coverage != "" {
		if err := json.Unmarshal([]byte(coverage), &b.ResolvedSetCoverage); err != nil {
			return Build{}, fmt.Errorf("build: decoding the resolved set coverage of %s: %w", b.ID, err)
		}
	}
	return b, nil
}

// Newest is the item's newest build, and false where the item has none. It is
// what a reader outside a run asks: a run holds the build it just made, and a
// command that reads the records rather than making one has to find it.
//
// Newest by the time the record was written, which is the order the builds were
// made in — a rebuild is a new build, so an item has as many as it was built.
func Newest(ctx context.Context, pool *pgxpool.Pool, itemID string) (Build, bool, error) {
	if itemID == "" {
		return Build{}, false, nil
	}
	b, err := scan(pool.QueryRow(ctx, selectBuild+` where item_id = $1 order by at desc, id desc limit 1`, itemID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Build{}, false, nil
	} else if err != nil {
		return Build{}, false, fmt.Errorf("build: reading the newest build of %s: %w", itemID, err)
	}
	return b, true, nil
}

// Resolved is what one build resolved, in the order the entries were written.
// It is a read of the table this package owns and takes the pool for the reason
// [Get] does.
//
// The merge queue is what asks: at a re-verification the re-resolved set's
// digests are compared to the approved build's, and a difference rejects the
// candidate there. A version is not an identity for bytes, so the comparison is
// of the digests and not of the versions, and an entry whose resolver produced
// no digest carries the field empty — which the comparison reads as it stands,
// this package deciding nothing about it.
func Resolved(ctx context.Context, pool *pgxpool.Pool, buildID string) ([]ResolvedEntry, error) {
	rows, err := pool.Query(ctx, `select ecosystem, source, package, version, digest, licence, required_by
		from `+ResolvedTable+` where build_id = $1 order by at, id`, buildID)
	if err != nil {
		return nil, fmt.Errorf("build: reading what %s resolved: %w", buildID, err)
	}
	defer rows.Close()

	var read []ResolvedEntry
	for rows.Next() {
		var e ResolvedEntry
		if err := rows.Scan(&e.Ecosystem, &e.Source, &e.Package, &e.Version, &e.Digest,
			&e.Licence, &e.RequiredBy); err != nil {
			return nil, fmt.Errorf("build: reading an entry of what %s resolved: %w", buildID, err)
		}
		read = append(read, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("build: reading what %s resolved: %w", buildID, err)
	}
	return read, nil
}

// Exposure is the exposure list one build's runner derived, and false where the
// record holds none — a build no extractor ran for, whose factor is resolved
// rather than read as nothing. An empty list is a reading and answers true: the
// diff reached nothing new.
//
// It is a read of its own and not a field of [Build], because it is the one
// column here a reader either wants whole or not at all: what a human at
// Implementation argues with is the list beside the diff, and every other reader
// of a build record wants none of it.
func Exposure(ctx context.Context, pool *pgxpool.Pool, buildID string) (exposure.Evidence, bool, error) {
	var stored *string
	err := pool.QueryRow(ctx, `select exposure from `+Table+` where id = $1`, buildID).Scan(&stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return exposure.Evidence{}, false, fmt.Errorf("%w: %s", ErrNotFound, buildID)
	} else if err != nil {
		return exposure.Evidence{}, false, fmt.Errorf("build: reading what %s reached: %w", buildID, err)
	}
	if stored == nil {
		return exposure.Evidence{}, false, nil
	}
	var read exposure.Evidence
	if err := json.Unmarshal([]byte(*stored), &read); err != nil {
		return exposure.Evidence{}, false, fmt.Errorf("build: decoding what %s reached: %w", buildID, err)
	}
	return read, true, nil
}
