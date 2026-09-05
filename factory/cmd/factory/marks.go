package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/window"
)

// marks is [score.Marks]: the releases whose rollback a named human at Ops
// marked as not caused by the release. The mark is a row of package window's
// mark table and it points at the rollback's own deploy record, so the release
// the score excludes is that record's failed release — which is why this read
// is composed here rather than being a method of either package: window does
// not read deploy records and score reads neither table directly.
//
// A mark whose deploy cannot be read is skipped rather than failing the pass. A
// mark is evidence the learning excludes, so a mark the factory cannot resolve
// leaves the rollback counted, which is the direction that teaches the score
// more rather than less.
type marks struct{ pool *pgxpool.Pool }

// marksOf is the reader the composition hands the score and the learning pass.
func marksOf(pool *pgxpool.Pool) score.Marks { return marks{pool: pool} }

// NotCausedByTheRelease is the failed release of every marked rollback.
func (m marks) NotCausedByTheRelease(ctx context.Context) ([]string, error) {
	all, err := window.Marks(ctx, m.pool)
	if err != nil {
		return nil, err
	}
	releases := make([]string, 0, len(all))
	for _, one := range all {
		dep, err := deploy.Get(ctx, m.pool, one.DeployID)
		if err != nil {
			continue
		}
		if dep.Undoing.FailedReleaseID != "" {
			releases = append(releases, dep.Undoing.FailedReleaseID)
		}
	}
	return releases, nil
}
