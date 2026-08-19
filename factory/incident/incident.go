package incident

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

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

var (
	// ErrNotAComponent is returned for an actor that is not a component. The
	// comparison is the only writer of this record; a human's judgment about live
	// software reaches production through veto after the fact and the page they
	// may fire, and never by writing one of these.
	ErrNotAComponent = errors.New("incident: an incident is written by the comparison and not by a human")
	// ErrIncomplete is returned by [Writer.Raise] for an incident missing
	// something every one of them names.
	ErrIncomplete = errors.New("incident: the incident is missing something every one of them names")
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
	// is a record on production and nowhere else, the comparison existing there
	// and nowhere else.
	EnvironmentID string
	ServiceID     string
	// ReleaseID is the release running when the incident appeared, which is not
	// always the release that caused it. doc.go says what that costs.
	ReleaseID string
	DeployID  string
	// Crossing is what crossed, in the words the comparison reports it with: its
	// own boundary, or a threshold an owner stated.
	Crossing string
	// IntentID is the intent the crossing raised through intake, and is empty
	// where it raised none — which is a crossing inside the window, where what
	// follows is a rollback rather than an item.
	IntentID string
	// Observations is how many further crossings were recorded on this incident
	// rather than raising a second intent.
	Observations int
	Status       Status
	ResolvedAt   string
}

// Open reports whether the incident is still open.
func (i Incident) Open() bool { return i.Status == StatusOpen }

// Raising is what [Writer.Raise] is given: where the crossing happened, what
// crossed, and the intent it raised where it raised one.
type Raising struct {
	EnvironmentID string
	ServiceID     string
	ReleaseID     string
	DeployID      string
	Crossing      string
	// IntentID is empty for a crossing inside the window, where what follows is a
	// rollback and not an item.
	IntentID string
}

// Writer is the one writer of incident records: the comparison.
type Writer struct {
	pool *pgxpool.Pool
}

// NewWriter returns the writer over pool.
func NewWriter(pool *pgxpool.Pool) *Writer { return &Writer{pool: pool} }

// Raise writes the incident, open with no observations on it. A second open
// incident on one service and one release is refused by the partial unique index
// rather than by this method: a caller that did not ask [Open] first gets the
// store's answer, which is the same answer.
func (w *Writer) Raise(ctx context.Context, actor record.Actor, r Raising) (Incident, error) {
	if err := actor.Validate(); err != nil {
		return Incident{}, err
	}
	if actor.Kind != record.KindComponent {
		return Incident{}, fmt.Errorf("%w: %s %q", ErrNotAComponent, actor.Kind, actor.Name)
	}
	for _, required := range []struct{ what, value string }{
		{"environment", r.EnvironmentID}, {"service", r.ServiceID},
		{"release", r.ReleaseID}, {"deploy", r.DeployID}, {"crossing", r.Crossing},
	} {
		if required.value == "" {
			return Incident{}, fmt.Errorf("%w: no %s", ErrIncomplete, required.what)
		}
	}

	i := Incident{
		ID:            record.NewID(IDPrefix),
		Actor:         actor,
		At:            record.Now(),
		EnvironmentID: r.EnvironmentID,
		ServiceID:     r.ServiceID,
		ReleaseID:     r.ReleaseID,
		DeployID:      r.DeployID,
		Crossing:      r.Crossing,
		IntentID:      r.IntentID,
		Status:        StatusOpen,
	}
	_, err := w.pool.Exec(ctx, `insert into `+Table+`
		(id, actor_kind, actor_name, at, environment_id, service_id, release_id, deploy_id,
		 crossing, intent_id, observations, status, resolved_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 0, $11, '')`,
		i.ID, string(i.Actor.Kind), i.Actor.Name, i.At,
		i.EnvironmentID, i.ServiceID, i.ReleaseID, i.DeployID,
		i.Crossing, i.IntentID, string(i.Status),
	)
	if err != nil {
		return Incident{}, fmt.Errorf("incident: raising %s on release %s: %w", i.ID, r.ReleaseID, err)
	}
	return i, nil
}

// Observe records a further crossing on an open incident: one more observation
// and nothing else. It is what the comparison does instead of raising a second
// intent, and it is one statement so two concurrent observations both land.
func (w *Writer) Observe(ctx context.Context, id string) (Incident, error) {
	tag, err := w.pool.Exec(ctx, `update `+Table+`
		set observations = observations + 1 where id = $1 and status = $2`,
		id, string(StatusOpen))
	if err != nil {
		return Incident{}, fmt.Errorf("incident: observing on %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return Incident{}, notOpen(ctx, w.pool, id)
	}
	return Get(ctx, w.pool, id)
}

// Resolve advances the incident to resolved and writes the time with it. Its
// caller is what decides that the two conditions hold — the crossing stopped, and
// what was raised from it finished — because both are facts about records this
// package does not read. What it enforces is that an incident resolves once, and
// from open.
func (w *Writer) Resolve(ctx context.Context, id string) (Incident, error) {
	tag, err := w.pool.Exec(ctx, `update `+Table+`
		set status = $1, resolved_at = $2 where id = $3 and status = $4`,
		string(StatusResolved), record.Now(), id, string(StatusOpen))
	if err != nil {
		return Incident{}, fmt.Errorf("incident: resolving %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return Incident{}, notOpen(ctx, w.pool, id)
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

const selectIncident = `select id, actor_kind, actor_name, at, environment_id, service_id,
	release_id, deploy_id, crossing, intent_id, observations, status, resolved_at
	from ` + Table

func scan(row pgx.Row) (Incident, error) {
	var i Incident
	var kind, status string
	err := row.Scan(&i.ID, &kind, &i.Actor.Name, &i.At, &i.EnvironmentID, &i.ServiceID,
		&i.ReleaseID, &i.DeployID, &i.Crossing, &i.IntentID, &i.Observations, &status, &i.ResolvedAt)
	if err != nil {
		return Incident{}, err
	}
	i.Actor.Kind = record.Kind(kind)
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
// there is none. It is what the comparison keys its deduplication on: an open one
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
