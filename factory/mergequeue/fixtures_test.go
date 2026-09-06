// The database tests of this package are in mergequeue_test and open the pool
// through package postgres, the way every record package's do; deps.txt records
// the test edges. The repository is a fake here, and that is the point: what this
// package does is read master, order the members, speculate, mint on a pass and
// read a failure three ways, and what a re-verification actually does to a
// repository and a candidate environment is the command-line interface's own
// demonstration.
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package mergequeue_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/mergequeue"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
)

const serviceID = "svc_00000000000000000000000000000000"

var (
	decompositionActor = record.Actor{Kind: record.KindComponent, Key: "decomposition", Basis: record.BasisClaimed}
	dispatchActor      = record.Actor{Kind: record.KindComponent, Key: "dispatch", Basis: record.BasisClaimed}
	detectorActor      = record.Actor{Kind: record.KindComponent, Key: "constraints_pass", Basis: record.BasisClaimed}
	healthMonitorActor = record.Actor{Kind: record.KindComponent, Key: "health_monitor", Basis: record.BasisClaimed}
	buildRunnerActor   = record.Actor{Kind: record.KindComponent, Key: "build_runner", Basis: record.BasisClaimed}
	owner              = record.Actor{Kind: record.KindHuman, Key: "owner", Basis: record.BasisClaimed}
	testActor          = record.Actor{Kind: record.KindComponent, Key: "test", Basis: record.BasisClaimed}
	// testReading is the same test actor as a principal, which is what a read
	// of the log takes.
	testReading = principal.OfComponent("test")
)

// fakeRepository answers every operation on master and on a candidate's
// environment from a script, and records what it was asked and in what order —
// which is what a test of the queue's order, of its speculation, and of its
// readings of a failure reads.
type fakeRepository struct {
	// head is master's head and held is every commit master holds. A head that
	// is not in held is a test of the reading that finds the records ahead of
	// git.
	head string
	held map[string]bool

	verified map[string]mergequeue.Verified
	// verify is what a re-verification answers where the test needs the build
	// written as the re-verification runs, which is when it is written for real:
	// the approved build is the item's newest until the re-verification writes
	// another one over the master it will actually merge into.
	verify func(it item.Item) mergequeue.Verified
	// confirmed is what the confirming run answers, by item.
	confirmed map[string]mergequeue.Confirmation
	// ofCommit is what re-verifying an accepted commit answers, by commit.
	ofCommit map[string]mergequeue.Verified

	err error
	// onFastForward is what a test moves master with between two of a pass's
	// merges, which is what the reading before every mint is for.
	onFastForward func(commit string)

	reverified    []string
	speculations  map[string][]string
	confirmations []string
	fastForwards  []string
}

func newRepository() *fakeRepository {
	return &fakeRepository{
		held:         map[string]bool{},
		verified:     map[string]mergequeue.Verified{},
		confirmed:    map[string]mergequeue.Confirmation{},
		ofCommit:     map[string]mergequeue.Verified{},
		speculations: map[string][]string{},
	}
}

func (r *fakeRepository) Head(context.Context, string) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	return r.head, nil
}

func (r *fakeRepository) Holds(_ context.Context, _, commit string) (bool, error) {
	if r.err != nil {
		return false, r.err
	}
	return r.held[commit], nil
}

func (r *fakeRepository) Reverify(_ context.Context, it item.Item, ahead []item.Item) (mergequeue.Verified, error) {
	r.reverified = append(r.reverified, it.ID)
	named := make([]string, 0, len(ahead))
	for _, a := range ahead {
		named = append(named, a.ID)
	}
	r.speculations[it.ID] = named
	if r.err != nil {
		return mergequeue.Verified{}, r.err
	}
	if r.verify != nil {
		return r.verify(it), nil
	}
	return r.verified[it.ID], nil
}

func (r *fakeRepository) Confirm(_ context.Context, it item.Item, _ mergequeue.Verified) (mergequeue.Confirmation, error) {
	r.confirmations = append(r.confirmations, it.ID)
	if r.err != nil {
		return mergequeue.Confirmation{}, r.err
	}
	return r.confirmed[it.ID], nil
}

