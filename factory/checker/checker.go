package checker

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

// Actor is who the independent checker's rows are written as. It is a component
// like any other: what makes this store independent is that no factory
// component may write it, not that its rows have no author.
var Actor = record.Actor{Kind: record.KindComponent, Name: "checker"}

var (
	// ErrPassIncomplete is returned by [Writer.Record] for a pass missing something
	// every comparison names, or for an unreached target with no reason.
	ErrPassIncomplete = errors.New("checker: the pass is missing something every comparison names")
	// ErrNotFound is returned where no mismatch has that id.
	ErrNotFound = errors.New("checker: no mismatch has that id")
	// ErrAlreadyCleared is returned by [Writer.Clear] for a mismatch already
	// cleared. A mismatch is cleared once, by the human who read the evidence.
	ErrAlreadyCleared = errors.New("checker: the mismatch is cleared already")
	// ErrClearedByEmpty is returned by [Writer.Clear] naming no human. Clearing is a
	// human's act at the independent checker and the record says whose.
	ErrClearedByEmpty = errors.New("checker: a mismatch is cleared by a named human")
)

// Mismatch is one disagreement between what a production target runs and what the
// factory recorded as that service's current release.
type Mismatch struct {
	ID    string
	Actor record.Actor
	At    string
	// ServiceID and Target are what disagreed: the service, and the address the
	// independent checker read. The target is named rather than the environment,
	// which is the stronger check — a deploy record per target would let three
	// targets disagree with a fourth and call each of them right.
	ServiceID string
	Target    string
	// RunningBuild is what the target said it runs, and is empty where it runs
	// nothing at all — which is a mismatch like any other against a factory that says
	// a release is live.
	RunningBuild string
	// RecordedReleaseID and RecordedBuildID are what the factory's production deploy
	// record named. Both are empty where the factory recorded nothing running, which
	// is a mismatch against a target that is running something.
	RecordedReleaseID string
	RecordedBuildID   string
	// LaterAgreements is how many passes after this one agreed. A mismatch remains
	// until a human clears it even where a later comparison agrees, and this is that
	// recorded on it so the human clearing has the evidence.
	LaterAgreements int
	ClearedAt       string
	ClearedBy       string
}

// Cleared reports whether a human has cleared the mismatch. An uncleared one holds
// that service's production deploys.
func (m Mismatch) Cleared() bool { return m.ClearedAt != "" }

// Why is the mismatch in words a human reads on a gate's opening row. It is
// composed here rather than stored, because every part of it is a field and a stored
// sentence would be a second copy able to disagree with them.
func (m Mismatch) Why() string {
	running := "nothing"
	if m.RunningBuild != "" {
		running = "build " + m.RunningBuild
	}
	recorded := "nothing running"
	if m.RecordedBuildID != "" {
		recorded = "build " + m.RecordedBuildID + " as release " + m.RecordedReleaseID
	}
	return fmt.Sprintf("%s: the target %s runs %s and the factory recorded %s",
		HoldWords, m.Target, running, recorded)
}

// HoldWords opens every mismatch's own words. It is here rather than in package
// gate because the sentence is composed from this record's fields, and gate's own
// constant for the hold names the condition rather than one instance of it.
const HoldWords = "the independent checker found a record disagreeing with what runs"

// LastCheck is the last check of one service on one target, overwritten each
// pass.
type LastCheck struct {
	ID                string
	Actor             record.Actor
	At                string
	ServiceID         string
	Target            string
	Reached           bool
	Why               string
	RunningBuild      string
	RecordedReleaseID string
	RecordedBuildID   string
	Agreed            bool
}

// Pass is one comparison the independent checker performed, as its caller hands
// it over.
type Pass struct {
	ServiceID string
	Target    string
	// Reached is whether the target answered at all. Failing to reach one is not a
	// mismatch — a network blip would otherwise hold every production deploy — so an
	// unreached pass writes the last check and nothing else.
	Reached bool
	// Why is why it could not be reached, required where Reached is false.
	Why               string
	RunningBuild      string
	RecordedReleaseID string
	RecordedBuildID   string
	// Excused is a running build the caller says an open window accounts for, as the
	// release under watch or as the control that window's deploy record names. Set, it
	// makes a disagreement no mismatch. doc.go says why nothing sets it here.
	Excused bool
}

// Agreed reports whether the target runs what the factory recorded. An unreached
// target agrees with nothing and disagrees with nothing, which is why [Writer.Record]
// asks Reached first.
func (p Pass) Agreed() bool { return p.Excused || p.RunningBuild == p.RecordedBuildID }

func (p Pass) validate() error {
	if p.ServiceID == "" || p.Target == "" {
		return fmt.Errorf("%w: service %q, target %q", ErrPassIncomplete, p.ServiceID, p.Target)
	}
	if !p.Reached && p.Why == "" {
		return fmt.Errorf("%w: an unreached target says why", ErrPassIncomplete)
	}
	return nil
}

// Recorded is what one pass wrote: the last check always, and one of a new
// mismatch, a later agreement on the one standing, or neither.
type Recorded struct {
	LastCheck LastCheck
	// Raised is the mismatch this pass wrote, and is empty where it wrote none.
	Raised string
	// Agreed is the standing mismatch this pass recorded an agreement on, and is
	// empty where there was none to record one on.
	Agreed string
}

