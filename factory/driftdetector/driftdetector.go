package driftdetector

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

// Actor is who the drift detector's rows are written as. It is a component
// like any other: what makes this store independent is that no factory
// component may write it, not that its rows have no author.
var Actor = record.Actor{Kind: record.KindComponent, Key: "driftdetector", Basis: record.BasisClaimed}

var (
	// ErrPassIncomplete is returned by [Writer.Record] for a pass missing something
	// every comparison names, or for an unreached target with no reason.
	ErrPassIncomplete = errors.New("driftdetector: the pass is missing something every comparison names")
	// ErrNotFound is returned where no mismatch has that id.
	ErrNotFound = errors.New("driftdetector: no mismatch has that id")
	// ErrAlreadyCleared is returned by [Writer.Clear] for a mismatch already
	// cleared. A mismatch is cleared once, by the human who read the evidence.
	ErrAlreadyCleared = errors.New("driftdetector: the mismatch is cleared already")
	// ErrClearedByEmpty is returned by [Writer.Clear] naming no human. Clearing is a
	// human's act at the drift detector and the record says whose.
	ErrClearedByEmpty = errors.New("driftdetector: a mismatch is cleared by a named human")
)

// Mismatch is one disagreement: either what a production target runs against
// what the factory recorded as that service's current release ([Kind] is
// [MismatchKindTarget]), or the log's chain no longer holding the head this
// store recorded last pass ([Kind] is [MismatchKindChain]).
type Mismatch struct {
	ID    string
	Actor record.Actor
	At    string
	// Kind is which of the two this is. A target mismatch names ServiceID
	// and Target; a chain mismatch names neither, because it holds every
	// service's production deploys at once.
	Kind string
	// ServiceID and Target are what disagreed: the service, and the address the
	// drift detector read. The target is named rather than the environment,
	// which is the stronger check — a deploy record per target would let three
	// targets disagree with a fourth and call each of them right. Both are
	// empty on a [MismatchKindChain] mismatch.
	ServiceID string
	Target    string
	// Detail is the words of a chain or a stale-component mismatch, and is empty
	// on a target mismatch, which composes [Mismatch.Why] from the other fields
	// instead.
	Detail string
	// Component is the factory component whose last check is past the interval
	// it promised, on a [MismatchKindStaleComponent] mismatch, and is empty on
	// every other kind.
	Component string
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

// Why is the mismatch in words a human reads on a gate's open event. It is
// composed here rather than stored, because every part of it is a field and a stored
// sentence would be a second copy able to disagree with them.
func (m Mismatch) Why() string {
	if m.Kind == MismatchKindChain || m.Kind == MismatchKindStaleComponent {
		return HoldWords + ": " + m.Detail
	}
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
const HoldWords = "the drift detector found a record disagreeing with what runs"

// LastCheck is the last check of one production target, overwritten each
// pass. ServiceID is kept for reference; the record is one per target and
// not one per service and target, 08-drift-detection.md:33's "the last
// check per production target".
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
	// DigestReported is whether the target answered the first comparison's
	// digest read at all. False is not disagreement — the last check
	// records it so a target the comparison cannot reach is listed rather
	// than read as agreeing.
	DigestReported bool
	// Interval is what this pass promises the next one within, and
	// FurtherPassOwed is whether a further pass is still owed over this
	// target — the same two fields every last check in the factory carries,
	// so a stopped detector is visible the way a stopped factory component
	// is.
	Interval        time.Duration
	FurtherPassOwed bool
}

// Stale is whether this record has missed a pass as of now: past the
// interval it names, with a further pass owed. It mirrors
// lastcheck.LastCheck.Stale for the one last check this store keeps outside
// the factory.
func (c LastCheck) Stale(now time.Time) (bool, error) {
	if !c.FurtherPassOwed {
		return false, nil
	}
	checked, err := record.ParseTime(c.At)
	if err != nil {
		return false, fmt.Errorf("driftdetector: the time on %s: %w", c.ID, err)
	}
	return now.After(checked.Add(c.Interval)), nil
}

// Pass is one comparison the drift detector performed, as its caller hands
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
	// RunningDigest and RecordedDigest are the first comparison's own: the
	// digest of the artifact the target reports running, against the
	// digest the release's build names for the artifact the build runner
	// produced. RunningDigest empty means the target reported none, and the
	// comparison falls back to the build id alone.
	RunningDigest  string
	RecordedDigest string
	// Excused is a running build the caller says an open window accounts for, as the
	// release under watch or as the control that window's deploy record names. Set, it
	// makes a disagreement no mismatch. doc.go says why nothing sets it here.
	Excused bool
	// Interval and LastPass are this pass's own last check fields: the
	// interval the next pass is promised within, and whether the writer
	// says no further pass is owed over this target.
	Interval time.Duration
	LastPass bool
}

