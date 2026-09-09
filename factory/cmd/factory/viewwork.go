package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/constraint"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/screens"
	"github.com/dulguun0225/borg/factory/window"
)

// Work's own addresses: one item's timeline, one decision, and a constraint
// that arrived with a single intent, which is read at Work on its intent.

// itemVersions is the four kinds a timeline shows, in the order the path
// authors them.
var itemVersions = []artifact.Kind{
	artifact.KindSpec, artifact.KindImplementationPlan,
	artifact.KindTasks, artifact.KindImplementation,
}

// Item is one item's timeline in the order intent, spec, plan, tasks,
// implementation, rollout and the release it ends in happened, with each gate
// shown inline at the point where it fired.
func (v *views) Item(ctx context.Context, who principal.Principal, id string) (screens.Item, error) {
	it, err := item.Get(ctx, v.p.d.pool, id)
	if err != nil {
		return screens.Item{}, fmt.Errorf("%w: %s", screens.ErrNotFound, id)
	}
	view := screens.Item{ID: it.ID}

	if it.IntentID != "" {
		in, err := intent.Get(ctx, v.p.d.pool, it.IntentID)
		if err != nil {
			return screens.Item{}, err
		}
		view.IntentID, view.IntentStatement = in.ID, in.Statement
		if in.Source == intent.SourceReports {
			if view.Reports, err = v.reportsUnder(ctx, who, in.ID); err != nil {
				return screens.Item{}, err
			}
			if view.IntentAwaitsAdmission, err = v.intentAwaitsAdmission(ctx, in); err != nil {
				return screens.Item{}, err
			}
		}
		_, of, addresses, err := v.p.liveItems(ctx, in.ID)
		if err != nil {
			return screens.Item{}, err
		}
		if of > 1 {
			if view.PartlyDelivered, err = item.PartlyDelivered(ctx, v.p.d.pool, in.ID,
				v.p.production.ID, addresses); err != nil {
				return screens.Item{}, err
			}
		}
	}

	for _, kind := range itemVersions {
		version, found, err := artifact.NewestOfKind(ctx, v.p.d.pool, it.ID, kind)
		if err != nil {
			return screens.Item{}, err
		}
		if !found {
			continue
		}
		view.Versions = append(view.Versions, screens.ArtifactVersion{
			ID: version.ID, Kind: string(version.Kind), AuthoredAt: version.At,
		})
	}

	decisions, err := v.decisionsOn(ctx, who, it.ID)
	if err != nil {
		return screens.Item{}, err
	}
	view.Decisions = decisions

	if view.ImplementationDiff, err = v.implementationDiff(ctx, it); err != nil {
		return screens.Item{}, err
	}

	rel, minted, err := release.ForItem(ctx, v.p.d.pool, it.ID)
	if err != nil {
		return screens.Item{}, err
	}
	if minted {
		view.Release = &screens.ReleaseSummary{ServiceID: rel.ServiceID, Number: rel.Number}
		deploys, err := deploy.ByRelease(ctx, v.p.d.pool, v.p.production.ID, rel.ID)
		if err != nil {
			return screens.Item{}, err
		}
		for _, one := range deploys {
			targets, err := deploy.Targets(ctx, v.p.d.pool, one.ID)
			if err != nil {
				return screens.Item{}, err
			}
			for _, target := range targets {
				view.Deploys = append(view.Deploys, screens.DeploySummary{
					EnvironmentID: one.EnvironmentID, TargetID: target.Address,
					CompletedAt: target.CompleteAt,
				})
			}
		}
		if w, found, err := window.ForRelease(ctx, v.p.d.pool, rel.ID); err != nil {
			return screens.Item{}, err
		} else if found {
			view.Windows = append(view.Windows, screens.WindowSummary{ID: w.ID, Exit: string(w.Exit)})
		}
	}

	held, _, err := v.p.dispatch.Open(ctx)
	if err != nil {
		return screens.Item{}, err
	}
	view.Stop = stopOnItem(held, it.ID)
	if view.FactoryHold, err = v.holdAtTheProductionDeploy(ctx, it, view); err != nil {
		return screens.Item{}, err
	}
	view.ApprovableThrough = view.FactoryHold != ""
	return view, nil
}

// reportsUnder is the reports grouped into one intent, which Work renders
// under that intent's own entry and nowhere else. A composition with no
// report store renders none: the store's URL has no default, so a subcommand
// that makes one pass has no store to read them from.
//
// The words are taken one report at a time through the store's own read,
// which appends a read event naming the principal first — what makes who had
// already read them answerable after a redaction. The enumeration answers
// with ids for the same reason, so a list of the group serves no words.
func (v *views) reportsUnder(ctx context.Context, who principal.Principal,
	intentID string) ([]screens.ReportSummary, error) {
	store := v.p.d.reports
	if store == nil {
		return nil, nil
	}
	grouped, err := store.ByIntent(ctx, intentID)
	if err != nil {
		return nil, err
	}
	under := make([]screens.ReportSummary, 0, len(grouped))
	for _, id := range grouped {
		one, err := store.Get(ctx, who, id)
		if err != nil {
			return nil, err
		}
		under = append(under, screens.ReportSummary{
			ID: one.ID, Kind: string(one.Kind), HarmMarked: one.HarmMarked,
			CollectedAt: one.CollectedAt, NoticeID: one.NoticeID,
			Admitted: one.AdmittedAt != "", Text: one.Text,
		})
	}
	return under, nil
}

