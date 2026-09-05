package incident

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// Status is where an incident is. It advances in place, the record being an
// ordinary record and not a row of the log.
type Status string

const (
	// StatusOpen is how every incident is written.
	StatusOpen Status = "open"
	// StatusResolved is the crossing having stopped against what is running and
	// what was raised from the incident having finished. A rollback alone does not
	// reach it: production is still worse, which is what the hold and the page
	// both say.
	StatusResolved Status = "resolved"
)

// Statuses is every status an incident may have. The CHECK in [DDL] lists the
// same two, and TestDDLListsEveryStatus fails if the two stop agreeing.
var Statuses = []Status{StatusOpen, StatusResolved}

// Reading is which of the three readings crossed. More than one runs on one
// service — the comparison against the control, the reading against the
// service's own recent history, and an explicit threshold where a safeguard set
// one — so an incident that did not name which would not say what happened.
type Reading string

const (
	// ReadingComparison is the comparison against the control, or against the
	// release below on the target where the strategy kept none.
	ReadingComparison Reading = "comparison"
	// ReadingOwnHistory is the reading against the service's own recent history,
	// which runs whether or not a window is open and states an average run length
	// where the comparison states a confidence.
	ReadingOwnHistory Reading = "own_history"
	// ReadingExplicitThreshold is a threshold an owner's safeguard set, read at a
	// run length of its own.
	ReadingExplicitThreshold Reading = "explicit_threshold"
)

// Readings is every reading that can cross. The CHECK in [DDL] lists the same
// three, and TestDDLListsEveryReading fails if the two stop agreeing.
var Readings = []Reading{ReadingComparison, ReadingOwnHistory, ReadingExplicitThreshold}

// StatesARunLength reports whether this reading is read at an average run length
// rather than at a confidence. A reading that never closes has no last look to
// spend a rate against, so it states the mean traffic between crossings instead.
func (r Reading) StatesARunLength() bool {
	return r == ReadingOwnHistory || r == ReadingExplicitThreshold
}

var (
	// ErrNotAComponent is returned for an actor that is not a component. The
	// health monitor is the only writer of this record; a human's judgment about
	// live software reaches production through undoing a change after it shipped
	// and the page they may fire, and never by writing one of these.
	ErrNotAComponent = errors.New("incident: an incident is written by the health monitor and not by a human")
	// ErrIncomplete is returned by [Writer.Raise] for an incident missing
	// something every one of them names.
	ErrIncomplete = errors.New("incident: the incident is missing something every one of them names")
	// ErrReadingUnknown is returned by [Writer.Raise] for a reading outside
	// [Readings].
	ErrReadingUnknown = errors.New("incident: the crossing names no reading this factory takes")
	// ErrBoundaryIncomplete is returned by [Writer.Raise] where the boundary the
	// crossing was read against is not stated: a size, and a confidence or a run
	// length according to the reading. A crossing is not interpretable against
	// anything but the boundary it was actually read against.
	ErrBoundaryIncomplete = errors.New("incident: the crossing does not state the boundary it was read against")
	// ErrNotFound is returned where no incident has that id.
	ErrNotFound = errors.New("incident: no incident has that id")
	// ErrNotOpen is returned by [Writer.Resolve] and [Writer.Observe] for an
	// incident that is not open. An incident resolves once and nothing reopens
	// one.
	ErrNotOpen = errors.New("incident: only an open incident is observed on or resolved")
)

