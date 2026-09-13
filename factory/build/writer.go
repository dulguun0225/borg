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
	// ErrArtifactDigestEmpty is returned by [Writer.Create] for a build that ran
	// but names no artifact digest.
	ErrArtifactDigestEmpty = errors.New("build: the artifact digest is empty")
	// ErrRunReasonEmpty is returned by [Writer.Create] for a build marked as not
	// run without explaining why it stopped.
	ErrRunReasonEmpty = errors.New("build: the run reason is empty")
	// ErrShippedBundleIdentityEmpty is returned by [Writer.Create] for a build
	// naming no release of the product. Every build names one, which is what
	// says which way in the build carries.
	ErrShippedBundleIdentityEmpty = errors.New("build: the shipped bundle identity is empty")
	// ErrSearchOriginEmpty is returned when a search build names no release build.
	ErrSearchOriginEmpty = errors.New("build: the search build origin is empty")
	// ErrResolvedSetEmpty is returned when a build names neither a resolved set
	// nor coverage nor the reason no set could be derived.
	ErrResolvedSetEmpty = errors.New("build: the resolved set has no entries or coverage")
	// ErrNoticeFileMismatch is returned when the notice does not describe the
	// resolved-set state the record names.
	ErrNoticeFileMismatch = errors.New("build: the notice file does not match the resolved set")
	// ErrDesignSystemConstraintMismatch is returned when a search build tries to
	// name a constraint different from its origin build.
	ErrDesignSystemConstraintMismatch = errors.New("build: the search build constraint differs from its origin")
	// ErrNotFound is returned where the named build does not exist.
	ErrNotFound = errors.New("build: no build has that id")
)

// CouldNotDeriveNotice is the notice stored beside a set that could not be
// derived.
const CouldNotDeriveNotice = "could not derive"

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
	RunTime    bool
	BuildTime  bool
}

// RunState says whether the build process ran. A did-not-run record is still a
// build record: it names the resolved set and the reason Implementation must
// reject it, but it has no artifact.
type RunState string

const (
	// RunStarted is the state between writing the resolved set and completing
	// the build process.
	RunStarted   RunState = "started"
	RunCompleted RunState = "ran"
	RunDidNotRun RunState = "did_not_run"
)

// Coverage is typed evidence of what a toolchain's resolver covered.
type Coverage struct {
	Ecosystem                 string
	Source                    string
	BaseImagePackages         bool
	VendoredSource            bool
	StaticallyLinkedCode      bool
	Digests                   bool
	FetchWithoutRunning       bool
	FetchWithoutRunningReason string
}

