package grouper

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/reportstore"
)

var (
	// ErrProjectIDEmpty is returned by [Grouper.Pass] for a pass naming no
	// project. The role is put on a project and its scope is that project, so
	// a pass over none reads the reports of nobody.
	ErrProjectIDEmpty = errors.New("grouper: the pass names no project")
	// ErrReplyNamesNoReport is returned where every id of one group names a
	// report this pass did not read. A group of ids that resolve to nothing
	// links nothing, and applying the groups beside it would leave that group
	// silently dropped.
	ErrReplyNamesNoReport = errors.New("grouper: a group names no report this pass read")
)

// Fleet is the one dispatch this pass makes: the grouper role put on the
// project, the reports handed over as the material, and the report ids of each
// group it answered with.
//
// It is an interface rather than a call on the component that runs an agent
// because the material an input manifest names and the reply an agent parses
// are the fleet's own vocabulary: a pass that named either would import the
// whole of what runs an agent to make one call, and what this package is about
// is reports and intents.
type Fleet interface {
	Group(ctx context.Context, projectID string, reports []reportstore.Report) ([][]string, error)
}

// Services is which services lie in one project. A report names the service
// whose way in wrote it, and which project that service is in is a field of
// the service record — a record this package does not read, the project being
// the only subject the pass is given.
type Services interface {
	InProject(ctx context.Context, projectID string) ([]string, error)
}

// Decompositions is whether an intent has been decomposed into the items that
// answer it, which is the boundary the design draws: before it the role may
// still split a group it got wrong and this pass moves the reports, and after
// it a report matching work already decomposed attaches and raises the count
// rather than being taken out of it.
//
// It is an interface because whether an intent has items is a read of the item
// record, which this pass does not import: what it is given is a project, and
// the records it writes through are the intent and the report.
type Decompositions interface {
	Decomposed(ctx context.Context, intentID string) (bool, error)
}

// Admission is whether the safeguard whose subject is the report store is in
// force. With one, an arrived report waits ungrouped until a human admits it
// at Work, and only an admitted report reaches the grouper.
//
// It is an interface because a safeguard is a record of the factory's graph
// read through the component that writes gate policy, and this pass reads
// neither.
type Admission interface {
	HoldsArrivingReports(ctx context.Context) (bool, error)
}

// AdmitsEverything is what a pass composed with no admission reading uses:
// nothing waits for a human, which is an install with no such safeguard.
type AdmitsEverything struct{}

// HoldsArrivingReports reports that nothing is held.
func (AdmitsEverything) HoldsArrivingReports(context.Context) (bool, error) { return false, nil }

// Composition is what a grouper is built from. Every field but Admission is
// required.
type Composition struct {
	// Pool is the factory's own store, read for the state of an intent a group
	// already names and for whether an intent already carries the page a
	// harm-marked report fires, which is the notifier's own record on the same
	// store. The reports are in a store of their own, behind Reports.
	Pool *pgxpool.Pool
	// Reports is the report store: the reads this pass makes, and the link it
	// writes on a report it grouped.
	Reports *reportstore.Store
	// Intake is the one writer of an intent, which this pass calls once per
	// group that raises one.
	Intake *intent.Intake
	// Notifier is where the page a harm-marked report fires goes.
	Notifier *notifier.Notifier
	Fleet    Fleet
	Services Services
	// Decompositions is the boundary a split stops at.
	Decompositions Decompositions
	// Admission is whether the admission safeguard is in force. A nil value is
	// [AdmitsEverything].
	Admission Admission
	// Actor is who the intents this pass raises are written as. It is the
	// composition's and not this package's: the write is intake's own, the
	// intent's one writer being intake whichever of the three sources called
	// it, so the actor on the row is intake's.
	Actor record.Actor
	// ReadsAs is who the read of a report's words is made as, which the read
	// event names. It is the composition's for the reason Actor is:
	// ../../end-goal/components.md gives the grouper no row, it being an agent
	// and not a component, so what a read event calls this pass is not a name
	// this package coins.
	ReadsAs principal.Principal
}

// Grouper is the pass that turns reports into intents: it reads the reports
// that arrived under one project, dispatches the grouper role over them, and
// applies the answer.
//
// It holds nothing between passes. What a pass left is on the records — the
// intent a report is marked with, and the ungrouped count over the rest — so a
// start reads both rather than resuming anything.
type Grouper struct {
	c Composition
}

// New returns the pass. It refuses a composition missing anything, because a
// grouper that could not link a report or raise an intent would spend a model
// call on words and leave no record that it read them.
func New(c Composition) (*Grouper, error) {
	missing := []struct {
		what   string
		absent bool
	}{
		{"a pool", c.Pool == nil},
		{"the report store", c.Reports == nil},
		{"intake", c.Intake == nil},
		{"a notifier", c.Notifier == nil},
		{"a fleet to dispatch through", c.Fleet == nil},
		{"the services of a project", c.Services == nil},
		{"a reading of what has been decomposed", c.Decompositions == nil},
	}
	for _, one := range missing {
		if one.absent {
			return nil, fmt.Errorf("grouper: the composition names no %s", one.what)
		}
	}
	if err := c.Actor.Validate(); err != nil {
		return nil, fmt.Errorf("grouper: the actor the intents are written as: %w", err)
	}
	if err := c.ReadsAs.Validate(); err != nil {
		return nil, fmt.Errorf("grouper: who the reports are read as: %w", err)
	}
	if c.Admission == nil {
		c.Admission = AdmitsEverything{}
	}
	return &Grouper{c: c}, nil
}

