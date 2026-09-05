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
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/driftdetector"
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

// The three things the health monitor and the notifier are handed, all three
// substrate: where the quantity comes from, what performs a rollback, and where a
// delivery goes. Each is an interface in the package that needs it, so neither the
// health monitor nor the notifier knows it is running against a local process and a
// terminal.

// signalFiles reads the quantity out of the file each deployed process emits into.
// One file per build in the directory that build ran in, which is what makes a
// release's own counts tellable from the counts of the build that ran there before
// it — package localtarget names the file, being the thing that told the process
// where to write it.
//
// A build that emitted nothing reads as no units, which the boundary treats as a
// window with nothing to say yet. That is right for a release just deployed and it
// is also what an implementation that ignored the instruction to emit looks like, and
// nothing here can tell the two apart.
type signalFiles struct{ dir string }

func (s signalFiles) Read(_ context.Context, q healthmonitor.Quantity) (boundary.Observed, error) {
	units, failures, err := countSignal(localtarget.SignalFile(s.dir, q.BuildID))
	if err != nil {
		return boundary.Observed{}, err
	}
	observed := boundary.Observed{Units: units, Failures: failures}
	if q.BaselineBuildID == "" {
		return observed, nil
	}
	observed.BaselineUnits, observed.BaselineFailures, err = countSignal(localtarget.SignalFile(s.dir, q.BaselineBuildID))
	return observed, err
}

// countSignal is the lines one build emitted and how many of them were not "ok". A
// file that is not there is nothing emitted rather than an error: a build deployed a
// moment ago has emitted nothing yet, and so has one that was never started.
//
// Any line that is not exactly "ok" counts as a failure, rather than only the word
// the instruction names. That is the lenient direction and it is the safe one here:
// a program emitting something the factory cannot read is not a program the factory
// should read as healthy.
func countSignal(path string) (int64, int64, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, 0, nil
	} else if err != nil {
		return 0, 0, fmt.Errorf("factory: reading the quantity at %s: %w", path, err)
	}
	var units, failures int64
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		units++
		if line != "ok" {
			failures++
		}
	}
	return units, failures, nil
}

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

// RollBack is [healthmonitor.Rollbacker]: the deploy agent performing the slow
// rollback the health monitor called for. It is the deploy agent's because reaching a
// deploy target is, and the health monitor reaches none.
//
// The target's build is already in production's directory, put there by the deploy
// that shipped it and never removed — so restoring it is a deploy of a binary that is
// still on disk, and there is nothing to rebuild. What that costs is that a directory
// pruned between the deploy and the rollback would leave the rollback with nothing to
// put back, which nothing here prunes.
func (p *path) RollBack(ctx context.Context, r healthmonitor.Rollback) error {
	dep, err := deploy.Restore(ctx, p.deploys, p.d.targets.at(p.d.dir), deployActor,
		r.ServiceID, r.ServiceName, r.EnvironmentID,
		deploy.OfRelease(r.ToReleaseID, r.ToBuildID),
		deploy.Undoing{
			FailedReleaseID: r.FailedReleaseID,
			SweptReleaseIDs: r.SweptReleaseIDs,
			Source:          r.Source,
			RevertIntentID:  r.RevertIntentID,
		}, p.d.credential)
	if err != nil {
		return err
	}
	fmt.Fprintf(p.d.out, "Rollback %s complete: build %s of release %s is back on the target\n",
		dep.ID, r.ToBuildID, r.ToReleaseID)
	fmt.Fprintf(p.d.out, "  it failed release %s and swept %d above it; source: %s\n",
		r.FailedReleaseID, len(r.SweptReleaseIDs), r.Source)
	fmt.Fprintf(p.d.out, "  the revert it raised is intent %s, and every production deploy of this service holds until that ships\n",
		r.RevertIntentID)
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

	readings, err := p.healthMonitor.Watch(ctx, w)
	for _, reading := range readings {
		p.reportReading(reading)
	}
	if err != nil {
		return err
	}

	after, found, err := p.healthMonitor.AfterWindow(ctx, w)
	if err != nil {
		return err
	}
	if found && after.Boundary.Harm {
		p.reportReading(after)
	}

	resolved, err := p.healthMonitor.ResolveSettled(ctx, w)
	if err != nil {
		return err
	}
	for _, i := range resolved {
		fmt.Fprintf(p.d.out, "Incident %s resolved: the crossing has stopped against what runs and what it raised has shipped\n", i.ID)
	}
	return p.driftDetectorPages(ctx, svc)
}

