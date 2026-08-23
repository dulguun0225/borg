// The database tests of this package are in mergequeue_test and open the pool
// through package postgres, the way every record package's do; deps.txt records
// the test edge. The repository is a fake here, and that is the point: what this
// package does is order the members, mint on a pass, and reject on a failure, and
// what a re-verification actually does to a repository and a candidate environment
// is the crude interface's own demonstration.
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package mergequeue_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/mergequeue"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
)

const serviceID = "svc_00000000000000000000000000000000"

var (
	decompositionActor = record.Actor{Kind: record.KindComponent, Name: "decomposition"}
	dispatchActor      = record.Actor{Kind: record.KindComponent, Name: "dispatch"}
	owner              = record.Actor{Kind: record.KindHuman, Name: "owner"}
)

// fakeRepository answers a re-verification from a script keyed by item, and
// records the order it was asked in — which is what a test of the queue's order
// reads.
type fakeRepository struct {
	verified     map[string]mergequeue.Verified
	err          error
	reverified   []string
	fastForwards []string
}

func (r *fakeRepository) Reverify(_ context.Context, it item.Item) (mergequeue.Verified, error) {
	r.reverified = append(r.reverified, it.ID)
	if r.err != nil {
		return mergequeue.Verified{}, r.err
	}
	return r.verified[it.ID], nil
}

func (r *fakeRepository) FastForward(_ context.Context, _ item.Item, commit string) error {
	r.fastForwards = append(r.fastForwards, commit)
	return nil
}

// newQueue gives a test a schema of its own with the whole factory schema applied,
// a queue over it, and the fake repository the queue reaches through.
func newQueue(t *testing.T, repo *fakeRepository) (context.Context, *pgxpool.Pool, *mergequeue.Queue) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "mq_" + hex.EncodeToString(suffix[:])

	pool, err := postgres.Open(ctx, inSchema(t, postgres.URL(), schema))
	if err != nil {
		t.Fatalf("the database at %s is not reachable, and these tests do not skip: %v", postgres.URL(), err)
	}
	t.Cleanup(func() {
		// t.Context is already cancelled by the time cleanup runs.
		drop, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := pool.Exec(drop, `drop schema if exists `+pgx.Identifier{schema}.Sanitize()+` cascade`); err != nil {
			t.Errorf("dropping schema %s: %v", schema, err)
		}
		pool.Close()
	})
	if _, err := pool.Exec(ctx, `create schema `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("creating schema %s: %v", schema, err)
	}
	if err := postgres.Apply(ctx, pool); err != nil {
		t.Fatalf("applying the schema: %v", err)
	}

	q := mergequeue.New(pool, decisionlog.NewWriter(pool), release.NewWriter(pool), item.NewDispatch(pool), repo)
	return ctx, pool, q
}

// inSchema points a connection URL at one schema and nothing else, so every
// unqualified name in the DDL and in the writers' statements resolves there.
func inSchema(t *testing.T, base, schema string) string {
	t.Helper()
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parsing %s: %v", base, err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// queued decomposes an item and advances it to the stage the queue's membership is: the
// Merge to master gate approved it and its fast-forward has not happened.
func queued(ctx context.Context, t *testing.T, pool *pgxpool.Pool, n int) item.Item {
	t.Helper()
	it, err := item.NewDecomposition(pool).Create(ctx, decompositionActor, item.New{
		IntentID:  fmt.Sprintf("in_%032d", n),
		ServiceID: serviceID,
		Branch:    fmt.Sprintf("item/%d", n),
	})
	if err != nil {
		t.Fatalf("decomposing item %d: %v", n, err)
	}
	dispatch := item.NewDispatch(pool)
	for _, stage := range []item.Stage{item.StageImplementation, item.StageQueued} {
		if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, stage); err != nil {
			t.Fatalf("advancing item %d to %s: %v", n, stage, err)
		}
	}
	return it
}

