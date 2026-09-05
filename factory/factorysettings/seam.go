package factorysettings

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ErrSeam5NotTurnedOff is returned by [SetSeam5Enforced] for a write that would
// turn enforcement off. An owner turns it on once and nothing turns it off again,
// so the refusal is here rather than in whoever calls it.
var ErrSeam5NotTurnedOff = errors.New("factorysettings: seam 5 is turned on once and never off")

// SetSeam5Enforced writes whether seam 5 is enforced, inside tx. It is one-way:
// off at install, turned on once, and never off again. A safeguard does not reach
// it — only a constraint of the document kind may require that it be turned on,
// and that constraint is not built.
func SetSeam5Enforced(ctx context.Context, tx pgx.Tx, settingsID string, enforced bool) error {
	if !enforced {
		return ErrSeam5NotTurnedOff
	}
	return update(ctx, tx, settingsID, `seam_5_enforced = $1`, true)
}
