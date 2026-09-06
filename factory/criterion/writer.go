package criterion

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrReasonMissing is returned by [Insert] for a sentence fitting no
	// pattern and carrying no reason. A sentence fitting no pattern is
	// admitted only with a tagged reason.
	ErrReasonMissing = errors.New("criterion: a sentence fitting no pattern is admitted only with a tagged reason")
	// ErrReasonRefused is returned by [Insert] for a sentence that fits a
	// pattern and carries a reason anyway. Only a sentence fitting no pattern
	// carries one, so the tag keeps meaning that the form was not fitted.
	ErrReasonRefused = errors.New("criterion: only a sentence fitting no pattern carries a reason")
	// ErrServiceIDEmpty is returned by [Insert] for a criterion naming no
	// service. record's doc.go states what a link is checked for.
	ErrServiceIDEmpty = errors.New("criterion: the service id is empty")
	// ErrSpecArtifactIDEmpty is returned by [Insert] and [Withdraw] for a row
	// naming no spec version.
	ErrSpecArtifactIDEmpty = errors.New("criterion: the spec artifact id is empty")
	// ErrItemIDEmpty is returned by [Insert] and [Withdraw] for a row naming
	// no item.
	ErrItemIDEmpty = errors.New("criterion: the item id is empty")
	// ErrRequirementIDEmpty is returned by [Insert] for a criterion that fits
	// a pattern and names no requirement. The gate rejects in both directions
	// over that field, so a criterion that answers nothing nameable is not a
	// criterion.
	ErrRequirementIDEmpty = errors.New("criterion: the requirement id is empty")
	// ErrCriterionIDEmpty is returned by [Withdraw] and by the result writers
	// for a row naming no criterion.
	ErrCriterionIDEmpty = errors.New("criterion: the criterion id is empty")
	// ErrAreaIDEmpty is returned by [CheckHazardControlled] for a build read
	// as irreversible and naming no area. A grade is declared on an area, so a
	// grade with no area is the caller's defect and not a rejection.
	ErrAreaIDEmpty = errors.New("criterion: the area id is empty")
)

// Of is what a criterion belongs to: the service it is a promise of, the spec
// version that introduced it, and the item that spec version belongs to.
type Of struct {
	ServiceID      string
	SpecArtifactID string
	ItemID         string
}

// Draft is one criterion as the artifact store's caller hands it in. The
// sentence is classified by [Insert] and never by the caller; the three
// provenance fields are read at introduction and written once.
type Draft struct {
	Sentence string
	// NoPatternReason is set exactly when the sentence fits no pattern.
	NoPatternReason string
	// RequirementID is the requirement the criterion answers, required of
	// every criterion that fits a pattern. Where several items answer one
	// requirement, it is the requirement decomposition derived for this
	// item's share.
	RequirementID string
	// ConstraintDerived names each constraint record the drafting stage held
	// as its evidence for the criterion. It is a link and not a mark, which is
	// what makes [ForConstraint] and [UnderWithdrawnConstraints] queries.
	ConstraintDerived []string
	// HazardDerived is the area whose hazardous operation the criterion
	// bounds, and is empty on a criterion that bounds none.
	HazardDerived string
}

// Criterion is one criterion of a service as it is stored.
type Criterion struct {
	ID    string
	Actor record.Actor
	At    string
	// ServiceID is the service the criterion is a promise of.
	ServiceID string
	// SpecArtifactID is the spec version that introduced it.
	SpecArtifactID string
	// ItemID is the item that spec version belongs to, which is what [InForce]
	// filters on. doc.go says why it is a field rather than a hop through the
	// artifact.
	ItemID            string
	Sentence          string
	Pattern           Pattern
	NoPatternReason   string
	RequirementID     string
	ConstraintDerived []string
	HazardDerived     string
}

const insertCriterion = `insert into ` + Table + `
	(id, format_version, actor_kind, actor_key, actor_key_basis, at, service_id, spec_artifact_id, item_id,
	sentence, pattern, no_pattern_reason, requirement_id, constraint_derived, hazard_derived)
	values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`