// holdAtTheProductionDeploy is the hold standing at one item's production
// deploy row, which is what the emergency action at that row is offered
// against. It is read only where the item has a release and no deploy of it:
// the hold stops that row being fired, so what it holds is a numbered release
// waiting to deploy, and an item that has deployed is past the row whatever a
// later reading of the same four conditions would say.
func (v *views) holdAtTheProductionDeploy(ctx context.Context, it item.Item, view screens.Item) (string, error) {
	if view.Release == nil || len(view.Deploys) > 0 {
		return "", nil
	}
	svc, err := v.p.serviceOf(ctx, it.ServiceID)
	if err != nil {
		return "", err
	}
	return v.p.factoryHoldsAsRead(ctx, svc, it)
}

// implementationDiff is the build's diff against master, which is the one place
// the product renders code. It is taken from the repository at read time with
// the same git the firing's own measurement takes it with and is stored
// nowhere: a diff kept on a record would be a second copy of a checkout able to
// disagree with it.
//
// It is empty before the implementation stage has built anything, and empty
// where git will not answer — a screen showing no diff reads as a diff not yet
// taken, and there is nothing here for a failure to make worse.
func (v *views) implementationDiff(ctx context.Context, it item.Item) (string, error) {
	newest, found, err := build.Newest(ctx, v.p.d.pool, it.ID)
	if err != nil || !found {
		return "", err
	}
	svc, err := v.p.serviceOf(ctx, it.ServiceID)
	if err != nil {
		return "", err
	}
	base, err := masterCommit(svc.Repository)
	if err != nil {
		return "", nil
	}
	if base == "" {
		base = emptyTree
	}
	diff, err := git(svc.Repository, "diff", base, newest.CommitHash)
	if err != nil {
		return "", nil
	}
	return diff, nil
}

