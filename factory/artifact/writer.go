package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/screenstatemachine"
)

// Kind is what an artifact is a version of. The first three belong to an
// item, one per stage; the other three are the fleet's, and belong to a role,
// a subject, or the factory as a whole rather than to an item — [ItemKinds]
// and [FleetKinds] split them, and the DDL's chain_key_matches_kind CHECK is
// the same split enforced on every write.
type Kind string

const (
	// KindSpec is a spec version; its content is the spec text.
	KindSpec Kind = "spec"
	// KindImplementation is an implementation version; its content is the
	// commit hash the stage produced.
	KindImplementation Kind = "implementation"
	// KindConsumerContract is a consumer contract version, authored at the
	// implementation stage from the same build: its content is the words a human
	// reads the consumer contract by, and what it introduces is the predicates. A
	// kind of its own rather than a field of the implementation version, because
	// the two are derived from that build separately and either can be authored
	// again while the other stands.
	KindConsumerContract Kind = "consumer_contract"
	// KindRolePrompt is what an agent in a role works from. It belongs to the
	// role and not to an item, and its chain is named by role rather than by
	// item_id.
	KindRolePrompt Kind = "role_prompt"
	// KindSkill is a procedure an agent is given where the item it was
	// dispatched onto matches the skill's subject. It belongs to the subject
	// — an area, a service, or a project — and not to an item.
	KindSkill Kind = "skill"
	// KindSelectionRule is what context assembly selects by. There is one,
	// for the whole factory, and it belongs to none of item, role or
	// subject.
	KindSelectionRule Kind = "selection_rule"
)

// Kinds is every kind an artifact may be a version of. The CHECK constraint in
// [DDL] lists the same six, and TestDDLListsEveryKind fails if the two lists
// stop agreeing.
var Kinds = []Kind{KindSpec, KindImplementation, KindConsumerContract, KindRolePrompt, KindSkill, KindSelectionRule}

// ItemKinds is every kind that belongs to an item: the three authored at a
// stage of the path.
var ItemKinds = []Kind{KindSpec, KindImplementation, KindConsumerContract}

// FleetKinds is every kind that belongs to the fleet rather than to an item:
// a role prompt, a skill, or the one selection rule.
var FleetKinds = []Kind{KindRolePrompt, KindSkill, KindSelectionRule}

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

// Authorships is every authorship a version an agent, a human, or the gate
// wrote may have. The CHECK constraint in [DDL] admits these three plus the
// empty string, which [By.Empty] names and [Authorships] deliberately
// excludes: it is not a fourth authorship, it is the absence of one.
var Authorships = []Authorship{AuthorshipAgent, AuthorshipHuman, AuthorshipGate}

var (
	// ErrAuthorshipUnknown is returned for an authorship that is none of
	// [Authorships]. The store refuses it again through the CHECK constraint,
	// so a row inserted around the methods is refused too.
	ErrAuthorshipUnknown = errors.New("artifact: authorship is neither agent, human, nor gate")
	// ErrItemIDEmpty is returned by the item-kind submissions for a version
	// naming no item. record's doc.go states what a link is checked for.
	ErrItemIDEmpty = errors.New("artifact: the item id is empty")
	// ErrAuthorEmpty is returned for an authored version naming no author. A
	// version whose author is not on the record is one no per-author prior
	// can be computed from, which is what the field is for.
	ErrAuthorEmpty = errors.New("artifact: the author is empty")
	// ErrRoleEmpty is returned by [Store.SubmitFleet] and [Store.EnterShipped]
	// for a role prompt naming no role.
	ErrRoleEmpty = errors.New("artifact: the role is empty")
	// ErrSubjectEmpty is returned by [Store.SubmitFleet] and
	// [Store.EnterShipped] for a skill naming no subject.
	ErrSubjectEmpty = errors.New("artifact: the subject is empty")
	// ErrFleetKindUnknown is returned by [Store.SubmitFleet] and
	// [Store.EnterShipped] for a kind outside [FleetKinds].
	ErrFleetKindUnknown = errors.New("artifact: the kind is none of role_prompt, skill, or selection_rule")
	// ErrShippedBundleIdentityEmpty is returned by [Store.EnterShipped] for a
	// call naming no release of the product.
	ErrShippedBundleIdentityEmpty = errors.New("artifact: the shipped bundle identity is empty")
)

// Artifact is one version of an artifact as it is stored.
type Artifact struct {
	ID         string
	Actor      record.Actor
	At         string
	ItemID     string
	Role       string
	Subject    string
	Kind       Kind
	Version    int
	Supersedes string
	Authorship Authorship
	// Author is the identity a prior is kept on: the model version for a
	// version an agent wrote, the person's name for one a human wrote, and
	// empty on the one entry nobody wrote.
	Author  string
	Content string
	// ContentDigest is the sha256 of Content in hexadecimal, computed at the
	// write.
	ContentDigest string
	// ShippedBundleIdentity is the release of the product that entered this
	// version, present exactly on the entry the factory wrote itself and
	// empty on every authored one.
	ShippedBundleIdentity string
	// InputManifestID names the input manifest the version was authored
	// from. It is empty until the dispatcher that writes one exists, and on
	// every shipped version, an entry authoring nothing having read no
	// manifest.
	InputManifestID string
}

