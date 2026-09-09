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

// Count is what one key of the counter table holds: the refusals made
// against it, a submission of a shape this store does not read among them.
// The empty service is the whole channel.
type Count struct {
	ServiceID string
	Refusals  int64
}

// Counts is every counter this store keeps, the channel's first and then one
// per service, which is what Factory reads beside the ungrouped count. It is
// a counter and not a query, and what that costs is that a lost counter is
// lost: nothing here can be recomputed from the reports.
func (s *Store) Counts(ctx context.Context) ([]Count, error) {
	rows, err := s.pool.Query(ctx, `select service_id, refusals
		from `+CounterTable+` order by service_id`)
	if err != nil {
		return nil, fmt.Errorf("reportstore: reading the counters: %w", err)
	}
	defer rows.Close()

	var read []Count
	for rows.Next() {
		var c Count
		if err := rows.Scan(&c.ServiceID, &c.Refusals); err != nil {
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

// UngroupedIn is how many of these services' reports are linked to no intent,
// which is what says the pass that groups them has anything to do. It reads no
// words and appends no read event, so a pass with nothing to group costs one
// count and never a read of a stranger's words.
//
// admittedOnly is the admission safeguard in force: with it, a report no human
// has admitted is not counted here either, because it is not one the grouper
// may read.
func (s *Store) UngroupedIn(ctx context.Context, serviceIDs []string, admittedOnly bool) (int, error) {
	if len(serviceIDs) == 0 {
		return 0, nil
	}
	var n int
	if err := s.pool.QueryRow(ctx, `select count(*) from `+ReportTable+
		` where intent_id = '' and service_id = any($1)`+admitted(admittedOnly),
		serviceIDs).Scan(&n); err != nil {
		return 0, fmt.Errorf("reportstore: counting the ungrouped reports of %v: %w", serviceIDs, err)
	}
	return n, nil
}

// Reports is every report of these services, oldest first, each with its words
// served through the redactions naming it and a read event appended before it
// is answered with.
//
// The grouped reports come back beside the ungrouped ones, because deciding
// which reports are one problem is a decision over all of them: a report that
// arrived after a group was raised can only be matched against the reports of
// that group. What that costs is that the whole of a project's reports are read
// at every pass that has anything to group.
//
// admittedOnly is the admission safeguard in force: with it, a report no human
// has admitted is not answered at all, so its words never leave the install
// unread.
func (s *Store) Reports(ctx context.Context, p principal.Principal, serviceIDs []string,
	admittedOnly bool) ([]Report, error) {
	return s.reports(ctx, p, "the reports of", ` where service_id = any($1)`+admitted(admittedOnly),
		serviceIDs)
}

// AwaitingAdmission is every report of these services that is linked to no
// intent and that no human has admitted, oldest first, with its words. It is
// what Work renders while the safeguard holding an arrived report stands: the
// report waits ungrouped, and admitting it is what lets the grouper read it.
//
// It is a read of its own rather than [Store.Reports] filtered by its caller
// because the two answer different questions of the same table, and a screen
// that read every report to show the few waiting would serve words nobody asked
// for — each of which appends a read event.
func (s *Store) AwaitingAdmission(ctx context.Context, p principal.Principal,
	serviceIDs []string) ([]Report, error) {
	return s.reports(ctx, p, "the reports awaiting admission of",
		` where service_id = any($1) and intent_id = '' and admitted_at = ''`, serviceIDs)
}

// reports is what the two reads above share: the rows the clause selects,
// oldest first, each served through the redactions naming it after a read event
// naming the principal is appended. The event is spelled the way [Store.Get]
// spells it, so one search over the log finds every read of a report's words,
// and a read event that cannot be appended is a report that is not served.
func (s *Store) reports(ctx context.Context, p principal.Principal, what, where string,
	serviceIDs []string) ([]Report, error) {
	if len(serviceIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, selectReport+where+` order by collected_at, id`, serviceIDs)
	if err != nil {
		return nil, fmt.Errorf("reportstore: reading %s %v: %w", what, serviceIDs, err)
	}
	defer rows.Close()

	var read []Report
	for rows.Next() {
		report, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("reportstore: reading one of %s %v: %w", what, serviceIDs, err)
		}
		read = append(read, report)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reportstore: reading %s %v: %w", what, serviceIDs, err)
	}

	// The redactions are read once for the whole answer rather than once per
	// row, the way package intent's own read of many statements does.
	redactions, err := s.redactions.OverReports(ctx)
	if err != nil {
		return nil, fmt.Errorf("reportstore: reading the redactions naming a report: %w", err)
	}
	for n, report := range read {
		if err := s.events.Append(ctx, p, "report "+report.ID); err != nil {
			return nil, fmt.Errorf("reportstore: appending the read event for %s: %w", report.ID, err)
		}
		for _, redaction := range redactions {
			if redaction.ReportID != report.ID {
				continue
			}
			if read[n].Text, err = destroySpans(read[n].Text, redaction.Spans); err != nil {
				return nil, fmt.Errorf("reportstore: serving %s through redaction %s: %w",
					report.ID, redaction.ID, err)
			}
		}
	}
	return read, nil
}

// admitted is the clause the admission safeguard adds to a read of reports.
// [Store.UngroupedIn] and [Store.Reports] spell it once rather than twice, so a
// change to what admission means reaches both.
func admitted(only bool) string {
	if only {
		return ` and admitted_at <> ''`
	}
	return ""
}

// BeforeAndAfter is one group's reports counted on each side of an instant,
// with the instant the oldest of them arrived. It is what the rate of reports
// before and after the release meant to fix what they describe is computed
// from, and it is why grouping keeps a report rather than removing one.
//
// The counts are of reports and never of words: nothing here returns text, so
// no read event is appended and the measurement costs nobody an answer for
// having read a stranger's words.
type BeforeAndAfter struct {
	Before int64
	After  int64
	// OldestAt is when the oldest report of the group was collected, in
	// record.TimeLayout, and empty where the group holds none.
	OldestAt string
}

// CountsAround is [BeforeAndAfter] over the reports linked to any of these
// intents, split at at. The caller passes the intent the reports were grouped
// into and every intent recurring on it, because a report matching work
// already shipped is a new intent linked to the first as a recurrence and is
// the same problem still arriving.
func (s *Store) CountsAround(ctx context.Context, intentIDs []string, at string) (BeforeAndAfter, error) {
	if len(intentIDs) == 0 || at == "" {
		return BeforeAndAfter{}, nil
	}
	var counted BeforeAndAfter
	var oldest *string
	if err := s.pool.QueryRow(ctx, `select
		count(*) filter (where collected_at < $2),
		count(*) filter (where collected_at >= $2),
		min(collected_at)
		from `+ReportTable+` where intent_id = any($1)`,
		intentIDs, at).Scan(&counted.Before, &counted.After, &oldest); err != nil {
		return BeforeAndAfter{}, fmt.Errorf("reportstore: counting the reports of %v around %s: %w",
			intentIDs, at, err)
	}
	if oldest != nil {
		counted.OldestAt = *oldest
	}
	return counted, nil
}
