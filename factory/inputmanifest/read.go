package inputmanifest

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

// columns is every column of the table, in the order [scan] reads them.
const columns = `id, actor_kind, actor_key, actor_key_basis, at,
	item_id, stage, intent_id, materials, read_at_once_bound, selection_rule_version, excluded`

func scan(row pgx.Row) (Manifest, error) {
	var m Manifest
	var kind, basis, materials, excluded string
	var bound *int64
	err := row.Scan(&m.ID, &kind, &m.Actor.Key, &basis, &m.At,
		&m.ItemID, &m.Stage, &m.IntentID, &materials, &bound, &m.SelectionRuleVersion, &excluded)
	if err != nil {
		return Manifest{}, err
	}
	m.Actor.Kind = record.Kind(kind)
	m.Actor.Basis = record.Basis(basis)
	m.ReadAtOnceBound = bound
	if m.Materials, err = unmarshalMaterials(materials); err != nil {
		return Manifest{}, err
	}
	if m.Excluded, err = unmarshalExcluded(excluded); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// Get is one manifest by id. It takes the pool and not a [Writer], because
// reading a manifest is not a reason to be handed the thing that writes them.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Manifest, error) {
	m, err := scan(pool.QueryRow(ctx, `select `+columns+` from `+Table+` where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Manifest{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return Manifest{}, fmt.Errorf("inputmanifest: reading %s: %w", id, err)
	}
	return m, nil
}
