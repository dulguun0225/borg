package reportstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrIntentIDEmpty is returned by [Store.Link] for a call naming no
	// intent. A report is either linked to the intent it was grouped into or
	// counted as ungrouped, and a link to nothing is neither.
	ErrIntentIDEmpty = errors.New("reportstore: the intent id is empty")
	// ErrAlreadyGrouped is returned by [Store.Link] for a report already
	// linked. A later report attaches to an intent and never rewrites one,
	// and the same rule holds one level down: the link is written once.
	ErrAlreadyGrouped = errors.New("reportstore: the report is already linked to an intent")
)

// Link marks the report with the intent it was grouped into and keeps it. The
// report is not deleted on grouping: what removes one is retention, and the
// rate of reports before and after the release meant to fix what they
// describe is the measurement keeping them makes possible.
func (s *Store) Link(ctx context.Context, reportID, intentID string) error {
	if reportID == "" {
		return ErrIDEmpty
	}
	if intentID == "" {
		return ErrIntentIDEmpty
	}
	tag, err := s.pool.Exec(ctx, `update `+ReportTable+` set intent_id = $1
		where id = $2 and intent_id = ''`, intentID, reportID)
	if err != nil {
		return fmt.Errorf("reportstore: linking %s to %s: %w", reportID, intentID, err)
	}
	if tag.RowsAffected() == 0 {
		return s.whyNoRow(ctx, reportID, ErrAlreadyGrouped)
	}
	return nil
}

// Admit records that a human admitted the report at Work, which is what an
// arrived report waits for while the admission safeguard is in force. It is
// this store's own instant: what it says is when the admission was recorded.
// Admitting a report already admitted changes nothing.
func (s *Store) Admit(ctx context.Context, reportID string) error {
	if reportID == "" {
		return ErrIDEmpty
	}
	tag, err := s.pool.Exec(ctx, `update `+ReportTable+` set admitted_at = $1
		where id = $2 and admitted_at = ''`, record.Now(), reportID)
	if err != nil {
		return fmt.Errorf("reportstore: admitting %s: %w", reportID, err)
	}
	if tag.RowsAffected() == 0 {
		return s.whyNoRow(ctx, reportID, nil)
	}
	return nil
}

// Group is what one intent's reports amount to without their words: how many
// were grouped into it, and whether any of them says a person is being harmed
// by the software.
type Group struct {
	Reports    int
	HarmMarked bool
}

// Grouped is [Group] for one intent. It reads no words and appends no read
// event: a count and a mark are facts about the group and not what the
// reporters wrote, so a screen showing how large a group is serves nothing
// anybody has to be answerable for having read.
func (s *Store) Grouped(ctx context.Context, intentID string) (Group, error) {
	if intentID == "" {
		return Group{}, ErrIntentIDEmpty
	}
	var g Group
	if err := s.pool.QueryRow(ctx, `select count(*), coalesce(bool_or(harm_marked), false)
		from `+ReportTable+` where intent_id = $1`, intentID).Scan(&g.Reports, &g.HarmMarked); err != nil {
		return Group{}, fmt.Errorf("reportstore: reading the group of %s: %w", intentID, err)
	}
	return g, nil
}

// Ungrouped is how many reports are linked to no intent, which is the number
// Factory reads: a report that is never grouped is work nobody sees, so every
// report is either linked or counted here.
func (s *Store) Ungrouped(ctx context.Context) (int64, error) {
	var n int64
	if err := s.pool.QueryRow(ctx, `select count(*) from `+ReportTable+
		` where intent_id = ''`).Scan(&n); err != nil {
		return 0, fmt.Errorf("reportstore: counting the ungrouped reports: %w", err)
	}
	return n, nil
}

// whyNoRow tells a write that matched no row from one that matched a row
// already carrying what it would have written: the first is [ErrNotFound] and
// the second is already, which a caller writing the same thing twice may
// treat as done.
func (s *Store) whyNoRow(ctx context.Context, reportID string, already error) error {
	var exists bool
	err := s.pool.QueryRow(ctx, `select exists (select 1 from `+ReportTable+` where id = $1)`,
		reportID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("reportstore: reading whether %s exists: %w", reportID, err)
	}
	if !exists {
		return fmt.Errorf("%w: %s", ErrNotFound, reportID)
	}
	if already != nil {
		return fmt.Errorf("%w: %s", already, reportID)
	}
	return nil
}

// ByIntent is the ids of the reports grouped into one intent, oldest first,
// which is what Work renders under that intent's own entry.
//
// It answers with ids and never with the reports: every read here that returns
// words appends a read event naming who read them — [Store.Get] one report at a
// time, [Store.Reports] and [Store.AwaitingAdmission] one per report they
// answer with — and an enumeration that carried the words would serve them with
// nobody answerable for the read.
func (s *Store) ByIntent(ctx context.Context, intentID string) ([]string, error) {
	if intentID == "" {
		return nil, ErrIntentIDEmpty
	}
	rows, err := s.pool.Query(ctx, `select id from `+ReportTable+`
		where intent_id = $1 order by collected_at, id`, intentID)
	if err != nil {
		return nil, fmt.Errorf("reportstore: reading the reports grouped into %s: %w", intentID, err)
	}
	defer rows.Close()

	var grouped []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("reportstore: reading a report grouped into %s: %w", intentID, err)
		}
		grouped = append(grouped, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reportstore: reading the reports grouped into %s: %w", intentID, err)
	}
	return grouped, nil
}