// Insert writes one criterion inside tx. Its one caller is the artifact
// store, which calls it inside the transaction that submits the spec version
// introducing the criterion — that is why it takes a transaction rather than
// a pool, so the artifact row and the criterion rows commit together or not
// at all. Nothing else may call it; doc.go says why the writer is another
// package.
//
// It classifies the sentence itself with [Classify]. An unmatched sentence
// with no reason is refused with [ErrReasonMissing]; a matched sentence
// carrying a reason is refused with [ErrReasonRefused]; an unmatched
// sentence with a reason is written as [PatternNoPattern]. A matched sentence
// naming no requirement is refused with [ErrRequirementIDEmpty].
func Insert(ctx context.Context, tx pgx.Tx, actor record.Actor, of Of, draft Draft) (Criterion, error) {
	if err := actor.Validate(); err != nil {
		return Criterion{}, err
	}
	if of.ServiceID == "" {
		return Criterion{}, ErrServiceIDEmpty
	}
	if of.SpecArtifactID == "" {
		return Criterion{}, ErrSpecArtifactIDEmpty
	}
	if of.ItemID == "" {
		return Criterion{}, ErrItemIDEmpty
	}

	pattern, matched := Classify(draft.Sentence)
	switch {
	case !matched && draft.NoPatternReason == "":
		return Criterion{}, fmt.Errorf("%w: %q", ErrReasonMissing, draft.Sentence)
	case matched && draft.NoPatternReason != "":
		return Criterion{}, fmt.Errorf("%w: %q fits %s and carries %q",
			ErrReasonRefused, draft.Sentence, pattern, draft.NoPatternReason)
	case !matched:
		pattern = PatternNoPattern
	}
	if matched && draft.RequirementID == "" {
		return Criterion{}, fmt.Errorf("%w: %q", ErrRequirementIDEmpty, draft.Sentence)
	}

	c := Criterion{
		ID:                record.NewID(IDPrefix),
		Actor:             actor,
		At:                record.Now(),
		ServiceID:         of.ServiceID,
		SpecArtifactID:    of.SpecArtifactID,
		ItemID:            of.ItemID,
		Sentence:          draft.Sentence,
		Pattern:           pattern,
		NoPatternReason:   draft.NoPatternReason,
		RequirementID:     draft.RequirementID,
		ConstraintDerived: draft.ConstraintDerived,
		HazardDerived:     draft.HazardDerived,
	}
	if c.ConstraintDerived == nil {
		c.ConstraintDerived = []string{}
	}
	if _, err := tx.Exec(ctx, insertCriterion,
		c.ID, FormatVersion, string(c.Actor.Kind), c.Actor.Key, string(c.Actor.Basis), c.At,
		c.ServiceID, c.SpecArtifactID, c.ItemID, c.Sentence, string(c.Pattern), c.NoPatternReason,
		c.RequirementID, c.ConstraintDerived, c.HazardDerived,
	); err != nil {
		return Criterion{}, fmt.Errorf("criterion: writing %s: %w", c.ID, err)
	}
	return c, nil
}

// Withdraw records that a spec version withdraws a criterion, inside tx. Its
// one caller is the artifact store, in the same transaction that submits the
// withdrawing spec version — so a version the gate rejects takes its
// withdrawal down with it, the withdrawal being recorded on the version and
// never on the criterion.
//
// The row names the item as well as the version, because in force is per
// build and a build is a set of items: [InForce] reads a withdrawal only where
// the withdrawing item is in the build.
func Withdraw(ctx context.Context, tx pgx.Tx, actor record.Actor, of Of, criterionID string) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if of.SpecArtifactID == "" {
		return ErrSpecArtifactIDEmpty
	}
	if of.ItemID == "" {
		return ErrItemIDEmpty
	}
	if criterionID == "" {
		return ErrCriterionIDEmpty
	}
	_, err := tx.Exec(ctx, `insert into `+WithdrawalTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, spec_artifact_id, item_id, criterion_id)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		record.NewID(WithdrawalIDPrefix), FormatVersionWithdrawal,
		string(actor.Kind), actor.Key, string(actor.Basis), record.Now(),
		of.SpecArtifactID, of.ItemID, criterionID)
	if err != nil {
		return fmt.Errorf("criterion: withdrawing %s on %s: %w", criterionID, of.SpecArtifactID, err)
	}
	return nil
}