// Writer is the one writer of both records: the independent checker's own process. No
// factory component holds one, which is the whole of what "a store no factory
// component may write" is enforced by here — that, and the store being reached
// through a URL of its own.
type Writer struct {
	pool *pgxpool.Pool
}

// NewWriter returns the writer over pool.
func NewWriter(pool *pgxpool.Pool) *Writer { return &Writer{pool: pool} }

// Record writes what one pass found: the last check, overwritten; a mismatch
// where the target disagrees and none already stands for that service and target; and
// a later agreement on the one standing where the pass agrees.
//
// The last check is written first and unconditionally. It is the record that
// says the independent checker ran, and a pass that failed to write a mismatch
// afterwards is still a pass that happened — where the other order would leave a
// stopped independent checker and a raised mismatch indistinguishable from one
// that never ran.
func (w *Writer) Record(ctx context.Context, p Pass) (Recorded, error) {
	if err := p.validate(); err != nil {
		return Recorded{}, err
	}

	agreed := p.Reached && p.Agreed()
	c := LastCheck{
		ID:                record.NewID(LastCheckIDPrefix),
		Actor:             Actor,
		At:                record.Now(),
		ServiceID:         p.ServiceID,
		Target:            p.Target,
		Reached:           p.Reached,
		Why:               p.Why,
		RunningBuild:      p.RunningBuild,
		RecordedReleaseID: p.RecordedReleaseID,
		RecordedBuildID:   p.RecordedBuildID,
		Agreed:            agreed,
	}
	_, err := w.pool.Exec(ctx, `insert into `+LastCheckTable+`
		(id, actor_kind, actor_name, at, service_id, target, reached, why,
		 running_build, recorded_release_id, recorded_build_id, agreed)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		on conflict (service_id, target) do update set
			at = excluded.at, reached = excluded.reached, why = excluded.why,
			running_build = excluded.running_build,
			recorded_release_id = excluded.recorded_release_id,
			recorded_build_id = excluded.recorded_build_id,
			agreed = excluded.agreed`,
		c.ID, string(c.Actor.Kind), c.Actor.Name, c.At, c.ServiceID, c.Target,
		c.Reached, c.Why, c.RunningBuild, c.RecordedReleaseID, c.RecordedBuildID, c.Agreed,
	)
	if err != nil {
		return Recorded{}, fmt.Errorf("checker: recording the last check of %s on %s: %w",
			p.ServiceID, p.Target, err)
	}
	recorded := Recorded{LastCheck: c}

	if !p.Reached {
		return recorded, nil
	}
	standing, found, err := unclearedOn(ctx, w.pool, p.ServiceID, p.Target)
	if err != nil {
		return recorded, err
	}
	switch {
	case agreed && found:
		if _, err := w.pool.Exec(ctx, `update `+MismatchTable+`
			set later_agreements = later_agreements + 1 where id = $1`, standing.ID); err != nil {
			return recorded, fmt.Errorf("checker: recording a later agreement on %s: %w", standing.ID, err)
		}
		recorded.Agreed = standing.ID
	case !agreed && !found:
		m := Mismatch{
			ID:                record.NewID(MismatchIDPrefix),
			Actor:             Actor,
			At:                record.Now(),
			ServiceID:         p.ServiceID,
			Target:            p.Target,
			RunningBuild:      p.RunningBuild,
			RecordedReleaseID: p.RecordedReleaseID,
			RecordedBuildID:   p.RecordedBuildID,
		}
		_, err := w.pool.Exec(ctx, `insert into `+MismatchTable+`
			(id, actor_kind, actor_name, at, service_id, target, running_build,
			 recorded_release_id, recorded_build_id, later_agreements, cleared_at, cleared_by)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, 0, '', '')`,
			m.ID, string(m.Actor.Kind), m.Actor.Name, m.At, m.ServiceID, m.Target,
			m.RunningBuild, m.RecordedReleaseID, m.RecordedBuildID,
		)
		if err != nil {
			return recorded, fmt.Errorf("checker: raising a mismatch on %s: %w", p.Target, err)
		}
		recorded.Raised = m.ID
	}
	return recorded, nil
}

// Clear is a human at the independent checker clearing one mismatch. It is here
// and there is no counterpart in the factory: clearing it from Ops would make
// the factory a writer of the record that says the factory is wrong.
//
// What that costs is two actions in two places at the moment production is worst —
// approve through at the gate, then clear here.
func (w *Writer) Clear(ctx context.Context, id, by string) (Mismatch, error) {
	if by == "" {
		return Mismatch{}, ErrClearedByEmpty
	}
	tag, err := w.pool.Exec(ctx, `update `+MismatchTable+`
		set cleared_at = $1, cleared_by = $2 where id = $3 and cleared_at = ''`,
		record.Now(), by, id)
	if err != nil {
		return Mismatch{}, fmt.Errorf("checker: clearing %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		m, err := Get(ctx, w.pool, id)
		if err != nil {
			return Mismatch{}, err
		}
		return Mismatch{}, fmt.Errorf("%w: %s at %s by %s", ErrAlreadyCleared, id, m.ClearedAt, m.ClearedBy)
	}
	return Get(ctx, w.pool, id)
}
