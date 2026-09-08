package grouper

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/reportstore"
)

// apply is one group: the intent it belongs to, and the reports of it that are
// not yet marked with one.
//
// The group is walked in the order the reports arrived and not in the order the
// reply listed them, so which intent a group already names is the earliest
// grouped report's and not whichever id the role wrote first.
//
// Three cases, and the intent's own state is what tells them apart:
//
//   - no report of the group is grouped yet, so this is the first of a group
//     and it raises an intent through intake;
//   - the group already names an intent that has not finished, so the arriving
//     reports attach to it and raise its count. Nothing is rewritten: what
//     refines an intent is attached to it and every reader reads both, which
//     is what keeps an approval pointing at what was approved;
//   - the group names an intent whose timeline is finished. The fix shipped,
//     so evidence that it did not work is a new intent linked to the first as
//     a recurrence and never a reopening of it.
//
// A report already marked with an intent is never moved. Before decomposition
// the role may still split a group it got wrong, and what a split moves is
// where the arriving reports go; the link already written stands, and two
// problems in one intent are separated one stage down where decomposition
// yields an item each.
func (g *Grouper) apply(ctx context.Context, projectID string, reports []reportstore.Report,
	group []string, placed map[string]bool, did *Grouped) error {
	var arriving []reportstore.Report
	named, serviceID, marked, known := "", "", 0, 0
	for _, report := range reports {
		if !slices.Contains(group, report.ID) {
			continue
		}
		known++
		placed[report.ID] = true
		if report.IntentID != "" {
			if named == "" {
				named = report.IntentID
			}
			continue
		}
		arriving = append(arriving, report)
		if report.HarmMarked {
			marked++
		}
		if serviceID == "" {
			serviceID = report.ServiceID
		}
	}
	if known == 0 {
		return fmt.Errorf("%w: %v", ErrReplyNamesNoReport, group)
	}
	if len(arriving) == 0 {
		return nil
	}

	// The group raises an intent where it names none, and where the one it
	// names has finished — the recurrence, which carries the link back to it.
	raises := named == ""
	if !raises {
		finished, err := g.finished(ctx, named)
		if err != nil {
			return err
		}
		raises = finished
	}
	intentID := named
	if raises {
		raised, err := g.raise(ctx, projectID, arriving, named)
		if err != nil {
			return err
		}
		intentID = raised
		did.Raised = append(did.Raised, raised)
	}

	for _, report := range arriving {
		if err := g.c.Reports.Link(ctx, report.ID, intentID); err != nil {
			return err
		}
		did.Linked++
	}
	if marked == 0 {
		return nil
	}
	if err := g.page(ctx, intentID, serviceID, marked); err != nil {
		return err
	}
	did.Paged++
	return nil
}

// finished reports whether an intent's timeline is over: delivered, which is
// the fix shipped and confirmed, or dropped, which is a human ending it for
// good. An escalated intent is not finished — it waits on a human who may take
// it over — so a report matching it attaches.
func (g *Grouper) finished(ctx context.Context, intentID string) (bool, error) {
	in, err := intent.Get(ctx, g.c.Pool, intentID)
	if err != nil {
		return false, fmt.Errorf("grouper: reading the intent %s a group already names: %w", intentID, err)
	}
	return in.State == intent.StateDelivered || in.State == intent.StateDropped, nil
}

// raise writes one intent through intake, which is the one writer of one
// whichever of the three sources called it. recurrenceOf is the intent the new
// one recurs on, and empty where it recurs on none.
func (g *Grouper) raise(ctx context.Context, projectID string, reports []reportstore.Report,
	recurrenceOf string) (string, error) {
	raised, err := g.c.Intake.TakeIn(ctx, g.c.Actor, intent.Arrival{
		Source:       intent.SourceReports,
		Statement:    statementOf(reports),
		ProjectID:    projectID,
		RecurrenceOf: recurrenceOf,
	})
	if err != nil {
		return "", fmt.Errorf("grouper: raising the intent for a group of %d report(s): %w",
			len(reports), err)
	}
	return raised.ID, nil
}

// page is the page a harm-marked report fires, one per intent however many of
// its reports carry the mark. The row is the intent, so the page's events are
// the sequence on it; the wait belongs to no duty, which routes it to the
// owner; and the service is what the cap on how many such pages go out per
// interval is read against, the mark's own key bounding nothing here.
func (g *Grouper) page(ctx context.Context, intentID, serviceID string, marked int) error {
	_, err := g.c.Notifier.Notify(ctx, notifier.Wait{
		Row:  intentID,
		Kind: notifier.KindHarmMarkedReport,
		Waiting: fmt.Sprintf("%d report(s) grouped into %s say a person is being harmed by the software",
			marked, intentID),
		Worse:     true,
		ServiceID: serviceID,
	})
	if err != nil {
		return fmt.Errorf("grouper: firing the page for the marked reports of %s: %w", intentID, err)
	}
	return nil
}

// quoted is how many of a group's reports the statement quotes. A group is
// unbounded — five hundred reports of one slow button are one intent — and a
// statement holding all of them would be a field nobody reads; the reports
// themselves are what the interview and the spec are authored against, and this
// is what a reader who has not opened them is shown.
const quoted = 3

// statementOf is the statement of an intent grouped from reports: what the
// reports say, summarized. It is this pass's own and not the role's — the role
// decides which reports are one problem and nothing else — and it is written
// once: a later report attaches to the intent and never rewrites it, so this is
// over the reports that raised it and over no others.
func statementOf(reports []reportstore.Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d end-user report(s) grouped as one problem", len(reports))
	if bugs := count(reports, reportstore.KindBug); bugs > 0 {
		fmt.Fprintf(&b, ", %d of them bug reports", bugs)
	}
	b.WriteString(": ")
	for n, report := range reports {
		if n == quoted {
			fmt.Fprintf(&b, "; and %d more", len(reports)-quoted)
			break
		}
		if n > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%q", strings.TrimSpace(report.Text))
	}
	return b.String()
}

// count is how many of the reports are of one kind.
func count(reports []reportstore.Report, kind reportstore.Kind) int {
	n := 0
	for _, report := range reports {
		if report.Kind == kind {
			n++
		}
	}
	return n
}
