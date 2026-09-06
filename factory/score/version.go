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
	// Rules is the published rules by which each supplied value moves, which is
	// what an owner disagreeing with a moved value argues with.
	Rules           string
	LearningVersion string
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
	Rules                 string                `json:"rules"`
	LearningVersion       string                `json:"learning_version"`
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
	learned, err := Learn(ctx, w.pool, w.token, w.marks)
	if err != nil {
		return Version{}, err
	}
	return w.append(ctx, actor, func(newest Version, found bool) (Version, bool) {
		next := Version{
			FormulaVersion:  FormulaVersion,
			Formula:         Formula,
			Weights:         newestWeights(newest, found),
			Rules:           Rules,
			LearningVersion: LearningVersion,
			Supplied:        learned.Supplied,
			Bands:           learned.Bands,
			Drift:           learned.Drift,
			FalseAlarms:     learned.FalseAlarms,
		}
		next.FactorSets = FactorSetsText(next.Weights)
		next.Branch = BranchSupplied
		if found && (newest.FormulaVersion != FormulaVersion || newest.FactorSets != next.FactorSets) {
			next.Branch = BranchFormula
		}
		return next, !found || differs(newest, next)
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
	return w.append(ctx, actor, func(newest Version, found bool) (Version, bool) {
		next := newest
		next.FormulaVersion = FormulaVersion
		next.Formula = Formula
		next.Rules = Rules
		next.LearningVersion = LearningVersion
		next.Weights = fitted
		next.FactorSets = FactorSetsText(fitted)
		next.Branch = BranchRecalibration
		if !found {
			next.Supplied = StartingValues()
		}
		return next, !found || newest.FactorSets != next.FactorSets
	})
}

// EnterShipped appends the version install's first start writes, naming the
// shipped-bundle identity the formula and the weights came from and nothing an
// outcome moved. It is the one version no owner and no outcome authored.
func (w *Writer) EnterShipped(ctx context.Context, actor record.Actor, shippedBundleIdentity string) (Version, error) {
	if shippedBundleIdentity == "" {
		return Version{}, fmt.Errorf("score: the shipped version names no release of the product")
	}
	return w.append(ctx, actor, func(newest Version, found bool) (Version, bool) {
		next := Version{
			FormulaVersion:        FormulaVersion,
			Formula:               Formula,
			Weights:               ShippedWeightsBySet(),
			Rules:                 Rules,
			LearningVersion:       LearningVersion,
			Supplied:              StartingValues(),
			Branch:                BranchFormula,
			ShippedBundleIdentity: shippedBundleIdentity,
		}
		next.FactorSets = FactorSetsText(next.Weights)
		return next, !found || newest.ShippedBundleIdentity != shippedBundleIdentity
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

// differs is whether the version this pass computed says anything the newest
// stored one does not. Nothing refuses two versions that say the same thing where
// they are not adjacent — a learned value that moved and moved back is ordinary —
// so what is compared is this version against the one below it and nothing else.
func differs(newest, next Version) bool {
	if newest.FormulaVersion != next.FormulaVersion || newest.Formula != next.Formula ||
		newest.FactorSets != next.FactorSets || newest.Rules != next.Rules ||
		newest.LearningVersion != next.LearningVersion {
		return true
	}
	return !sameJSON(newest.Supplied, next.Supplied) || !sameJSON(newest.Bands, next.Bands) ||
		!sameJSON(newest.Drift, next.Drift) || !sameJSON(newest.FalseAlarms, next.FalseAlarms)
}

func sameJSON(a, b any) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(left) == string(right)
}

// append holds [AdvisoryLockKey] over the read of the newest version and the
// append that supersedes it, so two processes ensuring at once append one
// version and not two. The lock is session-level on a connection of its own
// because the append is the log's own transaction and not this package's.
func (w *Writer) append(ctx context.Context, actor record.Actor,
	compose func(newest Version, found bool) (Version, bool)) (Version, error) {

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
	next, changed := compose(newest, found)
	if !changed {
		return newest, nil
	}
	next.Supersedes = newest.ID

	payload, err := json.Marshal(versionPayload{
		FormulaVersion: next.FormulaVersion, Formula: next.Formula, Weights: next.Weights,
		FactorSets: next.FactorSets, Rules: next.Rules, LearningVersion: next.LearningVersion,
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

// versions is every score version in the log, oldest first.
func versions(ctx context.Context, pool *pgxpool.Pool, token lease.Token) ([]Version, error) {
	rows, err := decisionlog.NewReader(pool, token).Read(ctx, componentPrincipal)
	if err != nil {
		return nil, err
	}
	return versionsIn(rows)
}

// versionsIn is every score version among rows already read, oldest first. It is
// separate from [versions] so that a caller reading the log for two shapes at
// once — [InForceAt], which also reads the confirmations off the policy version
// rows — reads it once and appends one read event.
func versionsIn(rows []decisionlog.Row) ([]Version, error) {
	var read []Version
	for _, row := range rows {
		if row.Shape != decisionlog.ShapeScoreVersion {
			continue
		}
		var payload versionPayload
		if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
			return nil, fmt.Errorf("score: reading the version in row %s: %w", row.ID, err)
		}
		read = append(read, Version{
			ID: row.ID, Actor: row.Actor, At: row.At,
			FormulaVersion: payload.FormulaVersion, Formula: payload.Formula, Weights: payload.Weights,
			FactorSets: payload.FactorSets, Rules: payload.Rules, LearningVersion: payload.LearningVersion,
			Supplied: payload.Supplied, Bands: payload.Bands, Drift: payload.Drift,
			FalseAlarms: payload.FalseAlarms, Branch: payload.Branch,
			ShippedBundleIdentity: payload.ShippedBundleIdentity, Supersedes: payload.Supersedes,
		})
	}
	return read, nil
}

// Newest is the version in force, and false where none has been appended. The
// order is the log's own: a row that came later in the chain is a later version.
func Newest(ctx context.Context, pool *pgxpool.Pool, token lease.Token) (Version, bool, error) {
	read, err := versions(ctx, pool, token)
	if err != nil || len(read) == 0 {
		return Version{}, false, err
	}
	return read[len(read)-1], true, nil
}

// Get is one version by id, which is what a reader of a decision follows to
// what the score published when it was decided.
func Get(ctx context.Context, pool *pgxpool.Pool, token lease.Token, id string) (Version, error) {
	read, err := versions(ctx, pool, token)
	if err != nil {
		return Version{}, err
	}
	for _, v := range read {
		if v.ID == id {
			return v, nil
		}
	}
	return Version{}, fmt.Errorf("%w: %s", ErrNoVersion, id)
}

// All is every version, oldest first. It is what a reader following a supplied
// value's movement walks: each names the one it superseded, so the sequence is
// readable from either end, and what makes a movement readable beside it is
// every decision naming the version it was decided under.
func All(ctx context.Context, pool *pgxpool.Pool, token lease.Token) ([]Version, error) {
	return versions(ctx, pool, token)
}
