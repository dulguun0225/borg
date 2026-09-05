package notifier

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/factorysettings"
)

// harmMarkPagesOff reads whether the harm mark's page is turned off, the
// factory-wide settings' own field: "an owner who will not be woken by a
// stranger turns it off." A factory with no settings record yet — an
// install this milestone does not build — has never turned it off, so the
// zero value's "not found" reads as on, matching the shipped default the
// factorysettings package itself carries.
//
// What this does not do is the cap: how many intents a service's marked
// reports may page per interval is a count [Notify]'s caller keeps, since
// nothing here is told which intent this page is about or how many arrived
// before it — the intake that raises a harm-marked report is not built, and
// its dispatch is where that counter belongs.
func harmMarkPagesOff(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	settings, err := factorysettings.Get(ctx, pool)
	if errors.Is(err, factorysettings.ErrNotFound) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("notifier: reading whether the harm mark's page is on: %w", err)
	}
	return !settings.HarmMarkPages, nil
}
