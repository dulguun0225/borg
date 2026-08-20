package mergequeue

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
)

// Actor is who the queue's writes are made as. The rejection row names it as the
// caller and as the actor, the design's arrangement for something that happened
// where no gate fired.
var Actor = record.Actor{Kind: record.KindComponent, Name: "merge_queue"}

// lockName is what [AdvisoryLockKey] hashes, the service id appended. It names
// this package so that no other part of the factory derives the same key from a
// name of its own — in particular not package release, whose mint takes a lock of
// its own inside the transaction this one's holder calls it from.
const lockName = "borg/factory/mergequeue/"

// AdvisoryLockKey is the PostgreSQL advisory lock [Queue.Run] holds for the whole
// of a run, one key per service: the first eight bytes of SHA-256 of [lockName]
// plus the service id, big-endian, with the top bit cleared so the value is
// positive. Per service rather than one key, because two services' merges have
// nothing to serialise against each other for.
func AdvisoryLockKey(serviceID string) int64 {
	sum := sha256.Sum256([]byte(lockName + serviceID))
	return int64(binary.BigEndian.Uint64(sum[:8]) & 0x7fffffffffffffff)
}

// ErrServiceIDEmpty is returned by [Queue.Run] for a run naming no service. The
// order is per service, so a run over every service at once would be a queue the
// design does not have.
var ErrServiceIDEmpty = errors.New("mergequeue: the service id is empty")

// Verified is what a re-verification produced: the commit the candidate branch
// reached once master was merged into it, the build made from that commit, and
// whether every pre-merge check decided against the candidate-environment run
// passed.
//
// A re-verification that changed nothing — master already an ancestor of the
// candidate — names the build already in force rather than a new one: a rebuild is
// a new build, and nothing was rebuilt.
//
// Why is what failed, in words a human reads on the rejection row, and is empty
// where it passed. A merge conflict, a criterion that failed, a breaking contract
// diff, and a consumer's declaration the candidate does not satisfy all arrive here
// as a candidate that failed its own re-verification, which is the same disposition
// for several reasons — so the reason is on the row.
//
// Forms is what the re-verified build publishes, derived from the checkout the
// re-verification produced. The queue does not reach a checkout, so the derivation
// is the deploy agent's and the write is the queue's — which is the same division
// the criteria already have, where the agent decides them on the environment and
// the queue reads what it produced.
type Verified struct {
	Commit  string
	BuildID string
	Passed  bool
	Why     string
	Forms   []contract.Form
}

// Repository is everything the queue needs done to the service's repository and
// the candidate's environment, which the queue does not reach itself. Whatever
// composes the deploy agent implements it.
type Repository interface {
	// Reverify builds the candidate branch onto the master it will actually merge
	// into, recomposes the candidate's environment, puts that build on it, and
	// decides the criteria there. An error is an infrastructure failure and stops
	// the run; a candidate that failed on its merits is a [Verified] with Passed
	// false and Why saying what.
	Reverify(ctx context.Context, it item.Item) (Verified, error)
	// FastForward moves the service's master to the commit the re-verification
	// produced, and refuses anything that is not a fast-forward.
	FastForward(ctx context.Context, it item.Item, commit string) error
}

// Outcome is what the queue did with one candidate. Merged names the release it
// minted; rejected names what the re-verification found and the log row that says
// so.
type Outcome struct {
	ItemID  string
	Merged  bool
	Release release.Release
	BuildID string
	Commit  string
	Why     string
	WaitRow string
	// Published is what the fast-forward did to each contract the build declares:
	// the contract created where this is its first release, the version minted
	// where the form moved, and what the diff was. It is empty on a rejection and
	// on a merge of a build that declares no contract, which is most of them.
	Published []contract.Published
}

// RejectionKind is what a rejection row says it is, so a reader can tell the
// queue's wait rows from every other kind.
const RejectionKind = "merge_queue_rejection"

