package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/contractcheck"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/window"
)

// The watch: the health monitor read until every window it holds open has
// closed, and everything downstream of a deploy beside it. What the health
// monitor reads is emission.go, what performs a rollback is rollback.go, and
// where a delivery goes is the terminal below — each an interface in the
// package that needs it, so neither the health monitor nor the notifier knows
// it is running against a local process and a terminal.

// terminal is where a delivery goes on an install with nothing to deliver to: all
// three channels to the run's own output, told apart by the channel they went out
// on. The page is still the only one that writes a record, which is the difference
// that matters and is not this type's to make.
//
// One fault here stops all three channels at once and the factory falls back to
// whoever remembers to look, which is the cost the design states for having one
// notifier and is not a cost of this implementation.
type terminal struct{ out io.Writer }

func (t terminal) Deliver(_ context.Context, d notifier.Delivery) error {
	if d.Event == "" {
		fmt.Fprintf(t.out, "%s to %s: %s\n", d.Channel, d.To, d.Wait.Waiting)
		return nil
	}
	fmt.Fprintf(t.out, "PAGE %s to %s: %s\n", d.Event, d.To, d.Wait.Waiting)
	return nil
}

// watchTo runs the health monitor over one service until every window it holds
// open has closed, or until the deadline. Nothing but the health monitor closes
// a window, so a run that gives up leaves them open — which fills the window
// limit and holds the next deploy, and is what `factory watch` is for.
func (p *path) watchTo(ctx context.Context, svc service.Service, deadline time.Time, every time.Duration) error {
	for {
		if err := p.watchPass(ctx, svc); err != nil {
			return err
		}
		open, err := window.CountOpen(ctx, p.d.pool, svc.ID)
		if err != nil {
			return err
		}
		if open == 0 {
			return nil
		}
		if !time.Now().Add(every).Before(deadline) {
			fmt.Fprintf(p.d.out, "%d analysis window(s) are still open on %s; `factory watch %s` continues from here\n",
				open, svc.Name, svc.Name)
			fmt.Fprintln(p.d.out, "  a window nothing closes reaches the window limit and holds this service's production deploys — a wait on the factory, which does not page")
			return nil
		}
		time.Sleep(every)
	}
}

// watchPass is one evaluation of everything downstream of a deploy on one service:
// every open window, the release whose window has closed, the incidents that have
// settled, and the drift detector's own store.
func (p *path) watchPass(ctx context.Context, svc service.Service) error {
	w := healthmonitor.Watching{ID: svc.ID, Name: svc.Name, EnvironmentID: p.production.ID}

	watched, err := p.healthMonitor.Watch(ctx, w)
	for _, one := range watched {
		p.reportWatched(one)
	}
	if err != nil {
		return err
	}

	after, found, err := p.healthMonitor.AfterWindow(ctx, w)
	if err != nil {
		return err
	}
	if found && after.Crossed {
		p.reportAfter(after)
	}

	resolved, err := p.healthMonitor.ResolveSettled(ctx, w)
	if err != nil {
		return err
	}
	for _, i := range resolved {
		fmt.Fprintf(p.d.out, "Incident %s resolved: the crossing has stopped against what runs and what it raised has shipped\n", i.ID)
	}
	if err := p.pagesHeldToTheHours(ctx); err != nil {
		return err
	}
	return p.driftDetectorPages(ctx)
}

// pagesHeldToTheHours is the notifier's own pass over the pages a service's
// authored paging hours held back, which go out at the next hour that service
// allows. It runs on every watch pass, that being the pass this interface makes
// while anything is waiting.
//
// The drift detector's store goes with it, nil where none is installed: a
// mismatch cleared there is the one wait that ends where nothing calls, and the
// pass reads it so that the hours coming round do not page about one a human
// has already cleared.
func (p *path) pagesHeldToTheHours(ctx context.Context) error {
	if p.notifier == nil {
		return nil
	}
	paged, err := p.notifier.PageDeferred(ctx, p.d.driftdetector)
	if err != nil {
		return err
	}
	for _, row := range paged {
		fmt.Fprintf(p.d.out, "Page about %s went out: the hours its service pages within have come round\n", row)
	}
	return nil
}

