package score

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
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
func (s *Score) prior(ctx context.Context, version Version, c Change) (reading, error) {
	author, err := s.authorOf(ctx, c)
	if err != nil {
		return reading{}, err
	}
	if author == "" {
		return reading{unavailable: "the version under decision names no author, so there is no author to hold a prior on"}, nil
	}
	if version.PriorDrifted(author) {
		return reading{
			resolved: fmt.Sprintf("the prior on %s no longer separates the held-out releases whose windows failed from the ones whose windows passed, so it is resolved until a recalibration is in force at this gate", author),
			cause:    CauseDrifted,
			words:    author + ": the prior stands drifted",
		}, nil
	}
	// A restarted prior counts only what closed after the time it restarted at:
	// the width and the count of closes it may narrow on both start over from
	// there, which is the treatment an unseen author already gets.
	since, restarted := version.RestartedAt(author)

	authored, err := artifact.IDsByAuthor(ctx, s.pool, author)
	if err != nil {
		return reading{}, err
	}
	verdicts, err := s.humanVerdicts(ctx, func(opening OpenEvent) bool {
		return contains(authored, opening.ArtifactID)
	}, since)
	if err != nil {
		return reading{}, err
	}

	exits, err := s.exitsOfAuthor(ctx, author, since)
	if err != nil {
		return reading{}, err
	}
	good := verdicts.approved + exits.passed
	bad := verdicts.rejected + exits.failed + exits.undone + exits.complainedOf
	closes := exits.passed + exits.failed
	width, from := priorFloor(version, author, closes)
	if restarted {
		from = fmt.Sprintf("%s; the prior restarted at %s, so only what closed after it counts", from, since)
	}
	return reading{
		level:  math.Max(evidenceLevel(good, bad), width),
		width:  width,
		closes: closes,
		words: fmt.Sprintf("%s: %d human approval(s) and %d rejection(s) on its own versions, %d release(s) whose window closed passed, %d failed, %d undone by a human, %d later report(s) or advisories on its work, %d window(s) that ruled nothing out%s",
			author, verdicts.approved, verdicts.rejected, exits.passed, exits.failed, exits.undone,
			exits.complainedOf, exits.ruledNothingOut, from),
		claimed:  verdicts.claimed,
		verified: verdicts.verified,
	}, nil
}

// priorFloor is how wide the prior may be at this count of closes, and the words
// that say where the width came from. An author the factory has not seen starts
// at the width no closes support, or at the prior the product shipped for its
// model version where the version carries one — and narrows from there as
// evidence arrives, the width rule taking over as soon as its own closes
// support something narrower.
func priorFloor(version Version, author string, closes int) (float64, string) {
	width := priorWidth(closes)
	shipped, ok := version.ShippedPrior(author)
	if !ok || shipped >= width {
		return width, ""
	}
	return shipped, fmt.Sprintf("; the product shipped a prior of %.2f for this model version, which is where it starts", shipped)
}

// shippedPriors is the per-author prior the product ships for a model version,
// by model version, which [Writer.EnterShipped] writes onto the version it
// appends. An author whose model version is here starts there rather than at
// the width no closes support.
//
// It is empty, and that is the reading rather than a gap: a prior shipped for a
// model version is a calibration over that version's outcomes across installs,
// this factory has none, and a number invented here would narrow every unseen
// author's prior on nothing. The design admits the empty case in the same
// sentence — an author the factory has not seen starts wide, or at the prior
// the product shipped for that model version where it shipped one.
var shippedPriors = map[string]float64{}

// ShippedPriors is that table, copied, which is what a version carries.
func ShippedPriors() map[string]float64 {
	out := map[string]float64{}
	for version, level := range shippedPriors {
		out[version] = level
	}
	return out
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
	// complainedOf is the releases a later advisory or report arrived on, the
	// window having let them stand. The exposure factor learns from no outcome
	// and this is where the one outcome that speaks to it lands instead.
	complainedOf int
	// ruledNothingOut is the windows that timed out or were skipped, and the
	// ones a mark or a revert's batch excluded. They are on the vector so that a
	// reader can see what the prior did not learn from.
	ruledNothingOut int
}

