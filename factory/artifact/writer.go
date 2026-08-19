package artifact

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/record"
)

// Kind is what an artifact is a version of. M1 has the two the pipeline's
// path touches.
type Kind string

const (
	// KindSpec is a spec version; its content is the spec text.
	KindSpec Kind = "spec"
	// KindImplementation is an implementation version; its content is the
	// commit hash the stage produced.
	KindImplementation Kind = "implementation"
)

// Authorship is which of the store's three callers authored the version. It
// is an attribute of the version and not of the item, because authorship is
// per stage.
type Authorship string

const (
	// AuthorshipAgent is the agent in the stage's role.
	AuthorshipAgent Authorship = "agent"
	// AuthorshipHuman is a human backstopping the stage.
	AuthorshipHuman Authorship = "human"
	// AuthorshipGate is the gate component, where a human takes Edit in
	// place.
	AuthorshipGate Authorship = "gate"
)

// Authorships is every authorship a version may have. The CHECK constraint
// in [DDL] lists the same three, and TestDDLListsEveryAuthorship fails if
// the two lists stop agreeing.
var Authorships = []Authorship{AuthorshipAgent, AuthorshipHuman, AuthorshipGate}

var (
	// ErrAuthorshipUnknown is returned for an authorship that is none of
	// [Authorships]. The store refuses it again through the CHECK constraint,
	// so a row inserted around the methods is refused too.
	ErrAuthorshipUnknown = errors.New("artifact: authorship is neither agent, human, nor gate")
	// ErrItemIDEmpty is returned by both submissions for a version naming no
	// item. record's doc.go states what a link is checked for.
	ErrItemIDEmpty = errors.New("artifact: the item id is empty")
	// ErrAuthorEmpty is returned for a version naming no author. A version
	// whose author is not on the record is one no authorship prior can be
	// computed from, which is what the field is for.
	ErrAuthorEmpty = errors.New("artifact: the author is empty")
)

// Artifact is one version of an artifact as it is stored.
type Artifact struct {
	ID         string
	Actor      record.Actor
	At         string
	ItemID     string
	Kind       Kind
	Version    int
	Supersedes string
	Authorship Authorship
	// Author is the identity a prior is kept on: the model version for a
	// version an agent wrote, the person's name for one a human wrote.
	Author  string
	Content string
}

// Draft is one criterion as a caller of [Store.SubmitSpec] hands it in:
// the sentence, and a reason exactly when the sentence fits no pattern.
// Classification is [criterion.Insert]'s, not the caller's.
type Draft struct {
	Sentence     string
	EscapeReason string
}

