package build

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/exposure"
)

// Exposure reads the exposure evidence and distinguishes unavailable from an
// empty evidence list.
func Exposure(ctx context.Context, pool *pgxpool.Pool, buildID string) (exposure.Evidence, bool, error) {
	var stored *string
	var unavailable string
	err := pool.QueryRow(ctx, `select exposure, resolved_set_could_not_derive from `+Table+` where id = $1`, buildID).Scan(&stored, &unavailable)
	if errors.Is(err, pgx.ErrNoRows) {
		return exposure.Evidence{}, false, fmt.Errorf("%w: %s", ErrNotFound, buildID)
	} else if err != nil {
		return exposure.Evidence{}, false, fmt.Errorf("build: reading what %s reached: %w", buildID, err)
	}
	if unavailable != "" {
		return exposure.Evidence{Unavailable: "the resolved set could not be derived: " + unavailable}, true, nil
	}
	if stored == nil {
		return exposure.Evidence{}, false, nil
	}
	var read exposure.Evidence
	if err := json.Unmarshal([]byte(*stored), &read); err != nil {
		return exposure.Evidence{}, false, fmt.Errorf("build: decoding what %s reached: %w", buildID, err)
	}
	return read, true, nil
}
