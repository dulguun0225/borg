package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/reportstore"
	"github.com/dulguun0225/borg/factory/screens"
)

// The two admissions a safeguard on the report store adds a human with: what
// each is holding as Work renders it, and the call that ends each wait. They
// are one file because they are one subject, and they are not in viewwork.go or
// callswork.go because both of those are at the length a file is held to.
//
// What waits here is the one thing that waits on a human with no item behind it
// — an unadmitted report has been grouped into nothing and an unadmitted intent
// has been decomposed into nothing — so it is read for the home view rather
// than for the board.
//
// Both readings begin with the safeguards in force: an install where an owner
// placed neither reads no report and no intent, so the channel costs the home
// view nothing until somebody asks for the wait.

// awaiting is what the home view shows as waiting for a human's admission. It
// is read as the human who opened the screen: the reports carry a stranger's
// words, and the read event each one appends names who read them.
func (v *views) awaiting(ctx context.Context, who principal.Principal) (screens.AwaitingAdmission, error) {
	var waiting screens.AwaitingAdmission
	in, err := v.p.policy.ReportStoreAdmissions(ctx)
	if err != nil {
		return waiting, err
	}
	if in.ArrivingReports {
		if waiting.Reports, err = v.reportsAwaitingAdmission(ctx, who); err != nil {
			return waiting, err
		}
	}
	if in.ReportDerivedIntents {
		if waiting.Intents, err = v.intentsAwaitingAdmission(ctx); err != nil {
			return waiting, err
		}
	}
	return waiting, nil
}

// reportsAwaitingAdmission is every arrived report of this project that is
// grouped into nothing and that no human has admitted, with its words. A
// composition with no report store shows none: the store's URL has no default,
// so a subcommand that makes one pass has no store to read them from.
func (v *views) reportsAwaitingAdmission(ctx context.Context,
	who principal.Principal) ([]screens.ReportSummary, error) {
	store := v.p.d.reports
	if store == nil {
		return nil, nil
	}
	services, err := servicesInAProject{p: v.p}.InProject(ctx, v.p.projectID)
	if err != nil {
		return nil, err
	}
	arrived, err := store.AwaitingAdmission(ctx, who, services)
	if err != nil {
		return nil, err
	}
	waiting := make([]screens.ReportSummary, 0, len(arrived))
	for _, one := range arrived {
		waiting = append(waiting, screens.ReportSummary{
			ID: one.ID, Kind: string(one.Kind), HarmMarked: one.HarmMarked,
			CollectedAt: one.CollectedAt, NoticeID: one.NoticeID,
			Admitted: false, Text: one.Text,
		})
	}
	return waiting, nil
}

// intentsAwaitingAdmission is every intent of this project grouped from reports
// that no human has admitted. The size of each group and whether any report of
// it marks harm are read without the words: a count and a mark are facts about
// the group, so the row that says how much is waiting serves nothing anybody has
// to be answerable for having read.
func (v *views) intentsAwaitingAdmission(ctx context.Context) ([]screens.IntentAwaitingAdmission, error) {
	all, err := intent.InProject(ctx, v.p.d.pool, v.p.projectID)
	if err != nil {
		return nil, err
	}
	var waiting []screens.IntentAwaitingAdmission
	for _, one := range all {
		if one.Source != intent.SourceReports || one.AdmittedAt != "" {
			continue
		}
		row := screens.IntentAwaitingAdmission{
			IntentID: one.ID, Statement: one.Statement, ArrivedAt: one.At,
		}
		if store := v.p.d.reports; store != nil {
			group, err := store.Grouped(ctx, one.ID)
			if err != nil {
				return nil, err
			}
			row.Reports, row.HarmMarked = group.Reports, group.HarmMarked
		}
		waiting = append(waiting, row)
	}
	return waiting, nil
}

// intentAwaitsAdmission is whether one item's intent is still waiting for a
// human's admission, which is what offers the same action on the item's own
// view. It is the reading dispatch makes before it puts an agent on anything:
// the intent came from reports, no human has admitted it, and the safeguard
// that holds one is in force.
func (v *views) intentAwaitsAdmission(ctx context.Context, in intent.Intent) (bool, error) {
	if in.Source != intent.SourceReports || in.AdmittedAt != "" {
		return false, nil
	}
	admissions, err := v.p.policy.ReportStoreAdmissions(ctx)
	if err != nil {
		return false, err
	}
	return admissions.ReportDerivedIntents, nil
}

// AdmitIntent is the admission a safeguard on the report store makes a
// report-derived intent wait for: until it is written, dispatch puts no agent
// on the intent and no interview round runs. It is one action per group, the
// group already being one intent, and the write is the intent record's own —
// the reports grouped into it take the second admission, one report at a time,
// or none where that safeguard was never placed.
func (c *calls) AdmitIntent(ctx context.Context, who principal.Principal, args screens.AdmitIntentArgs) error {
	acting, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if err := c.p.intake.Admit(ctx, acting, args.IntentID); err != nil {
		if errors.Is(err, intent.ErrIntentNotFound) {
			return fmt.Errorf("%w: %v", screens.ErrNotFound, err)
		}
		if errors.Is(err, intent.ErrAdmissionNotFromReports) {
			return fmt.Errorf("%w: %v", screens.ErrRefused, err)
		}
		return err
	}
	c.changed("work", listAddressID)
	c.changed("home", listAddressID)
	return nil
}

// AdmitReport is the admission the second safeguard makes one arrived report
// wait for before the grouper reads it at all. It is one action per report:
// such a report is ungrouped and belongs to no intent, so there is no group for
// one action to cover.
func (c *calls) AdmitReport(ctx context.Context, who principal.Principal, args screens.AdmitReportArgs) error {
	if _, err := c.acting(ctx, who); err != nil {
		return err
	}
	store := c.p.d.reports
	if store == nil {
		return fmt.Errorf("%w: this composition has no report store, so no admission is recorded", screens.ErrRefused)
	}
	if err := store.Admit(ctx, args.ReportID); err != nil {
		if errors.Is(err, reportstore.ErrNotFound) {
			return fmt.Errorf("%w: %v", screens.ErrNotFound, err)
		}
		return err
	}
	c.changed("work", listAddressID)
	c.changed("home", listAddressID)
	return nil
}