// reportWatched prints one window's reading as an owner would read it: the
// counts the verdict came from per quantity, the boundary the first crossing was
// read against, the exit where the window reached one, and what followed.
//
// Every number the exit was reached from is printed whether or not anything
// crossed, because a window that closed on evidence nobody can recompute is one
// nobody can argue with.
func (p *path) reportWatched(one healthmonitor.Watched) {
	out := p.d.out
	fmt.Fprintf(out, "The health monitor read window %s over release %d\n", one.Window.ID, one.Release.Number)
	if one.HasBaseline {
		fmt.Fprintf(out, "  against release %d, whose build the other arm runs\n", one.Baseline.Number)
	} else {
		fmt.Fprintf(out, "  against nothing: %s\n", boundary.NoBaseline)
	}
	for quantity, counts := range one.Evaluated.Read.Quantities {
		fmt.Fprintf(out, "  %s: %d unit(s), %d counted; baseline %d unit(s), %d counted\n",
			quantity, counts.Units, counts.Count, counts.BaselineUnits, counts.BaselineCount)
	}
	if !one.Evaluated.Volume {
		fmt.Fprintln(out, "  no series was read on both arms, so this window says nothing about the release yet")
	}
	if crossed := one.Evaluated.Crossed; crossed != nil {
		fmt.Fprintf(out, "  the %s reading crossed on %s of %s: log ratio %.3f against a crossing of %.3f (size %v, confidence %v)\n",
			crossed.Kind, crossed.Quantity, crossed.Operation,
			crossed.Reading.Log, crossed.Reading.Crossing, crossed.Boundary.Size, crossed.Boundary.Confidence)
	}
	if one.ControlCrossing != nil {
		fmt.Fprintln(out, "  the control is itself failing against the service's own history, so the passed exit is not available while it stands")
	}
	switch one.Exit {
	case window.ExitPassed:
		fmt.Fprintln(out, "  clean: a regression of the size worth catching is ruled out, and the window closed early on evidence")
	case window.ExitTimedOut:
		fmt.Fprintln(out, "  cap: neither exit was reached in the time allowed, so the window closed unresolved — weak protection, reported as weak")
	case window.ExitFailed:
		fmt.Fprintf(out, "  harm: the release is failed, incident %s raised, revert intent %s taken in\n",
			one.IncidentID, one.RaisedIntentID)
		if one.Rolled != nil {
			fmt.Fprintf(out, "  rolled back to release %d, skipping %d above the failed one\n",
				one.Target.Number, len(one.Rolled.SkippedReleaseIDs))
		}
		for _, id := range one.SkippedWindows {
			fmt.Fprintf(out, "  window %s closed skipped: its release is no longer running\n", id)
		}
	}
	if one.WhyNoRollback != "" {
		fmt.Fprintf(out, "  nothing was rolled back: %s\n", one.WhyNoRollback)
	}
}

// reportAfter prints the reading the health monitor keeps making once the window
// has closed: the release read against the service's own recent history, and the
// intent a crossing there raises instead of a rollback.
func (p *path) reportAfter(after healthmonitor.AfterReading) {
	fmt.Fprintf(p.d.out, "A crossing after the window over release %d closed: incident %s, and intent %s at the start of the pipeline\n",
		after.Release.Number, after.IncidentID, after.RaisedIntentID)
	fmt.Fprintf(p.d.out, "  nothing was rolled back: %s\n", after.WhyNoRollback)
}

