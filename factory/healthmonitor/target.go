package healthmonitor

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/window"
)

// TargetBelow is the newest release of the service below number whose watch
// window closed without condemning it — cleared or timed out — and false where
// there is none. It answers two questions with one query, and they are the same
// question asked at two moments:
//
//   - what a rollback of that release returns to, which the design computes for
//     one rollback and never per service, because a query stated per service alone
//     would return a release above the condemned one and the factory would restore
//     the change it had just condemned;
//   - what a window over that release reads its comparison against, which needs
//     the same property for the same reason — a baseline above the release under
//     watch would measure a change against something that includes it.
//
// It descends past condemned, past skipped, and past any window still open. Nothing
// writes it: the release record is written once at the fast-forward and never
// again, so an outcome settled by a window closing long afterwards cannot be a
// field of it, and the fact is already implied by the records that exist. What that
// costs is that every path computes it rather than reading a field, and that a
// window which fails to close leaves the answer older than it should be — the
// rollback goes further back and undoes releases nothing condemned, up to the window
// limit, which
// is the safe direction and still a real loss.
func TargetBelow(ctx context.Context, pool *pgxpool.Pool, serviceID string, number int64) (release.Release, bool, error) {
	closed, err := window.ClosedWithoutCondemning(ctx, pool, serviceID)
	if err != nil {
		return release.Release{}, false, err
	}

	var best release.Release
	found := false
	for _, w := range closed {
		r, err := release.Get(ctx, pool, w.ReleaseID)
		if err != nil {
			return release.Release{}, false, fmt.Errorf("healthmonitor: reading the release window %s watched: %w", w.ID, err)
		}
		if r.Number >= number {
			continue
		}
		if !found || r.Number > best.Number {
			best, found = r, true
		}
	}
	return best, found, nil
}
