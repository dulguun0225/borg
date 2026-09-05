package score

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/window"
)

// reading is one factor as it was read: the level, the quantity in words, and
// the reason the score could not compute it.
type reading struct {
	level       float64
	words       string
	unavailable string
}

func (s *Score) size(_ context.Context, c Change) (reading, error) {
	if c.Measurement.Unavailable != "" {
		return reading{unavailable: c.Measurement.Unavailable}, nil
	}
	lines := c.Measurement.LinesChanged
	return reading{
		level: level(float64(lines), sizeBreakpoints, 1.0),
		words: fmt.Sprintf("%d lines changed", lines),
	}, nil
}

func (s *Score) reach(_ context.Context, c Change) (reading, error) {
	m := c.Measurement
	if m.Unavailable != "" {
		return reading{unavailable: m.Unavailable}, nil
	}
	if m.FilesInTree <= 0 {
		return reading{unavailable: "the build's tree holds no files, so the share one change touches is undefined"}, nil
	}
	share := float64(m.FilesChanged) / float64(m.FilesInTree)
	return reading{
		level: level(share, reachBreakpoints, 1.0),
		words: fmt.Sprintf("%d of the service's %d files", m.FilesChanged, m.FilesInTree),
	}, nil
}

// coverage reads the criteria that decided the build. Coverage in the sense of
// lines executed is measured by nothing in the factory, so what this factor
// reads is how many criteria decided this build and whether any of them failed,
// which is the protection the factory actually has. A failed criterion is the
// top of the scale on its own: the gate above is what rejects it, and a number
// that read low on a failing build would be the score disagreeing with a run.
func (s *Score) coverage(_ context.Context, c Change) (reading, error) {
	if c.CriteriaFailed > 0 {
		return reading{
			level: 1.0,
			words: fmt.Sprintf("%d of %d criteria failed against this build", c.CriteriaFailed, c.CriteriaInForce),
		}, nil
	}
	return reading{
		level: level(float64(c.CriteriaInForce), criteriaBreakpoints, 0.1),
		words: fmt.Sprintf("%d criteria in force decided this build and all passed", c.CriteriaInForce),
	}, nil
}

// churn reads what else has been changing in the item's area lately. This
// item's own releases are left out: a change is not its own churn, and at the
// production deploy row its release already exists.
func (s *Score) churn(ctx context.Context, c Change) (reading, error) {
	if c.AreaID == "" {
		return reading{unavailable: "the item names no area, so nothing says what else has been changing around it"}, nil
	}
	items, err := item.IDsInArea(ctx, s.pool, c.AreaID)
	if err != nil {
		return reading{}, err
	}
	since := record.FormatTime(time.Now().Add(-ChurnWindow))
	releases, err := release.CountForItemsSince(ctx, s.pool, items, c.ItemID, since)
	if err != nil {
		return reading{}, err
	}
	return reading{
		level: level(float64(releases), churnBreakpoints, 1.0),
		words: fmt.Sprintf("%d releases in this area in the last %s", releases, ChurnWindow),
	}, nil
}

// reversibility reads whether the service has a release to return to, this
// item's own excluded. A first release has none, which is what the design says
// of one: no control, nothing able to close a window passed, and no rollback
// target.
func (s *Score) reversibility(ctx context.Context, c Change) (reading, error) {
	earlier, err := release.CountForService(ctx, s.pool, c.ServiceID, c.ItemID)
	if err != nil {
		return reading{}, err
	}
	if earlier == 0 {
		return reading{level: 1.0, words: "no earlier release of this service to return to"}, nil
	}
	return reading{
		level: 0.3,
		words: fmt.Sprintf("%d earlier releases of this service, none of them watched", earlier),
	}, nil
}

