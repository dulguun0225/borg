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
	// admitted only as an escape, and an escape is tagged with why.
	ErrReasonMissing = errors.New("criterion: a sentence fitting no pattern is admitted only with a tagged reason")
	// ErrReasonRefused is returned by [Insert] for a sentence that fits a
	// pattern and carries a reason anyway. Only an escape carries one, so
	// the tag keeps meaning that the form was escaped.
	ErrReasonRefused = errors.New("criterion: only an escape carries a reason")
	// ErrServiceIDEmpty is returned by [Insert] for a criterion naming no
	// service. record's doc.go states what a link is checked for.
	ErrServiceIDEmpty = errors.New("criterion: the service id is empty")
	// ErrSpecArtifactIDEmpty is returned by [Insert] for a criterion naming no
	// spec version as what introduced it.
	ErrSpecArtifactIDEmpty = errors.New("criterion: the spec artifact id is empty")
	// ErrItemIDEmpty is returned by [Insert] for a criterion naming no item as
	// what introduced it.
	ErrItemIDEmpty = errors.New("criterion: the item id is empty")
)

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
	ItemID       string
	Sentence     string
	Pattern      Pattern
	EscapeReason string
}

const insertCriterion = `insert into ` + Table + `
	(id, actor_kind, actor_name, at, service_id, spec_artifact_id, item_id, sentence, pattern, escape_reason)
	values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

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
// sentence with a reason is written as [PatternEscape].
func Insert(ctx context.Context, tx pgx.Tx, actor record.Actor, serviceID, specArtifactID, itemID, sentence, escapeReason string) (Criterion, error) {
	if err := actor.Validate(); err != nil {
		return Criterion{}, err
	}
	if serviceID == "" {
		return Criterion{}, ErrServiceIDEmpty
	}
	if specArtifactID == "" {
		return Criterion{}, ErrSpecArtifactIDEmpty
	}
	if itemID == "" {
		return Criterion{}, ErrItemIDEmpty
	}

	pattern, matched := Classify(sentence)
	switch {
	case !matched && escapeReason == "":
		return Criterion{}, fmt.Errorf("%w: %q", ErrReasonMissing, sentence)
	case matched && escapeReason != "":
		return Criterion{}, fmt.Errorf("%w: %q fits %s and carries %q", ErrReasonRefused, sentence, pattern, escapeReason)
	case !matched:
		pattern = PatternEscape
	}

	c := Criterion{
		ID:             record.NewID(IDPrefix),
		Actor:          actor,
		At:             record.Now(),
		ServiceID:      serviceID,
		SpecArtifactID: specArtifactID,
		ItemID:         itemID,
		Sentence:       sentence,
		Pattern:        pattern,
		EscapeReason:   escapeReason,
	}
	if _, err := tx.Exec(ctx, insertCriterion,
		c.ID, string(c.Actor.Kind), c.Actor.Name, c.At,
		c.ServiceID, c.SpecArtifactID, c.ItemID, c.Sentence, string(c.Pattern), c.EscapeReason,
	); err != nil {
		return Criterion{}, fmt.Errorf("criterion: writing %s: %w", c.ID, err)
	}
	return c, nil
}

const selectInForce = `select id, actor_kind, actor_name, at, service_id, spec_artifact_id, item_id,
	sentence, pattern, escape_reason
	from ` + Table + ` where service_id = $1 and item_id = any($2) order by at`

// InForce is every criterion in force for one build of the service, in the order
// they were written. A build is a set of items — the ones merged into the tree it
// was made from, plus the item whose branch it is — and itemIDs is that set,
// assembled by the caller.
//
// In force is per build and not per service, which is what the design's query
// already says: a criterion introduced by a sibling item that has not merged is a
// promise this build's tree could not keep, and holding it in force here would
// reject every candidate cut in parallel with the one that introduced it. The
// other half of that query, withdrawal, is not written anywhere yet — a spec
// version withdrawing a criterion arrives with the milestone that authors one — so
// what is filtered so far is introduction alone.
//
// No items is no criteria and no error: a build with no items is not something the
// cut produces, and an empty set would otherwise match nothing rather than
// everything, which is the safe direction of the two.
//
// It takes the pool and no transaction, because reading the set is not a
// reason to be inside the write that changes it.
func InForce(ctx context.Context, pool *pgxpool.Pool, serviceID string, itemIDs []string) ([]Criterion, error) {
	if len(itemIDs) == 0 {
		return nil, nil
	}
	rows, err := pool.Query(ctx, selectInForce, serviceID, itemIDs)
	if err != nil {
		return nil, fmt.Errorf("criterion: reading the set in force for %s: %w", serviceID, err)
	}
	defer rows.Close()

	var inForce []Criterion
	for rows.Next() {
		var c Criterion
		var kind, pattern string
		if err := rows.Scan(&c.ID, &kind, &c.Actor.Name, &c.At,
			&c.ServiceID, &c.SpecArtifactID, &c.ItemID, &c.Sentence, &pattern, &c.EscapeReason); err != nil {
			return nil, fmt.Errorf("criterion: reading a row: %w", err)
		}
		c.Actor.Kind = record.Kind(kind)
		c.Pattern = Pattern(pattern)
		inForce = append(inForce, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("criterion: reading the set in force for %s: %w", serviceID, err)
	}
	return inForce, nil
}
