package consumercontract

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
)

// Draft is one predicate as a derivation produced it, before it is a record: what
// the consumer's build says, plus the producer's service resolved to an id by
// whoever submits it. The derivation reads a checkout and cannot resolve a name to
// a record, so resolution is the submitter's and the empty id is a real answer.
type Draft struct {
	// Address is the entry of the consumer's configuration file the call site
	// reads its address from, and ProducerService and Interface are what that
	// entry names.
	Address           string
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

// Insert writes one consumer contract version's derivation and every predicate it
// introduces, inside tx. Its one caller is the artifact store — the rows are
// written in the same call that submits the consumer contract version, so the
// version, what derived it, and what was declared cannot disagree. That is why
// this takes a [pgx.Tx] rather than a pool: it runs inside the store's submitting
// transaction, and either every record commits or none does.
//
// A run that could not derive writes its derivation row and no predicates, which
// is the record the design asks for rather than an empty list: "no consumer reads
// this" and "no consumer's read was visible" call for opposite responses.
//
// A predicate is written exactly once and never updated. There is no withdrawal:
// a consumer that stops reading an element derives nothing at its next release,
// and the query that reads consumer contracts in force is what stops seeing it.
func Insert(ctx context.Context, tx pgx.Tx, actor record.Actor, of Of, derived Derived) (Derivation, []Predicate, error) {
	if err := actor.Validate(); err != nil {
		return Derivation{}, nil, err
	}
	if err := derived.validate(of); err != nil {
		return Derivation{}, nil, err
	}

	d := Derivation{
		ID:         record.NewID(DerivationIDPrefix),
		Actor:      actor,
		At:         record.Now(),
		ItemID:     of.ItemID,
		ServiceID:  of.ServiceID,
		ArtifactID: of.ArtifactID,
		Extractor:  derived.Extractor,
		Unfollowed: derived.Unfollowed,
		Cause:      derived.Cause,
		Reported:   derived.Reported,
	}
	if _, err := tx.Exec(ctx, `insert into `+DerivationTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, item_id, service_id, artifact_id,
		extractor, extractor_version, toolchain, factory_version, unfollowed, cause, reported)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
		d.ID, FormatVersionDerivation, string(d.Actor.Kind), d.Actor.Key, string(d.Actor.Basis), d.At,
		d.ItemID, d.ServiceID, d.ArtifactID, d.Extractor.Name, d.Extractor.Version, d.Extractor.Toolchain,
		d.Extractor.FactoryVersion, joinLines(d.Unfollowed), string(d.Cause), d.Reported,
	); err != nil {
		return Derivation{}, nil, fmt.Errorf("consumercontract: writing the derivation of %s: %w", of.ArtifactID, err)
	}

	written := make([]Predicate, 0, len(derived.Drafts))
	for _, draft := range derived.Drafts {
		p := Predicate{
			ID:                record.NewID(IDPrefix),
			Actor:             actor,
			At:                record.Now(),
			ItemID:            of.ItemID,
			ServiceID:         of.ServiceID,
			ArtifactID:        of.ArtifactID,
			Address:           draft.Address,
			ProducerService:   draft.ProducerService,
			ProducerServiceID: draft.ProducerServiceID,
			Interface:         draft.Interface,
			Element:           draft.Element,
			Kind:              draft.Kind,
			Argument:          draft.Argument,
		}
		if err := p.Validate(); err != nil {
			return Derivation{}, nil, err
		}
		if _, err := tx.Exec(ctx, `insert into `+Table+`
			(id, format_version, actor_kind, actor_key, actor_key_basis, at, item_id, service_id, artifact_id,
			address, producer_service, producer_service_id, interface_name, element, kind, argument)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
			p.ID, FormatVersion, string(p.Actor.Kind), p.Actor.Key, string(p.Actor.Basis), p.At,
			p.ItemID, p.ServiceID, p.ArtifactID, p.Address, p.ProducerService, p.ProducerServiceID,
			p.Interface, p.Element, string(p.Kind), p.Argument,
		); err != nil {
			return Derivation{}, nil, fmt.Errorf("consumercontract: writing %s of item %s: %w", p.Describe(), of.ItemID, err)
		}
		written = append(written, p)
	}
	return d, written, nil
}

// DeriveAgain writes a second derivation of one item's consumer contract, beside
// the earlier record and never over it. Its caller is the install's first-start
// step, which at an upgrade where the shipped extractor for a toolchain changed or
// was added derives again for every release in force on that toolchain, from the
// build the release names. That step is not built; this is what it calls.
//
// The new record names the new extractor, and a release's contract in force is its
// derivation by the newest extractor — which is what the in-force read already
// answers, the newest version of each item being what the item declares. Deriving
// again with the extractor the newest derivation already names is
// [ErrExtractorUnchanged]: the record would say what the record says.
//
// of.ArtifactID is a new consumer contract version, written by the artifact store
// in the same transaction, because a derivation is one row per version and this
// one is beside the earlier record rather than over it.
func DeriveAgain(ctx context.Context, tx pgx.Tx, actor record.Actor, of Of, derived Derived) (Derivation, []Predicate, error) {
	newest, found, err := NewestDerivation(ctx, tx, of.ItemID)
	if err != nil {
		return Derivation{}, nil, err
	}
	if found && newest.Extractor.Name == derived.Extractor.Name &&
		newest.Extractor.Version == derived.Extractor.Version {
		return Derivation{}, nil, fmt.Errorf("%w: %s %s", ErrExtractorUnchanged,
			derived.Extractor.Name, derived.Extractor.Version)
	}
	return Insert(ctx, tx, actor, of, derived)
}

// joinLines is a list as one column holds it, one per line — the arrangement
// item's waits_on column and environment's targets column both have.
func joinLines(lines []string) string { return strings.Join(lines, "\n") }

// splitLines is that column read back.
func splitLines(stored string) []string {
	var lines []string
	for _, line := range strings.Split(stored, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
