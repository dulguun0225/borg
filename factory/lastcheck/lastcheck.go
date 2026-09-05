package lastcheck

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

// The components that write a last check into the factory's own store. The
// seventh the design names is the drift detector's, which is written into the
// detector's own store and never here.
const (
	// ComponentHealthMonitor keeps one per service.
	ComponentHealthMonitor = "health_monitor"
	// ComponentDeployer keeps one per target of a persistent environment and
	// one for each platform a production environment record declares.
	ComponentDeployer = "deployer"
	// ComponentNotifier keeps a single one for itself.
	ComponentNotifier = "notifier"
	// ComponentConstraintsPass keeps a single one for the pass over the
	// constraints in force.
	ComponentConstraintsPass = "constraints_pass"
	// ComponentAdvisoryPass keeps a single one for the pass over the advisory
	// feed.
	ComponentAdvisoryPass = "advisory_pass"
	// ComponentDispatch keeps a single one for the pass that argues a fleet
	// proposal.
	ComponentDispatch = "dispatch"
)

// Components is every component that may write a record into this table. The
// CHECK in [DDL] lists the same ones, and TestDDLListsEveryComponent fails if
// the two stop agreeing.
var Components = []string{
	ComponentHealthMonitor,
	ComponentDeployer,
	ComponentNotifier,
	ComponentConstraintsPass,
	ComponentAdvisoryPass,
	ComponentDispatch,
}

// componentsKeepingASingleRecord are the components whose record is about
// themselves and names no subject. Every other component in [Components] keeps
// one record per thing and names which.
var componentsKeepingASingleRecord = []string{
	ComponentNotifier,
	ComponentConstraintsPass,
	ComponentAdvisoryPass,
	ComponentDispatch,
}

var (
	// ErrComponentUnknown is returned for a component outside [Components].
	ErrComponentUnknown = errors.New("lastcheck: the component is not one that writes a last check into this store")
	// ErrSubjectDoesNotMatchComponent is returned where a component that keeps
	// one record per thing names no subject, or one that keeps a single record
	// for itself names one.
	ErrSubjectDoesNotMatchComponent = errors.New("lastcheck: the subject does not match what the component keeps a record per")
	// ErrIntervalNotPositive is returned for a record naming no interval, or one
	// shorter than the second the column's resolution is. The interval is what a
	// reader that authored nothing holds the record against.
	ErrIntervalNotPositive = errors.New("lastcheck: a last check names the interval its writer promises the next pass within, in whole seconds")
	// ErrNotAComponent is returned for an actor that is not a component. A last
	// check is the writing component's own record of its own pass.
	ErrNotAComponent = errors.New("lastcheck: a last check is a component's record of its own pass")
)

// LastCheck is one component's record of its own most recent pass over one
// subject.
type LastCheck struct {
	ID    string
	Actor record.Actor
	At    string
	// Component is which component's pass this is, one of [Components].
	Component string
	// Subject is what the pass was over — a service id, a target address, or a
	// platform name — and is empty on the record a component keeps for itself.
	Subject string
	// CheckedAt is when the pass ran. It is the field every reader holds against
	// [LastCheck.Interval], and it is written by the writer and never by its
	// caller.
	CheckedAt string
	// Interval is what the writer promises the next pass within. It is on the
	// record because the age that means stopped has to be readable by something
	// that authored nothing. It is stored as whole seconds, so an interval
	// shorter than a second is refused rather than rounded to nothing.
	Interval time.Duration
	// LastPass is the writer saying this pass is the last one owed, which is what
	// a component records over a subject that has gone away — a retired service,
	// a withdrawn environment. It is written rather than owed being written
	// because the zero value of a bool is what a caller that says nothing gets,
	// and a record that silently owed no further pass would be a component that
	// stopped and nothing read it.
	LastPass bool
	// Payload is what the pass reports, as the writer wrote it: the deployer's
	// three counts for a platform, and whatever each other writer counts. This
	// package stores the text and reads nothing inside it.
	Payload string
}

// FurtherPassOwed is whether the thing this record names is still owed a pass. A
// record past the interval it names with a further pass owed is always something
// that stopped, and never something that went away.
func (c LastCheck) FurtherPassOwed() bool { return !c.LastPass }

// Stale is whether this record has missed a pass as of now: past the interval it
// names, with a further pass owed.
func (c LastCheck) Stale(now time.Time) (bool, error) {
	if !c.FurtherPassOwed() {
		return false, nil
	}
	checked, err := record.ParseTime(c.CheckedAt)
	if err != nil {
		return false, fmt.Errorf("lastcheck: the time on %s: %w", c.ID, err)
	}
	return now.After(checked.Add(c.Interval)), nil
}

// Writer is the one writer of this table. Every component in [Components] writes
// through it, each its own rows: the record is about the writing component and
// never about the work, so a component writing another's row is a component
// reporting a pass it did not perform.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewWriter returns the writer over pool, fencing every write with token.
func NewWriter(pool *pgxpool.Pool, token lease.Token) *Writer {
	return &Writer{pool: pool, token: token}
}