func (r *fakeRepository) FastForward(_ context.Context, _ item.Item, commit string) error {
	if r.err != nil {
		return r.err
	}
	r.fastForwards = append(r.fastForwards, commit)
	r.head = commit
	r.held[commit] = true
	if r.onFastForward != nil {
		r.onFastForward(commit)
	}
	return nil
}

func (r *fakeRepository) VerifyCommit(_ context.Context, _, commit string) (mergequeue.Verified, error) {
	if r.err != nil {
		return mergequeue.Verified{}, r.err
	}
	return r.ofCommit[commit], nil
}

// seen is the health monitor's store as the queue reads it: the highest number
// it names per service.
type seen map[string]int64

func (s seen) HighestSeen(_ context.Context, serviceID string) (int64, error) {
	return s[serviceID], nil
}

// sameDesignSystem is a reader of the constraint records that answers that two
// records never differ on anything the build uses, which is what a factory with
// the records to read would answer for a move that touched nothing.
type sameDesignSystem struct{}

func (sameDesignSystem) Differs(context.Context, string, string, string) (bool, error) {
	return false, nil
}

// newQueue gives a test a schema of its own with the whole factory schema
// applied, a queue over it, and the fakes the queue reaches through.
func newQueue(t *testing.T, c mergequeue.Composition) (context.Context, *pgxpool.Pool, lease.Token, *mergequeue.Queue) {
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
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}

	c.Pool, c.Token = pool, token
	c.Log = decisionlog.NewWriter(pool, token)
	c.Releases = release.NewWriter(pool, token)
	return ctx, pool, token, mergequeue.New(c)
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

// refined is an intent in the state that permits membership, raised by actor
// with the tier that orders the queue's members. A detector's intent takes its
// tier at the arrival and owes no confirming question, which is what makes it
// the cheapest intent for a test of the queue to author; who raised it is what
// the halt's two exceptions are read off.
func refined(ctx context.Context, t *testing.T, pool *pgxpool.Pool, token lease.Token,
	n int, actor record.Actor, tier int) intent.Intent {
	t.Helper()
	intake := intent.NewIntake(pool, token)
	arrival := intent.Arrival{
		Source:    intent.SourceDetector,
		Statement: fmt.Sprintf("intent %d", n),
		Evidence:  intent.Evidence{ServiceID: serviceID, ReleaseID: fmt.Sprintf("rel_%032d", n)},
	}
	if tier != 0 {
		arrival.Tier = intent.Tier{Value: tier, PolicyVersion: "dl_00000000000000000000000000000000"}
	}
	in, err := intake.TakeIn(ctx, actor, arrival)
	if err != nil {
		t.Fatalf("taking in intent %d: %v", n, err)
	}
	if _, err := intake.Confirm(ctx, actor, intent.Confirmation{
		IntentID: in.ID,
		Requirements: []intent.NewRequirement{
			{Statement: "The system shall do what intent " + fmt.Sprint(n) + " asks"},
		},
	}); err != nil {
		t.Fatalf("refining intent %d: %v", n, err)
	}
	read, err := intent.Get(ctx, pool, in.ID)
	if err != nil {
		t.Fatalf("reading intent %d back: %v", n, err)
	}
	return read
}

// requested is an intent somebody asked for, refined at a confirming round that
// proposes the tier: the tier orders the queue's members ahead of every priority,
// and a requester's is written at that round rather than at the arrival.
func requested(ctx context.Context, t *testing.T, pool *pgxpool.Pool, token lease.Token,
	n, tier int) intent.Intent {
	t.Helper()
	intake := intent.NewIntake(pool, token)
	in, err := intake.TakeIn(ctx, owner, intent.Arrival{
		Source: intent.SourceOwner, Statement: fmt.Sprintf("request %d", n),
	})
	if err != nil {
		t.Fatalf("taking in request %d: %v", n, err)
	}
	if _, err := intake.OpenRound(ctx, owner, in.ID); err != nil {
		t.Fatalf("opening the round of request %d: %v", n, err)
	}
	asked, err := intake.Ask(ctx, owner, in.ID, "is this what you want?")
	if err != nil {
		t.Fatalf("asking the confirming round of request %d: %v", n, err)
	}
	if _, err := intake.Confirm(ctx, owner, intent.Confirmation{
		IntentID:       in.ID,
		QuestionID:     asked.ID,
		Answer:         "yes",
		IntendedEffect: "the requester gets what request " + fmt.Sprint(n) + " asked for",
		Tier:           intent.Tier{Value: tier, PolicyVersion: "dl_00000000000000000000000000000000"},
		Requirements: []intent.NewRequirement{
			{Statement: "The system shall do what request " + fmt.Sprint(n) + " asks"},
		},
	}); err != nil {
		t.Fatalf("refining request %d: %v", n, err)
	}
	read, err := intent.Get(ctx, pool, in.ID)
	if err != nil {
		t.Fatalf("reading request %d back: %v", n, err)
	}
	return read
}