// RejectionPayload is what the queue writes into the log when a candidate fails
// its own re-verification. It is a wait and not a decision: no gate fired — the
// merge gate's own having closed as an approval — so there is no firing to open a
// row at and no factor vector computed for it.
//
// It says that an attempt was counted, which is the one thing about this row that
// a reader cannot see from the row: the count is on the item's per-stage row, and
// the design counts this rejection against the bound at the stage the item is sent
// back to.
type RejectionPayload struct {
	Kind            string `json:"kind"`
	ItemID          string `json:"item_id"`
	ServiceID       string `json:"service_id"`
	BuildID         string `json:"build_id"`
	Commit          string `json:"commit"`
	Why             string `json:"why"`
	ReturnsTo       string `json:"returns_to"`
	CountsAnAttempt bool   `json:"counts_an_attempt"`
}

// Queue is the merge queue over one factory.
type Queue struct {
	pool     *pgxpool.Pool
	log      *decisionlog.Writer
	releases *release.Writer
	dispatch *item.Dispatch
	repo     Repository
}

// New returns the queue over pool, writing through the log and the release
// writer, and reaching the repository through repo.
func New(pool *pgxpool.Pool, log *decisionlog.Writer, releases *release.Writer,
	dispatch *item.Dispatch, repo Repository) *Queue {
	return &Queue{pool: pool, log: log, releases: releases, dispatch: dispatch, repo: repo}
}

// Members is the queue's membership for one service, in the queue's order: the
// items whose stage says Merge to master approved them and whose fast-forward has
// not happened, ordered by the priority an owner set — greater first — and then by
// the time of that approval in the log.
//
// An item at that stage with no approval in the log is ordered last among its
// priority. It is not a state the path produces, the stage being written on the
// approval; ordering it rather than refusing it is what keeps a reader of the queue
// from being unable to see it at all.
func (q *Queue) Members(ctx context.Context, serviceID string) ([]item.Item, error) {
	if serviceID == "" {
		return nil, ErrServiceIDEmpty
	}
	members, err := item.AtStage(ctx, q.pool, serviceID, item.StageQueued)
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, nil
	}
	approved, err := gate.ApprovalTimes(ctx, q.pool, gate.MergeToMaster)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(members, func(a, b int) bool {
		first, second := members[a], members[b]
		if first.Priority != second.Priority {
			return first.Priority > second.Priority
		}
		firstAt, secondAt := approved[first.ID], approved[second.ID]
		if firstAt == secondAt {
			// The sort is stable and this is the last word, so a tie keeps the
			// order the membership query returned — which is the time the item was
			// cut. Ordering on the id instead would be an order derived from random
			// bytes, and it would throw away the one the store already gave.
			return false
		}
		// An unapproved item's empty time would sort first, and it belongs last:
		// an empty string is less than every timestamp.
		if firstAt == "" || secondAt == "" {
			return secondAt == ""
		}
		return firstAt < secondAt
	})
	return members, nil
}

