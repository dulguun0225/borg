package environment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

// Reason is why a compose-and-reclaim cycle ended. Three of the four are the
// teardown-for-good events, which end the environment with the cycle; the fourth
// is a reclamation, which ends the cycle and leaves the environment composable
// again.
type Reason string

const (
	// ReasonReclaimed is the deployer reclaiming an environment from an item
	// running nothing: an item stopped above Implementation, sent back to Spec,
	// waiting on its intent's interview, or escalated with nobody having taken it
	// over. The branch and the builds persist.
	ReasonReclaimed Reason = "reclaimed"
	// ReasonMerged is the item merging.
	ReasonMerged Reason = "merged"
	// ReasonDropped is the item being dropped.
	ReasonDropped Reason = "dropped"
	// ReasonSuperseded is the item being superseded by a re-decomposition.
	ReasonSuperseded Reason = "superseded"
)

// Reasons is every reason a cycle ends. The CHECK in [DDL] lists the same ones.
var Reasons = []Reason{ReasonReclaimed, ReasonMerged, ReasonDropped, ReasonSuperseded}

// ForGood is whether the reason ends the environment and not only the cycle.
func (r Reason) ForGood() bool {
	return r == ReasonMerged || r == ReasonDropped || r == ReasonSuperseded
}

// ErrReasonUnknown is returned by [Candidates.TearDown] for a reason outside
// [Reasons].
var ErrReasonUnknown = errors.New("environment: the reason is not one a cycle ends for")

// Rate is the service record's environment-hour rate as it was at the write. The
// converted amount is computed from it there and fixed, never repriced later, so
// a rate that was not in force is a cycle with no amount rather than an amount of
// nothing.
type Rate struct {
	PerHour float64
	InForce bool
}

// Cycle is one compose-and-reclaim cycle of one candidate's environment: when
// the deployer began composing it, when the run could start, when it was torn
// down and why, and what that span converted to where a rate was in force.
type Cycle struct {
	ID              string
	Actor           record.Actor
	At              string
	EnvironmentID   string
	BeganAt         string
	RunCouldStartAt string
	TornDownAt      string
	Reason          Reason
	Rate            Rate
	ConvertedAmount float64
}

// Open is whether the cycle has not been torn down. An environment has at most
// one open cycle, which the partial unique index in [DDL] is what enforces.
func (c Cycle) Open() bool { return c.TornDownAt == "" }

// Hours is composition start to teardown in hours, and composition start to now
// for a cycle still open. It is what environment-hours is summed from.
func (c Cycle) Hours(now time.Time) (float64, error) {
	began, err := record.ParseTime(c.BeganAt)
	if err != nil {
		return 0, fmt.Errorf("environment: the start of cycle %s: %w", c.ID, err)
	}
	end := now
	if !c.Open() {
		end, err = record.ParseTime(c.TornDownAt)
		if err != nil {
			return 0, fmt.Errorf("environment: the end of cycle %s: %w", c.ID, err)
		}
	}
	return end.Sub(began).Hours(), nil
}

// EnvironmentHours is composition start to teardown summed across every cycle,
// which is what an environment per candidate costs in hosting — counted rather
// than modelled.
func EnvironmentHours(cycles []Cycle, now time.Time) (float64, error) {
	var total float64
	for _, c := range cycles {
		hours, err := c.Hours(now)
		if err != nil {
			return 0, err
		}
		total += hours
	}
	return total, nil
}