// reportReading prints one reading as an owner would read it: the numbers the
// verdict came from, the exit where there was one, and what followed.
func (p *path) reportReading(r healthmonitor.Reading) {
	out := p.d.out
	where := "the window over release " + fmt.Sprint(r.Release.Number)
	if r.Window.ID == "" {
		where = "release " + fmt.Sprint(r.Release.Number) + ", whose window has closed"
	}
	fmt.Fprintf(out, "The health monitor read %s: %d unit(s), %d failed (rate %.4f)\n",
		where, r.Observed.Units, r.Observed.Failures, r.Boundary.Rate)
	if r.HasBaseline {
		fmt.Fprintf(out, "  against release %d: %d unit(s), %d failed — baseline rate %.4f, alternative %.4f\n",
			r.Baseline.Number, r.Observed.BaselineUnits, r.Observed.BaselineFailures,
			r.Boundary.BaselineRate, r.Boundary.Alternative)
	}
	if r.Boundary.Unavailable != "" {
		fmt.Fprintf(out, "  neither exit is reachable: %s\n", r.Boundary.Unavailable)
	} else {
		fmt.Fprintf(out, "  log ratio %.3f against a crossing of %.3f in either direction (size %v, confidence %v, %s)\n",
			r.Boundary.Log, r.Boundary.Crossing, r.Window.Size, r.Window.Confidence, r.Window.Formula)
	}
	switch r.Exit {
	case window.ExitPassed:
		fmt.Fprintln(out, "  clean: a regression of the size worth catching is ruled out, and the window closed early on evidence")
	case window.ExitTimedOut:
		fmt.Fprintln(out, "  cap: neither exit was reached in the time allowed, so the window closed unresolved — weak protection, reported as weak")
	case window.ExitFailed:
		fmt.Fprintf(out, "  harm: the release is failed, incident %s raised, revert intent %s taken in\n",
			r.IncidentID, r.RaisedIntentID)
	}
	if r.WhyNoRollback != "" {
		fmt.Fprintf(out, "  nothing was rolled back: %s\n", r.WhyNoRollback)
	}
	if r.Exit == "" && r.IncidentID != "" {
		fmt.Fprintf(out, "  a crossing after the window closed: incident %s, and intent %s at the start of the pipeline\n",
			r.IncidentID, r.RaisedIntentID)
	}
}

// driftDetectorPages is the notifier reading the drift detector's own store,
// which is the one wait nothing calls the notifier about: that store writes
// into nothing of the factory's and calls nothing, so both ends of its page are
// read rather than told.
//
// A mismatch nobody has been reached about is paged. One still uncleared on a later
// pass widens, once, to the owner. One a human has passed is answered — here, at the
// pass that finds it passed, because clearing it happened where nothing calls.
func (p *path) driftDetectorPages(ctx context.Context, svc service.Service) error {
	if p.d.driftdetector == nil || p.notifier == nil {
		return nil
	}
	all, err := driftdetector.All(ctx, p.d.driftdetector)
	if err != nil {
		return err
	}
	for _, m := range all {
		if m.ServiceID != svc.ID {
			continue
		}
		w := notifier.Wait{
			Row:     m.ID,
			Kind:    notifier.KindDriftMismatch,
			Waiting: m.Why(),
			Holding: people.OfObligation(people.ObligationDriftDetector),
			Worse:   true,
		}
		events, err := p.notifier.EventsFor(ctx, m.ID)
		if err != nil {
			return err
		}
		var reached, widened, answered bool
		for _, e := range events {
			switch notifier.Event(e.Event) {
			case notifier.EventReached:
				reached = true
			case notifier.EventWidened:
				widened = true
			case notifier.EventAnswered:
				answered = true
			}
		}

		switch {
		case !reached:
			if _, err := p.notifier.Notify(ctx, w); err != nil {
				return err
			}
		case m.Cleared() && !answered:
			if _, err := p.notifier.Answered(ctx, w, m.ClearedBy); err != nil {
				return err
			}
		case !m.Cleared() && !widened:
			if _, err := p.notifier.Widen(ctx, w); err != nil {
				return err
			}
		}
	}
	return nil
}

// escalated is the page a stage out of attempts fires, and the one place the
// page's condition is read off a record rather than settled by the kind of wait. An
// item whose intent an owner typed has nothing live that is worse, so the factory
// giving up on it waits in Work; one whose intent a detector wrote or end-user
// reports were grouped into is a defect that is live, so giving up on it is
// production staying worse until a human takes it over.
//
// That is the whole of the test, and it is not which of intake's three sources the
// intent came from — it is whether something live is worse, which the source is
// evidence about.
func (p *path) escalated(ctx context.Context, intentID, itemID, why string) error {
	if p.notifier == nil {
		return nil
	}
	in, err := intent.Get(ctx, p.d.pool, intentID)
	if err != nil {
		return err
	}
	row, kind := intentID, notifier.KindIntentEscalated
	if itemID != "" {
		row, kind = itemID, notifier.KindItemEscalated
	}
	_, err = p.notifier.Notify(ctx, notifier.Wait{
		Row:     row,
		Kind:    kind,
		Waiting: fmt.Sprintf("the factory gave up on %s: %s (its intent came from %s)", row, why, in.Source),
		Holding: people.OfDuty(takeOverIssues),
		Worse:   liveIsWorse(in.Source),
	})
	return err
}

// takeOverIssues is duty 12 — taking over issues the factory cannot fix on its
// own — which is the duty an escalation belongs to and so the duty a page about one
// routes by. The number is what people holds and the design cites; the words are in
// what-humans-do.md and are not copied here.
const takeOverIssues = people.Duty(12)

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

	opened, err := p.fireProduction(ctx, c)
	if err != nil {
		return err
	}
	report(p.d.out, opened, c.criteria)
	fmt.Fprintf(p.d.out, "  approving through accepts what the hold was preventing: %s\n", held)

	closing, err := p.gate.Decide(ctx, opened, p.human, verdict, reason)
	if err != nil {
		return err
	}
	c.deployGate = recordFiring(opened, closing)
	if verdict != gate.VerdictApprove {
		c.held = true
		fmt.Fprintf(p.d.out, "The verdict is %s; the hold stands and nothing is deployed\n", verdict)
		return nil
	}
	return p.putOnProduction(ctx, c)
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
	return readExchange(localtarget.ExchangeFile(env.Targets[0], c.BuildID))
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
	raised, err := p.contracts.RaiseRemovals(ctx)
	if err != nil {
		return err
	}
	for _, r := range raised {
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
