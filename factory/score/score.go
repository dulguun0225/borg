package score

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
)

// ErrChangeIncomplete is returned by [Score.Assess] for a change naming no item
// or no service. Every firing has both, so a blank is a caller's defect and not
// a factor to mark unavailable.
var ErrChangeIncomplete = errors.New("score: a change names an item and a service")

// ChurnWindow is how far back area churn is read. Two weeks is long enough that
// a service shipping weekly registers and short enough that an area quiet since
// last quarter reads as quiet.
const ChurnWindow = 14 * 24 * time.Hour

// Measurement is the build's diff, taken where the repository is by the
// component that built and handed here. It is not a record: the vector computed
// from it is, and a diff re-taken later against a repository other items have
// merged into is not the diff the decision was made on.
//
// Unavailable is why the diff could not be taken, and empty where it was. A
// measurement that could not be taken leaves size and reach unavailable, which
// the formula turns into a human at the gate.
type Measurement struct {
	LinesChanged int
	FilesChanged int
	FilesInTree  int
	Unavailable  string
}

// Change is what the score is asked about. The ids name records this package
// reads; the measurement and the two criteria counts are what the component that
// built the change already knows and the score cannot read.
//
// The build is not among them, and that is deliberate: nothing here reads a build
// record — what the score would want from one is the diff, which is the
// measurement, taken where the repository is. What says which build a vector was
// computed over is the opening row the gate writes it onto.
type Change struct {
	ItemID      string
	ServiceID   string
	AreaID      string
	Measurement Measurement
	// CriteriaInForce is how many criteria decided this build, and
	// CriteriaFailed how many of them its run failed.
	CriteriaInForce int
	CriteriaFailed  int
}

// Subject is the part of a decision's opening row this package reads back: the
// item the decision was about and the artifact version under decision. Package
// gate composes it into the payload it writes, so the two field names are
// declared once and an outcome the score counts is an outcome a gate wrote.
type Subject struct {
	ItemID     string `json:"item_id"`
	ArtifactID string `json:"artifact_id"`
}

// Score is the risk score over one pool, computing every vector under one
// version — the one in force when the process started, which every decision it
// produces names.
type Score struct {
	pool    *pgxpool.Pool
	version Version
}

// New returns the score over pool, computing under version.
func New(pool *pgxpool.Pool, version Version) *Score {
	return &Score{pool: pool, version: version}
}

// Version is the score version every assessment this score produces names.
func (s *Score) Version() Version { return s.version }

// Assess is the score's answer for one change: the eight factors read, the
// published formula applied, and the number it reduces to. It reads records and
// never writes one.
func (s *Score) Assess(ctx context.Context, c Change) (Assessment, error) {
	if c.ItemID == "" || c.ServiceID == "" {
		return Assessment{}, fmt.Errorf("%w: item %q, service %q", ErrChangeIncomplete, c.ItemID, c.ServiceID)
	}

	vector := make([]Factor, 0, len(definitions))
	for _, d := range definitions {
		r, err := d.read(s, ctx, c)
		if err != nil {
			return Assessment{}, fmt.Errorf("score: reading %s: %w", d.name, err)
		}
		f := Factor{
			Name: d.name, Group: d.group, Half: d.half, Weight: d.weight,
			Reading: r.words, Level: r.level, Unavailable: r.unavailable,
		}
		if f.Unavailable != "" {
			// A factor the score could not compute resolves to the top of the
			// scale, and the vector records which and why rather than leaving a
			// gap a reader has to interpret.
			f.Level = 1
		}
		vector = append(vector, f)
	}

	a := Assessment{
		Version:        s.version.ID,
		FormulaVersion: s.version.FormulaVersion,
		Vector:         vector,
	}
	a.Likelihood, a.Impact, a.Exposure, a.Number = reduce(vector)
	return a, nil
}

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
// of one: no control, nothing able to close a window clean, and no rollback
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

// prior reads the human verdicts on the author's own artifacts. The author is
// the one that wrote the implementation version the build was made from, and
// the prior is kept per author and not per role, so two agents on one model
// share it.
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
	approved, rejected, err := s.humanVerdicts(ctx, func(subject Subject) bool {
		return contains(authored, subject.ArtifactID)
	})
	if err != nil {
		return reading{}, err
	}
	return reading{
		level: evidenceLevel(approved, rejected),
		words: fmt.Sprintf("%s: %d human approvals and %d rejections on its own artifacts",
			implementation.Author, approved, rejected),
	}, nil
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
	approved, rejected, err := s.humanVerdicts(ctx, func(subject Subject) bool {
		return contains(items, subject.ItemID)
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
// publishes. Nothing derives a declaration until contracts are built, so the
// query is over nothing and the answer is none — which is what the records say
// and not what is true: an undeclared consumer is exactly what this factor
// cannot see.
func (s *Score) consumers(_ context.Context, _ Change) (reading, error) {
	return reading{
		level: level(0, consumersBreakpoints, 1.0),
		words: "no service declares it consumes this one, nothing deriving a declaration yet",
	}, nil
}

// humanVerdicts counts the closed decisions a human gave over a subject the
// caller accepts. A hold is neither: a hold teaches the score nothing, which is
// what separates it from a reject. An auto-passed decision is not counted
// either — its closing row's actor is the gate component, so the human test
// leaves it out, which is doc.go's point about what narrows a prior here.
func (s *Score) humanVerdicts(ctx context.Context, wanted func(Subject) bool) (approved, rejected int, err error) {
	closed, err := decisionlog.ClosedDecisions(ctx, s.pool)
	if err != nil {
		return 0, 0, err
	}
	for _, d := range closed {
		if d.Closing.Actor.Kind != record.KindHuman {
			continue
		}
		var subject Subject
		if err := json.Unmarshal([]byte(d.Opening.Payload), &subject); err != nil {
			// A payload this package cannot read is a row some other component
			// wrote in a shape it does not know, which is not evidence about an
			// author and is not an error either.
			continue
		}
		if !wanted(subject) {
			continue
		}
		var verdict struct {
			Verdict string `json:"verdict"`
		}
		if err := json.Unmarshal([]byte(d.Closing.Payload), &verdict); err != nil {
			continue
		}
		switch verdict.Verdict {
		case "approve":
			approved++
		case "reject":
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