// Agreed reports whether the target runs what the factory recorded. An
// unreached target agrees with nothing and disagrees with nothing, which is
// why [Writer.Record] asks Reached first. Where both sides report a digest
// the comparison is the digest's — a name says which build a target should
// run and a digest says whether the bytes there are that build's — and where
// either side reports none it falls back to the build id alone.
func (p Pass) Agreed() bool {
	if p.Excused {
		return true
	}
	if p.RunningBuild != p.RecordedBuildID {
		return false
	}
	if p.RunningDigest != "" && p.RecordedDigest != "" {
		return p.RunningDigest == p.RecordedDigest
	}
	return true
}

func (p Pass) validate() error {
	if p.ServiceID == "" || p.Target == "" {
		return fmt.Errorf("%w: service %q, target %q", ErrPassIncomplete, p.ServiceID, p.Target)
	}
	if !p.Reached && p.Why == "" {
		return fmt.Errorf("%w: an unreached target says why", ErrPassIncomplete)
	}
	if p.Interval <= 0 {
		return fmt.Errorf("%w: a pass names the interval the next one is promised within", ErrPassIncomplete)
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

// Writer is the one writer of both records: the drift detector's own process. No
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
// says the drift detector ran, and a pass that failed to write a mismatch
// afterwards is still a pass that happened — where the other order would leave a
// stopped drift detector and a raised mismatch indistinguishable from one
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
		DigestReported:    p.RunningDigest != "",
		Interval:          p.Interval,
		FurtherPassOwed:   !p.LastPass,
	}
	_, err := w.pool.Exec(ctx, `insert into `+LastCheckTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, service_id, target, reached, why,
		 running_build, recorded_release_id, recorded_build_id, agreed, digest_reported, interval_seconds, further_pass_owed)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		on conflict (target) do update set
			at = excluded.at, service_id = excluded.service_id, reached = excluded.reached, why = excluded.why,
			running_build = excluded.running_build,
			recorded_release_id = excluded.recorded_release_id,
			recorded_build_id = excluded.recorded_build_id,
			agreed = excluded.agreed,
			digest_reported = excluded.digest_reported,
			interval_seconds = excluded.interval_seconds,
			further_pass_owed = excluded.further_pass_owed`,
		c.ID, FormatVersionLastCheck, string(c.Actor.Kind), c.Actor.Key, string(c.Actor.Basis), c.At, c.ServiceID, c.Target,
		c.Reached, c.Why, c.RunningBuild, c.RecordedReleaseID, c.RecordedBuildID, c.Agreed,
		c.DigestReported, int64(c.Interval/time.Second), c.FurtherPassOwed,
	)
	if err != nil {
		return Recorded{}, fmt.Errorf("driftdetector: recording the last check of %s on %s: %w",
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
			return recorded, fmt.Errorf("driftdetector: recording a later agreement on %s: %w", standing.ID, err)
		}
		recorded.Agreed = standing.ID
	case !agreed && !found:
		m := Mismatch{
			ID:                record.NewID(MismatchIDPrefix),
			Actor:             Actor,
			At:                record.Now(),
			Kind:              MismatchKindTarget,
			ServiceID:         p.ServiceID,
			Target:            p.Target,
			RunningBuild:      p.RunningBuild,
			RecordedReleaseID: p.RecordedReleaseID,
			RecordedBuildID:   p.RecordedBuildID,
		}
		_, err := w.pool.Exec(ctx, `insert into `+MismatchTable+`
			(id, format_version, actor_kind, actor_key, actor_key_basis, at, kind, service_id, target, running_build,
			 recorded_release_id, recorded_build_id, detail, later_agreements, cleared_at, cleared_by)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, '', 0, '', '')`,
			m.ID, FormatVersionMismatch, string(m.Actor.Kind), m.Actor.Key, string(m.Actor.Basis), m.At, m.Kind, m.ServiceID, m.Target,
			m.RunningBuild, m.RecordedReleaseID, m.RecordedBuildID,
		)
		if err != nil {
			return recorded, fmt.Errorf("driftdetector: raising a mismatch on %s: %w", p.Target, err)
		}
		recorded.Raised = m.ID
	}
	return recorded, nil
}

// StaleComponent is what the third comparison found: a factory component whose
// last check is past the interval it names with a further pass owed, and what
// its stopping holds. The health monitor's record is per service, so its
// mismatch names that service and no target; the deployer's is per target of an
// environment and holds that environment's production deploys, which is one
// mismatch per service in it, each naming the target as well.
type StaleComponent struct {
	Component string
	ServiceID string
	Target    string
	Why       string
}

// RaiseStaleComponent is the third comparison's own write. A component that
// stopped is not evidence about the software, so this stays inside the rule the
// other two keep: it holds what the stopped component reaches and the detector
// still cannot cause anything to be built, deployed, scored, approved, or
// measured.
//
// One uncleared mismatch stands per component, service and target: a later pass
// finding the same component still stale raises nothing, the way a target that
// still disagrees records an agreement rather than a second row.
func (w *Writer) RaiseStaleComponent(ctx context.Context, s StaleComponent) (string, error) {
	if s.Component == "" || s.ServiceID == "" || s.Why == "" {
		return "", fmt.Errorf("%w: a stale component names the component, the service it holds, and what was found",
			ErrPassIncomplete)
	}
	var standing string
	err := w.pool.QueryRow(ctx, `select id from `+MismatchTable+`
		where kind = $1 and component = $2 and service_id = $3 and target = $4 and cleared_at = ''
		order by at limit 1`,
		MismatchKindStaleComponent, s.Component, s.ServiceID, s.Target).Scan(&standing)
	if err == nil {
		return "", nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("driftdetector: reading the standing mismatch on %s: %w", s.Component, err)
	}

	m := Mismatch{
		ID: record.NewID(MismatchIDPrefix), Actor: Actor, At: record.Now(),
		Kind: MismatchKindStaleComponent, ServiceID: s.ServiceID, Target: s.Target,
		Component: s.Component, Detail: s.Why,
	}
	_, err = w.pool.Exec(ctx, `insert into `+MismatchTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, kind, service_id, target, component,
		 running_build, recorded_release_id, recorded_build_id, detail, later_agreements, cleared_at, cleared_by)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, '', '', '', $11, 0, '', '')`,
		m.ID, FormatVersionMismatch, string(m.Actor.Kind), m.Actor.Key, string(m.Actor.Basis), m.At,
		m.Kind, m.ServiceID, m.Target, m.Component, m.Detail,
	)
	if err != nil {
		return "", fmt.Errorf("driftdetector: raising a mismatch on the stopped %s: %w", s.Component, err)
	}
	return m.ID, nil
}