// Incident is one incident as it is stored.
type Incident struct {
	ID    string
	Actor record.Actor
	At    string
	// EnvironmentID is the production environment the incident is on. An incident
	// is a record on production and nowhere else, the health monitor existing
	// there and nowhere else.
	EnvironmentID string
	ServiceID     string
	// ReleaseID is the release running when the incident appeared, which is not
	// always the release that caused it. What that costs is
	// ../../end-goal/how-the-factory-works/08-operations/06-incidents.md.
	ReleaseID string
	// DeployID is the deploy the crossing was read against, and through it the
	// release, the schema change it applied, the item and the intent — so what
	// caused an incident is answered by following those links.
	DeployID string
	// Reading is which of the three crossed, and Quantity is which quantity it
	// crossed on: the request rate, the error rate, the latency quantile, or the
	// count of a hazardous operation.
	Reading  Reading
	Quantity string
	// Size is that quantity's size at the crossing, and Confidence or RunLength
	// is the rest of the boundary: a confidence where the reading closes, an
	// average run length where it never does.
	Size       float64
	Confidence float64
	RunLength  float64
	// BoundaryVersion is the construction the reading was taken through, which is
	// what makes a crossing readable after the factory changes it.
	BoundaryVersion string
	// PolicyVersion and ScoreVersion are the two versions in force at the
	// reading, the same pair the window records at its open.
	PolicyVersion string
	ScoreVersion  string
	// FailureRecords is the failure records for that service, release and target
	// as the health monitor copied them at the crossing — a field of the incident
	// rather than a link to the store, kept for the incident's life and removable
	// the way any kept record's text is, by a redaction. It is bounded by the cap
	// already applied where they are kept.
	FailureRecords string
	// IntentID is the intent the crossing raised through intake, and is empty
	// where it raised none.
	IntentID string
	// Observations is how many further crossings were recorded on this incident
	// rather than raising a second intent.
	Observations int
	Status       Status
	ResolvedAt   string
}

// Open reports whether the incident is still open.
func (i Incident) Open() bool { return i.Status == StatusOpen }

// OverBound reports whether this incident has been open longer than bound at
// now. An incident whose crossing has not stopped, with no open window, pages
// whoever holds the duty while the item it raised is being worked, and the bound
// is how long that item may be worked before they are reached — a field of the
// service record, which this package does not read, so the caller hands it over.
func (i Incident) OverBound(now time.Time, bound time.Duration) (bool, error) {
	if !i.Open() || bound <= 0 {
		return false, nil
	}
	raised, err := record.ParseTime(i.At)
	if err != nil {
		return false, fmt.Errorf("incident: reading when %s was raised: %w", i.ID, err)
	}
	return now.Sub(raised) > bound, nil
}

// Raising is what [Writer.Raise] is given: where the crossing happened, which
// reading crossed on which quantity against which boundary, the failure records
// copied at that moment, and the intent it raised where it raised one.
type Raising struct {
	EnvironmentID string
	ServiceID     string
	ReleaseID     string
	DeployID      string
	Reading       Reading
	Quantity      string
	Size          float64
	// Confidence is stated for a reading that closes and RunLength for one that
	// does not; exactly one of the two is above nothing.
	Confidence      float64
	RunLength       float64
	BoundaryVersion string
	PolicyVersion   string
	ScoreVersion    string
	FailureRecords  string
	// IntentID is empty for a crossing inside the window, where what follows is a
	// rollback and not an item.
	IntentID string
}

func (r Raising) validate() error {
	for _, required := range []struct{ what, value string }{
		{"environment", r.EnvironmentID}, {"service", r.ServiceID},
		{"release", r.ReleaseID}, {"deploy", r.DeployID}, {"quantity", r.Quantity},
		{"boundary version", r.BoundaryVersion}, {"policy version", r.PolicyVersion},
		{"score version", r.ScoreVersion},
	} {
		if required.value == "" {
			return fmt.Errorf("%w: no %s", ErrIncomplete, required.what)
		}
	}
	if !slices.Contains(Readings, r.Reading) {
		return fmt.Errorf("%w: %q", ErrReadingUnknown, r.Reading)
	}
	if r.Size <= 0 {
		return fmt.Errorf("%w: the size is %v", ErrBoundaryIncomplete, r.Size)
	}
	if (r.Confidence > 0) == (r.RunLength > 0) {
		return fmt.Errorf("%w: confidence %v and run length %v, and a reading states one of the two",
			ErrBoundaryIncomplete, r.Confidence, r.RunLength)
	}
	if r.Reading.StatesARunLength() != (r.RunLength > 0) {
		return fmt.Errorf("%w: a %s crossing states %v as its run length",
			ErrBoundaryIncomplete, r.Reading, r.RunLength)
	}
	return nil
}

// Writer is the one writer of incident records: the health monitor.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewWriter returns the writer over pool, fencing every write with token.
func NewWriter(pool *pgxpool.Pool, token lease.Token) *Writer {
	return &Writer{pool: pool, token: token}
}

