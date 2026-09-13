package build

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func scanBuild(ctx context.Context, pool *pgxpool.Pool, row scanner) (Build, error) {
	b, err := scan(row)
	if err != nil {
		return Build{}, err
	}
	b.Coverage, err = ReadCoverage(ctx, pool, b.ID)
	if err != nil {
		return Build{}, err
	}
	return b, nil
}

// ReadCoverage reads the typed resolver coverage for a build.
func ReadCoverage(ctx context.Context, pool *pgxpool.Pool, buildID string) ([]Coverage, error) {
	rows, err := pool.Query(ctx, `select ecosystem, source, base_image_packages, vendored_source,
		statically_linked_code, digests, fetch_without_running, fetch_without_running_reason, missing_digests from `+CoverageTable+` where build_id = $1 order by at, id`, buildID)
	if err != nil {
		return nil, fmt.Errorf("build: reading coverage of %s: %w", buildID, err)
	}
	defer rows.Close()
	var read []Coverage
	for rows.Next() {
		var c Coverage
		if err := rows.Scan(&c.Ecosystem, &c.Source, &c.BaseImagePackages, &c.VendoredSource,
			&c.StaticallyLinkedCode, &c.Digests, &c.FetchWithoutRunning, &c.FetchWithoutRunningReason, &c.MissingDigests); err != nil {
			return nil, fmt.Errorf("build: reading coverage of %s: %w", buildID, err)
		}
		read = append(read, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("build: reading coverage of %s: %w", buildID, err)
	}
	return read, nil
}