// Store is the one writer of artifacts, and — through [criterion.Insert] —
// of criteria. Its three callers are the ones [Authorships] names; nothing
// else inserts into either table.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns the store over pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// SubmitSpec writes a spec version and every criterion it introduces, in one
// transaction: the artifact row first — version prior max + 1 for the item,
// supersedes naming the prior version's id — then each draft through
// [criterion.Insert] with the new version's id as the spec that introduced
// it. A draft the criterion package refuses rolls the whole call back, so no
// spec version exists whose criteria were not written.
//
// serviceID is the service the criteria belong to; the artifact row does not
// carry it, because an artifact is the item's and the item names its
// service.
func (s *Store) SubmitSpec(ctx context.Context, actor record.Actor, by By, itemID, serviceID, content string, criteria []Draft) (Artifact, []criterion.Criterion, error) {
	if err := refuse(actor, by, itemID); err != nil {
		return Artifact{}, nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Artifact{}, nil, fmt.Errorf("artifact: beginning the submission: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	submitted, err := insertVersion(ctx, tx, actor, by, itemID, KindSpec, content)
	if err != nil {
		return Artifact{}, nil, err
	}

	written := make([]criterion.Criterion, 0, len(criteria))
	for _, draft := range criteria {
		c, err := criterion.Insert(ctx, tx, actor, serviceID, submitted.ID, draft.Sentence, draft.EscapeReason)
		if err != nil {
			return Artifact{}, nil, err
		}
		written = append(written, c)
	}

	if err := tx.Commit(ctx); err != nil {
		return Artifact{}, nil, fmt.Errorf("artifact: committing %s: %w", submitted.ID, err)
	}
	return submitted, written, nil
}

// SubmitImplementation writes an implementation version — the same
// versioning as a spec, no criteria. The content is the commit hash the
// stage produced; the code lives in the repository, and the record names it.
func (s *Store) SubmitImplementation(ctx context.Context, actor record.Actor, by By, itemID, content string) (Artifact, error) {
	if err := refuse(actor, by, itemID); err != nil {
		return Artifact{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Artifact{}, fmt.Errorf("artifact: beginning the submission: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	submitted, err := insertVersion(ctx, tx, actor, by, itemID, KindImplementation, content)
	if err != nil {
		return Artifact{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Artifact{}, fmt.Errorf("artifact: committing %s: %w", submitted.ID, err)
	}
	return submitted, nil
}

func refuse(actor record.Actor, by By, itemID string) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if !slices.Contains(Authorships, by.Authorship) {
		return fmt.Errorf("%w: %q", ErrAuthorshipUnknown, by.Authorship)
	}
	if by.Author == "" {
		return fmt.Errorf("%w: authorship %q", ErrAuthorEmpty, by.Authorship)
	}
	if itemID == "" {
		return ErrItemIDEmpty
	}
	return nil
}

const insertArtifact = `insert into ` + Table + `
	(id, actor_kind, actor_name, at, item_id, kind, version, supersedes, authorship, author, content)
	values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`

// insertVersion is every submission: read the prior version for the item and
// kind, write the next one naming it. Two transactions reading the same
// prior write the same next version, and the unique constraint in [DDL]
// refuses the second — schema.go says why that needs no lock.
func insertVersion(ctx context.Context, tx pgx.Tx, actor record.Actor, by By, itemID string, kind Kind, content string) (Artifact, error) {
	priorID, priorVersion := "", 0
	err := tx.QueryRow(ctx,
		`select id, version from `+Table+` where item_id = $1 and kind = $2 order by version desc limit 1`,
		itemID, string(kind)).Scan(&priorID, &priorVersion)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Artifact{}, fmt.Errorf("artifact: reading the prior %s version of %s: %w", kind, itemID, err)
	}

	a := Artifact{
		ID:         record.NewID(IDPrefix),
		Actor:      actor,
		At:         record.Now(),
		ItemID:     itemID,
		Kind:       kind,
		Version:    priorVersion + 1,
		Supersedes: priorID,
		Authorship: by.Authorship,
		Author:     by.Author,
		Content:    content,
	}
	if _, err := tx.Exec(ctx, insertArtifact,
		a.ID, string(a.Actor.Kind), a.Actor.Name, a.At,
		a.ItemID, string(a.Kind), a.Version, a.Supersedes,
		string(a.Authorship), a.Author, a.Content,
	); err != nil {
		return Artifact{}, fmt.Errorf("artifact: writing %s: %w", a.ID, err)
	}
	return a, nil
}

const selectArtifact = `select id, actor_kind, actor_name, at, item_id, kind,
	version, supersedes, authorship, author, content
	from ` + Table

// Get is the artifact with the given id. It takes the pool and not a
// [Store], because reading a version is not a reason to be handed the thing
// that writes them.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Artifact, error) {
	var a Artifact
	var kind, authorship, actorKind string
	err := pool.QueryRow(ctx, selectArtifact+` where id = $1`, id).Scan(
		&a.ID, &actorKind, &a.Actor.Name, &a.At, &a.ItemID, &kind,
		&a.Version, &a.Supersedes, &authorship, &a.Author, &a.Content)
	if err != nil {
		return Artifact{}, fmt.Errorf("artifact: reading %s: %w", id, err)
	}
	a.Actor.Kind = record.Kind(actorKind)
	a.Kind = Kind(kind)
	a.Authorship = Authorship(authorship)
	return a, nil
}