// Raise writes the incident, open with no observations on it. A second open
// incident on one service and one release is refused by the partial unique index
// rather than by this method: a caller that did not ask [Open] first gets the
// store's answer, which is the same answer.
func (w *Writer) Raise(ctx context.Context, actor record.Actor, r Raising) (Incident, error) {
	if err := actor.Validate(); err != nil {
		return Incident{}, err
	}
	if actor.Kind != record.KindComponent {
		return Incident{}, fmt.Errorf("%w: %s %q", ErrNotAComponent, actor.Kind, actor.Key)
	}
	if err := r.validate(); err != nil {
		return Incident{}, err
	}

	i := Incident{
		ID:              record.NewID(IDPrefix),
		Actor:           actor,
		At:              record.Now(),
		EnvironmentID:   r.EnvironmentID,
		ServiceID:       r.ServiceID,
		ReleaseID:       r.ReleaseID,
		DeployID:        r.DeployID,
		Reading:         r.Reading,
		Quantity:        r.Quantity,
		Size:            r.Size,
		Confidence:      r.Confidence,
		RunLength:       r.RunLength,
		BoundaryVersion: r.BoundaryVersion,
		PolicyVersion:   r.PolicyVersion,
		ScoreVersion:    r.ScoreVersion,
		FailureRecords:  r.FailureRecords,
		IntentID:        r.IntentID,
		Status:          StatusOpen,
	}
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Incident{}, fmt.Errorf("incident: beginning the raising of %s: %w", i.ID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return Incident{}, err
	}

	_, err = tx.Exec(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, environment_id, service_id, release_id, deploy_id,
		 reading, quantity, size, confidence, run_length, boundary_version, policy_version, score_version, failure_records,
		 intent_id, observations, status, resolved_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, 0, $21, '')`,
		i.ID, FormatVersion, string(i.Actor.Kind), i.Actor.Key, string(i.Actor.Basis), i.At,
		i.EnvironmentID, i.ServiceID, i.ReleaseID, i.DeployID,
		string(i.Reading), i.Quantity, i.Size, i.Confidence, i.RunLength,
		i.BoundaryVersion, i.PolicyVersion, i.ScoreVersion, i.FailureRecords,
		i.IntentID, string(i.Status),
	)
	if err != nil {
		return Incident{}, fmt.Errorf("incident: raising %s on release %s: %w", i.ID, r.ReleaseID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Incident{}, fmt.Errorf("incident: committing the raising of %s: %w", i.ID, err)
	}
	return i, nil
}

// Observe records a further crossing on an open incident: one more observation
// and nothing else. It is what the health monitor does instead of raising a second
// intent, and it is one statement so two concurrent observations both land.
func (w *Writer) Observe(ctx context.Context, id string) (Incident, error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Incident{}, fmt.Errorf("incident: beginning the observation of %s: %w", id, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return Incident{}, err
	}

	tag, err := tx.Exec(ctx, `update `+Table+`
		set observations = observations + 1 where id = $1 and status = $2`,
		id, string(StatusOpen))
	if err != nil {
		return Incident{}, fmt.Errorf("incident: observing on %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return Incident{}, notOpen(ctx, w.pool, id)
	}
	if err := tx.Commit(ctx); err != nil {
		return Incident{}, fmt.Errorf("incident: committing the observation of %s: %w", id, err)
	}
	return Get(ctx, w.pool, id)
}

// Resolve advances the incident to resolved and writes the time with it. Its
// caller is what decides that the two conditions hold — the crossing stopped, and
// what was raised from it finished — because both are facts about records this
// package does not read. What it enforces is that an incident resolves once, and
// from open.
func (w *Writer) Resolve(ctx context.Context, id string) (Incident, error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Incident{}, fmt.Errorf("incident: beginning the resolution of %s: %w", id, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return Incident{}, err
	}

	tag, err := tx.Exec(ctx, `update `+Table+`
		set status = $1, resolved_at = $2 where id = $3 and status = $4`,
		string(StatusResolved), record.Now(), id, string(StatusOpen))
	if err != nil {
		return Incident{}, fmt.Errorf("incident: resolving %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return Incident{}, notOpen(ctx, w.pool, id)
	}
	if err := tx.Commit(ctx); err != nil {
		return Incident{}, fmt.Errorf("incident: committing the resolution of %s: %w", id, err)
	}
	return Get(ctx, w.pool, id)
}

// notOpen tells an incident that does not exist from one that is not open, which
// is what the two update statements above cannot tell apart from their row count
// alone.
func notOpen(ctx context.Context, pool *pgxpool.Pool, id string) error {
	i, err := Get(ctx, pool, id)
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: %s is %s", ErrNotOpen, id, i.Status)
}

const selectIncident = `select id, actor_kind, actor_key, actor_key_basis, at, environment_id, service_id,
	release_id, deploy_id, reading, quantity, size, confidence, run_length, boundary_version, policy_version,
	score_version, failure_records, intent_id, observations, status, resolved_at
	from ` + Table

func scan(row pgx.Row) (Incident, error) {
	var i Incident
	var kind, basis, status, reading string
	err := row.Scan(&i.ID, &kind, &i.Actor.Key, &basis, &i.At, &i.EnvironmentID, &i.ServiceID,
		&i.ReleaseID, &i.DeployID, &reading, &i.Quantity, &i.Size, &i.Confidence, &i.RunLength,
		&i.BoundaryVersion, &i.PolicyVersion, &i.ScoreVersion, &i.FailureRecords,
		&i.IntentID, &i.Observations, &status, &i.ResolvedAt)
	if err != nil {
		return Incident{}, err
	}
	i.Actor.Kind = record.Kind(kind)
	i.Actor.Basis = record.Basis(basis)
	i.Reading = Reading(reading)
	i.Status = Status(status)
	return i, nil
}

// Get is one incident by id. It takes the pool and not a [Writer], because
// reading an incident is not a reason to be handed the thing that raises them.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Incident, error) {
	i, err := scan(pool.QueryRow(ctx, selectIncident+` where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Incident{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return Incident{}, fmt.Errorf("incident: reading %s: %w", id, err)
	}
	return i, nil
}