// exitsOfAuthor is what became of the releases of the items this author wrote a
// version of. since is the time a restarted prior counts from, and empty where
// the prior has never restarted, in which case every release counts.
//
// A release is counted once at most. A release failed by its own window is
// usually also the release a rollback undid, and counting both would be one
// outcome told twice — so an undo is counted only where the window did not
// already fail it, which is the case the design means: a human undoing something
// the health monitor did not catch.
func (s *Score) exitsOfAuthor(ctx context.Context, author, since string) (exits, error) {
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
	complained, err := s.complainedOf(ctx)
	if err != nil {
		return counted, err
	}

	for _, itemID := range items {
		rel, released, err := release.ForItem(ctx, s.pool, itemID)
		if err != nil {
			return counted, err
		}
		if !released || (since != "" && rel.At <= since) {
			continue
		}
		w, watched, err := window.ForRelease(ctx, s.pool, rel.ID)
		if err != nil {
			return counted, err
		}
		batch, delivered, err := s.deliveredInABatch(ctx, rel.ID, w, watched)
		if err != nil {
			return counted, err
		}
		named := ""
		if batch && len(delivered) > 0 {
			if named, err = s.searchNamed(ctx, w.ServiceID, delivered); err != nil {
				return counted, err
			}
		}
		switch {
		case batch && named != "" && named == rel.ID:
			// Only the release the search settled on takes the batch's
			// failed outcome: a step's window ruling every other release of
			// the batch out on one end or the other, until one alone is left.
			counted.failed++
		case batch:
			// One comparison over several changes is a label the evidence does
			// not support, toward fault and toward trust alike. Where no
			// search ran, or it narrowed the batch no further than this, the
			// release teaches nothing on its own account — the reading the
			// design gives a batch no search named.
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
		case complained[rel.ID]:
			// A later advisory or report on this author's work. The window let
			// the release stand and something arrived about it afterwards,
			// which is an outcome on the author's own artifact like any other.
			counted.complainedOf++
		case watched && w.Exit == window.ExitPassed:
			counted.passed++
		case watched:
			counted.ruledNothingOut++
		}
	}
	return counted, nil
}

// undoneByAHuman is every release a human's undo failed. A rollback names the
// source that called for it and package deploy names two of them: the health
// monitor at the analysis window's failed exit, and a named human at Ops. A
// source that is not the first is a human's, which is what the prior counts as
// an undo.
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
// names a release it failed, and its source is not the one the factory calls
// for itself.
func humansUndo(d deploy.Deploy) bool {
	if d.Undoing.FailedReleaseID == "" {
		return false
	}
	return d.Undoing.Source != deploy.SourceHealthMonitorAtFailed
}

// deliveredInABatch is whether this release was one of several a revert's
// deploy delivered under one window, and the full batch where it was: the
// deploy that the window watched is the one that delivered it, and what says
// the comparison was over several changes is that deploy listing more than one
// release delivered — not what raised the item, which is a fact about the
// revert and not about how many changes the window compared.
//
// A release under a window of its own is not one of these however the factory
// came to deploy it: the comparison ruled something out about that release, and
// the exclusion is for a label several changes share.
func (s *Score) deliveredInABatch(ctx context.Context, releaseID string, w window.Window, watched bool) (bool, []string, error) {
	if !watched || w.DeployID == "" {
		return false, nil, nil
	}
	d, err := deploy.Get(ctx, s.pool, w.DeployID)
	if err != nil {
		// A window naming a deploy the store no longer holds says nothing about
		// how many releases it delivered or which the search settled on, and
		// reading it as one release would count a batch's close against this
		// author.
		return true, nil, nil
	}
	if !underOneWindow(releaseID, d.DeliveredReleaseIDs) {
		return false, nil, nil
	}
	return true, d.DeliveredReleaseIDs, nil
}

// underOneWindow is whether a deploy delivering these releases delivered this
// one among several. It is separate from the read of the record so that the
// rule is testable as a rule, the way every other arithmetic in this package
// is.
func underOneWindow(releaseID string, delivered []string) bool {
	return len(delivered) > 1 && contains(delivered, releaseID)
}

// searchStep is one closed search window read back off the records: the
// release its build's revert was applied onto, and whether its window failed.
// [HealthMonitor.Search] itself narrows the batch by the same two facts, read
// off the same shape of record — a search deploy's window, empty of a release,
// whose deploy lists the one release the build was made onto.
type searchStep struct {
	onto   string
	failed bool
}