// decisionsOn is every gate of one item shown inline on its timeline: the
// vector while the row is pending, the number beside the verdict once it is
// written, and when the row was opened in Work.
func (v *views) decisionsOn(ctx context.Context, who principal.Principal, itemID string) ([]screens.DecisionSummary, error) {
	rows, err := decisionlog.NewReader(v.p.d.pool, v.p.d.token).Read(ctx, who)
	if err != nil {
		return nil, err
	}
	closings, acknowledgements := pairings(rows)
	var summaries []screens.DecisionSummary
	for _, row := range rows {
		opened, is := openingOn(row, func(o gate.Opened) bool { return o.Subject.ItemID == itemID })
		if !is {
			continue
		}
		summary := screens.DecisionSummary{
			OpenEventID: row.ID,
			GateRow:     opened.Gate.String(),
			Vector:      vectorOf(opened),
			OpenedAt:    row.At,
		}
		for _, one := range acknowledgements[row.ID] {
			summary.Acknowledgements = append(summary.Acknowledgements,
				screens.Acknowledgement{HumanKey: one.Actor.Key, At: one.At})
		}
		if closing, closed := closings[row.ID]; closed {
			number := opened.Assessment.Number
			summary.Score = &number
			summary.Verdict = closing.Verdict
			summary.OpenedInWorkAt = closing.OpenedInWorkAt
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

// Decision is one gate's own view: the open event, the vector, the versions in
// force at the firing, who it routes to, its acknowledgements, its close or its
// abandonment, and every delivery attempted for it.
func (v *views) Decision(ctx context.Context, who principal.Principal, id string) (screens.Decision, error) {
	rows, err := decisionlog.NewReader(v.p.d.pool, v.p.d.token).Read(ctx, who)
	if err != nil {
		return screens.Decision{}, err
	}
	closings, acknowledgements := pairings(rows)
	abandonments := map[string]decisionlog.Row{}
	for _, row := range rows {
		if row.Shape == decisionlog.ShapeDecision && row.Part == decisionlog.PartAbandonment {
			abandonments[row.Closes] = row
		}
	}

	for _, row := range rows {
		if row.ID != id {
			continue
		}
		opened, err := gate.OpenedFrom(row)
		if err != nil {
			return screens.Decision{}, err
		}
		view := screens.Decision{
			OpenEventID:     row.ID,
			ItemID:          opened.Subject.ItemID,
			GateRow:         opened.Gate.String(),
			OpenedAt:        row.At,
			Vector:          vectorOf(opened),
			VersionsInForce: []string{row.PolicyVersion, row.ScoreVersion},
			RoutedToDuty:    int64(opened.WaitsOn.Duty),
			RoutedToHuman:   v.nameOf(ctx, opened.WaitsOn.Human),
		}
		number := opened.Assessment.Number
		view.Score = &number
		for _, one := range acknowledgements[row.ID] {
			view.Acknowledgements = append(view.Acknowledgements,
				screens.Acknowledgement{HumanKey: one.Actor.Key, At: one.At})
		}
		if closing, closed := closings[row.ID]; closed {
			payload, err := verdictOn(closing)
			if err != nil {
				return screens.Decision{}, err
			}
			view.OpenedInWorkAt = closing.OpenedInWorkAt
			view.Closed = &screens.DecisionClose{
				Verdict: closing.Verdict, Reason: payload.Reason,
				At: closing.At, Actor: v.nameOf(ctx, closing.Actor.Key),
			}
		}
		if abandoned, ended := abandonments[row.ID]; ended {
			view.Abandoned = &screens.DecisionAbandon{At: abandoned.At, Reason: abandoned.Reason}
		}
		deliveries, err := notifier.DeliveriesOf(ctx, v.p.d.pool, row.ID)
		if err != nil {
			return screens.Decision{}, err
		}
		for _, one := range deliveries {
			view.Deliveries = append(view.Deliveries, screens.Delivery{
				Channel:         string(one.Channel),
				Recipient:       v.nameOf(ctx, one.RecipientKey),
				Accepted:        one.TransportAccepted,
				FirstAcceptedAt: one.FirstAcceptedAt,
			})
		}
		return view, nil
	}
	return screens.Decision{}, fmt.Errorf("%w: %s", screens.ErrNotFound, id)
}

// Constraint is one constraint record's own view.
func (v *views) Constraint(ctx context.Context, _ principal.Principal, id string) (screens.Constraint, error) {
	one, err := constraint.Get(ctx, v.p.d.pool, id)
	if err != nil {
		return screens.Constraint{}, fmt.Errorf("%w: %s", screens.ErrNotFound, id)
	}
	return constraintView(one), nil
}

// constraintView maps one constraint record onto the view both Work and
// Factory render it with: Work on its intent for a constraint that arrived
// with one, Factory in the list of the permanent ones.
func constraintView(one constraint.Constraint) screens.Constraint {
	view := screens.Constraint{
		ID:          one.ID,
		Kind:        string(one.Kind),
		Reach:       reachOf(one),
		Statement:   one.Statement,
		SuppliedAt:  one.At,
		WithdrawnAt: one.WithdrawnAt,
		Replaces:    one.ReplacesID,
	}
	if one.BindsFrom != nil {
		view.BindsFrom, view.Zone = one.BindsFrom.Date, one.BindsFrom.Zone
	}
	if one.ReviewBy != nil {
		view.ReviewDate, view.Zone = one.ReviewBy.Date, one.ReviewBy.Zone
	}
	return view
}

// reachOf is the widest thing a constraint binds, as the view spells it: the
// reach and the record it names, or the reach alone where it names none.
func reachOf(one constraint.Constraint) string {
	if one.SubjectID == "" {
		return string(one.Reach)
	}
	return string(one.Reach) + ":" + one.SubjectID
}

// pairings is the close event and the acknowledgements of every open event in
// one read of the log, so a view answering over several decisions pairs them
// once rather than walking the rows again per decision.
func pairings(rows []decisionlog.Row) (map[string]decisionlog.Row, map[string][]decisionlog.Row) {
	closings := map[string]decisionlog.Row{}
	acknowledgements := map[string][]decisionlog.Row{}
	for _, row := range rows {
		if row.Shape != decisionlog.ShapeDecision {
			continue
		}
		switch row.Part {
		case decisionlog.PartClose:
			closings[row.Closes] = row
		case decisionlog.PartAcknowledgement:
			acknowledgements[row.Closes] = append(acknowledgements[row.Closes], row)
		}
	}
	return closings, acknowledgements
}

// openingOn reads one row back as a gate opening the predicate selects. A row
// this package cannot read as one is not a fault in the log — a payload is
// unconstrained bytes by decisionlog's contract — so it is passed over, the way
// every other reader of this log treats one.
func openingOn(row decisionlog.Row, of func(gate.Opened) bool) (gate.Opened, bool) {
	if row.Shape != decisionlog.ShapeDecision || row.Part != decisionlog.PartOpen {
		return gate.Opened{}, false
	}
	opened, err := gate.OpenedFrom(row)
	if err != nil || !of(opened) {
		return gate.Opened{}, false
	}
	return opened, true
}

// verdictOn is what a close event says, read out of its payload.
func verdictOn(row decisionlog.Row) (gate.ClosingPayload, error) {
	var payload gate.ClosingPayload
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		return gate.ClosingPayload{}, fmt.Errorf("factory: reading the closing payload of %s: %w", row.ID, err)
	}
	return payload, nil
}

// vectorOf is the factor vector as a screen renders it: the factor's name
// against the reading it was valued from, or against why it was resolved rather
// than valued.
func vectorOf(opened gate.Opened) map[string]string {
	vector := make(map[string]string, len(opened.Assessment.Vector))
	for _, f := range opened.Assessment.Vector {
		if f.Resolved != "" {
			vector[f.Name] = "resolved rather than valued: " + f.Resolved
			continue
		}
		vector[f.Name] = fmt.Sprintf("%.2f — %s", f.Level, f.Reading)
	}
	return vector
}
