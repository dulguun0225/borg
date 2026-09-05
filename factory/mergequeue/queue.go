package mergequeue

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
)

// Actor is who the queue's writes are made as. The rejection row and every wait
// it opens name it as the caller and as the actor, the design's arrangement for
// something that happened where no gate fired.
var Actor = record.Actor{Kind: record.KindComponent, Key: "merge_queue"}

// healthMonitorActorKey is the key the health monitor's own actor carries,
// spelled here rather than imported. One of the two items the halt's stop passes
// is an item the health monitor raised, which is read off the intent the item was
// decomposed from, and importing that component for one string would be an edge
// deps.txt has no reason for. The spelling is the one healthmonitor.Actor uses,
// so a change to it is found by one search.
const healthMonitorActorKey = "health_monitor"

// lockName is what [AdvisoryLockKey] hashes, the service id appended. It names
// this package so that no other part of the factory derives the same key from a
// name of its own — in particular not package release, whose mint takes a lock of
// its own inside the transaction this one's holder calls it from.
const lockName = "borg/factory/mergequeue/"

// AdvisoryLockKey is the PostgreSQL advisory lock a run and an acceptance hold
// for the whole of their work, one key per service: the first eight bytes of
// SHA-256 of [lockName] plus the service id, big-endian, with the top bit cleared
// so the value is positive. Per service rather than one key, because two
// services' merges have nothing to serialise against each other for.
func AdvisoryLockKey(serviceID string) int64 {
	sum := sha256.Sum256([]byte(lockName + serviceID))
	return int64(binary.BigEndian.Uint64(sum[:8]) & 0x7fffffffffffffff)
}

var (
	// ErrServiceIDEmpty is returned by [Queue.Run], [Queue.Members] and
	// [Queue.AcceptCommit] for a call naming no service. The order is per
	// service, so a run over every service at once would be a queue the design
	// does not have.
	ErrServiceIDEmpty = errors.New("mergequeue: the service id is empty")
	// ErrCommitEmpty is returned by [Queue.AcceptCommit] for an acceptance
	// naming no commit.
	ErrCommitEmpty = errors.New("mergequeue: the commit is empty")
	// ErrNotAHuman is returned by [Queue.AcceptCommit] for an acceptance a
	// component made. A commit the queue did not put on master is accepted by a
	// human at Work and by nothing else.
	ErrNotAHuman = errors.New("mergequeue: a commit the queue did not make is accepted by a human")
	// ErrNoWaitStanding is returned by [Queue.AcceptCommit] where no wait of the
	// queue's stands over that service and commit: either master never held a
	// commit the queue did not make, or the wait has already ended.
	ErrNoWaitStanding = errors.New("mergequeue: no wait of the queue's stands over that commit")
)

// Composition is what the queue is built from. It is a struct rather than eight
// arguments so that every one is named where the queue is composed, and so that
// a factory composed without a reader of the health monitor's store, of the
// design system constraint records, or of the rollbacks says which of them it is
// without.
type Composition struct {
	Pool     *pgxpool.Pool
	Token    lease.Token
	Log      *decisionlog.Writer
	Releases *release.Writer
	// Repository is everything done to a repository and a candidate's
	// environment. Every run reads master through it, so it is required.
	Repository Repository
	// Numbers is the second reading a mint takes. A nil value is [NoNumbersSeen].
	Numbers Numbers
	// DesignSystem reads two design system constraint records. A nil value is
	// [EveryMoveDiffers].
	DesignSystem DesignSystem
	// Backlog is how many releases wait behind a rollback hold. A nil value is
	// [NoBacklog].
	Backlog Backlog
}

// Queue is the merge queue over one factory.
type Queue struct {
	pool         *pgxpool.Pool
	token        lease.Token
	log          *decisionlog.Writer
	releases     *release.Writer
	repo         Repository
	numbers      Numbers
	designSystem DesignSystem
	backlog      Backlog
}

// New returns the queue over one composition, with the three optional readings
// replaced by the value that says a factory was composed without them.
func New(c Composition) *Queue {
	if c.Numbers == nil {
		c.Numbers = NoNumbersSeen{}
	}
	if c.DesignSystem == nil {
		c.DesignSystem = EveryMoveDiffers{}
	}
	if c.Backlog == nil {
		c.Backlog = NoBacklog{}
	}
	return &Queue{
		pool: c.Pool, token: c.Token, log: c.Log, releases: c.Releases, repo: c.Repository,
		numbers: c.Numbers, designSystem: c.DesignSystem, backlog: c.Backlog,
	}
}

// Outcome is what the queue did with one candidate. Merged names the release it
// minted; a rejection names what the re-verification found, which of the three
// readings it was, and the log row that says so; a stop names the condition that
// held the candidate and the wait row that stands for it.
//
// The item's own transition is not written here. The queue's row in
// ../../end-goal/components.md names the gate component, the build runner and the
// log, and names no dispatch, so what the outcome causes on the item — merged
// after a fast-forward, [Rejection.ReturnsTo] with an attempt counted there after
// a rejection — is written by the caller.
type Outcome struct {
	ItemID  string
	Merged  bool
	Release release.Release
	BuildID string
	Commit  string
	Why     string
	// Stopped is the condition that held this candidate, and is empty where the
	// queue reached it. WaitRow is the log row that stop stands as.
	Stopped string
	WaitRow string
	// Published is what the fast-forward did to each contract the build declares:
	// the contract created where this is its first release, the version minted
	// where the form moved, and what the diff was. It is empty on a rejection and
	// on a merge of a build that declares no contract, which is most of them.
	Published []contract.Published
	// Rejection is what the queue read the failure as and wrote into the log, and
	// is the zero value on a merge and on a stop.
	Rejection Rejection
	// SkippedNumbers is the numbers the mint passed over, which happens where the
	// health monitor's store names a higher number than the records do. It is
	// empty at every other mint.
	SkippedNumbers []int64
}

// Pass is what one run of the queue over one service did: what it read of master
// before it minted anything, the stop that held the whole service where one
// stood, and one outcome per candidate it reached.
type Pass struct {
	ServiceID string
	// Master is the reading the queue made of master against the service's
	// release records, at the start and before every mint.
	Master Master
	// Stopped is why the queue fast-forwarded nothing at all for this service,
	// and is empty where it ran. StopWaitRow is the log row that stop stands as.
	Stopped     string
	StopWaitRow string
	Outcomes    []Outcome
}

// lock takes the session-level advisory lock a run and an acceptance hold for the
// whole of their work, and answers with what releases it and the connection.
func (q *Queue) lock(ctx context.Context, serviceID string) (func(), error) {
	conn, err := q.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("mergequeue: taking a connection for %s: %w", serviceID, err)
	}
	key := AdvisoryLockKey(serviceID)
	if _, err := conn.Exec(ctx, `select pg_advisory_lock($1)`, key); err != nil {
		conn.Release()
		return nil, fmt.Errorf("mergequeue: taking the run lock for %s: %w", serviceID, err)
	}
	return func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), `select pg_advisory_unlock($1)`, key)
		conn.Release()
	}, nil
}
