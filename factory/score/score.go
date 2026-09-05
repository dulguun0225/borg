package score

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// component is the actor every read this package makes of the decision log is
// attributed to: the score reading its own evidence, and not any human or any
// other component.
var component = record.Actor{Kind: record.KindComponent, Key: "score"}

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
// computed over is the open event the gate writes it onto.
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

// OpenEvent is the part of a decision's open event this package reads back: the
// item the decision was about, the artifact version under decision, the row it
// fired at, the number and the threshold it was decided against, and whether the
// score's own sample had selected the item. Package gate composes it into the
// payload it writes, so every one of those field names is declared once and an
// outcome the score counts is an outcome a gate wrote.
//
// It is the part and not the whole payload. What a gate writes beside these is the
// vector, the criteria, the safeguards, and what the row waits on, none of which
// this package reads back — the vector because a vector is written where it was
// computed and never recomputed, and the rest because nothing here asks about it.
type OpenEvent struct {
	ItemID     string  `json:"item_id"`
	ArtifactID string  `json:"artifact_id"`
	Gate       string  `json:"gate"`
	Number     float64 `json:"number"`
	Threshold  float64 `json:"threshold"`
	// HeldOut is whether the score selected this item into its sample. It is
	// written on every decision on the item from the selection onward, which is
	// what makes an item selected once auto-pass every gate the score would have
	// gated.
	HeldOut bool `json:"held_out"`
}

// CloseEvent is the part of a decision's close event this package reads back: the
// verdict, and what auto-passed the firing where the factory decided for itself.
// The two are read together because neither answers a question on its own — an
// approval by a human is evidence about an author, and an approval by the factory
// is the factory agreeing with itself unless its own sample is what passed it.
type CloseEvent struct {
	Verdict         string `json:"verdict"`
	WhyItAutoPassed string `json:"why_it_auto_passed"`
}

// The two verdicts this package reads off a close event. Package gate owns the
// vocabulary and cannot be imported here, importing this package itself, so these
// are the two words this package reads and TestTheVerdictsGateWritesAreTheOnesTheScoreReads
// in that package is what holds the two spellings together.
const (
	// VerdictApproved is a decision approved.
	VerdictApproved = "approve"
	// VerdictRejected is a decision rejected. A hold is neither, and teaches the
	// score nothing.
	VerdictRejected = "reject"
)

// Score is the risk score over one pool, computing every vector under one
// version — the one in force when the process started, which every decision it
// produces names.
type Score struct {
	pool    *pgxpool.Pool
	version Version
	draw    Draw
	token   lease.Token
}

// New returns the score over pool, computing under version and drawing its
// held-out sample from draw, with token fencing every read it makes of the
// decision log. A nil draw is [RandomDraw]: a factory composed without one
// still holds a sample out, because a factory that quietly stopped sampling
// would be one whose threshold could only ever fall.
func New(pool *pgxpool.Pool, version Version, draw Draw, token lease.Token) *Score {
	if draw == nil {
		draw = RandomDraw{}
	}
	return &Score{pool: pool, version: version, draw: draw, token: token}
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