// TestRunMintsOnAPassAndRejectsOnAFailure is the queue's two outcomes over one
// service: a candidate that passes its re-verification fast-forwards, is minted a
// release naming the build that re-verification produced, and advances to merged;
// one that fails goes back to Implementation with an attempt counted there and a
// wait row saying why.
func TestRunMintsOnAPassAndRejectsOnAFailure(t *testing.T) {
	repo := &fakeRepository{verified: map[string]mergequeue.Verified{}}
	ctx, pool, q := newQueue(t, repo)

	passes := queued(ctx, t, pool, 1)
	fails := queued(ctx, t, pool, 2)
	repo.verified[passes.ID] = mergequeue.Verified{Commit: "commit-one", BuildID: "bl_one", Passed: true}
	repo.verified[fails.ID] = mergequeue.Verified{Commit: "commit-two", BuildID: "bl_two",
		Why: "criterion cr_a is failed against build bl_two"}

	outcomes, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("Run returned %d outcomes, two candidates were queued: %+v", len(outcomes), outcomes)
	}

	merged, rejected := outcomes[0], outcomes[1]
	if merged.ItemID != passes.ID || !merged.Merged {
		t.Fatalf("the first outcome is %+v, want %s merged", merged, passes.ID)
	}
	if merged.Release.Number != 1 || merged.Release.BuildID != "bl_one" {
		t.Errorf("the release is number %d of build %s, want 1 of bl_one",
			merged.Release.Number, merged.Release.BuildID)
	}
	if merged.Release.Actor != mergequeue.Actor {
		t.Errorf("the release was minted by %+v, want the queue", merged.Release.Actor)
	}
	if len(repo.fastForwards) != 1 || repo.fastForwards[0] != "commit-one" {
		t.Errorf("the fast-forwards were %v, want the commit that was verified", repo.fastForwards)
	}
	if it, err := item.Get(ctx, pool, passes.ID); err != nil || it.Stage != item.StageMerged {
		t.Errorf("the merged item is at %v, %v", it.Stage, err)
	}

	if rejected.ItemID != fails.ID || rejected.Merged {
		t.Fatalf("the second outcome is %+v, want %s rejected", rejected, fails.ID)
	}
	if rejected.WaitRow == "" || rejected.Why == "" {
		t.Errorf("the rejection reports wait row %q and reason %q", rejected.WaitRow, rejected.Why)
	}
	it, err := item.Get(ctx, pool, fails.ID)
	if err != nil {
		t.Fatalf("reading the rejected item: %v", err)
	}
	if it.Stage != item.StageImplementation {
		t.Errorf("the rejected item is at %s, want implementation", it.Stage)
	}
	stages, err := item.Stages(ctx, pool, fails.ID)
	if err != nil {
		t.Fatalf("reading the rejected item's stages: %v", err)
	}
	if len(stages) != 1 || stages[0].Stage != item.StageImplementation || stages[0].Attempts != 1 {
		t.Errorf("the rejected item's stage rows are %+v, want one attempt at implementation", stages)
	}

	// Only one release exists: the rejection mints none, which is what makes a
	// number record a change that was accepted.
	var releases int
	if err := pool.QueryRow(ctx, `select count(*) from `+release.Table).Scan(&releases); err != nil {
		t.Fatalf("counting releases: %v", err)
	}
	if releases != 1 {
		t.Errorf("%d releases exist, one candidate passed", releases)
	}

	// The rejection is a wait row the log wrote with the queue as caller and actor:
	// no gate fired, the Merge to master gate's own having closed as an approval.
	rows, err := decisionlog.Read(ctx, pool)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("the log holds %d rows, one candidate was rejected and nothing else writes here", len(rows))
	}
	if rows[0].Shape != decisionlog.ShapeWait || rows[0].Actor != mergequeue.Actor {
		t.Errorf("the row is shape %s by %+v, want a wait by the queue", rows[0].Shape, rows[0].Actor)
	}
	var payload mergequeue.RejectionPayload
	if err := json.Unmarshal([]byte(rows[0].Payload), &payload); err != nil {
		t.Fatalf("reading the rejection payload: %v", err)
	}
	if payload.Kind != mergequeue.RejectionKind || payload.ItemID != fails.ID {
		t.Errorf("the payload is %+v, want kind %q for item %s", payload, mergequeue.RejectionKind, fails.ID)
	}
	if payload.ReturnsTo != gate.ReturnsTo || !payload.CountsAnAttempt {
		t.Errorf("the payload returns the item to %q and counts an attempt %v", payload.ReturnsTo, payload.CountsAnAttempt)
	}
	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Errorf("the chain does not verify: %v", err)
	}
}