// driftDetectorPages is the notifier's own pass over the drift detector's
// store: the mismatches it found, the detector's per-target last check, and the
// deliveries the detector recorded for itself while the factory was not running.
// All three are reads and not calls, because that store writes into nothing of
// the factory's and calls nothing.
//
// The pass is the notifier's and not this interface's: it holds the routing, the
// page events and the delivery record, and a copy of the read here would be a
// second place deciding when a mismatch widens.
func (p *path) driftDetectorPages(ctx context.Context) error {
	if p.d.driftdetector == nil || p.notifier == nil {
		return nil
	}
	if err := p.notifier.SweepDriftDetector(ctx, p.d.driftdetector); err != nil {
		return err
	}
	if err := p.notifier.SweepDriftDetectorStale(ctx, p.d.driftdetector); err != nil {
		return err
	}
	if err := p.notifier.CatchUpDriftDetectorDelivery(ctx, p.d.driftdetector); err != nil {
		return err
	}
	return p.notifier.RecordOwnLastCheck(ctx, atLeastASecond(p.d.watchEvery))
}

// takeOverIssues is duty 12 — taking over issues the factory cannot fix on its
// own — which is the duty an escalation belongs to and so the duty a page about one
// routes by. The number is what people holds and the design cites; the words are in
// what-humans-do.md and are not copied here.
const takeOverIssues = people.Duty(12)

// answerTheInterview is duty 3 — answering the factory's interview for as many
// rounds as it asks — which is the duty a round of questions belongs to and so
// the duty the wait intake leaves routes by. The number is what people holds
// and the design cites; the words are in what-humans-do.md and are not copied
// here.
const answerTheInterview = people.Duty(3)

// liveIsWorse is whether something live is worse for the factory having given up.
// An owner's request is a feature nobody is running; a detector's intent and an
// intent grouped from end-user reports are both defects in software that is live.
func liveIsWorse(source intent.Source) bool { return source != intent.SourceOwner }

// approveThrough is a human approving through a factory hold at the production
// deploy row, which is the emergency action the design keeps there: approve now, not
// skip. The row fires with the hold on its open event and the human decides.
//
// It exists because four of the five holds the factory sets lift themselves, so the
// path waits them out rather than firing a row — and a hold nobody can approve
// through would be a hold the design does not have. What approving through the hold a
// rollback leaves redelivers is the defect that was just removed, which is the most
// damaging thing in the factory to approve through and the one most likely to be
// tried during an incident.
func (p *path) approveThrough(ctx context.Context, itemID string, verdict gate.Verdict, reason string) error {
	c, err := p.candidateFor(ctx, itemID)
	if err != nil {
		return err
	}
	if c.releaseID == "" {
		rel, minted, err := release.ForItem(ctx, p.d.pool, itemID)
		if err != nil {
			return err
		}
		if !minted {
			return fmt.Errorf("factory: item %s has no release, so there is no production deploy to approve through", itemID)
		}
		c.releaseID, c.releaseNumber, c.reverifiedBuildID = rel.ID, rel.Number, rel.BuildID
	}
	it, err := item.Get(ctx, p.d.pool, itemID)
	if err != nil {
		return err
	}
	held, err := p.factoryHolds(ctx, c.svc, it)
	if err != nil {
		return err
	}
	if held == "" {
		return fmt.Errorf("factory: nothing holds the production deploy of item %s, so there is nothing to approve through", itemID)
	}
	fmt.Fprintf(p.d.out, "The factory holds the production deploy of item %s: %s\n", itemID, held)

	opened, _, err := p.fireProduction(ctx, c)
	if err != nil {
		return err
	}
	report(p.d.out, opened, c.criteria)
	fmt.Fprintf(p.d.out, "  approving through accepts what the hold was preventing: %s\n", held)

	// The approve names the hold it is going through, which is the whole of what
	// approving through one is: a bare approve while a hold stands is refused.
	given := gate.Given{Actor: p.human, Verdict: verdict, Reason: reason}
	if verdict == gate.VerdictApprove {
		given.Holds = opened.Holds
	}
	closing, err := p.gate.Decide(ctx, opened, given)
	if err != nil {
		return err
	}
	c.deployGate = recordFiring(opened, closing)
	if verdict != gate.VerdictApprove {
		c.held = true
		fmt.Fprintf(p.d.out, "The verdict is %s; the hold stands and nothing is deployed\n", verdict)
		return nil
	}
	return p.putOnProduction(ctx, c, opened.Strategy)
}