// Grouped is what one pass did.
type Grouped struct {
	// Raised is every intent this pass raised, by id, a recurrence among them.
	Raised []string
	// Linked is how many reports were marked with the intent they were grouped
	// into, whether that intent was raised here or already stood.
	Linked int
	// Moved is how many reports were taken out of the intent they were in and
	// put in another, which is a group the role got wrong being split. A report
	// whose intent has been decomposed is never among them: decomposition is
	// the boundary, and after it a matching report attaches and raises the
	// count.
	Moved int
	// Dropped is every intent a split left holding no report, by id. Its
	// statement summarizes reports that are somewhere else, so it names
	// nothing and is ended through intake.
	Dropped []string
	// Paged is how many intents a harm-marked report fired a page for: one per
	// intent, however many of its reports carry the mark.
	Paged int
	// Declined is how many reports the reply left in no group, each of which
	// this pass raised an intent of its own for — a group of one — rather than
	// leaving linked to nothing. It is among Raised and not a count of anything
	// left ungrouped: what a pass reads always ends grouped, whatever the reply
	// does or does not name.
	Declined int
}

// Wrote reports whether the pass wrote anything, which is what the process's
// pass answers on.
func (g Grouped) Wrote() bool {
	return len(g.Raised) > 0 || g.Linked > 0 || g.Moved > 0 || len(g.Dropped) > 0
}

// Pass groups one project's reports once. Arrival is the trigger and not an
// interval: what hands each accepted report to this at once is the
// composition's own, since that is what puts an entrance in front of the
// report store this pass reads — a call this package does not make and does
// not need to, because grouping the one project a report arrived under reads
// every report of it regardless of how many arrived since the last call. A
// composition that also runs this on an interval gets the catch-up a restart
// needs: a report accepted while the factory was down reaches this no other
// way, and a call that finds nothing left ungrouped is what that catch-up
// costs once arrival has already handled everything.
//
// It reads how many of that project's reports are linked to no intent before
// it reads any words: a pass with nothing to group makes no model call and no
// read event, so a call that finds nothing new — whether the tick behind the
// interval above or a second arrival racing the first — costs nothing.
// Where there is something, every report of the project is read — the
// grouped ones beside the ungrouped ones, because a report that arrived
// after a group was raised can only be matched against the reports of that
// group — and the role is dispatched over all of them.
//
// Nothing waits for a batch or a count: the first report of a group raises its
// intent and later matching ones attach. Where the role answers with a group
// the last pass got wrong, the reports of it move — up to decomposition, which
// is where the boundary is and after which a matching report attaches instead
// and one that does not becomes a new intent linked to the first as a
// recurrence.
//
// Every report this pass read ends linked to an intent. One a report left in
// no group is applied the same way any other group is — a group of one — so
// what is left ungrouped once the pass returns is only what it did not read at
// all, and never a report the reply declined to place.
func (g *Grouper) Pass(ctx context.Context, projectID string) (Grouped, error) {
	var did Grouped
	if projectID == "" {
		return did, ErrProjectIDEmpty
	}
	services, err := g.c.Services.InProject(ctx, projectID)
	if err != nil {
		return did, fmt.Errorf("grouper: reading the services of %s: %w", projectID, err)
	}
	if len(services) == 0 {
		return did, nil
	}
	admittedOnly, err := g.c.Admission.HoldsArrivingReports(ctx)
	if err != nil {
		return did, fmt.Errorf("grouper: reading whether a report waits for a human: %w", err)
	}
	waiting, err := g.c.Reports.UngroupedIn(ctx, services, admittedOnly)
	if err != nil {
		return did, err
	}
	if waiting == 0 {
		return did, nil
	}

	reports, err := g.c.Reports.Reports(ctx, g.c.ReadsAs, services, admittedOnly)
	if err != nil {
		return did, err
	}
	if len(reports) == 0 {
		return did, nil
	}
	groups, err := g.c.Fleet.Group(ctx, projectID, reports)
	if err != nil {
		return did, err
	}

	// Which report raised each intent: the earliest one linked to it, over the
	// whole read. An intent belongs to the group holding that report, because
	// its statement was written over it, and every other group naming it is a
	// group the role split off — which is what moves reports.
	raisedBy := map[string]string{}
	for _, report := range reports {
		if report.IntentID != "" {
			if _, seen := raisedBy[report.IntentID]; !seen {
				raisedBy[report.IntentID] = report.ID
			}
		}
	}

	placed := map[string]bool{}
	for _, group := range groups {
		if err := g.apply(ctx, projectID, reports, raisedBy, group, placed, &did); err != nil {
			return did, err
		}
	}
	// A report the reply left in no group still ends this pass grouped: it
	// raises an intent of its own, a group of one, applied the same way any
	// other group is. One report from one end user is an intent, so the count
	// of reports left ungrouped at Factory is what a pass has not yet
	// reached and never what the reply declined to name.
	for _, report := range reports {
		if report.IntentID == "" && !placed[report.ID] {
			if err := g.apply(ctx, projectID, reports, raisedBy, []string{report.ID}, placed, &did); err != nil {
				return did, err
			}
			did.Declined++
		}
	}
	return did, nil
}