// queued decomposes an item of a refined intent and advances it to the stage the
// queue's membership is: the Merge to master gate approved it and its
// fast-forward has not happened.
func queued(ctx context.Context, t *testing.T, pool *pgxpool.Pool, token lease.Token, n int) item.Item {
	t.Helper()
	return queuedOf(ctx, t, pool, token, refined(ctx, t, pool, token, n, detectorActor, 0), n)
}

// queuedOf is [queued] over an intent the test authored itself, which is what a
// test of the tier's order and of the halt's exceptions needs.
func queuedOf(ctx context.Context, t *testing.T, pool *pgxpool.Pool, token lease.Token,
	in intent.Intent, n int) item.Item {
	t.Helper()
	it, err := item.NewDecomposition(pool, token).Create(ctx, decompositionActor, item.New{
		IntentID:  in.ID,
		ServiceID: serviceID,
		Branch:    fmt.Sprintf("item/%d", n),
	}, "", "", nil)
	if err != nil {
		t.Fatalf("decomposing item %d: %v", n, err)
	}
	dispatch := item.NewDispatch(pool, token)
	for _, stage := range []item.Stage{
		item.StageImplementationPlan, item.StageTasks, item.StageImplementation, item.StageQueued,
	} {
		if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, stage); err != nil {
			t.Fatalf("advancing item %d to %s: %v", n, stage, err)
		}
	}
	return it
}

// built writes a build record of one item, which is what the two comparisons no
// criterion reads are made over: the design system constraint record the build
// names, and the digests of what it resolved.
func built(ctx context.Context, t *testing.T, pool *pgxpool.Pool, token lease.Token,
	it item.Item, commit, designSystemRecord string, resolved []build.ResolvedEntry) build.Build {
	t.Helper()
	made, err := build.NewWriter(pool, token).Create(ctx, buildRunnerActor, build.Draft{
		ItemID:                   it.ID,
		ServiceID:                it.ServiceID,
		CommitHash:               commit,
		ArtifactDigest:           "sha256:" + commit,
		DesignSystemConstraintID: designSystemRecord,
		Resolved:                 resolved,
	})
	if err != nil {
		t.Fatalf("writing the build of %s at %s: %v", it.ID, commit, err)
	}
	return made
}

// readLog is every row in the log, read as [testActor] through a reader of the
// test's own.
func readLog(t *testing.T, ctx context.Context, pool *pgxpool.Pool, token lease.Token) []decisionlog.Row {
	t.Helper()
	rows, err := decisionlog.NewReader(pool, token).Read(ctx, testReading)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	return rows
}

// waitsOfKind is every wait row of the queue's whose payload names one kind,
// openings and closings alike.
func waitsOfKind(t *testing.T, rows []decisionlog.Row, kind mergequeue.WaitKind) (open, closed []decisionlog.Row) {
	t.Helper()
	openings := map[string]bool{}
	for _, row := range rows {
		if row.Shape != decisionlog.ShapeWait {
			continue
		}
		var payload mergequeue.WaitPayload
		if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
			continue
		}
		if payload.Kind != kind {
			continue
		}
		if row.Part == decisionlog.PartOpen {
			openings[row.ID] = true
			open = append(open, row)
		}
	}
	for _, row := range rows {
		if row.Shape == decisionlog.ShapeWait && row.Part == decisionlog.PartClose && openings[row.Closes] {
			closed = append(closed, row)
		}
	}
	return open, closed
}

func ids(items []item.Item) []string {
	named := make([]string, 0, len(items))
	for _, it := range items {
		named = append(named, it.ID)
	}
	return named
}
