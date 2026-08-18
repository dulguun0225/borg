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
)

// Criterion is one criterion of a service as it is stored.
type Criterion struct {
	ID             string
	Actor          record.Actor
	At             string
	ServiceID      string
	SpecArtifactID string
	Sentence       string
	Pattern        Pattern
	EscapeReason   string
}

const insertCriterion = `insert into ` + Table + `
	(id, actor_kind, actor_name, at, service_id, spec_artifact_id, sentence, pattern, escape_reason)
	values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

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
func Insert(ctx context.Context, tx pgx.Tx, actor record.Actor, serviceID, specArtifactID, sentence, escapeReason string) (Criterion, error) {
	if err := actor.Validate(); err != nil {
		return Criterion{}, err
	}
	if serviceID == "" {
		return Criterion{}, ErrServiceIDEmpty
	}
	if specArtifactID == "" {
		return Criterion{}, ErrSpecArtifactIDEmpty
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
		Sentence:       sentence,
		Pattern:        pattern,
		EscapeReason:   escapeReason,
	}
	if _, err := tx.Exec(ctx, insertCriterion,
		c.ID, string(c.Actor.Kind), c.Actor.Name, c.At,
		c.ServiceID, c.SpecArtifactID, c.Sentence, string(c.Pattern), c.EscapeReason,
	); err != nil {
		return Criterion{}, fmt.Errorf("criterion: writing %s: %w", c.ID, err)
	}
	return c, nil
}

const selectInForce = `select id, actor_kind, actor_name, at, service_id, spec_artifact_id, sentence, pattern, escape_reason
	from ` + Table + ` where service_id = $1 order by at`

// InForce is every criterion in force for the service, in the order they
// were written. The design's query is "in force unless a spec version
// withdrawing it belongs to an item in that build"; with no withdrawal
// written anywhere yet the query collapses to every criterion of the
// service, and the build parameter arrives with withdrawal.
//
// It takes the pool and no transaction, because reading the set is not a
// reason to be inside the write that changes it.
func InForce(ctx context.Context, pool *pgxpool.Pool, serviceID string) ([]Criterion, error) {
	rows, err := pool.Query(ctx, selectInForce, serviceID)
	if err != nil {
		return nil, fmt.Errorf("criterion: reading the set in force for %s: %w", serviceID, err)
	}
	defer rows.Close()

	var inForce []Criterion
	for rows.Next() {
		var c Criterion
		var kind, pattern string
		if err := rows.Scan(&c.ID, &kind, &c.Actor.Name, &c.At,
			&c.ServiceID, &c.SpecArtifactID, &c.Sentence, &pattern, &c.EscapeReason); err != nil {
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
