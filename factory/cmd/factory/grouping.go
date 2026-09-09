package main

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/fleetentry"
	"github.com/dulguun0225/borg/factory/grouper"
	"github.com/dulguun0225/borg/factory/inputmanifest"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/reportstore"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/service"
)

// The grouper as this process composes it: the three seams package grouper
// reaches the rest of the factory through, and the pass that runs it.
//
// Only a composition holding the report store composes one, which is the
// process that serves the entrance: a subcommand that makes one pass and exits
// opens no report store, so there is nothing for a grouper to read.

// groupsThroughTheFleet is [grouper.Fleet]: the grouper role put on the
// project, dispatched the way every other role is. The material is the reports
// themselves, one entry per report under the class a fleet entry names for
// them, so an owner narrowing an entry to exclude reports leaves the run with
// nothing and the manifest saying why.
//
// It is composed here and not in the pass because both halves of this call are
// the fleet's vocabulary: what the input manifest names, and the reply the
// agent parses.
type groupsThroughTheFleet struct{ p *path }

// Group dispatches the role and answers with the report ids of each group.
func (f groupsThroughTheFleet) Group(ctx context.Context, projectID string,
	reports []reportstore.Report) ([][]string, error) {
	var grouping agent.Grouping
	material := make([]inputmanifest.Material, 0, len(reports))
	for _, report := range reports {
		grouping.Reports = append(grouping.Reports, agent.Report{
			ID: report.ID, Kind: string(report.Kind), Text: report.Text,
		})
		material = append(material, inputmanifest.Material{
			Class: fleetentry.ClassReports, Reference: report.ID, Bytes: int64(len(report.Text)),
		})
	}
	read, _, err := f.p.dispatch.Grouper(ctx, dispatch.On{ProjectID: projectID}, material, grouping)
	if err != nil {
		return nil, err
	}
	return read.Groups, nil
}

// servicesInAProject is [grouper.Services]: which services lie in one project,
// read off the service record, which is where the project a report was made
// against is reachable from. A report names its service and never its project.
type servicesInAProject struct{ p *path }

// InProject is the ids of the services in that project.
func (s servicesInAProject) InProject(ctx context.Context, projectID string) ([]string, error) {
	every, err := service.All(ctx, s.p.d.pool)
	if err != nil {
		return nil, err
	}
	var in []string
	for _, one := range every {
		if one.ProjectID == projectID {
			in = append(in, one.ID)
		}
	}
	return in, nil
}

// admissionSafeguards is [grouper.Admission] and [dispatch.Admissions] both:
// the two safeguards drawn on the report store, read in force through the
// component that writes gate policy. One value implements both because the two
// readings are one query apiece against the same subject, and the caller each
// answers is what tells them apart — the grouper asks whether an arrived report
// waits, and dispatch whether a report-derived intent does.
//
// They are read at every pass and every dispatch rather than marked when a
// report or an intent arrived: a safeguard withdrawn is a safeguard not in
// force, so withdrawing one releases what was waiting on it.
type admissionSafeguards struct{ p *path }

// HoldsArrivingReports is [grouper.Admission].
func (a admissionSafeguards) HoldsArrivingReports(ctx context.Context) (bool, error) {
	in, err := a.p.policy.ReportStoreAdmissions(ctx)
	return in.ArrivingReports, err
}

// HoldsReportDerivedIntents is [dispatch.Admissions].
func (a admissionSafeguards) HoldsReportDerivedIntents(ctx context.Context) (bool, error) {
	in, err := a.p.policy.ReportStoreAdmissions(ctx)
	return in.ReportDerivedIntents, err
}

