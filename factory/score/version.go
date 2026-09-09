package score

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// FormatVersion is what every score version row of the decision log carries in
// its format_version column, and what declares the row's shape there.
const FormatVersion = "score_version/1"

// lockName is what [AdvisoryLockKey] hashes. It names this package so that no
// other part of the factory derives the same key from a name of its own.
const lockName = "borg/factory/score"

// AdvisoryLockKey is the PostgreSQL advisory lock [Writer.Ensure] holds across
// the read of the newest version and the append that supersedes it: the first
// eight bytes of SHA-256 of [lockName], big-endian, with the top bit cleared so
// the value is positive. TestAdvisoryLockKeyIsDerivedFromTheName recomputes it.
//
// One key and not one per anything: what it serialises is reading the newest
// version and appending the one that supersedes it, and there is one sequence.
// The log has a lock of its own over the chain; this one is over the comparison,
// which the log cannot see.
func AdvisoryLockKey() int64 {
	sum := sha256.Sum256([]byte(lockName))
	return int64(binary.BigEndian.Uint64(sum[:8]) & 0x7fffffffffffffff)
}

// ErrNoVersion is returned by [Get] where no row of the log is a score version
// with that id.
var ErrNoVersion = errors.New("score: no score version has that id")

// Branch is which of the three things wrote a version, which is what decides
// where it is in force. A version differing in a supplied value takes effect as
// it is appended — gating each would gate the learning itself. A version that
// changes the published formula or the factor set does not decide a gate an
// authored threshold binds until the owner has confirmed or re-authored that
// threshold against it, and a recalibration takes that same branch: under new
// weights the same change gets a different number.
type Branch string

const (
	// BranchSupplied is a version differing in a supplied value, in the bands,
	// in a drift reading or in a false-alarm count, and in nothing else.
	BranchSupplied Branch = "a supplied value moved"
	// BranchFormula is a version that changes the published formula or the
	// factor set.
	BranchFormula Branch = "the published formula or the factor set changed"
	// BranchRecalibration is a version differing in the weights and in nothing
	// else, which the score wrote by refitting them over the held-out decisions.
	BranchRecalibration Branch = "a recalibration refitted the weights"
)