// By is who authored a version: which of the store's three callers it came
// through, and the identity a per-author prior is kept on. The two are
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

// Empty reports whether by names neither an authorship nor an author — the
// one pair [Store.EnterShipped] writes and every other submission refuses as
// a partial one instead.
func (b By) Empty() bool { return b.Authorship == "" && b.Author == "" }

// Store is the one writer of artifacts, and — through [criterion.Insert],
// [criterion.Withdraw], [screenstatemachine.Insert], and
// [consumercontract.Insert] — of criteria, criterion withdrawals, screen state
// machines, and the predicates a consumer contract version introduces. Its
// callers are the ones [Authorships] names, plus the factory's own start for
// the one entry [Store.EnterShipped] writes; nothing else inserts into any of
// these tables.
type Store struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewStore returns the store over pool, fencing every write with token.
func NewStore(pool *pgxpool.Pool, token lease.Token) *Store { return &Store{pool: pool, token: token} }

func contentDigest(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// chainKey names one version chain: exactly one of ItemID, Role and Subject is
// set, depending on the kind, and [insertVersion] reads and writes the chain
// on that column alone.
type chainKey struct {
	ItemID, Role, Subject string
}

// SubmitSpec writes a spec version, every criterion it introduces, the
// withdrawal of every criterion id in withdrawnCriterionIDs, and every screen
// state machine it introduces or revises, all in one transaction: the
// artifact row first — version prior max + 1 for the item, supersedes naming
// the prior version's id — then each criterion draft through [criterion.Insert],
// then each withdrawal through [criterion.Withdraw], then each machine draft
// through [screenstatemachine.Insert], all naming the new version as the spec
// that introduced or withdrew them. Anything any of those four refuses rolls
// the whole call back, so no spec version exists whose criteria, withdrawals
// or machines were not written.
//
// serviceID is the service the criteria and machines belong to; the artifact
// row does not carry it, because an artifact is the item's and the item names
// its service.
func (s *Store) SubmitSpec(ctx context.Context, actor record.Actor, by By, itemID, serviceID, content string,
	criteria []criterion.Draft, withdrawnCriterionIDs []string, machines []screenstatemachine.Draft,
) (Artifact, []criterion.Criterion, []screenstatemachine.Machine, error) {
	if err := refuse(actor, by, itemID); err != nil {
		return Artifact{}, nil, nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Artifact{}, nil, nil, fmt.Errorf("artifact: beginning the submission: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, s.token); err != nil {
		return Artifact{}, nil, nil, err
	}

	submitted, err := insertVersion(ctx, tx, actor, by, chainKey{ItemID: itemID}, KindSpec, content, "")
	if err != nil {
		return Artifact{}, nil, nil, err
	}

	of := criterion.Of{ServiceID: serviceID, SpecArtifactID: submitted.ID, ItemID: itemID}
	written := make([]criterion.Criterion, 0, len(criteria))
	for _, draft := range criteria {
		c, err := criterion.Insert(ctx, tx, actor, of, draft)
		if err != nil {
			return Artifact{}, nil, nil, err
		}
		written = append(written, c)
	}
	for _, criterionID := range withdrawnCriterionIDs {
		if err := criterion.Withdraw(ctx, tx, actor, of, criterionID); err != nil {
			return Artifact{}, nil, nil, err
		}
	}

	machineOf := screenstatemachine.Of{ServiceID: serviceID, SpecArtifactID: submitted.ID, ItemID: itemID}
	writtenMachines := make([]screenstatemachine.Machine, 0, len(machines))
	for _, draft := range machines {
		m, err := screenstatemachine.Insert(ctx, tx, actor, machineOf, draft)
		if err != nil {
			return Artifact{}, nil, nil, err
		}
		writtenMachines = append(writtenMachines, m)
	}

	if err := tx.Commit(ctx); err != nil {
		return Artifact{}, nil, nil, fmt.Errorf("artifact: committing %s: %w", submitted.ID, err)
	}
	return submitted, written, writtenMachines, nil
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
	if err := lease.Fence(ctx, tx, s.token); err != nil {
		return Artifact{}, err
	}

	submitted, err := insertVersion(ctx, tx, actor, by, chainKey{ItemID: itemID}, KindImplementation, content, "")
	if err != nil {
		return Artifact{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Artifact{}, fmt.Errorf("artifact: committing %s: %w", submitted.ID, err)
	}
	return submitted, nil
}

// SubmitConsumerContract writes a consumer contract version, the derivation that
// produced it, and every predicate it introduces, in one transaction — the same
// arrangement [Store.SubmitSpec] has with the criteria, and taken for the same
// reason: a version whose predicates were not written would be a consumer contract
// nobody can decide against, and one [consumercontract.Insert] refuses rolls the
// version back with it.
//
// serviceID is the consumer's, which the predicates carry so that a reader of one
// knows whose assumption it is without walking to the item. The content is what a
// human reads the version by; what the extractor produced is what the factory
// decides, and a derivation that could not run at all is written as the record it
// is rather than as an empty list.
func (s *Store) SubmitConsumerContract(ctx context.Context, actor record.Actor, by By,
	itemID, serviceID, content string, derived consumercontract.Derived) (
	Artifact, consumercontract.Derivation, []consumercontract.Predicate, error) {
	if err := refuse(actor, by, itemID); err != nil {
		return Artifact{}, consumercontract.Derivation{}, nil, err
	}
	if serviceID == "" {
		return Artifact{}, consumercontract.Derivation{}, nil,
			fmt.Errorf("artifact: the consumer contract version of %s names no service", itemID)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Artifact{}, consumercontract.Derivation{}, nil,
			fmt.Errorf("artifact: beginning the submission: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, s.token); err != nil {
		return Artifact{}, consumercontract.Derivation{}, nil, err
	}

	submitted, err := insertVersion(ctx, tx, actor, by, chainKey{ItemID: itemID}, KindConsumerContract, content, "")
	if err != nil {
		return Artifact{}, consumercontract.Derivation{}, nil, err
	}
	derivation, written, err := consumercontract.Insert(ctx, tx, actor, consumercontract.Of{
		ItemID: itemID, ServiceID: serviceID, ArtifactID: submitted.ID,
	}, derived)
	if err != nil {
		return Artifact{}, consumercontract.Derivation{}, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Artifact{}, consumercontract.Derivation{}, nil,
			fmt.Errorf("artifact: committing %s: %w", submitted.ID, err)
	}
	return submitted, derivation, written, nil
}

// refuse is the item-kind submissions' validation: the actor, the authorship
// and author pair, and the item id.
func refuse(actor record.Actor, by By, itemID string) error {
	if err := refuseAuthored(actor, by); err != nil {
		return err
	}
	if itemID == "" {
		return ErrItemIDEmpty
	}
	return nil
}

// refuseAuthored is the actor and the authorship-and-author pair every
// authored submission requires, item-kind or fleet. [Store.EnterShipped]
// validates the actor alone: its pair is the empty one every other submission
// refuses here.
func refuseAuthored(actor record.Actor, by By) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if !slices.Contains(Authorships, by.Authorship) {
		return fmt.Errorf("%w: %q", ErrAuthorshipUnknown, by.Authorship)
	}
	if by.Author == "" {
		return fmt.Errorf("%w: authorship %q", ErrAuthorEmpty, by.Authorship)
	}
	return nil
}

const insertArtifact = `insert into ` + Table + `
	(id, format_version, actor_kind, actor_key, actor_key_basis, at, item_id, role, subject, kind, version,
	supersedes, authorship, author, content, content_digest, shipped_bundle_identity, input_manifest_id)
	values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`

// insertVersion is every submission: read the prior version for the chain and
// kind, write the next one naming it. Two transactions reading the same
// prior write the same next version, and the unique constraint in [DDL]
// refuses the second — schema.go says why that needs no lock.
func insertVersion(ctx context.Context, tx pgx.Tx, actor record.Actor, by By, key chainKey, kind Kind, content, shippedBundleIdentity string) (Artifact, error) {
	priorID, priorVersion := "", 0
	err := tx.QueryRow(ctx,
		`select id, version from `+Table+` where item_id = $1 and role = $2 and subject = $3 and kind = $4
			order by version desc limit 1`,
		key.ItemID, key.Role, key.Subject, string(kind)).Scan(&priorID, &priorVersion)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Artifact{}, fmt.Errorf("artifact: reading the prior %s version of the chain: %w", kind, err)
	}

	a := Artifact{
		ID:                    record.NewID(IDPrefix),
		Actor:                 actor,
		At:                    record.Now(),
		ItemID:                key.ItemID,
		Role:                  key.Role,
		Subject:               key.Subject,
		Kind:                  kind,
		Version:               priorVersion + 1,
		Supersedes:            priorID,
		Authorship:            by.Authorship,
		Author:                by.Author,
		Content:               content,
		ContentDigest:         contentDigest(content),
		ShippedBundleIdentity: shippedBundleIdentity,
	}
	if _, err := tx.Exec(ctx, insertArtifact,
		a.ID, FormatVersion, string(a.Actor.Kind), a.Actor.Key, string(a.Actor.Basis), a.At,
		a.ItemID, a.Role, a.Subject, string(a.Kind), a.Version, a.Supersedes,
		string(a.Authorship), a.Author, a.Content, a.ContentDigest, a.ShippedBundleIdentity, a.InputManifestID,
	); err != nil {
		return Artifact{}, fmt.Errorf("artifact: writing %s: %w", a.ID, err)
	}
	return a, nil
}