// Open is the open incident on one service and one release, and false where
// there is none. It is what the health monitor keys its deduplication on: an open one
// makes a further crossing an observation and never a second intent.
func Open(ctx context.Context, pool *pgxpool.Pool, serviceID, releaseID string) (Incident, bool, error) {
	if serviceID == "" || releaseID == "" {
		return Incident{}, false, nil
	}
	i, err := scan(pool.QueryRow(ctx, selectIncident+`
		where service_id = $1 and release_id = $2 and status = $3`,
		serviceID, releaseID, string(StatusOpen)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Incident{}, false, nil
	} else if err != nil {
		return Incident{}, false, fmt.Errorf("incident: reading the open incident on release %s: %w", releaseID, err)
	}
	return i, true, nil
}

// All is every incident in the store, oldest first, whatever the service. It is
// what the score learns from: an incident on a release whose own analysis window
// had already closed without failing a release is the crossing the health
// monitor could have seen and did not, which is the one outcome that says the
// window's size was too coarse.
//
// It is not per service for the reason every other whole-table read the score
// makes is not: the subjects it learns about are the services the records name,
// so asking per service would first mean being told which to ask about.
func All(ctx context.Context, pool *pgxpool.Pool) ([]Incident, error) {
	rows, err := pool.Query(ctx, selectIncident+` order by at, id`)
	if err != nil {
		return nil, fmt.Errorf("incident: reading every incident: %w", err)
	}
	defer rows.Close()

	var read []Incident
	for rows.Next() {
		i, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("incident: reading an incident: %w", err)
		}
		read = append(read, i)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("incident: reading every incident: %w", err)
	}
	return read, nil
}

// ForService is every incident of one service, oldest first.
func ForService(ctx context.Context, pool *pgxpool.Pool, serviceID string) ([]Incident, error) {
	rows, err := pool.Query(ctx, selectIncident+` where service_id = $1 order by at, id`, serviceID)
	if err != nil {
		return nil, fmt.Errorf("incident: reading the incidents of %s: %w", serviceID, err)
	}
	defer rows.Close()

	var read []Incident
	for rows.Next() {
		i, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("incident: reading an incident of %s: %w", serviceID, err)
		}
		read = append(read, i)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("incident: reading the incidents of %s: %w", serviceID, err)
	}
	return read, nil
}
