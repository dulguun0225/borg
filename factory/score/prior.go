package score

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/window"
)

// prior reads every outcome on the author's own work. The author is whoever
// wrote the version under decision, and the prior is kept per human and per AI
// model version and not per family or per role, so two agents on one model
// version share it and a fleet entry moved to a newer version starts its
// evidence over.
//
// Which window exits are outcomes is stated exit by exit and never as closing
// without failing: passed narrows the prior and failed widens it, each being a
// comparison that ruled something out, and timed out and skipped move it in
// neither direction. Two rollbacks are excluded for reasons of their own: one a
// human marked as not caused by the release, which is evidence about something
// other than the author, and a queue rejection laid to something that moved
// under the candidate, which this package counts as no outcome at all. A
// revert's deploy delivers several releases under one window, and that window's
// close moves no prior either way.
//
// How wide the prior is follows the same distinction: it may not narrow past the
// width its own count of passed and failed closes supports, so a service too
// quiet to close a window on evidence keeps the prior it was installed with
// however many releases it has shipped. That width and that count are written on
// the vector beside the factor, with the mix of bases behind the rows: a claimed
// row is learned from as a row that says so.
func (s *Score) prior(ctx context.Context, c Change) (reading, error) {
	author, err := s.authorOf(ctx, c)
	if err != nil {
		return reading{}, err
	}
	if author == "" {
		return reading{unavailable: "the version under decision names no author, so there is no author to hold a prior on"}, nil
	}
	if s.version.PriorDrifted(author) {
		return reading{
			resolved: fmt.Sprintf("the prior on %s no longer separates the held-out releases whose windows failed from the ones whose windows passed, so it is resolved until a recalibration is in force at this gate", author),
			cause:    CauseDrifted,
			words:    author + ": the prior stands drifted",
		}, nil
	}

	authored, err := artifact.IDsByAuthor(ctx, s.pool, author)
	if err != nil {
		return reading{}, err
	}
	verdicts, err := s.humanVerdicts(ctx, func(opening OpenEvent) bool {
		return contains(authored, opening.ArtifactID)
	})
	if err != nil {
		return reading{}, err
	}

	exits, err := s.exitsOfAuthor(ctx, author)
	if err != nil {
		return reading{}, err
	}
	good := verdicts.approved + exits.passed
	bad := verdicts.rejected + exits.failed + exits.undone
	closes := exits.passed + exits.failed
	width := priorWidth(closes)
	return reading{
		level:  math.Max(evidenceLevel(good, bad), width),
		width:  width,
		closes: closes,
		words: fmt.Sprintf("%s: %d human approval(s) and %d rejection(s) on its own versions, %d release(s) whose window closed passed, %d failed, %d undone by a human, %d window(s) that ruled nothing out",
			author, verdicts.approved, verdicts.rejected, exits.passed, exits.failed, exits.undone, exits.ruledNothingOut),
		claimed:  verdicts.claimed,
		verified: verdicts.verified,
	}, nil
}

// priorWidth is how far the prior may narrow on the count of passed and failed
// closes behind it: one over the square root of one more than that count, so an
// author with no resolved close reads at the top of the scale and one with nine
// may narrow to a third of it. It is published in [Formula], and it is what says
// whether a low number was earned or waited out.
func priorWidth(closes int) float64 { return 1 / math.Sqrt(float64(closes)+1) }

// authorOf is whoever wrote the version under decision: the artifact the change
// names, or the item's newest implementation version where it names none.
func (s *Score) authorOf(ctx context.Context, c Change) (string, error) {
	if c.ArtifactID != "" {
		a, err := artifact.Get(ctx, s.pool, c.ArtifactID)
		if err != nil {
			return "", err
		}
		return a.Author, nil
	}
	implementation, found, err := artifact.NewestOfKind(ctx, s.pool, c.ItemID, artifact.KindImplementation)
	if err != nil || !found {
		return "", err
	}
	return implementation.Author, nil
}

// exits is what the windows over one author's releases closed at, counted the
// way the design counts them.
type exits struct {
	passed int
	failed int
	undone int
	// ruledNothingOut is the windows that timed out or were skipped, and the
	// ones a mark or a revert's batch excluded. They are on the vector so that a
	// reader can see what the prior did not learn from.
	ruledNothingOut int
}

