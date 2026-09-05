package consumercontract

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
)

// Draft is one predicate as a derivation produced it, before it is a record: what
// the consumer's build says, plus the producer's service resolved to an id by
// whoever submits it. The derivation reads a checkout and cannot resolve a name to
// a record, so resolution is the submitter's and the empty id is a real answer.
type Draft struct {
	ProducerService   string
	ProducerServiceID string
	Interface         string
	Element           string
	Kind              gatepolicy.PredicateKind
	Argument          string
}

// Of is the consumer this consumer contract version belongs to: the item, the
// service that item changes, and the consumer contract version the predicates
// are introduced by.
type Of struct {
	ItemID     string
	ServiceID  string
	ArtifactID string
}

// Insert writes every predicate one consumer contract version introduces, inside
// tx. Its one caller is the artifact store — the rows are written in the same
// call that submits the consumer contract version, so the version and the
// predicates it introduces cannot disagree about what was declared. That is why
// this takes a [pgx.Tx] rather than a pool: it runs inside the store's submitting
// transaction, and either both records commit or neither does.
//
// A predicate is written exactly once and never updated. There is no withdrawal:
// a consumer that stops reading an element derives nothing at its next release,
// and the query that reads consumer contracts in force is what stops seeing it.
func Insert(ctx context.Context, tx pgx.Tx, actor record.Actor, of Of, drafts []Draft) ([]Predicate, error) {
	if err := actor.Validate(); err != nil {
		return nil, err
	}
	written := make([]Predicate, 0, len(drafts))
	for _, d := range drafts {
		p := Predicate{
			ID:                record.NewID(IDPrefix),
			Actor:             actor,
			At:                record.Now(),
			ItemID:            of.ItemID,
			ServiceID:         of.ServiceID,
			ArtifactID:        of.ArtifactID,
			ProducerService:   d.ProducerService,
			ProducerServiceID: d.ProducerServiceID,
			Interface:         d.Interface,
			Element:           d.Element,
			Kind:              d.Kind,
			Argument:          d.Argument,
		}
		if err := p.Validate(); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `insert into `+Table+`
			(id, format_version, actor_kind, actor_key, actor_key_basis, at, item_id, service_id, artifact_id,
			producer_service, producer_service_id, interface_name, element, kind, argument)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
			p.ID, FormatVersion, string(p.Actor.Kind), p.Actor.Key, string(p.Actor.Basis), p.At, p.ItemID, p.ServiceID, p.ArtifactID,
			p.ProducerService, p.ProducerServiceID, p.Interface, p.Element, string(p.Kind), p.Argument,
		); err != nil {
			return nil, fmt.Errorf("consumercontract: writing %s of item %s: %w", p.Describe(), of.ItemID, err)
		}
		written = append(written, p)
	}
	return written, nil
}