// exchangeFiles is [contractcheck.Exchanges]: the documents one build wrote on the
// environment it ran on. One file per build in that environment's own directory,
// which package localtarget names, being the thing that told the process where to
// write it.
//
// A build that wrote nothing reads as no documents, which enforcement treats as a
// failure wherever there is a consumer contract to decide — a producer that emitted
// nothing has not shown that a consumer's assumption holds. That is right for a
// build that ignored the instruction to write them and it is also what a build with
// no run behind it looks like, and nothing here can tell the two apart.
func (p *path) Observed(ctx context.Context, c contractcheck.Candidate) ([]consumercontract.Document, error) {
	env, err := environment.Get(ctx, p.d.pool, c.EnvironmentID)
	if err != nil {
		return nil, err
	}
	if len(env.Targets) == 0 {
		return nil, fmt.Errorf("factory: environment %s names no target to read an exchange from", c.EnvironmentID)
	}
	return readExchange(localtarget.ExchangeFile(env.Targets[0].Address, c.BuildID))
}

// readExchange is the documents one file holds, one JSON object per line. A file
// that is not there is nothing written rather than an error: a build deployed a
// moment ago has written nothing yet, and so has one that was never started.
//
// A line that is not a JSON object is skipped and counted as nothing. It is the
// lenient direction and it is the safe one here: a document the factory cannot read
// shows nothing, and showing nothing is what fails a consumer contract rather than
// passing it.
func readExchange(path string) ([]consumercontract.Document, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("factory: reading the exchange at %s: %w", path, err)
	}
	var documents []consumercontract.Document
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var one consumercontract.Document
		if err := json.Unmarshal([]byte(line), &one); err != nil {
			continue
		}
		documents = append(documents, one)
	}
	return documents, nil
}

// raiseRemovals is the detector: one pass over every deprecation-marked element,
// taking a removal intent in for each whose derived consumer contracts are gone.
// Nobody has to remember step three of a migration.
//
// It reports what it found either way, because a marked element with a list that
// has not emptied is the mechanism working and an owner reading a run should see it.
// An element whose brownout ran and established nothing is reported too: no pass
// of the detector can raise its removal, so an owner reading the run is who
// learns that raising it is theirs.
func (p *path) raiseRemovals(ctx context.Context) error {
	marked, err := p.contracts.Deprecated(ctx)
	if err != nil {
		return err
	}
	if len(marked) == 0 {
		return nil
	}
	for _, m := range marked {
		if m.Empty() && len(m.Safeguards) == 0 {
			continue
		}
		fmt.Fprintf(p.d.out, "%s.%s is marked deprecated and the list still names: %v",
			m.Contract.Name, m.Element.Name, m.Consumers())
		for _, s := range m.Safeguards {
			fmt.Fprintf(p.d.out, " and safeguard %s", s.SafeguardID)
		}
		fmt.Fprintln(p.d.out)
	}
	raised, err := p.contracts.Raise(ctx)
	if err != nil {
		return err
	}
	for _, r := range raised {
		if r.Stall() {
			fmt.Fprintln(p.d.out, r.Stalled)
			continue
		}
		if !r.New {
			fmt.Fprintf(p.d.out, "The removal of %s.%s is already asked for by intent %s; the detector takes nothing in\n",
				r.Marked.Contract.Name, r.Marked.Element.Name, r.Intent.ID)
			continue
		}
		fmt.Fprintf(p.d.out, "The list on %s.%s has emptied; intent %s taken in by the detector: %s\n",
			r.Marked.Contract.Name, r.Marked.Element.Name, r.Intent.ID, r.Intent.Statement)
	}
	return nil
}