// Version is one score version as it is stored: a row of the decision log,
// chained like every row there, holding what the score published at the moment
// every decision naming it was decided. Its id is the id of that row.
type Version struct {
	ID             string
	Actor          record.Actor
	At             string
	FormulaVersion string
	Formula        string
	// Weights is what each factor set gives each of its factors. It is a field
	// of the version from the first row and not a fact kept beside it: a
	// decision re-scored under the version it names has to return the number it
	// was decided on, and a formula recorded without its weights is a shape and
	// not a function.
	Weights map[FactorSet]Weights
	// FactorSets is the published text of every set and what it weighs.
	FactorSets string
	// ControlBound is the impact discounted by reversibility at or above which
	// the score picks the rollout strategy that keeps a control. It is a field of
	// the version because the version names every value the score supplies, and
	// the strategy is one of the three things one number decides.
	ControlBound float64
	// Rules is the published rules by which each supplied value moves, which is
	// what an owner disagreeing with a moved value argues with.
	Rules           string
	LearningVersion string
	// BandWidth is how wide one band of the number is, and with it how far the
	// risk threshold moves in one step: the two are one number, so a threshold
	// that falls one band and a rise on the held-out sample take the same step
	// and an owner reads that step off the record.
	BandWidth float64
	// ShippedPriors is the per-author prior the product ships for a model
	// version, by model version. An author the factory has not seen starts at
	// the prior shipped for its model version where one was shipped, and at the
	// width its count of no closes supports where none was.
	ShippedPriors map[string]float64
	// Scale is the last step of each set's fit: what maps the weighted means'
	// number onto the share of held-out windows that failed among decisions
	// taken at that number on that set, which is the one scale the three sets
	// share. It is fitted with the weights and counts as one of them: a
	// recalibration moves it with the weights and moves nothing else. An
	// unfitted set carries the zero scale, which is the identity.
	Scale map[FactorSet]Scale
	// RecalibratedThrough is the close time of the newest decision the last
	// recalibration read, and is empty where none has run. The calibration
	// readings are taken over the decisions after it: a recalibration is the
	// only exit from a drift, so a reading that went on reading what the
	// recalibration already answered would put the resolution back at the next
	// pass and no resolution would ever end.
	RecalibratedThrough string
	// PriorRestarts is every author whose per-author prior restarted as an
	// unseen author's, by the time it restarted at: a truncation of the log
	// removed every held-out decision on an author whose prior stood drifted, so
	// the drift's own evidence could no longer arrive and the resolution would
	// otherwise stand forever. From that time on, [Score.prior] counts only what
	// closed after it, so the level narrows again as new closes arrive. It is
	// carried forward by every version this package appends: an author
	// restarted once stays restarted, recalibration reading fresh evidence over
	// it being a different thing from a restart.
	PriorRestarts map[string]string
	// Supplied is every value the score supplies: the starting value of each
	// parameter and a row per subject an outcome has moved it for.
	Supplied SuppliedValues
	// Bands is the share of held-out releases whose windows failed within each
	// band of the number, per factor set and within each set per service and
	// factory-wide. It is what says whether the number ranks anything at all.
	Bands []Band
	// Drift is every factor calibration found drifted and every per-author prior
	// its own held-out reading found drifted. A factor named here is resolved at
	// every firing under this version.
	Drift []Drift
	// FalseAlarms is how many rejections resolved as false alarms, per human. It
	// moves no threshold and is published because rejecting would otherwise be
	// the response that costs the person nothing.
	FalseAlarms []FalseAlarm
	Branch      Branch
	// ShippedBundleIdentity names the release of the product this version came
	// from, and is empty on every version an outcome or a recalibration wrote. It
	// is present on the version install's first start appends, which no owner
	// authored.
	ShippedBundleIdentity string
	// Supersedes is the version this one replaced, and is empty on the first.
	Supersedes string
}

// versionPayload is what the log row carries. The row's id, actor and time are
// the log's own columns and are not repeated here.
type versionPayload struct {
	FormulaVersion        string                `json:"formula_version"`
	Formula               string                `json:"formula"`
	Weights               map[FactorSet]Weights `json:"weights"`
	FactorSets            string                `json:"factor_sets"`
	ControlBound          float64               `json:"control_bound"`
	Rules                 string                `json:"rules"`
	LearningVersion       string                `json:"learning_version"`
	BandWidth             float64               `json:"band_width,omitempty"`
	ShippedPriors         map[string]float64    `json:"shipped_priors,omitempty"`
	Scale                 map[FactorSet]Scale   `json:"scale,omitempty"`
	RecalibratedThrough   string                `json:"recalibrated_through,omitempty"`
	PriorRestarts         map[string]string     `json:"prior_restarts,omitempty"`
	Supplied              SuppliedValues        `json:"supplied"`
	Bands                 []Band                `json:"bands"`
	Drift                 []Drift               `json:"drift"`
	FalseAlarms           []FalseAlarm          `json:"false_alarms"`
	Branch                Branch                `json:"branch"`
	ShippedBundleIdentity string                `json:"shipped_bundle_identity,omitempty"`
	Supersedes            string                `json:"supersedes"`
}

// Value is what this version supplies for one parameter on one subject. It is the
// read package policy makes: the value in force is what an owner authored where
// they authored one and what the version in force supplies otherwise, clamped by
// any safeguard.
//
// A zero version answers the starting values, which is the answer for a factory
// that has appended no version yet.
func (v Version) Value(p gatepolicy.Parameter, subject string) (Supplied, bool) {
	return v.Supplied.Value(p, subject)
}

