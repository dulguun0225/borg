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

// apply is one group: the intent it is, the reports of it that have to join
// that intent, and the intent a split may leave holding nothing.
//
// The group is walked in the order the reports arrived and not in the order the
// reply listed them, so what a group resolves to is decided by arrival and not
// by whichever id the role wrote first.
//
// Which intent the group is. An intent belongs to the group holding the report
// that raised it — its statement was written over that report — so a group
// claims an intent only where that report is a member of it. A group naming an
// intent it does not hold the first report of is a group the role split off,
// and its members move. Where the group claims none, it raises one.
//
// Then every member not already in that intent joins it: an unlinked one
// through a link, a linked one through a move. A member whose own intent has
// been decomposed does not move — decomposition is the boundary, and after it a
// report matching work already decomposed attaches and raises the count rather
// than being taken out — so it stays where it is and this group goes on without
// it.
//
// Two cases sit outside that. Where the group claims no intent it raises one,
// which is the first report of a group raising it — unless a member the
// decomposition boundary left behind names what this one was judged against:
// a report that does not match work already decomposed still moves the group
// no further, so the new intent it raises is linked to the one it was left
// in as a recurrence, the same link a group naming a finished intent gets.
// Where the intent it claims has finished, the fix shipped: what is not
// already in it is a new intent linked to it as a recurrence and never a
// reopening.
//
// An intent a split leaves holding no report names nothing — its statement
// summarizes reports that are somewhere else — so it is ended through intake.
func (g *Grouper) apply(ctx context.Context, projectID string, reports []reportstore.Report,
	raisedBy map[string]string, group []string, placed map[string]bool, did *Grouped) error {
	var members []reportstore.Report
	for _, report := range reports {
		if !slices.Contains(group, report.ID) {
			continue
		}
		members = append(members, report)
		placed[report.ID] = true
	}
	if len(members) == 0 {
		return fmt.Errorf("%w: %v", ErrReplyNamesNoReport, group)
	}

	// The intent this group is: the first one a member names whose own raising
	// report is a member here.
	named := ""
	for _, report := range members {
		if report.IntentID != "" && raisedBy[report.IntentID] == report.ID {
			named = report.IntentID
			break
		}
	}

	joining, emptying, blockedFrom, marked, serviceID, err := g.joining(ctx, members, named)
	if err != nil {
		return err
	}
	if len(joining) == 0 {
		return nil
	}

	// The group raises an intent where it claims none, and where the one it
	// claims has finished — the recurrence, which carries the link back to it.
	// A group claiming none that held a member the boundary blocked from
	// joining is the other half of the same rule: decomposition stops the
	// member moving, and what it was judged against — the decomposed intent it
	// was left in — is what the new intent recurs on instead of naming nothing.
	raises := named == ""
	recurrenceOf := named
	if !raises {
		finished, err := g.finished(ctx, named)
		if err != nil {
			return err
		}
		raises = finished
	} else if len(blockedFrom) > 0 {
		recurrenceOf = blockedFrom[0]
	}
	intentID := named
	if raises {
		raised, err := g.raise(ctx, projectID, joining, recurrenceOf)
		if err != nil {
			return err
		}
		intentID = raised
		did.Raised = append(did.Raised, raised)
	}

	for _, report := range joining {
		if report.IntentID == "" {
			if err := g.c.Reports.Link(ctx, report.ID, intentID); err != nil {
				return err
			}
			did.Linked++
			continue
		}
		if err := g.c.Reports.Relink(ctx, report.ID, intentID); err != nil {
			return err
		}
		did.Moved++
	}
	if err := g.dropEmptied(ctx, emptying, did); err != nil {
		return err
	}
	if marked == 0 {
		return nil
	}
	paged, err := g.alreadyPaged(ctx, intentID)
	if err != nil {
		return err
	}
	if paged {
		return nil
	}
	if err := g.page(ctx, intentID, serviceID, marked); err != nil {
		return err
	}
	did.Paged++
	return nil
}

// joining is what one group's members have to be written to put them in the
// intent the group is: every member not already in it, the intents a move may
// leave empty, the decomposed intents a member was left in rather than moved
// out of, how many of them mark harm, and the service the group is against.
//
// A member whose own intent has been decomposed is left out of joining:
// decomposition is the boundary a split stops at, so it stays where it is.
// Its intent is recorded on blockedFrom rather than emptying — it is not a
// candidate to end, holding at least this one report still — so that a group
// left with nothing of its own but this can still name what it was judged
// against. Reading Decomposed is one query per intent a group names and none
// for a group that names none, which is every group of arriving reports.
func (g *Grouper) joining(ctx context.Context, members []reportstore.Report,
	named string) ([]reportstore.Report, []string, []string, int, string, error) {
	var joining []reportstore.Report
	var emptying, blockedFrom []string
	marked, serviceID := 0, ""
	for _, report := range members {
		if report.IntentID != "" && report.IntentID == named {
			continue
		}
		if report.IntentID != "" {
			decomposed, err := g.c.Decompositions.Decomposed(ctx, report.IntentID)
			if err != nil {
				return nil, nil, nil, 0, "", fmt.Errorf(
					"grouper: reading whether %s has been decomposed: %w", report.IntentID, err)
			}
			if decomposed {
				if !slices.Contains(blockedFrom, report.IntentID) {
					blockedFrom = append(blockedFrom, report.IntentID)
				}
				continue
			}
			if !slices.Contains(emptying, report.IntentID) {
				emptying = append(emptying, report.IntentID)
			}
		}
		joining = append(joining, report)
		if report.HarmMarked {
			marked++
		}
		if serviceID == "" {
			serviceID = report.ServiceID
		}
	}
	return joining, emptying, blockedFrom, marked, serviceID, nil
}

// dropEmptied ends every intent a split left holding no report. Its statement
// summarizes reports that are somewhere else, so it names nothing: what it once
// was is on the reports, which are now in another intent, and nothing was ever
// spent on it — a split is only possible before decomposition.
//
// An intent that still holds a report is left alone, which is every one a split
// took some but not all of.
func (g *Grouper) dropEmptied(ctx context.Context, emptying []string, did *Grouped) error {
	for _, intentID := range emptying {
		left, err := g.c.Reports.Grouped(ctx, intentID)
		if err != nil {
			return err
		}
		if left.Reports > 0 {
			continue
		}
		if err := g.c.Intake.DropEmptied(ctx, g.c.Actor, intentID); err != nil {
			return fmt.Errorf("grouper: ending %s, which a split left holding no report: %w", intentID, err)
		}
		did.Dropped = append(did.Dropped, intentID)
	}
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

// alreadyPaged reports whether intentID already carries a standing page: it
// is one page per intent however many reports mark harm, and never per
// report, so a later report attaching to an intent already paging raises
// nothing further whichever pass reads it. The notifier keeps one delivery
// record per row it has ever paged, [notifier.KindHarmMarkedReport] being the
// only kind an intent grouped from reports pages under, so any record at all
// against this row is that page.
func (g *Grouper) alreadyPaged(ctx context.Context, intentID string) (bool, error) {
	delivered, err := notifier.DeliveriesOf(ctx, g.c.Pool, intentID)
	if err != nil {
		return false, fmt.Errorf("grouper: reading whether %s already carries a page: %w", intentID, err)
	}
	return len(delivered) > 0, nil
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
