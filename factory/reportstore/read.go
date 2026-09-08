package reportstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/principal"
)

const selectReport = `select id, collected_at, shipped_bundle_identity, deploy_id, service_id,
	environment_id, kind, text, harm_marked, source_key, notice_id, intent_id, admitted_at
	from ` + ReportTable

// Get is one report, with its words served through the redactions naming it.
// It appends a read event naming the principal first, which is what makes who
// had already read the words answerable after a redaction, and a read event
// that cannot be appended is a report that is not served.
//
// The words are read through the redactions from the moment a redaction
// exists, whether or not this store's own destruction pass has reached the
// row yet, so a store whose destruction lags serves nothing meanwhile.
func (s *Store) Get(ctx context.Context, p principal.Principal, id string) (Report, error) {
	if id == "" {
		return Report{}, ErrIDEmpty
	}
	if err := s.events.Append(ctx, p, "report "+id); err != nil {
		return Report{}, fmt.Errorf("reportstore: appending the read event for %s: %w", id, err)
	}

	report, err := scan(s.pool.QueryRow(ctx, selectReport+` where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Report{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return Report{}, fmt.Errorf("reportstore: reading %s: %w", id, err)
	}

	redactions, err := s.redactions.ForReport(ctx, id)
	if err != nil {
		return Report{}, fmt.Errorf("reportstore: reading the redactions naming %s: %w", id, err)
	}
	for _, redaction := range redactions {
		report.Text, err = destroySpans(report.Text, redaction.Spans)
		if err != nil {
			return Report{}, fmt.Errorf("reportstore: serving %s through redaction %s: %w",
				id, redaction.ID, err)
		}
	}
	return report, nil
}

// Count is what one key of the counter table holds: the refusals made against
// it, and the submissions written under a shape this store does not read. The
// empty service is the whole channel.
type Count struct {
	ServiceID       string
	Refusals        int64
	UnreadableShape int64
}

// Counts is every counter this store keeps, the channel's first and then one
// per service, which is what Factory reads beside the ungrouped count. They
// are counters and not queries, and what that costs is that a lost counter is
// lost: nothing here can be recomputed from the reports.
func (s *Store) Counts(ctx context.Context) ([]Count, error) {
	rows, err := s.pool.Query(ctx, `select service_id, refusals, unreadable_shape
		from `+CounterTable+` order by service_id`)
	if err != nil {
		return nil, fmt.Errorf("reportstore: reading the counters: %w", err)
	}
	defer rows.Close()

	var read []Count
	for rows.Next() {
		var c Count
		if err := rows.Scan(&c.ServiceID, &c.Refusals, &c.UnreadableShape); err != nil {
			return nil, fmt.Errorf("reportstore: reading a counter: %w", err)
		}
		read = append(read, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reportstore: reading the counters: %w", err)
	}
	return read, nil
}

func scan(row pgx.Row) (Report, error) {
	var r Report
	var kind string
	if err := row.Scan(&r.ID, &r.CollectedAt, &r.ShippedBundleIdentity, &r.DeployID, &r.ServiceID,
		&r.EnvironmentID, &kind, &r.Text, &r.HarmMarked, &r.SourceKey, &r.NoticeID,
		&r.IntentID, &r.AdmittedAt); err != nil {
		return Report{}, err
	}
	r.Kind = Kind(kind)
	return r, nil
}