// groupReports is the grouper's own pass: one grouping of the project this
// composition works in. It does nothing where no report store is composed,
// which is every subcommand but the process that serves the entrance.
func (p *path) groupReports(ctx context.Context) (bool, error) {
	if p.grouper == nil {
		return false, nil
	}
	did, err := p.grouper.Pass(ctx, p.projectID)
	for _, raised := range did.Raised {
		fmt.Fprintf(p.d.out, "The grouper raised %s from reports\n", raised)
	}
	if did.Linked > 0 {
		fmt.Fprintf(p.d.out, "The grouper marked %d report(s) with the intent they were grouped into, and %d page(s) went out for a report marking harm\n",
			did.Linked, did.Paged)
	}
	if did.Moved > 0 {
		fmt.Fprintf(p.d.out, "The grouper moved %d report(s) into another group, which is a group it got wrong being split\n",
			did.Moved)
	}
	for _, dropped := range did.Dropped {
		fmt.Fprintf(p.d.out, "%s was left holding no report by that split and is ended\n", dropped)
	}
	if did.LeftUngrouped > 0 {
		fmt.Fprintf(p.d.out, "%d report(s) were left in no group and stay ungrouped\n", did.LeftUngrouped)
	}
	return did.Wrote(), err
}

// itemsOfAnIntent is [grouper.Decompositions]: whether an intent has been
// decomposed into the items that answer it, which is the boundary a split stops
// at. It is read off the item record, which the pass does not import — what the
// pass is given is a project, and the records it writes through are the intent
// and the report.
type itemsOfAnIntent struct{ p *path }

// Decomposed reports whether any item answers that intent.
func (i itemsOfAnIntent) Decomposed(ctx context.Context, intentID string) (bool, error) {
	items, err := item.ForIntent(ctx, i.p.d.pool, intentID)
	if err != nil {
		return false, err
	}
	return len(items) > 0, nil
}

// groupedReports is [score.GroupedReports]: how many reports are grouped into
// an item's intent, and the ids of the ones marking harm. The score cannot
// read the report store — it is a second database and no record of the graph
// carries either — so the composition answers, through the count the store
// keeps and the ids it names without the words.
//
// A composition with no report store answers nothing grouped, which is every
// subcommand that makes one pass: no report reached it, so none of them
// grouped anything.
type groupedReports struct{ p *path }

// Grouped is the group of reports grouped into intentID: how many, and the
// ids of the ones marking harm.
func (g groupedReports) Grouped(ctx context.Context, intentID string) (score.ReportGroup, error) {
	store := g.p.d.reports
	if store == nil || intentID == "" {
		return score.ReportGroup{}, nil
	}
	group, err := store.Grouped(ctx, intentID)
	if err != nil {
		return score.ReportGroup{}, err
	}
	marked, err := store.MarkedIn(ctx, intentID)
	if err != nil {
		return score.ReportGroup{}, err
	}
	return score.ReportGroup{Reports: group.Reports, Marked: marked}, nil
}

// newGrouper is the pass over the report store this composition opened, and nil
// where it opened none. Where the entrance is this same composition's — the
// channel [openReportStore] made and [theChannel] held onto — it is wired the
// hand-off a report's own Submit makes at once, so arrival is what triggers a
// pass and the interval [passGrouper] runs on is left as the catch-up a
// restart needs.
func newGrouper(p *path) (*grouper.Grouper, error) {
	if p.d.reports == nil {
		return nil, nil
	}
	g, err := grouper.New(grouper.Composition{
		Pool:           p.d.pool,
		Reports:        p.d.reports,
		Intake:         p.intake,
		Notifier:       p.notifier,
		Fleet:          groupsThroughTheFleet{p: p},
		Services:       servicesInAProject{p: p},
		Decompositions: itemsOfAnIntent{p: p},
		// The intent is intake's own write whichever source raised it, so the
		// actor is intake's; the read of a report's words is made as the pass
		// that made it, which ../../../end-goal/components.md gives no row
		// because the grouper is an agent and not a component.
		Actor:     intakeActor,
		ReadsAs:   grouperPrincipal,
		Admission: admissionSafeguards{p: p},
	})
	if err != nil {
		return nil, err
	}
	if theChannel != nil {
		// p.groupReports reads p.grouper, which compose sets from this call's
		// own return value right after it returns — a moment after this line
		// runs and well before an entrance the channel serves could reach it.
		theChannel.wireGrouper(p.groupReports)
	}
	return g, nil
}
