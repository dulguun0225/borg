// Tests of the legal-hold subcommand and of the row that ends a hold: a hold
// set on a subject written kind:name, a withdrawal written pending, and the gate
// row that decides it closed by a human.
package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/legalhold"
)

// TestALegalHoldEndsOnlyAtTheRowThatDecidesItsWithdrawal: writing the withdrawal
// leaves the hold reaching its subject, and what lifts it is the close event of
// the gate row — not the write, and not a field flipped in place.
func TestALegalHoldEndsOnlyAtTheRowThatDecidesItsWithdrawal(t *testing.T) {
	ctx, pool := newOwner(t)
	install(t, ctx, pool)
	svc := decomposeService(t, ctx, pool, "checkout")

	if err := legalHoldCommand([]string{
		"-subject", "service:checkout", "-reason", "counsel asked for it",
	}); err != nil {
		t.Fatalf("legal-hold: %v", err)
	}
	subject := legalhold.Subject{Kind: legalhold.SubjectService, ID: svc.ID}
	reaching, err := legalhold.Reaching(ctx, pool, subject)
	if err != nil {
		t.Fatalf("Reaching: %v", err)
	}
	if !reaching {
		t.Fatalf("the hold does not reach the service it was set on")
	}

	standing, err := legalhold.Standing(ctx, pool)
	if err != nil {
		t.Fatalf("Standing: %v", err)
	}
	if len(standing) != 1 {
		t.Fatalf("%d holds stand, want the one that was set", len(standing))
	}
	if err := legalHoldCommand([]string{"-withdraw", standing[0].ID}); err != nil {
		t.Fatalf("legal-hold -withdraw: %v", err)
	}

	// The withdrawal's id is read out of its own table: package legalhold has no
	// read that lists withdrawals, there being no caller for one but this.
	var withdrawalID string
	if err := pool.QueryRow(ctx, `select id from `+legalhold.WithdrawalTable+
		` where legal_hold_id = $1`, standing[0].ID).Scan(&withdrawalID); err != nil {
		t.Fatalf("reading the withdrawal that was written: %v", err)
	}
	reaching, err = legalhold.Reaching(ctx, pool, subject)
	if err != nil {
		t.Fatalf("Reaching: %v", err)
	}
	if !reaching {
		t.Errorf("writing the withdrawal lifted the hold on its own, with nothing having decided it")
	}

	before := decisionCount(t, ctx, pool)
	if err := approveCommand([]string{"-legal-hold-withdrawal", withdrawalID}); err != nil {
		t.Fatalf("approve -legal-hold-withdrawal: %v", err)
	}
	reaching, err = legalhold.Reaching(ctx, pool, subject)
	if err != nil {
		t.Fatalf("Reaching: %v", err)
	}
	if reaching {
		t.Errorf("the hold still reaches its subject after the row that decides its withdrawal closed")
	}

	// The row fired and was closed: an open event and a close event, and the
	// close says the one owner both wrote the withdrawal and decided it.
	after := decisionCount(t, ctx, pool)
	if after.opens != before.opens+1 || after.closes != before.closes+1 {
		t.Errorf("the approval left %d openings and %d closings, want one more of each",
			after.opens-before.opens, after.closes-before.closes)
	}
}

// counted is how many openings and closings the log holds, which is what says
// the approval fired a row rather than writing the record on its own.
type counted struct{ opens, closes int }

func decisionCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool) counted {
	t.Helper()
	var found counted
	if err := pool.QueryRow(ctx, `select
		count(*) filter (where part = $1), count(*) filter (where part = $2)
		from `+decisionlog.Table+` where shape = $3`,
		string(decisionlog.PartOpen), string(decisionlog.PartClose),
		string(decisionlog.ShapeDecision)).Scan(&found.opens, &found.closes); err != nil {
		t.Fatalf("counting the decisions in the log: %v", err)
	}
	return found
}