// Run takes each member of the service's queue in order and re-verifies it
// against the master that actually resulted. A candidate that passes fast-forwards,
// is minted a release, and advances to merged; one that fails goes back to
// Implementation with an attempt counted there and a row in the log saying why, and
// the run goes on to the next.
//
// The whole run holds one advisory lock per service, so two runs of one service
// cannot interleave. It is session-level and taken on one connection of its own,
// because a run performs git work and several transactions and a transaction held
// across all of it would hold row locks nothing needs held.
func (q *Queue) Run(ctx context.Context, serviceID string) ([]Outcome, error) {
	if serviceID == "" {
		return nil, ErrServiceIDEmpty
	}
	conn, err := q.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("mergequeue: taking a connection for the run of %s: %w", serviceID, err)
	}
	defer conn.Release()
	key := AdvisoryLockKey(serviceID)
	if _, err := conn.Exec(ctx, `select pg_advisory_lock($1)`, key); err != nil {
		return nil, fmt.Errorf("mergequeue: taking the run lock for %s: %w", serviceID, err)
	}
	defer func() { _, _ = conn.Exec(context.WithoutCancel(ctx), `select pg_advisory_unlock($1)`, key) }()

	// The membership is read under the lock, so an approval landing mid-run joins
	// the next run rather than a run that has already decided its order.
	members, err := q.Members(ctx, serviceID)
	if err != nil {
		return nil, err
	}

	outcomes := make([]Outcome, 0, len(members))
	for _, it := range members {
		outcome, err := q.one(ctx, it)
		if err != nil {
			return outcomes, err
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes, nil
}

// one is the queue's whole treatment of a single candidate.
//
// A member that already has a release is a fast-forward and a mint that landed and
// an advance that did not, which is the one half-write the three writes below can
// leave. It is finished here rather than re-verified: re-verifying it would find
// master already in its branch, fast-forward to the commit master is at, and mint a
// second number for one merge — and nothing in the store refuses that, a release
// being unique on the service and the number and not on the item.
func (q *Queue) one(ctx context.Context, it item.Item) (Outcome, error) {
	minted, found, err := release.ForItem(ctx, q.pool, it.ID)
	if err != nil {
		return Outcome{}, err
	}
	if found {
		if _, err := q.dispatch.Advance(ctx, Actor, it.ID, item.StageMerged); err != nil {
			return Outcome{}, err
		}
		return Outcome{ItemID: it.ID, Merged: true, Release: minted, BuildID: minted.BuildID}, nil
	}

	verified, err := q.repo.Reverify(ctx, it)
	if err != nil {
		return Outcome{}, fmt.Errorf("mergequeue: re-verifying %s: %w", it.ID, err)
	}
	if !verified.Passed {
		return q.reject(ctx, it, verified)
	}
	if verified.Commit == "" || verified.BuildID == "" {
		return Outcome{}, fmt.Errorf("mergequeue: the re-verification of %s passed and names commit %q, build %q",
			it.ID, verified.Commit, verified.BuildID)
	}

	if err := q.repo.FastForward(ctx, it, verified.Commit); err != nil {
		return Outcome{}, fmt.Errorf("mergequeue: fast-forwarding master of %s to %s: %w", it.ServiceID, verified.Commit, err)
	}
	// The mint, and the contract versions the release publishes, in one
	// transaction. A contract changes only inside its service's items and every
	// write to it happens at a release, so the fast-forward is the event for both —
	// and one merge must not be able to leave a number with no version, or a version
	// under a number nothing minted.
	var published []contract.Published
	minted, err = q.releases.MintWith(ctx, Actor, it.ServiceID, verified.BuildID, it.ID,
		func(ctx context.Context, tx pgx.Tx, r release.Release) error {
			published, err = contract.PublishAll(ctx, tx, Actor,
				it.ServiceID, r.ID, r.Number, it.ID, verified.Forms)
			return err
		})
	if err != nil {
		return Outcome{}, err
	}
	if _, err := q.dispatch.Advance(ctx, Actor, it.ID, item.StageMerged); err != nil {
		return Outcome{}, err
	}
	return Outcome{
		ItemID:    it.ID,
		Merged:    true,
		Release:   minted,
		BuildID:   verified.BuildID,
		Commit:    verified.Commit,
		Published: published,
	}, nil
}

// reject is what the queue does with a candidate that failed its own
// re-verification: it writes the row, then sends the item back. In that order,
// because the row is the only record of the rejection and an item back at
// Implementation with nothing saying why is the worse of the two half-writes.
func (q *Queue) reject(ctx context.Context, it item.Item, verified Verified) (Outcome, error) {
	payload, err := json.Marshal(RejectionPayload{
		Kind:            RejectionKind,
		ItemID:          it.ID,
		ServiceID:       it.ServiceID,
		BuildID:         verified.BuildID,
		Commit:          verified.Commit,
		Why:             verified.Why,
		ReturnsTo:       gate.ReturnsTo,
		CountsAnAttempt: true,
	})
	if err != nil {
		return Outcome{}, fmt.Errorf("mergequeue: marshalling the rejection of %s: %w", it.ID, err)
	}
	row, err := q.log.AppendWait(ctx, decisionlog.Entry{Actor: Actor, Payload: string(payload)})
	if err != nil {
		return Outcome{}, err
	}
	if _, err := q.dispatch.SendBack(ctx, Actor, it.ID, item.StageImplementation); err != nil {
		return Outcome{}, err
	}
	return Outcome{
		ItemID:  it.ID,
		BuildID: verified.BuildID,
		Commit:  verified.Commit,
		Why:     verified.Why,
		WaitRow: row.ID,
	}, nil
}
