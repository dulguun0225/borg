package decisionlog_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
)

// TestAClosingClosesAnOpeningAndNothingElse is the closing's naming rule,
// checked at the methods: a closing names an open event that exists, and no
// other kind of row names anything.
func TestAClosingClosesAnOpeningAndNothingElse(t *testing.T) {
	ctx, pool, log := newLog(t)

	opening, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: "the firing", PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpen: %v", err)
	}
	page, err := log.AppendPageEvent(ctx, decisionlog.Entry{Actor: gate, Payload: "a page"})
	if err != nil {
		t.Fatalf("AppendPageEvent: %v", err)
	}

	t.Run("a closing names something", func(t *testing.T) {
		entry := decisionlog.Entry{Actor: owner, Payload: "a verdict over nothing"}
		if _, err := log.AppendDecisionClose(ctx, entry); !errors.Is(err, decisionlog.ErrClosesMissing) {
			t.Errorf("a closing naming no row: %v, want ErrClosesMissing", err)
		}
	})

	t.Run("nothing else names anything", func(t *testing.T) {
		naming := decisionlog.Entry{
			Actor: gate, Payload: "x", PolicyVersion: "policy-1", ScoreVersion: "score-1",
			Closes: opening.ID,
		}
		if _, err := log.AppendDecisionOpen(ctx, naming); !errors.Is(err, decisionlog.ErrClosesRefused) {
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
		if _, err := log.AppendDecisionClose(ctx, entry); !errors.Is(err, decisionlog.ErrNotAnOpening) {
			t.Errorf("a closing naming no row that exists: %v, want ErrNotAnOpening", err)
		}
		entry.Closes = page.ID
		if _, err := log.AppendDecisionClose(ctx, entry); !errors.Is(err, decisionlog.ErrNotAnOpening) {
			t.Errorf("a closing naming a page event: %v, want ErrNotAnOpening", err)
		}

		closing, err := log.AppendDecisionClose(ctx, decisionlog.Entry{
			Actor: owner, Payload: "a verdict", Closes: opening.ID,
		})
		if err != nil {
			t.Fatalf("AppendDecisionClose: %v", err)
		}
		entry.Closes = closing.ID
		if _, err := log.AppendDecisionClose(ctx, entry); !errors.Is(err, decisionlog.ErrNotAnOpening) {
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

	opening, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: "the firing", PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpen: %v", err)
	}
	if _, err := log.AppendDecisionClose(ctx, decisionlog.Entry{
		Actor: owner, Payload: "the verdict", Closes: opening.ID,
	}); err != nil {
		t.Fatalf("AppendDecisionClose: %v", err)
	}

	const want = "decision_log_one_closing"

	_, err = log.AppendDecisionClose(ctx, decisionlog.Entry{
		Actor: owner, Payload: "a second verdict", Closes: opening.ID,
	})
	if got := refusedBy(t, err); got != want {
		t.Errorf("a second closing through the method was refused by %q, want %q", got, want)
	}

	second := aRow()
	second.Shape = decisionlog.ShapeDecision
	second.Part = decisionlog.PartClose
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
	page.Part = decisionlog.PartClose
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

	opening, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: "the firing", PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpen: %v", err)
	}
	closing, err := log.AppendDecisionClose(ctx, decisionlog.Entry{
		Actor: owner, Payload: "the verdict", Closes: opening.ID,
	})
	if err != nil {
		t.Fatalf("AppendDecisionClose: %v", err)
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