// TestTheOrderIsThePriorityThenTheApproval: the queue's order is the item's
// priority, greater first, and then the time of the merge approval in the log.
// Reordering changes when a candidate re-verifies and never what it has to pass.
func TestTheOrderIsThePriorityThenTheApproval(t *testing.T) {
	repo := &fakeRepository{verified: map[string]mergequeue.Verified{}}
	ctx, pool, q := newQueue(t, repo)

	first := queued(ctx, t, pool, 1)
	second := queued(ctx, t, pool, 2)
	third := queued(ctx, t, pool, 3)

	// The queue with nothing else to go on takes them in the order they were decomposed,
	// no approval being in the log yet.
	members, err := q.Members(ctx, serviceID)
	if err != nil {
		t.Fatalf("Members: %v", err)
	}
	if len(members) != 3 || members[0].ID != first.ID || members[2].ID != third.ID {
		t.Fatalf("the members are %+v, want them in the order they were decomposed", ids(members))
	}

	// An owner pushes the last one to the front.
	if _, err := item.NewDispatch(pool).SetPriority(ctx, owner, third.ID, 5); err != nil {
		t.Fatalf("SetPriority: %v", err)
	}
	if members, err = q.Members(ctx, serviceID); err != nil {
		t.Fatalf("Members after the priority: %v", err)
	}
	if members[0].ID != third.ID {
		t.Errorf("the members are %v, want the pushed item %s first", ids(members), third.ID)
	}

	// Nothing here fires a gate, so no approval time is in the log and the order
	// falls back to decomposition's. What the approval time does to the order is package
	// gate's own demonstration, that being where the payload's shape lives.
	_ = second

	for _, member := range members {
		repo.verified[member.ID] = mergequeue.Verified{
			Commit: "commit-" + member.ID, BuildID: "bl_" + member.ID, Passed: true,
		}
	}
	if _, err := q.Run(ctx, serviceID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(repo.reverified) != 3 || repo.reverified[0] != third.ID {
		t.Errorf("the queue re-verified %v, want the pushed item first", repo.reverified)
	}
	// The numbers follow the order the queue merged in, which is what makes a
	// number order builds.
	pushed, found, err := release.Highest(ctx, pool, serviceID)
	if err != nil || !found {
		t.Fatalf("Highest = found %v, %v", found, err)
	}
	if pushed.Number != 3 {
		t.Errorf("the highest number is %d after three merges", pushed.Number)
	}
}

// TestAnEmptyQueueAndAnUnnamedServiceAreRefusedOrEmpty: the order is per service,
// so a run naming none is an error, and a service with nothing queued is no
// outcomes and no error.
func TestAnEmptyQueueAndAnUnnamedServiceAreRefusedOrEmpty(t *testing.T) {
	repo := &fakeRepository{verified: map[string]mergequeue.Verified{}}
	ctx, _, q := newQueue(t, repo)

	if _, err := q.Run(ctx, ""); !errors.Is(err, mergequeue.ErrServiceIDEmpty) {
		t.Errorf("Run naming no service = %v, want ErrServiceIDEmpty", err)
	}
	if _, err := q.Members(ctx, ""); !errors.Is(err, mergequeue.ErrServiceIDEmpty) {
		t.Errorf("Members naming no service = %v, want ErrServiceIDEmpty", err)
	}
	outcomes, err := q.Run(ctx, serviceID)
	if err != nil || len(outcomes) != 0 {
		t.Errorf("Run over an empty queue = %+v, %v", outcomes, err)
	}
}

// TestAReverificationErrorStopsTheRun: a repository that cannot be read is
// infrastructure and not a candidate failing on its merits, so it stops the run
// rather than rejecting the item — nothing is minted and nothing is sent back.
func TestAReverificationErrorStopsTheRun(t *testing.T) {
	unreachable := errors.New("the repository is unreadable")
	repo := &fakeRepository{verified: map[string]mergequeue.Verified{}, err: unreachable}
	ctx, pool, q := newQueue(t, repo)
	it := queued(ctx, t, pool, 1)

	if _, err := q.Run(ctx, serviceID); !errors.Is(err, unreachable) {
		t.Fatalf("Run = %v, want the repository's error", err)
	}
	read, err := item.Get(ctx, pool, it.ID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	if read.Stage != item.StageQueued {
		t.Errorf("the item is at %s, and an infrastructure failure leaves it in the queue", read.Stage)
	}
	rows, err := decisionlog.Read(ctx, pool)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("the log holds %d rows, and nothing about the candidate was decided", len(rows))
	}
}

// TestTheLockKeyIsNotTheMints is why the key is derived from this package's own
// name: the run holds a session-level lock across the fast-forward and the mint,
// and the mint takes a lock of its own inside a transaction on another connection.
// One key for both would be a deadlock the pool could not resolve.
func TestTheLockKeyIsNotTheMints(t *testing.T) {
	if mergequeue.AdvisoryLockKey(serviceID) == release.AdvisoryLockKey(serviceID) {
		t.Fatal("the queue's lock key is the mint's, and the run holds one while the mint waits for the other")
	}
	if mergequeue.AdvisoryLockKey("svc_a") == mergequeue.AdvisoryLockKey("svc_b") {
		t.Error("two services share one key, and their merges have nothing to serialise against each other for")
	}
	if key := mergequeue.AdvisoryLockKey(serviceID); key < 0 {
		t.Errorf("the key is %d, and the top bit is cleared so the value is positive", key)
	}
}

func ids(items []item.Item) []string {
	named := make([]string, 0, len(items))
	for _, it := range items {
		named = append(named, it.ID)
	}
	return named
}

// TestAMemberThatAlreadyHasAReleaseIsFinishedNotReverified is the one half-write
// the three writes of a merge can leave: the fast-forward and the mint landed and
// the advance did not. Re-verifying that member would fast-forward to the commit
// master is already at and mint a second number for one merge — a release being
// unique on the service and the number and not on the item, so nothing in the store
// would refuse it. It is finished instead.
func TestAMemberThatAlreadyHasAReleaseIsFinishedNotReverified(t *testing.T) {
	repo := &fakeRepository{verified: map[string]mergequeue.Verified{}}
	ctx, pool, q := newQueue(t, repo)
	it := queued(ctx, t, pool, 1)

	// The state a failed advance leaves: a release for an item still at queued.
	minted, err := release.NewWriter(pool).Mint(ctx, mergequeue.Actor, serviceID, "bl_one", it.ID)
	if err != nil {
		t.Fatalf("minting the release the advance did not follow: %v", err)
	}

	outcomes, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(outcomes) != 1 || !outcomes[0].Merged {
		t.Fatalf("Run returned %+v, want the member finished as merged", outcomes)
	}
	if outcomes[0].Release.ID != minted.ID {
		t.Errorf("the outcome names release %s, want the one already minted, %s",
			outcomes[0].Release.ID, minted.ID)
	}
	if len(repo.reverified) != 0 {
		t.Errorf("the queue re-verified %v, and a member that already has a release is finished rather than verified again",
			repo.reverified)
	}
	if len(repo.fastForwards) != 0 {
		t.Errorf("the queue fast-forwarded %v, and master is already at that commit", repo.fastForwards)
	}

	var releases int
	if err := pool.QueryRow(ctx, `select count(*) from `+release.Table+` where item_id = $1`, it.ID).Scan(&releases); err != nil {
		t.Fatalf("counting the item's releases: %v", err)
	}
	if releases != 1 {
		t.Errorf("the item has %d releases, and one merge is one number", releases)
	}
	if read, err := item.Get(ctx, pool, it.ID); err != nil || read.Stage != item.StageMerged {
		t.Errorf("the item is at %v, %v — the advance the earlier run did not make is what this one repairs", read.Stage, err)
	}
}