// prior reads every outcome on the author's own work. The author is the one that
// wrote the implementation version the build was made from, and the prior is kept
// per model version and not per family or per role, so two agents on one model
// version share it and a fleet entry moved to a newer version starts its evidence
// over.
//
// Three kinds of outcome, and the design says all three move it: a human's
// verdict on a version that author wrote, a analysis window closing over a release of
// an item that author wrote, and a human undoing one of those releases after it
// shipped. A window closing without failing the release counts for the author
// and one that fails it counts against — which is what "every outcome on that
// author's artifact moves it, a window closing without failing the release
// included" asks for, and it is what lets a prior
// narrow on a factory that has stopped putting humans at gates.
//
// A swept window is not counted either way: a rollback aimed below the release
// undid it, so its health monitor stopped before it decided anything. An undo is
// counted whatever reason the human gave — the restriction to evidence traceable
// to the health monitor is the analysis window's parameters' rule and not this one's,
// because building the wrong thing well says something about the author and
// nothing about the health monitor.
//
// It reads two rows per item the author wrote, on top of the whole log. That is
// what an outcome-based prior costs while the store is small, and it is the same
// cost, one level up, that reading the whole log for one author's verdicts already
// carries.
func (s *Score) prior(ctx context.Context, c Change) (reading, error) {
	implementation, found, err := artifact.NewestOfKind(ctx, s.pool, c.ItemID, artifact.KindImplementation)
	if err != nil {
		return reading{}, err
	}
	if !found || implementation.Author == "" {
		return reading{unavailable: "the item has no implementation version naming an author, so there is no author to hold a prior on"}, nil
	}
	authored, err := artifact.IDsByAuthor(ctx, s.pool, implementation.Author)
	if err != nil {
		return reading{}, err
	}
	approved, rejected, err := s.humanVerdicts(ctx, func(opening OpenEvent) bool {
		return contains(authored, opening.ArtifactID)
	})
	if err != nil {
		return reading{}, err
	}

	shipped, failed, undone, err := s.outcomesOfAuthor(ctx, implementation.Author)
	if err != nil {
		return reading{}, err
	}
	return reading{
		level: evidenceLevel(approved+shipped, rejected+failed+undone),
		words: fmt.Sprintf("%s: %d human approval(s) and %d rejection(s) on its own versions, %d release(s) watched without being failed, %d failed by a window, %d undone by a human",
			implementation.Author, approved, rejected, shipped, failed, undone),
	}, nil
}

// outcomesOfAuthor is what became of the releases of the items this author wrote a
// version of: how many were watched to a close without being failed, how many a window
// failed, and how many a human undid.
//
// A release is counted once at most. A release failed by its own window is
// usually also the release a rollback undid, and counting both would be one
// outcome told twice — so an undo is counted only where the window did not
// already fail it, which is the case the design means: a human undoing
// something the health monitor did not catch.
func (s *Score) outcomesOfAuthor(ctx context.Context, author string) (shipped, failed, undone int, err error) {
	items, err := artifact.ItemsByAuthor(ctx, s.pool, author)
	if err != nil {
		return 0, 0, 0, err
	}
	if len(items) == 0 {
		return 0, 0, 0, nil
	}

	rollbacks, err := deploy.Rollbacks(ctx, s.pool)
	if err != nil {
		return 0, 0, 0, err
	}
	undoneRelease := map[string]bool{}
	for _, d := range rollbacks {
		if d.Undoing.Source != deploy.SourceHealthMonitorAtFailed {
			undoneRelease[d.Undoing.FailedReleaseID] = true
		}
	}

	for _, itemID := range items {
		rel, released, err := release.ForItem(ctx, s.pool, itemID)
		if err != nil {
			return 0, 0, 0, err
		}
		if !released {
			continue
		}
		w, watched, err := window.ForRelease(ctx, s.pool, rel.ID)
		if err != nil {
			return 0, 0, 0, err
		}
		switch {
		case watched && w.Exit == window.ExitFailed:
			failed++
		case undoneRelease[rel.ID]:
			undone++
		case watched && w.Exit.Counts():
			shipped++
		}
	}
	return shipped, failed, undone, nil
}

