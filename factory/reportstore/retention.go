package reportstore

import (
	"context"
	"fmt"
	"time"

	"github.com/dulguun0225/borg/factory/record"
)

// Retired is what one retention pass did: how many reports it removed, and
// the services whose reports it left because a legal hold stands over them.
// The second is what the caller records as the refusal the hold made.
type Retired struct {
	Removed int64
	Held    []string
}

// Retire removes the reports older than the retention an owner authored. It
// is the only thing that removes a report, and grouping never does: what a
// kept report buys is the rate of reports before and after the release meant
// to fix what they describe.
//
// Where an owner authored no retention, reports are kept for the life of the
// install and this removes nothing: no outcome teaches a retention period, so
// there is nothing to supply where one was not authored.
//
// A service a legal hold reaches keeps every report it has until the hold is
// withdrawn, and the pass goes on to the services no hold reaches rather than
// stopping, so one hold suspends one service's removals and not the pass.
func (s *Store) Retire(ctx context.Context, now time.Time) (Retired, error) {
	retention, authored, err := s.settings.ReportRetention(ctx)
	if err != nil {
		return Retired{}, fmt.Errorf("reportstore: reading how long a report is kept: %w", err)
	}
	if !authored || retention <= 0 {
		return Retired{}, nil
	}
	older := record.FormatTime(now.Add(-retention))

	services, err := s.servicesWithReportsBefore(ctx, older)
	if err != nil {
		return Retired{}, err
	}
	var retired Retired
	for _, serviceID := range services {
		held, err := s.holds.ReachingService(ctx, serviceID)
		if err != nil {
			return Retired{}, fmt.Errorf("reportstore: reading whether a hold reaches %s: %w", serviceID, err)
		}
		if held {
			retired.Held = append(retired.Held, serviceID)
			continue
		}
		tag, err := s.pool.Exec(ctx, `delete from `+ReportTable+`
			where service_id = $1 and collected_at < $2`, serviceID, older)
		if err != nil {
			return Retired{}, fmt.Errorf("reportstore: removing the expired reports of %s: %w", serviceID, err)
		}
		retired.Removed += tag.RowsAffected()
	}
	return retired, nil
}

// servicesWithReportsBefore is every service holding a report collected
// before older, in name order. The pass asks per service because what
// suspends a removal is a hold over a service, so one delete over the whole
// store could not tell the held reports from the rest.
func (s *Store) servicesWithReportsBefore(ctx context.Context, older string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `select distinct service_id from `+ReportTable+`
		where collected_at < $1 order by service_id`, older)
	if err != nil {
		return nil, fmt.Errorf("reportstore: reading which services hold an expired report: %w", err)
	}
	defer rows.Close()

	var services []string
	for rows.Next() {
		var serviceID string
		if err := rows.Scan(&serviceID); err != nil {
			return nil, fmt.Errorf("reportstore: reading a service holding an expired report: %w", err)
		}
		services = append(services, serviceID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reportstore: reading which services hold an expired report: %w", err)
	}
	return services, nil
}