// searchNamed is the release of a batch a search settled on, read off the
// closed search windows of the service: sorted by release number, narrowed
// between what a passed or timed-out step ruled out below and what a failed
// step ruled out above, the way the search's own bisection narrows. Where
// nothing narrows it to one release it answers "", which is the reading a
// search that never ran already gets.
func (s *Score) searchNamed(ctx context.Context, serviceID string, delivered []string) (string, error) {
	releases := make([]release.Release, 0, len(delivered))
	for _, id := range delivered {
		r, err := release.Get(ctx, s.pool, id)
		if err != nil {
			return "", err
		}
		releases = append(releases, r)
	}
	sort.Slice(releases, func(i, j int) bool { return releases[i].Number < releases[j].Number })
	batch := make([]string, len(releases))
	for i, r := range releases {
		batch[i] = r.ID
	}

	steps, err := s.searchSteps(ctx, serviceID)
	if err != nil {
		return "", err
	}
	return namedBySearch(batch, steps), nil
}

// searchSteps is every closed search window of the service: one named build
// and no release, whose deploy lists the one release its build's revert was
// applied onto. A step naming a release outside the batch [namedBySearch]
// narrows is left for that function to ignore, the way it ignores the first
// step of any search, which tests the revert against the release below the
// whole batch.
func (s *Score) searchSteps(ctx context.Context, serviceID string) ([]searchStep, error) {
	all, err := window.All(ctx, s.pool, serviceID)
	if err != nil {
		return nil, err
	}
	var steps []searchStep
	for _, w := range all {
		if w.ReleaseID != "" || w.Open() {
			continue
		}
		d, err := deploy.Get(ctx, s.pool, w.DeployID)
		if err != nil {
			continue
		}
		if len(d.DeliveredReleaseIDs) != 1 {
			continue
		}
		steps = append(steps, searchStep{onto: d.DeliveredReleaseIDs[0], failed: w.Exit == window.ExitFailed})
	}
	return steps, nil
}

// namedBySearch is which release of batch — sorted ascending by release
// number — the search settled on, read apart from the records the way
// [underOneWindow] is: a failed step narrows the upper bound to the release it
// tested, a passed or timed-out step narrows the lower bound past it, and the
// search has settled once exactly one release of the batch is left between
// them. A step naming a release outside the batch takes no part: the first
// step of a search tests the revert against the release below the whole
// batch, which narrows nothing inside it.
func namedBySearch(batch []string, steps []searchStep) string {
	if len(batch) < 2 {
		return ""
	}
	index := map[string]int{}
	for i, id := range batch {
		index[id] = i
	}
	low, high := -1, len(batch)-1
	for _, st := range steps {
		k, found := index[st.onto]
		if !found {
			continue
		}
		if st.failed {
			if k < high {
				high = k
			}
		} else if k > low {
			low = k
		}
	}
	if high-low == 1 {
		return batch[high]
	}
	return ""
}

// complainedOf is every release a later advisory or report arrived on, read
// through the seam a composition supplies: an advisory and a report are both
// text from outside the graph, and neither is a record this package reads.
func (s *Score) complainedOf(ctx context.Context) (map[string]bool, error) {
	return s.complaints.OnReleases(ctx)
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
// caller accepts, closed after since where since names a restarted prior's
// restart time and every one otherwise. A hold is neither: a hold teaches the
// score nothing, which is what separates it from a reject. An auto-passed
// decision is not counted either — its close event's actor is the gate
// component, so the human test leaves it out. A rejection is counted here as
// evidence about the author, which is not the reading the risk threshold
// takes: that one waits for the rejection to resolve, and rejection.go is
// where it does.
func (s *Score) humanVerdicts(ctx context.Context, wanted func(OpenEvent) bool, since string) (verdicts, error) {
	var counted verdicts
	closed, err := decisionlog.NewReader(s.pool, s.token).ClosedDecisions(ctx, componentPrincipal)
	if err != nil {
		return counted, err
	}
	for _, d := range closed {
		if since != "" && d.CloseEvent.At <= since {
			continue
		}
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