// exitsOfAuthor is what became of the releases of the items this author wrote a
// version of.
//
// A release is counted once at most. A release failed by its own window is
// usually also the release a rollback undid, and counting both would be one
// outcome told twice — so an undo is counted only where the window did not
// already fail it, which is the case the design means: a human undoing something
// the health monitor did not catch.
func (s *Score) exitsOfAuthor(ctx context.Context, author string) (exits, error) {
	var counted exits
	items, err := artifact.ItemsByAuthor(ctx, s.pool, author)
	if err != nil {
		return counted, err
	}
	if len(items) == 0 {
		return counted, nil
	}

	excluded, err := s.marked(ctx)
	if err != nil {
		return counted, err
	}
	undone, err := s.undoneByAHuman(ctx)
	if err != nil {
		return counted, err
	}

	for _, itemID := range items {
		rel, released, err := release.ForItem(ctx, s.pool, itemID)
		if err != nil {
			return counted, err
		}
		if !released {
			continue
		}
		batch, err := s.deliveredInABatch(ctx, itemID)
		if err != nil {
			return counted, err
		}
		w, watched, err := window.ForRelease(ctx, s.pool, rel.ID)
		if err != nil {
			return counted, err
		}
		switch {
		case batch:
			// One comparison over several changes is a label the evidence does
			// not support, toward fault and toward trust alike.
			counted.ruledNothingOut++
		case excluded[rel.ID]:
			counted.ruledNothingOut++
		case watched && w.Exit == window.ExitFailed:
			counted.failed++
		case undone[rel.ID]:
			// The window did not fail it and a human undid it anyway, which is
			// the case the design counts: a change undone after it shipped, on a
			// release the health monitor let stand.
			counted.undone++
		case watched && w.Exit == window.ExitPassed:
			counted.passed++
		case watched:
			counted.ruledNothingOut++
		}
	}
	return counted, nil
}

// undoneByAHuman is every release a human's undo failed. A rollback names the
// source that called for it and package deploy names three of them: the health
// monitor at the analysis window's failed exit, the search at the end of one of
// its windows, and a named human at Ops. A source that is neither of the first
// two is a human's, which is what the prior counts as an undo.
//
// The releases the rollback skipped are not counted. They were never failed —
// their code is still on master and the revert redelivers them — so counting
// them would read one human's undo as an outcome on every author who merged
// while the hold stood.
func (s *Score) undoneByAHuman(ctx context.Context) (map[string]bool, error) {
	rollbacks, err := deploy.Rollbacks(ctx, s.pool)
	if err != nil {
		return nil, err
	}
	undone := map[string]bool{}
	for _, d := range rollbacks {
		if humansUndo(d) {
			undone[d.Undoing.FailedReleaseID] = true
		}
	}
	return undone, nil
}

// humansUndo is whether one rollback is a human's undo of a shipped change: it
// names a release it failed, and its source is neither of the two the factory
// calls for itself.
func humansUndo(d deploy.Deploy) bool {
	if d.Undoing.FailedReleaseID == "" {
		return false
	}
	return d.Undoing.Source != deploy.SourceHealthMonitorAtFailed && d.Undoing.Source != deploy.SourceSearch
}

// deliveredInABatch is whether this item's release was delivered under a
// revert's window. A revert is the item the health monitor raised through
// intake, so its intent is one the factory found itself, and its one deploy
// delivers every release that merged while the hold stood.
func (s *Score) deliveredInABatch(ctx context.Context, itemID string) (bool, error) {
	it, err := item.Get(ctx, s.pool, itemID)
	if err != nil {
		return false, err
	}
	if it.IntentID == "" {
		return false, nil
	}
	in, err := intent.Get(ctx, s.pool, it.IntentID)
	if err != nil {
		return false, err
	}
	return in.Source == intent.SourceDetector, nil
}

// verdicts is what the humans decided about one author's versions, with the mix
// of bases behind the rows.
type verdicts struct {
	approved int
	rejected int
	claimed  int
	verified int
}

// humanVerdicts counts the closed decisions a human gave over a subject the
// caller accepts. A hold is neither: a hold teaches the score nothing, which is
// what separates it from a reject. An auto-passed decision is not counted
// either — its close event's actor is the gate component, so the human test
// leaves it out. A rejection is counted here as evidence about the author, which
// is not the reading the risk threshold takes: that one waits for the rejection
// to resolve, and rejection.go is where it does.
func (s *Score) humanVerdicts(ctx context.Context, wanted func(OpenEvent) bool) (verdicts, error) {
	var counted verdicts
	closed, err := decisionlog.NewReader(s.pool, s.token).ClosedDecisions(ctx, componentPrincipal)
	if err != nil {
		return counted, err
	}
	for _, d := range closed {
		if d.CloseEvent.Actor.Kind != record.KindHuman {
			continue
		}
		var opening OpenEvent
		if err := json.Unmarshal([]byte(d.OpenEvent.Payload), &opening); err != nil {
			// A payload this package cannot read is a row some other component
			// wrote in a shape it does not know, which is not evidence about an
			// author and is not an error either.
			continue
		}
		if !wanted(opening) {
			continue
		}
		var closing CloseEvent
		if err := json.Unmarshal([]byte(d.CloseEvent.Payload), &closing); err != nil {
			continue
		}
		switch closing.Verdict {
		case VerdictApproved:
			counted.approved++
		case VerdictRejected:
			counted.rejected++
		default:
			continue
		}
		if d.CloseEvent.Actor.Basis == record.BasisVerified {
			counted.verified++
		} else {
			counted.claimed++
		}
	}
	return counted, nil
}

func contains(values []string, want string) bool {
	if want == "" {
		return false
	}
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
