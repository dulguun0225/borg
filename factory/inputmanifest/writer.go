package inputmanifest

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrNamedNothing is returned by [Writer.Write] for a manifest naming
	// neither an item nor an intent. A manifest is written for a dispatch, and
	// a dispatch is always for one of the two.
	ErrNamedNothing = errors.New("inputmanifest: the manifest names neither an item nor an intent")
	// ErrStageWithoutAnItem is returned for a stage on a manifest that names
	// no item. A stage is the item's, so one without an item names nothing.
	ErrStageWithoutAnItem = errors.New("inputmanifest: the manifest names a stage and no item")
	// ErrReadAtOnceBoundNegative is returned for a bound below zero.
	ErrReadAtOnceBoundNegative = errors.New("inputmanifest: the read-at-once bound is negative")
	// ErrMaterialIncomplete is returned for a material entry with no class or
	// no reference.
	ErrMaterialIncomplete = errors.New("inputmanifest: a material entry names no class or no reference")
	// ErrExclusionIncomplete is returned for an exclusion entry with no what
	// or no reason.
	ErrExclusionIncomplete = errors.New("inputmanifest: an exclusion entry names no source or no reason")
	// ErrNotFound is returned where no manifest has the id.
	ErrNotFound = errors.New("inputmanifest: no manifest has that id")
)

// Writer is the one writer of input manifests. Context assembly is not built
// yet, so the caller that dispatches an agent holds this and writes the
// manifest before the run starts; doc.go says so.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewWriter returns the writer over pool, fencing every write with token.
func NewWriter(pool *pgxpool.Pool, token lease.Token) *Writer {
	return &Writer{pool: pool, token: token}
}

// New is what the dispatch knows before the run: what it is for, what it
// handed over, and what it left out.
type New struct {
	ItemID   string
	Stage    string
	IntentID string

	Materials            []Material
	ReadAtOnceBound      *int64
	SelectionRuleVersion string
	Excluded             []Exclusion
}

// Write writes the manifest, once. There is no update method: the manifest
// names what reached the agent at this run, and a later change to the fleet
// entry or the selection rule does not reach back into it.
func (w *Writer) Write(ctx context.Context, actor record.Actor, n New) (Manifest, error) {
	if err := actor.Validate(); err != nil {
		return Manifest{}, err
	}
	if n.ItemID == "" && n.IntentID == "" {
		return Manifest{}, ErrNamedNothing
	}
	if n.Stage != "" && n.ItemID == "" {
		return Manifest{}, ErrStageWithoutAnItem
	}
	if n.ReadAtOnceBound != nil && *n.ReadAtOnceBound < 0 {
		return Manifest{}, fmt.Errorf("%w: %d", ErrReadAtOnceBoundNegative, *n.ReadAtOnceBound)
	}
	for _, m := range n.Materials {
		if m.Class == "" || m.Reference == "" {
			return Manifest{}, fmt.Errorf("%w: %+v", ErrMaterialIncomplete, m)
		}
	}
	for _, e := range n.Excluded {
		if e.What == "" || e.Reason == "" {
			return Manifest{}, fmt.Errorf("%w: %+v", ErrExclusionIncomplete, e)
		}
	}

	m := Manifest{
		ID:                   record.NewID(IDPrefix),
		Actor:                actor,
		At:                   record.Now(),
		ItemID:               n.ItemID,
		Stage:                n.Stage,
		IntentID:             n.IntentID,
		Materials:            n.Materials,
		ReadAtOnceBound:      n.ReadAtOnceBound,
		SelectionRuleVersion: n.SelectionRuleVersion,
		Excluded:             n.Excluded,
	}

	materials, err := marshalMaterials(m.Materials)
	if err != nil {
		return Manifest{}, err
	}
	excluded, err := marshalExcluded(m.Excluded)
	if err != nil {
		return Manifest{}, err
	}
	var bound any
	if m.ReadAtOnceBound != nil {
		bound = *m.ReadAtOnceBound
	}

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Manifest{}, fmt.Errorf("inputmanifest: beginning the write of %s: %w", m.ID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return Manifest{}, err
	}

	_, err = tx.Exec(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at,
		item_id, stage, intent_id, materials, read_at_once_bound, selection_rule_version, excluded)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		m.ID, FormatVersion, string(m.Actor.Kind), m.Actor.Key, string(m.Actor.Basis), m.At,
		m.ItemID, m.Stage, m.IntentID, materials, bound, m.SelectionRuleVersion, excluded,
	)
	if err != nil {
		return Manifest{}, fmt.Errorf("inputmanifest: writing %s: %w", m.ID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Manifest{}, fmt.Errorf("inputmanifest: committing %s: %w", m.ID, err)
	}
	return m, nil
}
