package policy

import (
	"context"

	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gatepolicy"
)

// AttemptLimit is how many attempts one stage gets: what an owner authored on the
// factory-wide settings record, the score's supplied value where they authored
// none, and a safeguard's ceiling over either.
func (r *Reader) AttemptLimit(ctx context.Context, s Subjects) (Effective, error) {
	settings, err := factorysettings.Get(ctx, r.pool)
	if err != nil {
		return Effective{}, err
	}
	subject, err := factorysettings.OfStage(s.Stage)
	if err != nil {
		return Effective{}, err
	}
	authored, err := factorysettings.AttemptLimit(ctx, r.pool, settings.ID, subject)
	if err != nil {
		return Effective{}, err
	}
	return r.resolve(ctx, gatepolicy.AttemptLimit, authored, s)
}