// Record writes one pass, overwriting the component's record for that subject.
// The time is the writer's own, so what the record says about when the pass ran
// is what the writer observed; check.CheckedAt, check.ID and check.At are
// ignored and the written record is returned.
//
// The row is overwritten rather than appended to: what every reader asks is when
// the newest pass was, and a history of passes is a table that grows once per
// component per interval forever to answer a question about one row.
func (w *Writer) Record(ctx context.Context, actor record.Actor, check LastCheck) (LastCheck, error) {
	if err := actor.Validate(); err != nil {
		return LastCheck{}, err
	}
	if actor.Kind != record.KindComponent {
		return LastCheck{}, fmt.Errorf("%w: %s", ErrNotAComponent, actor.Kind)
	}
	if !slices.Contains(Components, check.Component) {
		return LastCheck{}, fmt.Errorf("%w: %q", ErrComponentUnknown, check.Component)
	}
	if slices.Contains(componentsKeepingASingleRecord, check.Component) != (check.Subject == "") {
		return LastCheck{}, fmt.Errorf("%w: %s named %q", ErrSubjectDoesNotMatchComponent, check.Component, check.Subject)
	}
	if check.Interval < time.Second {
		return LastCheck{}, fmt.Errorf("%w: %v", ErrIntervalNotPositive, check.Interval)
	}

	written := check
	written.Actor = actor
	written.At = record.Now()
	written.CheckedAt = written.At

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return LastCheck{}, fmt.Errorf("lastcheck: beginning the pass of %s over %q: %w", check.Component, check.Subject, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return LastCheck{}, err
	}

	err = tx.QueryRow(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, component, subject, checked_at, interval_seconds, further_pass_owed, payload)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		on conflict (component, subject) do update set
			actor_kind = excluded.actor_kind,
			actor_key = excluded.actor_key,
			actor_key_basis = excluded.actor_key_basis,
			at = excluded.at,
			checked_at = excluded.checked_at,
			interval_seconds = excluded.interval_seconds,
			further_pass_owed = excluded.further_pass_owed,
			payload = excluded.payload
		returning id`,
		record.NewID(IDPrefix), FormatVersion, string(actor.Kind), actor.Key, string(actor.Basis), written.At,
		written.Component, written.Subject, written.CheckedAt, int64(written.Interval/time.Second),
		written.FurtherPassOwed(), written.Payload,
	).Scan(&written.ID)
	if err != nil {
		return LastCheck{}, fmt.Errorf("lastcheck: recording the pass of %s over %q: %w", check.Component, check.Subject, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return LastCheck{}, fmt.Errorf("lastcheck: committing the pass of %s over %q: %w", check.Component, check.Subject, err)
	}
	return written, nil
}

const selectCheck = `select id, actor_kind, actor_key, actor_key_basis, at, component, subject,
	checked_at, interval_seconds, further_pass_owed, payload
	from ` + Table

// All is every last check in this store, ordered by component and then subject.
// It takes the pool and not a [Writer], because reading the records is not a
// reason to hold the thing that writes them.
func All(ctx context.Context, pool *pgxpool.Pool) ([]LastCheck, error) {
	return query(ctx, pool, selectCheck+` order by component, subject`)
}

// ForComponent is every last check one component keeps, ordered by subject.
func ForComponent(ctx context.Context, pool *pgxpool.Pool, component string) ([]LastCheck, error) {
	return query(ctx, pool, selectCheck+` where component = $1 order by subject`, component)
}

// Get is the record one component keeps over one subject, and false where it
// keeps none. A component that keeps a single record for itself is read with an
// empty subject.
func Get(ctx context.Context, pool *pgxpool.Pool, component, subject string) (LastCheck, bool, error) {
	rows, err := query(ctx, pool, selectCheck+` where component = $1 and subject = $2`, component, subject)
	if err != nil || len(rows) == 0 {
		return LastCheck{}, false, err
	}
	return rows[0], true, nil
}

// Stale is every record past the interval it names with a further pass owed, as
// of now — every component that has stopped, and no component whose subject went
// away. It is what the home view lists and what the drift detector's third
// comparison reads.
//
// The comparison is arithmetic here rather than SQL because the stored time is
// text in [record.TimeLayout] and the interval is a count of seconds: a database
// that added the two would be parsing the format a second time, in a second
// place able to disagree with [record.ParseTime].
func Stale(ctx context.Context, pool *pgxpool.Pool, now time.Time) ([]LastCheck, error) {
	all, err := All(ctx, pool)
	if err != nil {
		return nil, err
	}
	var stale []LastCheck
	for _, c := range all {
		missed, err := c.Stale(now)
		if err != nil {
			return nil, err
		}
		if missed {
			stale = append(stale, c)
		}
	}
	return stale, nil
}

func query(ctx context.Context, pool *pgxpool.Pool, statement string, args ...any) ([]LastCheck, error) {
	rows, err := pool.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("lastcheck: reading the last checks: %w", err)
	}
	defer rows.Close()

	var checks []LastCheck
	for rows.Next() {
		check, err := scan(rows)
		if err != nil {
			return nil, err
		}
		checks = append(checks, check)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("lastcheck: reading the last checks: %w", err)
	}
	return checks, nil
}

func scan(row pgx.Row) (LastCheck, error) {
	var c LastCheck
	var actorKind, actorBasis string
	var seconds int64
	var furtherPassOwed bool
	err := row.Scan(&c.ID, &actorKind, &c.Actor.Key, &actorBasis, &c.At, &c.Component, &c.Subject,
		&c.CheckedAt, &seconds, &furtherPassOwed, &c.Payload)
	if err != nil {
		return LastCheck{}, fmt.Errorf("lastcheck: reading a last check: %w", err)
	}
	c.Actor.Kind = record.Kind(actorKind)
	c.Actor.Basis = record.Basis(actorBasis)
	c.Interval = time.Duration(seconds) * time.Second
	c.LastPass = !furtherPassOwed
	return c, nil
}