// TearDown ends the cycle in progress and, where the reason is one of the three
// teardown-for-good events, the environment with it. A reclamation leaves the
// environment standing: [Candidates.Recompose] composes it again and opens the
// next cycle.
//
// Its caller stops the software first: the record and the process are two facts,
// and a record saying torn down over a process still running is the disagreement
// the drift detector exists to find — which for a candidate environment is what
// the deployer's own pass over the platform finds, the detector reading
// production targets alone.
//
// Where rate is in force the converted amount for this cycle's span is computed
// here and written beside the timestamps, fixed at the write and never repriced.
func (c *Candidates) TearDown(ctx context.Context, actor record.Actor, id string, reason Reason, rate Rate) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if !containsReason(Reasons, reason) {
		return fmt.Errorf("%w: %q", ErrReasonUnknown, reason)
	}
	return c.write(ctx, id, "tearing down", func(tx pgx.Tx, e Environment) error {
		cycle, err := openCycleOf(ctx, tx, id)
		if err != nil {
			return err
		}
		at := record.Now()
		cycle.TornDownAt = at
		// The cycle is closed above, so the moment passed here is not read.
		hours, err := cycle.Hours(time.Now())
		if err != nil {
			return err
		}
		amount := 0.0
		if rate.InForce {
			amount = hours * rate.PerHour
		}
		if _, err := tx.Exec(ctx, `update `+CycleTable+`
			set torn_down_at = $1, reason = $2, priced = $3, rate_per_hour = $4, converted_amount = $5
			where id = $6`,
			at, string(reason), rate.InForce, rate.PerHour, amount, cycle.ID); err != nil {
			return fmt.Errorf("environment: ending cycle %s: %w", cycle.ID, err)
		}
		if !reason.ForGood() {
			return nil
		}
		if _, err := tx.Exec(ctx, `update `+Table+`
			set torn_down_at = $1, torn_down_reason = $2 where id = $3`, at, string(reason), id); err != nil {
			return fmt.Errorf("environment: tearing down %s: %w", id, err)
		}
		return nil
	})
}

const selectCycle = `select id, actor_kind, actor_key, actor_key_basis, at, environment_id,
	began_at, run_could_start_at, torn_down_at, reason, priced, rate_per_hour, converted_amount
	from ` + CycleTable

// Cycles is every compose-and-reclaim cycle of one environment, oldest first. It
// takes the pool and not a writer, because reading what an environment cost is
// not a reason to hold the thing that composes them.
func Cycles(ctx context.Context, pool *pgxpool.Pool, environmentID string) ([]Cycle, error) {
	rows, err := pool.Query(ctx, selectCycle+` where environment_id = $1 order by began_at, id`, environmentID)
	if err != nil {
		return nil, fmt.Errorf("environment: reading the cycles of %s: %w", environmentID, err)
	}
	defer rows.Close()

	var cycles []Cycle
	for rows.Next() {
		cycle, err := scanCycle(rows)
		if err != nil {
			return nil, err
		}
		cycles = append(cycles, cycle)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("environment: reading the cycles of %s: %w", environmentID, err)
	}
	return cycles, nil
}

// openCycle writes the row that says composing began now. The partial unique
// index is what refuses a second open cycle on one environment.
func openCycle(ctx context.Context, tx pgx.Tx, actor record.Actor, environmentID string) error {
	began := record.Now()
	_, err := tx.Exec(ctx, `insert into `+CycleTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, environment_id,
		 began_at, run_could_start_at, torn_down_at, reason, priced, rate_per_hour, converted_amount)
		values ($1, $2, $3, $4, $5, $6, $7, $8, '', '', '', false, 0, 0)`,
		record.NewID(CycleIDPrefix), FormatVersionCycle, string(actor.Kind), actor.Key, string(actor.Basis),
		began, environmentID, began,
	)
	if err != nil {
		return fmt.Errorf("environment: opening a cycle on %s: %w", environmentID, err)
	}
	return nil
}

// openCycleOf is the cycle in progress on one environment, locked for update, and
// [ErrNoOpenCycle] where the environment was reclaimed and not composed again.
func openCycleOf(ctx context.Context, tx pgx.Tx, environmentID string) (Cycle, error) {
	cycle, err := scanCycle(tx.QueryRow(ctx, selectCycle+`
		where environment_id = $1 and torn_down_at = '' for update`, environmentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Cycle{}, fmt.Errorf("%w: %s", ErrNoOpenCycle, environmentID)
	}
	return cycle, err
}

func scanCycle(row pgx.Row) (Cycle, error) {
	var c Cycle
	var actorKind, actorBasis, reason string
	err := row.Scan(&c.ID, &actorKind, &c.Actor.Key, &actorBasis, &c.At, &c.EnvironmentID,
		&c.BeganAt, &c.RunCouldStartAt, &c.TornDownAt, &reason,
		&c.Rate.InForce, &c.Rate.PerHour, &c.ConvertedAmount)
	if err != nil {
		return Cycle{}, err
	}
	c.Actor.Kind = record.Kind(actorKind)
	c.Actor.Basis = record.Basis(actorBasis)
	c.Reason = Reason(reason)
	return c, nil
}

func containsReason(reasons []Reason, reason Reason) bool {
	for _, r := range reasons {
		if r == reason {
			return true
		}
	}
	return false
}