// WeightsOf is what this version gives one factor set, falling back to the
// weights the product ships for a version that names none — a version appended
// before a set existed holds no weights for it.
func (v Version) WeightsOf(set FactorSet) Weights {
	if w, ok := v.Weights[set]; ok && len(w) > 0 {
		return w
	}
	return ShippedWeights(set)
}

// ControlBoundOrShipped is the bound this version names, falling back to
// [ShippedControlBound] for a version appended before the bound was a field of
// one — a version holding no bound is not a factory that picks a control for
// every release.
func (v Version) ControlBoundOrShipped() float64 {
	if v.ControlBound <= 0 {
		return ShippedControlBound
	}
	return v.ControlBound
}

// BandWidthOrShipped is the band width this version names, falling back to
// [ShippedBandWidth] for a version appended before the width was a field of one.
func (v Version) BandWidthOrShipped() float64 {
	if v.BandWidth <= 0 {
		return ShippedBandWidth
	}
	return v.BandWidth
}

// ShippedPrior is the prior the product shipped for one author's model version,
// and false where it shipped none. An author with no shipped prior starts at the
// width a count of no closes supports.
func (v Version) ShippedPrior(author string) (float64, bool) {
	level, shipped := v.ShippedPriors[author]
	return level, shipped
}

// ScaleOf is the scale this version fits one set's number by, and the identity
// for a set no recalibration has fitted.
func (v Version) ScaleOf(set FactorSet) Scale { return v.Scale[set] }

// Under is what one learning pass reads off the version in force: the band the
// threshold steps by, the point the last recalibration read to, which authors'
// priors stand drifted under it, and which have already restarted.
func (v Version) Under() Under {
	return Under{
		BandWidth:           v.BandWidthOrShipped(),
		RecalibratedThrough: v.RecalibratedThrough,
		DriftedPriors:       v.driftedPriors(),
		PriorRestarts:       v.PriorRestarts,
	}
}

// driftedPriors is every author this version's own drift readings name.
func (v Version) driftedPriors() []string {
	var authors []string
	for _, d := range v.Drift {
		if d.Author != "" {
			authors = append(authors, d.Author)
		}
	}
	return authors
}

// RestartedAt is when this author's prior last restarted as an unseen
// author's, and false where it never has. [Score.prior] reads it to count only
// what closed after that time.
func (v Version) RestartedAt(author string) (string, bool) {
	at, ok := v.PriorRestarts[author]
	return at, ok
}

// Drifted reports whether calibration found this factor drifted under this
// version. A drifted factor takes the treatment an unavailable factor takes at
// every firing until a recalibration is in force at the gate.
func (v Version) Drifted(factor string) bool {
	for _, d := range v.Drift {
		if d.Factor == factor && d.Author == "" {
			return true
		}
	}
	return false
}

// PriorDrifted reports whether the per-author prior on this author stands
// drifted. It is read on the held-out sample and nowhere else, and a prior
// standing drifted stops the sample selecting on that author at all.
func (v Version) PriorDrifted(author string) bool {
	for _, d := range v.Drift {
		if d.Author == author {
			return true
		}
	}
	return false
}

// Writer appends score versions to the log. There is no method that edits one:
// the log is append-only and chained, which is what makes a decision naming a
// version a decision readable against what that version said.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
	marks Marks
}

// NewWriter returns the writer over pool, fencing every write with token and
// reading the rollbacks a human marked through marks. A nil marks is [NoMarks].
func NewWriter(pool *pgxpool.Pool, token lease.Token, marks Marks) *Writer {
	if marks == nil {
		marks = NoMarks{}
	}
	return &Writer{pool: pool, token: token, marks: marks}
}

