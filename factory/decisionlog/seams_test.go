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
		(seq, id, actor_kind, actor_name, at, shape, payload, policy_version, score_version, part, closes, prev_hash, hash)
		values (nextval('`+decisionlog.Sequence+`'), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		row.ID, string(row.Actor.Kind), row.Actor.Name, row.At, string(row.Shape),
		row.Payload, row.PolicyVersion, row.ScoreVersion, string(row.Part), row.Closes,
		row.PrevHash, row.Hash)
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
// the four append methods, and the CHECK constraint that catches a row
// written around them.
func TestOnlyADecisionNamesTheVersions(t *testing.T) {
	ctx, pool, log := newLog(t)

	t.Run("the methods refuse", func(t *testing.T) {
		withPolicy := decisionlog.Entry{Actor: gate, Payload: "x", PolicyVersion: "policy-1"}
		withScore := decisionlog.Entry{Actor: gate, Payload: "x", ScoreVersion: "score-1"}
		closingWithPolicy := withPolicy
		closingWithPolicy.Closes = "dl_00112233445566778899aabbccddeeff"
		closingWithScore := withScore
		closingWithScore.Closes = closingWithPolicy.Closes
		refused := map[string]func() error{
			"page event with a policy version": func() error { _, err := log.AppendPageEvent(ctx, withPolicy); return err },
			"page event with a score version":  func() error { _, err := log.AppendPageEvent(ctx, withScore); return err },
			"wait with a policy version":       func() error { _, err := log.AppendWait(ctx, withPolicy); return err },
			"wait with a score version":        func() error { _, err := log.AppendWait(ctx, withScore); return err },
			"closing with a policy version": func() error {
				_, err := log.AppendDecisionClosing(ctx, closingWithPolicy)
				return err
			},
			"closing with a score version": func() error {
				_, err := log.AppendDecisionClosing(ctx, closingWithScore)
				return err
			},
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
			if _, err := log.AppendDecisionOpening(ctx, entry); !errors.Is(err, decisionlog.ErrVersionsMissing) {
				t.Errorf("an opening with %s: %v, want ErrVersionsMissing", name, err)
			}
		}
	})

	t.Run("the store refuses", func(t *testing.T) {
		const want = "versions_match_part"

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

		opening := aRow()
		opening.Shape = decisionlog.ShapeDecision
		opening.Part = decisionlog.PartOpening
		if got := refusedBy(t, insertAround(ctx, pool, opening)); got != want {
			t.Errorf("an opening naming no version was refused by %q, want %q", got, want)
		}

		closing := aRow()
		closing.Shape = decisionlog.ShapeDecision
		closing.Part = decisionlog.PartClosing
		closing.Closes = "dl_00112233445566778899aabbccddeeff"
		closing.PolicyVersion = "policy-1"
		closing.ScoreVersion = "score-1"
		if got := refusedBy(t, insertAround(ctx, pool, closing)); got != want {
			t.Errorf("a closing naming both versions was refused by %q, want %q", got, want)
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

// TestAClosingClosesAnOpeningAndNothingElse is the closing's naming rule,
// checked at the methods: a closing names an opening row that exists, and no
// other kind of row names anything.
func TestAClosingClosesAnOpeningAndNothingElse(t *testing.T) {
	ctx, pool, log := newLog(t)

	opening, err := log.AppendDecisionOpening(ctx, decisionlog.Entry{
		Actor: gate, Payload: "the firing", PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpening: %v", err)
	}
	page, err := log.AppendPageEvent(ctx, decisionlog.Entry{Actor: gate, Payload: "a page"})
	if err != nil {
		t.Fatalf("AppendPageEvent: %v", err)
	}

	t.Run("a closing names something", func(t *testing.T) {
		entry := decisionlog.Entry{Actor: owner, Payload: "a verdict over nothing"}
		if _, err := log.AppendDecisionClosing(ctx, entry); !errors.Is(err, decisionlog.ErrClosesMissing) {
			t.Errorf("a closing naming no row: %v, want ErrClosesMissing", err)
		}
	})

	t.Run("nothing else names anything", func(t *testing.T) {
		naming := decisionlog.Entry{
			Actor: gate, Payload: "x", PolicyVersion: "policy-1", ScoreVersion: "score-1",
			Closes: opening.ID,
		}
		if _, err := log.AppendDecisionOpening(ctx, naming); !errors.Is(err, decisionlog.ErrClosesRefused) {
			t.Errorf("an opening naming a row: %v, want ErrClosesRefused", err)
		}
		naming.PolicyVersion, naming.ScoreVersion = "", ""
		if _, err := log.AppendPageEvent(ctx, naming); !errors.Is(err, decisionlog.ErrClosesRefused) {
			t.Errorf("a page event naming a row: %v, want ErrClosesRefused", err)
		}
		if _, err := log.AppendWait(ctx, naming); !errors.Is(err, decisionlog.ErrClosesRefused) {
			t.Errorf("a wait naming a row: %v, want ErrClosesRefused", err)
		}
	})

	t.Run("the named row is an opening", func(t *testing.T) {
		entry := decisionlog.Entry{Actor: owner, Payload: "a verdict", Closes: "dl_00112233445566778899aabbccddeeff"}
		if _, err := log.AppendDecisionClosing(ctx, entry); !errors.Is(err, decisionlog.ErrNotAnOpening) {
			t.Errorf("a closing naming no row that exists: %v, want ErrNotAnOpening", err)
		}
		entry.Closes = page.ID
		if _, err := log.AppendDecisionClosing(ctx, entry); !errors.Is(err, decisionlog.ErrNotAnOpening) {
			t.Errorf("a closing naming a page event: %v, want ErrNotAnOpening", err)
		}

		closing, err := log.AppendDecisionClosing(ctx, decisionlog.Entry{
			Actor: owner, Payload: "a verdict", Closes: opening.ID,
		})
		if err != nil {
			t.Fatalf("AppendDecisionClosing: %v", err)
		}
		entry.Closes = closing.ID
		if _, err := log.AppendDecisionClosing(ctx, entry); !errors.Is(err, decisionlog.ErrNotAnOpening) {
			t.Errorf("a closing naming a closing: %v, want ErrNotAnOpening", err)
		}
	})

	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}

// TestOneOpeningTakesOneClosing is what the partial unique index is for. The
// method does not check for a second closing itself; the index refuses it,
// through the method and around it.
func TestOneOpeningTakesOneClosing(t *testing.T) {
	ctx, pool, log := newLog(t)

	opening, err := log.AppendDecisionOpening(ctx, decisionlog.Entry{
		Actor: gate, Payload: "the firing", PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpening: %v", err)
	}
	if _, err := log.AppendDecisionClosing(ctx, decisionlog.Entry{
		Actor: owner, Payload: "the verdict", Closes: opening.ID,
	}); err != nil {
		t.Fatalf("AppendDecisionClosing: %v", err)
	}

	const want = "decision_log_one_closing"

	_, err = log.AppendDecisionClosing(ctx, decisionlog.Entry{
		Actor: owner, Payload: "a second verdict", Closes: opening.ID,
	})
	if got := refusedBy(t, err); got != want {
		t.Errorf("a second closing through the method was refused by %q, want %q", got, want)
	}

	second := aRow()
	second.Shape = decisionlog.ShapeDecision
	second.Part = decisionlog.PartClosing
	second.Closes = opening.ID
	if got := refusedBy(t, insertAround(ctx, pool, second)); got != want {
		t.Errorf("a second closing around the method was refused by %q, want %q", got, want)
	}

	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}

// TestPartAndClosesMatchTheShape is the two remaining CHECK constraints,
// reached around the methods: a part on anything but a decision, and a closes
// on anything but a closing.
func TestPartAndClosesMatchTheShape(t *testing.T) {
	ctx, pool, _ := newLog(t)

	// The page event carries closes too, so part_matches_shape is the one
	// constraint the row breaks.
	page := aRow()
	page.Shape = decisionlog.ShapePageEvent
	page.Part = decisionlog.PartClosing
	page.Closes = "dl_00112233445566778899aabbccddeeff"
	if got, want := refusedBy(t, insertAround(ctx, pool, page)), "part_matches_shape"; got != want {
		t.Errorf("a page event with a part was refused by %q, want %q", got, want)
	}

	wait := aRow()
	wait.Closes = "dl_00112233445566778899aabbccddeeff"
	if got, want := refusedBy(t, insertAround(ctx, pool, wait)), "closes_matches_part"; got != want {
		t.Errorf("a wait closing a row was refused by %q, want %q", got, want)
	}

	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}

// TestATamperedPartOrClosesIsNamed is the chain covering the two new fields
// in the store. The constraints allow no lone edit to part — an opening's
// part requires its versions and a closing's part requires its closes — so
// the part tamper rewrites the row into the other part's shape, which the
// store accepts and the chain still names.
func TestATamperedPartOrClosesIsNamed(t *testing.T) {
	ctx, pool, log := newLog(t)

	opening, err := log.AppendDecisionOpening(ctx, decisionlog.Entry{
		Actor: gate, Payload: "the firing", PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpening: %v", err)
	}
	closing, err := log.AppendDecisionClosing(ctx, decisionlog.Entry{
		Actor: owner, Payload: "the verdict", Closes: opening.ID,
	})
	if err != nil {
		t.Fatalf("AppendDecisionClosing: %v", err)
	}

	tampers := map[string]string{
		"closes": `update decision_log set closes = 'dl_ffeeddccbbaa99887766554433221100' where seq = $1`,
		"part": `update decision_log set part = 'opening', closes = '',
			policy_version = 'policy-1', score_version = 'score-1' where seq = $1`,
	}
	undo := `update decision_log set part = $1, closes = $2,
		policy_version = $3, score_version = $4 where seq = $5`
	for field, tamper := range tampers {
		if _, err := pool.Exec(ctx, tamper, closing.Seq); err != nil {
			t.Fatalf("tampering with %s: %v", field, err)
		}
		broken := brokenBy(t, decisionlog.Verify(ctx, pool))
		if broken.Row.Seq != closing.Seq {
			t.Errorf("%s tampered: Verify names row %d, the tampered row is %d", field, broken.Row.Seq, closing.Seq)
		}
		if broken.Break != decisionlog.BreakFields {
			t.Errorf("%s tampered: Verify reports %v, want %v", field, broken.Break, decisionlog.BreakFields)
		}
		if _, err := pool.Exec(ctx, undo,
			string(closing.Part), closing.Closes, closing.PolicyVersion, closing.ScoreVersion, closing.Seq,
		); err != nil {
			t.Fatalf("undoing the %s tamper: %v", field, err)
		}
		if err := decisionlog.Verify(ctx, pool); err != nil {
			t.Fatalf("the undone %s tamper still breaks the chain: %v", field, err)
		}
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
			if _, err := log.AppendDecisionOpening(ctx, entry); !errors.Is(err, c.want) {
				t.Errorf("an opening with %s: %v, want %v", name, err, c.want)
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
	if _, err := log.AppendDecisionOpening(ctx, decisionlog.Entry{
		Actor:         gate,
		Payload:       `{"gate":"deploy","credential":"` + credential.Name() + `","waits_on":"owner"}`,
		PolicyVersion: "policy-1",
		ScoreVersion:  "score-1",
	}); err != nil {
		t.Fatalf("AppendDecisionOpening: %v", err)
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