// businessArea reads the human verdicts on items in the same area. What the
// design has this factor read is what the change touches in this customer's
// business, and nothing in the factory says what an area is worth to a business
// — so what it reads is the area's own record of being got wrong, which starts
// wide and narrows the way a prior does. On a factory where one author works
// one area it says nearly what the prior says, and the two only diverge once
// there are several of each.
func (s *Score) businessArea(ctx context.Context, c Change) (reading, error) {
	if c.AreaID == "" {
		return reading{unavailable: "the item names no area, so nothing says what part of the business it touches"}, nil
	}
	items, err := item.IDsInArea(ctx, s.pool, c.AreaID)
	if err != nil {
		return reading{}, err
	}
	approved, rejected, err := s.humanVerdicts(ctx, func(opening OpenEvent) bool {
		return contains(items, opening.ItemID)
	})
	if err != nil {
		return reading{}, err
	}
	return reading{
		level: evidenceLevel(approved, rejected),
		words: fmt.Sprintf("%d human approvals and %d rejections on items in this area", approved, rejected),
	}, nil
}

// consumers reads which sibling services declare they consume what this one
// publishes. It is a query over the graph a contract and a consumer contract make:
// the contracts this service publishes, and the other services whose consumer
// contracts name one of them.
//
// Two filters and both are deliberate. A consumer contract whose item has no
// release is left out, because a consumer contract is written at the
// implementation stage and a candidate that never merges leaves one behind; what
// says it is a release's is a release naming the same item. And this service's
// own consumer contracts are left out, because a service declaring against its
// own store contract is its own past and not a sibling — that consumer is real
// and is what a store's forward promise is for, and it is not what this factor is
// asking about.
//
// What it does not filter by is the in-force range. That range is enforcement's
// question about one candidate at one moment; this is a reading about the service,
// computed at every firing over the whole graph the way every other factor here is.
//
// An undeclared consumer is still exactly what this factor cannot see, which is
// the derivation's blind case one level up rather than a limit of this query.
func (s *Score) consumers(ctx context.Context, c Change) (reading, error) {
	published, err := contract.OfService(ctx, s.pool, c.ServiceID)
	if err != nil {
		return reading{}, err
	}
	if len(published) == 0 {
		return reading{
			level: level(0, consumersBreakpoints, 1.0),
			words: "this service publishes no contract, so nothing declares it consumes one",
		}, nil
	}

	predicates, err := consumercontract.AgainstProducer(ctx, s.pool, c.ServiceID)
	if err != nil {
		return reading{}, err
	}
	var items []string
	for _, p := range predicates {
		if p.ServiceID != c.ServiceID && !slices.Contains(items, p.ItemID) {
			items = append(items, p.ItemID)
		}
	}
	released, err := release.ItemsWithRelease(ctx, s.pool, items)
	if err != nil {
		return reading{}, err
	}
	var services []string
	for _, p := range predicates {
		if p.ServiceID == c.ServiceID || !slices.Contains(released, p.ItemID) {
			continue
		}
		if !slices.Contains(services, p.ServiceID) {
			services = append(services, p.ServiceID)
		}
	}
	return reading{
		level: level(float64(len(services)), consumersBreakpoints, 1.0),
		words: fmt.Sprintf("%d sibling service(s) declare they consume one of the %d contract(s) this service publishes",
			len(services), len(published)),
	}, nil
}

// humanVerdicts counts the closed decisions a human gave over a subject the
// caller accepts. A hold is neither: a hold teaches the score nothing, which is
// what separates it from a reject. An auto-passed decision is not counted
// either — its close event's actor is the gate component, so the human test
// leaves it out.
func (s *Score) humanVerdicts(ctx context.Context, wanted func(OpenEvent) bool) (approved, rejected int, err error) {
	closed, err := decisionlog.NewReader(s.pool, s.token).ClosedDecisions(ctx, component)
	if err != nil {
		return 0, 0, err
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
			approved++
		case VerdictRejected:
			rejected++
		}
	}
	return approved, rejected, nil
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
