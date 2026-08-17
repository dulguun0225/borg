package decisionlog_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
)

// insertAround writes a row without going through the writer, which is how a
// test reaches the constraints rather than the methods. It fills the hash
// columns with the values given rather than computing them, because what is
// under test is what the store refuses and not what the chain says.
func insertAround(ctx context.Context, pool *pgxpool.Pool, row decisionlog.Row) error {
	_, err := pool.Exec(ctx, `insert into decision_log
		(seq, id, actor_kind, actor_name, at, shape, payload, policy_version, score_version, prev_hash, hash)
		values (nextval('`+decisionlog.Sequence+`'), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		row.ID, string(row.Actor.Kind), row.Actor.Name, row.At, string(row.Shape),
		row.Payload, row.PolicyVersion, row.ScoreVersion, row.PrevHash, row.Hash)
	return err
}

// aRow is a row that the store accepts, for a test to spoil one field of.
func aRow() decisionlog.Row {
	id := record.NewID("dl")
	return decisionlog.Row{
		ID:       id,
		Actor:    gate,
		At:       record.Now(),
		Shape:    decisionlog.ShapeWait,
		Payload:  "written around the writer",
		PrevHash: "prev-" + id,
		Hash:     "hash-" + id,
	}
}

// refusedBy is the constraint the store named, or a failure where it accepted
// the row.
func refusedBy(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("the store accepted the row, want it refused")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("the store returned %v, want a constraint violation", err)
	}
	return pgErr.ConstraintName
}

// TestOnlyADecisionNamesTheVersions checks both places the rule is enforced:
// the three append methods, and the CHECK constraint that catches a row
// written around them.
func TestOnlyADecisionNamesTheVersions(t *testing.T) {
	ctx, pool, log := newLog(t)

	t.Run("the methods refuse", func(t *testing.T) {
		withPolicy := decisionlog.Entry{Actor: gate, Payload: "x", PolicyVersion: "policy-1"}
		withScore := decisionlog.Entry{Actor: gate, Payload: "x", ScoreVersion: "score-1"}
		refused := map[string]func() error{
			"page event with a policy version": func() error { _, err := log.AppendPageEvent(ctx, withPolicy); return err },
			"page event with a score version":  func() error { _, err := log.AppendPageEvent(ctx, withScore); return err },
			"wait with a policy version":       func() error { _, err := log.AppendWait(ctx, withPolicy); return err },
			"wait with a score version":        func() error { _, err := log.AppendWait(ctx, withScore); return err },
		}
		for name, append := range refused {
			if err := append(); !errors.Is(err, decisionlog.ErrVersionsRefused) {
				t.Errorf("%s: %v, want ErrVersionsRefused", name, err)
			}
		}

		missing := map[string]decisionlog.Entry{
			"neither version": {Actor: gate, Payload: "x"},
			"no score":        {Actor: gate, Payload: "x", PolicyVersion: "policy-1"},
			"no policy":       {Actor: gate, Payload: "x", ScoreVersion: "score-1"},
		}
		for name, entry := range missing {
			if _, err := log.AppendDecision(ctx, entry); !errors.Is(err, decisionlog.ErrVersionsMissing) {
				t.Errorf("a decision with %s: %v, want ErrVersionsMissing", name, err)
			}
		}
	})

	t.Run("the store refuses", func(t *testing.T) {
		const want = "versions_match_shape"

		page := aRow()
		page.Shape = decisionlog.ShapePageEvent
		page.PolicyVersion = "policy-1"
		if got := refusedBy(t, insertAround(ctx, pool, page)); got != want {
			t.Errorf("a page event naming a policy version was refused by %q, want %q", got, want)
		}

		wait := aRow()
		wait.ScoreVersion = "score-1"
		if got := refusedBy(t, insertAround(ctx, pool, wait)); got != want {
			t.Errorf("a wait naming a score version was refused by %q, want %q", got, want)
		}

		decision := aRow()
		decision.Shape = decisionlog.ShapeDecision
		if got := refusedBy(t, insertAround(ctx, pool, decision)); got != want {
			t.Errorf("a decision naming no version was refused by %q, want %q", got, want)
		}

		unknown := aRow()
		unknown.Shape = "veto"
		if got, want := refusedBy(t, insertAround(ctx, pool, unknown)), "shape_known"; got != want {
			t.Errorf("a fourth shape was refused by %q, want %q", got, want)
		}
	})

	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}

// TestEveryRecordCarriesAnActor is seam 1, checked in both places: the writer
// validates, and the store refuses what a writer that did not validate would
// have written.
func TestEveryRecordCarriesAnActor(t *testing.T) {
	ctx, pool, log := newLog(t)

	t.Run("the writer refuses", func(t *testing.T) {
		cases := map[string]struct {
			actor record.Actor
			want  error
		}{
			"no actor at all": {record.Actor{}, record.ErrKindUnknown},
			"no kind":         {record.Actor{Name: "owner"}, record.ErrKindUnknown},
			"unknown kind":    {record.Actor{Kind: "robot", Name: "owner"}, record.ErrKindUnknown},
			"no name":         {record.Actor{Kind: record.KindHuman}, record.ErrNameEmpty},
		}
		for name, c := range cases {
			entry := decisionlog.Entry{Actor: c.actor, Payload: "x", PolicyVersion: "p", ScoreVersion: "s"}
			if _, err := log.AppendDecision(ctx, entry); !errors.Is(err, c.want) {
				t.Errorf("a decision with %s: %v, want %v", name, err, c.want)
			}
			entry.PolicyVersion, entry.ScoreVersion = "", ""
			if _, err := log.AppendWait(ctx, entry); !errors.Is(err, c.want) {
				t.Errorf("a wait with %s: %v, want %v", name, err, c.want)
			}
		}
	})

	t.Run("the store refuses", func(t *testing.T) {
		unknown := aRow()
		unknown.Actor = record.Actor{Kind: "robot", Name: "owner"}
		if got, want := refusedBy(t, insertAround(ctx, pool, unknown)), "actor_kind_known"; got != want {
			t.Errorf("an unknown actor kind was refused by %q, want %q", got, want)
		}

		empty := aRow()
		empty.Actor = record.Actor{Kind: record.KindHuman}
		if got, want := refusedBy(t, insertAround(ctx, pool, empty)), "actor_name_present"; got != want {
			t.Errorf("an empty actor name was refused by %q, want %q", got, want)
		}

		none := aRow()
		none.Actor = record.Actor{}
		if got, want := refusedBy(t, insertAround(ctx, pool, none)), "actor_kind_known"; got != want {
			t.Errorf("no actor at all was refused by %q, want %q", got, want)
		}
	})

	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}

// TestTheStoreRefusesATimestampThatIsNotTheLayout is what the record.Columns
// timestamp is worth to a package that is not this one. This writer always
// uses record.Now, so the constraint says nothing about it; what it says is
// that the next package to compose record.Columns cannot quietly store a
// second format. The chain would hash and verify whatever bytes were there,
// so the store is the only thing that can refuse them.
//
// The accepting case needs no assertion here: every other database test in
// this package writes record.Now through the writer, so the constraint
// refusing what the writer produces would fail all of them.
func TestTheStoreRefusesATimestampThatIsNotTheLayout(t *testing.T) {
	ctx, pool, _ := newLog(t)
	for _, at := range []string{
		"",
		"2026-08-17T01:30:00Z",
		"2026-08-17T01:30:00.000Z",
		"2026-08-17T01:30:00.000000000+00:00",
		"2026-08-17T01:30:00.000000000Z ",
		"not a time at all",
	} {
		row := aRow()
		row.At = at
		if got, want := refusedBy(t, insertAround(ctx, pool, row)), "at_is_time_layout"; got != want {
			t.Errorf("the timestamp %q was refused by %q, want %q", at, got, want)
		}
	}
	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}

// TestAResolvedSecretReachesNoRecord is seam 3 meeting seam 2: the value is
// resolved and used, the record names the reference, and the bytes in the
// table are searched for the value rather than the Go values being trusted.
func TestAResolvedSecretReachesNoRecord(t *testing.T) {
	ctx, pool, log := newLog(t)

	const value = "sk-the-value-nothing-else-may-see"
	path := filepath.Join(t.TempDir(), "secrets")
	if err := os.WriteFile(path, []byte("deploy.staging="+value+"\n"), 0o600); err != nil {
		t.Fatalf("writing the secrets file: %v", err)
	}
	resolver, err := secretref.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	credential := secretref.MustNew("deploy.staging")
	resolved, err := resolver.Resolve(credential)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved != value {
		t.Fatalf("Resolve = %q, want the value", resolved)
	}

	// What a component writes about a deploy: the reference, which is what it
	// has, because the value it resolved is used at the moment it connects and
	// goes into nothing that is stored.
	if _, err := log.AppendDecision(ctx, decisionlog.Entry{
		Actor:         gate,
		Payload:       `{"gate":"deploy","credential":"` + credential.Name() + `","verdict":"pass"}`,
		PolicyVersion: "policy-1",
		ScoreVersion:  "score-1",
	}); err != nil {
		t.Fatalf("AppendDecision: %v", err)
	}
	if _, err := log.AppendPageEvent(ctx, decisionlog.Entry{
		Actor:   owner,
		Payload: "the deploy used " + credential.String(),
	}); err != nil {
		t.Fatalf("AppendPageEvent: %v", err)
	}

	// Every column of every row, as the database holds it. Casting the row to
	// text is what makes this a claim about the stored bytes and not about
	// the columns the test remembered to name.
	var holding int
	if err := pool.QueryRow(ctx,
		`select count(*) from decision_log r where strpos(r::text, $1) > 0`, value,
	).Scan(&holding); err != nil {
		t.Fatalf("searching the rows: %v", err)
	}
	if holding != 0 {
		t.Fatalf("%d rows hold the secret value", holding)
	}

	var naming int
	if err := pool.QueryRow(ctx,
		`select count(*) from decision_log r where strpos(r::text, $1) > 0`, credential.Name(),
	).Scan(&naming); err != nil {
		t.Fatalf("searching the rows: %v", err)
	}
	if naming != 2 {
		t.Fatalf("%d rows name the reference, want 2 — the search found nothing to trust", naming)
	}

	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

// TestConcurrentAppendsChainInOrder is the one-writer rule under load. Without
// the advisory lock two transactions read the same head and write two rows
// naming the same predecessor, which is the fork the unique constraint on
// prev_hash refuses and Verify would otherwise find.
func TestConcurrentAppendsChainInOrder(t *testing.T) {
	ctx, pool, log := newLog(t)

	const appenders, each = 8, 6
	var wg sync.WaitGroup
	failures := make(chan error, appenders*each)
	for a := range appenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range each {
				_, err := log.AppendWait(ctx, decisionlog.Entry{
					Actor:   record.Actor{Kind: record.KindComponent, Name: "appender"},
					Payload: strings.Repeat("x", a) + "-" + strings.Repeat("y", n),
				})
				if err != nil {
					failures <- err
				}
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Errorf("AppendWait: %v", err)
	}

	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	rows, err := decisionlog.Read(ctx, pool)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(rows) != appenders*each {
		t.Fatalf("the log holds %d rows, want %d", len(rows), appenders*each)
	}
}