// Draft is one build as a caller of [Writer.Create] hands it in.
type Draft struct {
	// ItemID is empty on a search build, which names a service and no item.
	ItemID     string
	ServiceID  string
	CommitHash string
	// ArtifactDigest is the digest of the artifact the build runner produced;
	// it is empty on a build that did not run.
	ArtifactDigest string
	Resolved       []ResolvedEntry
	Coverage       []Coverage
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
	RunState              RunState
	RunReason             string
	SearchBuild           bool
	SearchOriginBuildID   string
	SchemaMarks           []string
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
	Coverage                  []Coverage
	ResolvedSetCouldNotDerive string
	NoticeFile                string
	DesignSystemConstraintID  string
	ShippedBundleIdentity     string
	RunState                  RunState
	RunReason                 string
	SearchBuild               bool
	SearchOriginBuildID       string
	SchemaMarks               []string
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
	run_state, run_reason, artifact_digest, resolved_set_could_not_derive, notice_file,
	design_system_constraint_id, shipped_bundle_identity, search_origin_build_id, search_build,
	schema_marks, exposure, declares_schema_change)
	values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)`

const insertResolvedEntry = `insert into ` + ResolvedTable + `
	(id, format_version, actor_kind, actor_key, actor_key_basis, at, build_id, ecosystem, source, package,
	version, digest, licence, required_by, run_time, build_time)
	values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`

const insertCoverage = `insert into ` + CoverageTable + `
	(id, format_version, actor_kind, actor_key, actor_key_basis, at, build_id, ecosystem, source,
	base_image_packages, vendored_source, statically_linked_code, digests, fetch_without_running,
	fetch_without_running_reason)
	values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`

// Create writes the build record, its coverage, resolved entries, and — through
// [criterion.InsertResults] — what the build's own process decided, all in one
// transaction. A [RunStarted] record has no artifact or results yet and is
// completed by [Writer.Complete], without creating a second build row.
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
	if draft.RunState == "" {
		draft.RunState = RunCompleted
	}
	if draft.RunState != RunStarted && draft.RunState != RunCompleted && draft.RunState != RunDidNotRun {
		return Build{}, fmt.Errorf("build: unknown run state %q", draft.RunState)
	}
	if draft.RunState == RunCompleted && draft.ArtifactDigest == "" {
		return Build{}, ErrArtifactDigestEmpty
	}
	if draft.RunState == RunDidNotRun && draft.RunReason == "" {
		return Build{}, ErrRunReasonEmpty
	}
	if draft.RunState == RunStarted && (draft.ArtifactDigest != "" || draft.RunReason != "") {
		return Build{}, fmt.Errorf("build: a started record has an artifact or run reason")
	}
	if draft.ShippedBundleIdentity == "" {
		return Build{}, ErrShippedBundleIdentityEmpty
	}
	if draft.ItemID == "" && (!draft.SearchBuild || draft.SearchOriginBuildID == "") {
		return Build{}, ErrSearchOriginEmpty
	}
	if len(draft.Resolved) == 0 && len(draft.Coverage) == 0 && draft.ResolvedSetCouldNotDerive == "" {
		return Build{}, ErrResolvedSetEmpty
	}
	if (draft.NoticeFile == CouldNotDeriveNotice) != noticeNeedsDerivation(draft) {
		return Build{}, ErrNoticeFileMismatch
	}
	if draft.SearchBuild {
		origin, err := Get(ctx, w.pool, draft.SearchOriginBuildID)
		if err != nil {
			return Build{}, fmt.Errorf("build: reading search origin %s: %w", draft.SearchOriginBuildID, err)
		}
		if draft.DesignSystemConstraintID != "" && draft.DesignSystemConstraintID != origin.DesignSystemConstraintID {
			return Build{}, ErrDesignSystemConstraintMismatch
		}
		draft.DesignSystemConstraintID = origin.DesignSystemConstraintID
	}

	marks, err := json.Marshal(draft.SchemaMarks)
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
		Coverage:                  draft.Coverage,
		ResolvedSetCouldNotDerive: draft.ResolvedSetCouldNotDerive,
		NoticeFile:                draft.NoticeFile,
		DesignSystemConstraintID:  draft.DesignSystemConstraintID,
		ShippedBundleIdentity:     draft.ShippedBundleIdentity,
		RunState:                  draft.RunState,
		RunReason:                 draft.RunReason,
		SearchBuild:               draft.SearchBuild,
		SearchOriginBuildID:       draft.SearchOriginBuildID,
		SchemaMarks:               draft.SchemaMarks,
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
		b.ItemID, b.ServiceID, b.CommitHash, b.RunState, b.RunReason, b.ArtifactDigest,
		b.ResolvedSetCouldNotDerive, b.NoticeFile, b.DesignSystemConstraintID, b.ShippedBundleIdentity,
		b.SearchOriginBuildID, b.SearchBuild, string(marks), reached, b.DeclaresSchemaChange,
	); err != nil {
		return Build{}, fmt.Errorf("build: creating %s: %w", b.ID, err)
	}

	for _, entry := range draft.Resolved {
		if _, err := tx.Exec(ctx, insertResolvedEntry,
			record.NewID(ResolvedIDPrefix), FormatVersionResolved,
			string(actor.Kind), actor.Key, string(actor.Basis), record.Now(),
			b.ID, entry.Ecosystem, entry.Source, entry.Package, entry.Version,
			entry.Digest, entry.Licence, entry.RequiredBy, entry.RunTime, entry.BuildTime,
		); err != nil {
			return Build{}, fmt.Errorf("build: recording what %s resolved: %w", b.ID, err)
		}
	}
	for _, coverage := range draft.Coverage {
		if _, err := tx.Exec(ctx, insertCoverage,
			record.NewID(CoverageIDPrefix), FormatVersionCoverage,
			string(actor.Kind), actor.Key, string(actor.Basis), record.Now(), b.ID,
			coverage.Ecosystem, coverage.Source, coverage.BaseImagePackages,
			coverage.VendoredSource, coverage.StaticallyLinkedCode,
			coverage.Digests, coverage.FetchWithoutRunning, coverage.FetchWithoutRunningReason,
		); err != nil {
			return Build{}, fmt.Errorf("build: recording coverage of %s: %w", b.ID, err)
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

func noticeNeedsDerivation(draft Draft) bool {
	if draft.ResolvedSetCouldNotDerive != "" {
		return true
	}
	for _, entry := range draft.Resolved {
		if entry.Source == "" || entry.Version == "" || entry.Licence == "" {
			return true
		}
	}
	return false
}

const selectBuild = `select id, actor_kind, actor_key, actor_key_basis, at, item_id, service_id, commit_hash,
	run_state, run_reason, artifact_digest, resolved_set_could_not_derive, notice_file,
	design_system_constraint_id, shipped_bundle_identity, search_origin_build_id, search_build,
	schema_marks, declares_schema_change
	from ` + Table

// Get is one build by id. It takes the pool and not a [Writer], because
// reading a build is not a reason to be handed the thing that writes them.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Build, error) {
	b, err := scanBuild(ctx, pool, pool.QueryRow(ctx, selectBuild+` where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Build{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return Build{}, fmt.Errorf("build: reading %s: %w", id, err)
	}
	return b, nil
}