// RaiseChainMismatch is the second comparison's own write: the log's chain no
// longer holds the head this store recorded last pass, extended and nothing
// else. It holds every service's production deploys and not one, so it names
// neither — [MismatchKindChain] — and a chain mismatch already standing is
// left alone rather than raised twice.
func (w *Writer) RaiseChainMismatch(ctx context.Context, why string) (string, error) {
	standing, err := UnclearedChain(ctx, w.pool)
	if err != nil {
		return "", err
	}
	if len(standing) > 0 {
		return "", nil
	}
	m := Mismatch{
		ID: record.NewID(MismatchIDPrefix), Actor: Actor, At: record.Now(), Kind: MismatchKindChain, Detail: why,
	}
	_, err = w.pool.Exec(ctx, `insert into `+MismatchTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, kind, service_id, target, running_build,
		 recorded_release_id, recorded_build_id, detail, later_agreements, cleared_at, cleared_by)
		values ($1, $2, $3, $4, $5, $6, $7, '', '', '', '', '', $8, 0, '', '')`,
		m.ID, FormatVersionMismatch, string(m.Actor.Kind), m.Actor.Key, string(m.Actor.Basis), m.At, m.Kind, why,
	)
	if err != nil {
		return "", fmt.Errorf("driftdetector: raising a chain mismatch: %w", err)
	}
	return m.ID, nil
}

// Clear is a human at the drift detector clearing one mismatch. It is here
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
		return Mismatch{}, fmt.Errorf("driftdetector: clearing %s: %w", id, err)
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