const selectCriterion = `select id, actor_kind, actor_key, actor_key_basis, at, service_id, spec_artifact_id, item_id,
	sentence, pattern, no_pattern_reason, requirement_id, constraint_derived, hazard_derived
	from ` + Table

// notWithdrawn is the second half of the in-force query: no spec version in
// the build withdraws the criterion. It reads the withdrawal rows of the same
// item set, which is what makes the query right for master and for the
// withdrawing candidate at once.
const notWithdrawn = ` and not exists (select 1 from ` + WithdrawalTable + ` w
	where w.criterion_id = ` + Table + `.id and w.item_id = any($2))`

// InForce is every criterion in force for one build of the service, in the order
// they were written. A build is a set of items — the ones merged into the
// repository it was made from, plus the item whose branch it is — and itemIDs
// is that set, assembled by the caller.
//
// Both halves of the design's query are read here: the item that introduced the
// criterion is in the build, and no spec version in the build withdraws it. In
// force is per build and not per service, because a criterion introduced by a
// sibling item that has not merged is a promise this build's tree could not
// keep, and holding it in force here would reject every candidate decomposed in
// parallel with the one that introduced it.
//
// No items is no criteria and no error: a build with no items is not something the
// decomposition produces, and an empty set would otherwise match nothing rather than
// everything, which is the safe direction of the two.
//
// It takes the pool and no transaction, because reading the set is not a
// reason to be inside the write that changes it.
func InForce(ctx context.Context, pool *pgxpool.Pool, serviceID string, itemIDs []string) ([]Criterion, error) {
	if len(itemIDs) == 0 {
		return nil, nil
	}
	return query(ctx, pool, serviceID,
		selectCriterion+` where service_id = $1 and item_id = any($2)`+notWithdrawn+` order by at`,
		serviceID, itemIDs)
}

// Withdrawn is every criterion the build's own items withdraw, whether or not
// the criterion was introduced in this build. It is what the encoding check
// reads for its third direction: an encoding naming a criterion that same
// build withdraws decides a promise the service no longer makes.
func Withdrawn(ctx context.Context, pool *pgxpool.Pool, itemIDs []string) ([]string, error) {
	if len(itemIDs) == 0 {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `select criterion_id from `+WithdrawalTable+`
		where item_id = any($1) order by at, criterion_id`, itemIDs)
	if err != nil {
		return nil, fmt.Errorf("criterion: reading what the build withdraws: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("criterion: reading a withdrawal: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("criterion: reading what the build withdraws: %w", err)
	}
	return ids, nil
}

// query runs one of this package's selects and scans the rows into criteria.
// Every read of [Table] goes through it, so a column added to the select list
// is added in one place.
func query(ctx context.Context, pool *pgxpool.Pool, serviceID, sql string, args ...any) ([]Criterion, error) {
	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("criterion: reading the criteria of %s: %w", serviceID, err)
	}
	defer rows.Close()

	var read []Criterion
	for rows.Next() {
		var c Criterion
		var kind, basis, pattern string
		if err := rows.Scan(&c.ID, &kind, &c.Actor.Key, &basis, &c.At,
			&c.ServiceID, &c.SpecArtifactID, &c.ItemID, &c.Sentence, &pattern, &c.NoPatternReason,
			&c.RequirementID, &c.ConstraintDerived, &c.HazardDerived); err != nil {
			return nil, fmt.Errorf("criterion: reading a row: %w", err)
		}
		c.Actor.Kind = record.Kind(kind)
		c.Actor.Basis = record.Basis(basis)
		c.Pattern = Pattern(pattern)
		read = append(read, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("criterion: reading the criteria of %s: %w", serviceID, err)
	}
	return read, nil
}