// ForCommit is the newest build of one item at one commit, and false where
// there is none. Multiple records may name that commit because each build is a
// separate attempt.
func ForCommit(ctx context.Context, pool *pgxpool.Pool, itemID, serviceID, commitHash string) (Build, bool, error) {
	b, err := scanBuild(ctx, pool, pool.QueryRow(ctx, selectBuild+` where item_id = $1 and service_id = $2 and commit_hash = $3 order by at desc, id desc limit 1`,
		itemID, serviceID, commitHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return Build{}, false, nil
	} else if err != nil {
		return Build{}, false, fmt.Errorf("build: reading the build of %s at %s: %w", itemID, commitHash, err)
	}
	return b, true, nil
}

// ForServiceCommit is every build naming one service and one commit, any item
// included and none required, oldest first. The merge queue's reading of
// master is compared against the builds the records hold and not against the
// build of one item it already knows, so this is the query that answers it —
// [ForCommit] answers for one item, and this is the same read widened to the
// service.
func ForServiceCommit(ctx context.Context, pool *pgxpool.Pool, serviceID, commitHash string) ([]Build, error) {
	rows, err := pool.Query(ctx, selectBuild+` where service_id = $1 and commit_hash = $2 order by at, id`,
		serviceID, commitHash)
	if err != nil {
		return nil, fmt.Errorf("build: reading the builds of %s at %s: %w", serviceID, commitHash, err)
	}
	defer rows.Close()

	var all []Build
	for rows.Next() {
		b, err := scanBuild(ctx, pool, rows)
		if err != nil {
			return nil, err
		}
		all = append(all, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("build: reading the builds of %s at %s: %w", serviceID, commitHash, err)
	}
	return all, nil
}

// scanner is what [pgx.Row] and [pgx.Rows] share, so one scan reads either.
type scanner interface {
	Scan(dest ...any) error
}

func scan(row scanner) (Build, error) {
	var b Build
	var kind, basis, marks string
	if err := row.Scan(&b.ID, &kind, &b.Actor.Key, &basis, &b.At, &b.ItemID, &b.ServiceID, &b.CommitHash,
		&b.RunState, &b.RunReason, &b.ArtifactDigest, &b.ResolvedSetCouldNotDerive, &b.NoticeFile,
		&b.DesignSystemConstraintID, &b.ShippedBundleIdentity, &b.SearchOriginBuildID,
		&b.SearchBuild, &marks, &b.DeclaresSchemaChange); err != nil {
		return Build{}, err
	}
	b.Actor.Kind = record.Kind(kind)
	b.Actor.Basis = record.Basis(basis)
	if marks != "" {
		if err := json.Unmarshal([]byte(marks), &b.SchemaMarks); err != nil {
			return Build{}, fmt.Errorf("build: decoding the schema marks of %s: %w", b.ID, err)
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
	b, err := scanBuild(ctx, pool, pool.QueryRow(ctx, selectBuild+` where item_id = $1 order by at desc, id desc limit 1`, itemID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Build{}, false, nil
	} else if err != nil {
		return Build{}, false, fmt.Errorf("build: reading the newest build of %s: %w", itemID, err)
	}
	return b, true, nil
}

// ForItem is every build of one item, oldest first, which is the order they
// were made in. A resumed pass reads it for the builds an item's own criteria
// have been decided against: a rebuild is a new build, and that history is not
// held anywhere else.
func ForItem(ctx context.Context, pool *pgxpool.Pool, itemID string) ([]Build, error) {
	if itemID == "" {
		return nil, nil
	}
	rows, err := pool.Query(ctx, selectBuild+` where item_id = $1 order by at, id`, itemID)
	if err != nil {
		return nil, fmt.Errorf("build: reading the builds of %s: %w", itemID, err)
	}
	defer rows.Close()

	var all []Build
	for rows.Next() {
		b, err := scanBuild(ctx, pool, rows)
		if err != nil {
			return nil, err
		}
		all = append(all, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("build: reading the builds of %s: %w", itemID, err)
	}
	return all, nil
}

// Resolved reads the entries a build resolved, in insertion order.
func Resolved(ctx context.Context, pool *pgxpool.Pool, buildID string) ([]ResolvedEntry, error) {
	rows, err := pool.Query(ctx, `select ecosystem, source, package, version, digest, licence, required_by, run_time, build_time
		from `+ResolvedTable+` where build_id = $1 order by at, id`, buildID)
	if err != nil {
		return nil, fmt.Errorf("build: reading what %s resolved: %w", buildID, err)
	}
	defer rows.Close()

	var read []ResolvedEntry
	for rows.Next() {
		var e ResolvedEntry
		if err := rows.Scan(&e.Ecosystem, &e.Source, &e.Package, &e.Version, &e.Digest,
			&e.Licence, &e.RequiredBy, &e.RunTime, &e.BuildTime); err != nil {
			return nil, fmt.Errorf("build: reading an entry of what %s resolved: %w", buildID, err)
		}
		read = append(read, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("build: reading what %s resolved: %w", buildID, err)
	}
	return read, nil
}