// Ensure is the version in force: the newest stored version where it still says
// what this source publishes and what the outcomes in the store supply, and a
// freshly appended one naming it as its predecessor where it does not. So a
// change to the formula, the factor sets, the rules, or any supplied value moves
// the version by the ordinary path, and starting the factory twice over an
// unchanged store appends nothing.
//
// This is where the score learns. The learning is a pass over records that
// already exist and never a write at a firing: an outcome arrives long after the
// decision it judges, so nothing at a gate could have computed it, and a version
// that moved mid-process would leave two decisions of one run naming different
// numbers.
func (w *Writer) Ensure(ctx context.Context, actor record.Actor) (Version, error) {
	return w.append(ctx, actor, func(newest Version, found bool) (Version, bool, error) {
		// The pass reads under the version below it: the band it steps the
		// threshold by, and the point the last recalibration read to, both of
		// which are fields of that version and not of this source.
		learned, err := Learn(ctx, w.pool, w.token, w.marks, newest.Under())
		if err != nil {
			return Version{}, false, err
		}
		next := Version{
			FormulaVersion:      FormulaVersion,
			Formula:             Formula,
			Weights:             newestWeights(newest, found),
			ControlBound:        ShippedControlBound,
			Rules:               Rules,
			LearningVersion:     LearningVersion,
			BandWidth:           newest.BandWidthOrShipped(),
			ShippedPriors:       newest.ShippedPriors,
			Scale:               newest.Scale,
			RecalibratedThrough: newest.RecalibratedThrough,
			PriorRestarts:       learned.PriorRestarts,
			Supplied:            learned.Supplied,
			Bands:               learned.Bands,
			Drift:               learned.Drift,
			FalseAlarms:         learned.FalseAlarms,
		}
		next.FactorSets = FactorSetsText(next.Weights)
		next.Branch = BranchSupplied
		if found && (newest.FormulaVersion != FormulaVersion || newest.FactorSets != next.FactorSets ||
			newest.ControlBoundOrShipped() != next.ControlBound) {
			next.Branch = BranchFormula
		}
		return next, !found || differs(newest, next), nil
	})
}

// Recalibrate refits the weights over the held-out decisions and appends a
// version differing in the weights and in nothing else. It takes the branch a
// formula change takes: under new weights the same change gets a different
// number, factory-wide and at once, so it is in force as appended where no owner
// authored the threshold and waits on the owner's confirmation where one did.
func (w *Writer) Recalibrate(ctx context.Context, actor record.Actor) (Version, error) {
	e, err := ReadEvidence(ctx, w.pool, w.token, w.marks)
	if err != nil {
		return Version{}, err
	}
	fitted := Fit(e)
	scaled := FitScale(e, fitted)
	read := e.newestClose()
	return w.append(ctx, actor, func(newest Version, found bool) (Version, bool, error) {
		next := recalibrated(newest, found, fitted, scaled, read)
		return next, !found || differs(newest, next), nil
	})
}

// recalibrated is the version a recalibration writes: newest with only the
// weights, the scale fitted with them, the factor-set text they produce, the
// branch, the drift a recalibration ends and the point it read to changed —
// and, where nothing has been appended yet, the starting supplied values.
// Nothing else moves: [Writer.Recalibrate]'s own comment says why. It is
// separate from that method so the composition is testable without a store.
func recalibrated(newest Version, found bool, fitted map[FactorSet]Weights, scaled map[FactorSet]Scale, readThrough string) Version {
	next := newest
	next.Weights = fitted
	next.Scale = scaled
	next.FactorSets = FactorSetsText(fitted)
	next.Branch = BranchRecalibration
	// The recalibration is the exit from every drift standing under the
	// version below it: it read the held-out decisions those readings were
	// made over, so the resolutions they put at the gates end here and the
	// next pass reads the decisions after this one.
	next.Drift = nil
	next.RecalibratedThrough = readThrough
	if !found {
		next.Supplied = StartingValues()
	}
	return next
}

