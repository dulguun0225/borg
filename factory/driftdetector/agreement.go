package driftdetector

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// RecordChainAgreement is the second comparison's later agreement: the chain
// found sound again over an uncleared [MismatchKindChain] mismatch, recorded
// on it the way a later agreeing pass is on a target mismatch —
// [Mismatch.LaterAgreements] — so the human clearing it has that evidence
// too. It records nothing where no chain mismatch stands uncleared.
func (w *Writer) RecordChainAgreement(ctx context.Context) (string, error) {
	standing, err := UnclearedChain(ctx, w.pool)
	if err != nil {
		return "", err
	}
	if len(standing) == 0 {
		return "", nil
	}
	id := standing[0].ID
	if _, err := w.pool.Exec(ctx, `update `+MismatchTable+`
		set later_agreements = later_agreements + 1 where id = $1`, id); err != nil {
		return "", fmt.Errorf("driftdetector: recording a later agreement on %s: %w", id, err)
	}
	return id, nil
}

// RecordStaleComponentAgreement is the third comparison's later agreement: a
// component's last check answering fresh again over what an uncleared
// [MismatchKindStaleComponent] mismatch names for component, serviceID and
// target, recorded on it the way a later agreeing pass is on a target
// mismatch — [Mismatch.LaterAgreements] — so the human clearing it has that
// evidence too. It records nothing where no such mismatch stands uncleared.
func (w *Writer) RecordStaleComponentAgreement(ctx context.Context, component, serviceID, target string) (string, error) {
	var standing string
	err := w.pool.QueryRow(ctx, `select id from `+MismatchTable+`
		where kind = $1 and component = $2 and service_id = $3 and target = $4 and cleared_at = ''
		order by at limit 1`,
		MismatchKindStaleComponent, component, serviceID, target).Scan(&standing)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	} else if err != nil {
		return "", fmt.Errorf("driftdetector: reading the standing mismatch on %s: %w", component, err)
	}
	if _, err := w.pool.Exec(ctx, `update `+MismatchTable+`
		set later_agreements = later_agreements + 1 where id = $1`, standing); err != nil {
		return "", fmt.Errorf("driftdetector: recording a later agreement on %s: %w", standing, err)
	}
	return standing, nil
}