// EnterShipped appends the version install's first start writes, naming the
// shipped-bundle identity the formula, the weights, the band width and the
// priors per model version came from, and nothing an outcome moved. It is the
// one version no owner and no outcome authored.
func (w *Writer) EnterShipped(ctx context.Context, actor record.Actor, shippedBundleIdentity string) (Version, error) {
	if shippedBundleIdentity == "" {
		return Version{}, fmt.Errorf("score: the shipped version names no release of the product")
	}
	return w.append(ctx, actor, func(newest Version, found bool) (Version, bool, error) {
		next := Version{
			FormulaVersion:        FormulaVersion,
			Formula:               Formula,
			Weights:               ShippedWeightsBySet(),
			ControlBound:          ShippedControlBound,
			BandWidth:             ShippedBandWidth,
			ShippedPriors:         ShippedPriors(),
			Rules:                 Rules,
			LearningVersion:       LearningVersion,
			Supplied:              StartingValues(),
			Branch:                BranchFormula,
			ShippedBundleIdentity: shippedBundleIdentity,
		}
		next.FactorSets = FactorSetsText(next.Weights)
		return next, !found || newest.ShippedBundleIdentity != shippedBundleIdentity, nil
	})
}

// newestWeights is the weights a learning version carries: the ones in force,
// which only a recalibration moves.
func newestWeights(newest Version, found bool) map[FactorSet]Weights {
	if !found || len(newest.Weights) == 0 {
		return ShippedWeightsBySet()
	}
	return newest.Weights
}

// append holds [AdvisoryLockKey] over the read of the newest version and the
// append that supersedes it, so two processes ensuring at once append one
// version and not two. The lock is session-level on a connection of its own
// because the append is the log's own transaction and not this package's.
func (w *Writer) append(ctx context.Context, actor record.Actor,
	compose func(newest Version, found bool) (Version, bool, error)) (Version, error) {

	if err := actor.Validate(); err != nil {
		return Version{}, err
	}
	conn, err := w.pool.Acquire(ctx)
	if err != nil {
		return Version{}, fmt.Errorf("score: taking a connection for the version lock: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `select pg_advisory_lock($1)`, AdvisoryLockKey()); err != nil {
		return Version{}, fmt.Errorf("score: taking the version lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), `select pg_advisory_unlock($1)`, AdvisoryLockKey())
	}()

	newest, found, err := Newest(ctx, w.pool, w.token)
	if err != nil {
		return Version{}, err
	}
	next, changed, err := compose(newest, found)
	if err != nil {
		return Version{}, err
	}
	if !changed {
		return newest, nil
	}
	next.Supersedes = newest.ID

	payload, err := json.Marshal(versionPayload{
		FormulaVersion: next.FormulaVersion, Formula: next.Formula, Weights: next.Weights,
		FactorSets: next.FactorSets, ControlBound: next.ControlBound,
		Rules: next.Rules, LearningVersion: next.LearningVersion,
		BandWidth: next.BandWidth, ShippedPriors: next.ShippedPriors, Scale: next.Scale,
		RecalibratedThrough: next.RecalibratedThrough, PriorRestarts: next.PriorRestarts,
		Supplied: next.Supplied, Bands: next.Bands, Drift: next.Drift, FalseAlarms: next.FalseAlarms,
		Branch: next.Branch, ShippedBundleIdentity: next.ShippedBundleIdentity, Supersedes: next.Supersedes,
	})
	if err != nil {
		return Version{}, fmt.Errorf("score: encoding the version: %w", err)
	}
	row, err := decisionlog.NewWriter(w.pool, w.token).AppendScoreVersion(ctx, decisionlog.Entry{
		Actor:         actor,
		Payload:       string(payload),
		FormatVersion: FormatVersion,
	})
	if err != nil {
		return Version{}, fmt.Errorf("score: appending a version of %s: %w", next.FormulaVersion, err)
	}
	next.ID, next.Actor, next.At = row.ID, row.Actor, row.At
	return next, nil
}
