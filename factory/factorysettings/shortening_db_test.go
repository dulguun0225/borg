package factorysettings_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/factorysettings"
)

// TestAShorteningIsWrittenPendingAndApprovedOnce: a shorter decision-log
// retention value is a record of its own, written pending with the actor who
// authored it on it — which is what the gate row that decides it is routed away
// from — and marked approved by a second write. A second approval is refused:
// that would be a second decision on one row.
func TestAShorteningIsWrittenPendingAndApprovedOnce(t *testing.T) {
	ctx, pool, _, token := newTable(t)

	var written factorysettings.Shortening
	inTx(t, ctx, pool, func(tx pgx.Tx) error {
		var err error
		written, err = factorysettings.InsertShortening(ctx, tx, token, owner, 30*24*3600)
		return err
	})
	if written.Seconds != 30*24*3600 || written.Approved || written.ApprovedAt != "" {
		t.Fatalf("the shortening reads %+v, want thirty days pending", written)
	}

	read, err := factorysettings.GetShortening(ctx, pool, written.ID)
	if err != nil {
		t.Fatalf("GetShortening: %v", err)
	}
	if read.Actor.Key != owner.Key || read.Approved {
		t.Errorf("the shortening reads back as %+v, want the owner's and pending", read)
	}

	inTx(t, ctx, pool, func(tx pgx.Tx) error {
		return factorysettings.ApproveShortening(ctx, tx, token, written.ID)
	})
	read, err = factorysettings.GetShortening(ctx, pool, written.ID)
	if err != nil {
		t.Fatalf("GetShortening: %v", err)
	}
	if !read.Approved || read.ApprovedAt == "" {
		t.Errorf("the approved shortening reads %+v, want approved with a time on it", read)
	}

	// A second approval, a value of nothing, and an id nothing wrote.
	tx := begin(t, ctx, pool)
	if err := factorysettings.ApproveShortening(ctx, tx, token, written.ID); !errors.Is(err,
		factorysettings.ErrShorteningAlreadyApproved) {
		t.Errorf("approving one shortening twice = %v, want ErrShorteningAlreadyApproved", err)
	}
	if _, err := factorysettings.InsertShortening(ctx, tx, token, owner, 0); !errors.Is(err,
		factorysettings.ErrRetentionNotPositive) {
		t.Errorf("a shortening to nothing = %v, want ErrRetentionNotPositive", err)
	}
	if err := factorysettings.ApproveShortening(ctx, tx, token, "fss_nothing"); !errors.Is(err,
		factorysettings.ErrShorteningNotFound) {
		t.Errorf("approving a record nothing wrote = %v, want ErrShorteningNotFound", err)
	}
	if _, err := factorysettings.GetShortening(ctx, pool, "fss_nothing"); !errors.Is(err,
		factorysettings.ErrShorteningNotFound) {
		t.Errorf("GetShortening of a record nothing wrote = %v, want ErrShorteningNotFound", err)
	}
}

// TestShorteningsAwaitingADecisionAreWhatFactoryLists: the row outside every
// item that decides a shortening is fired and closed in the one call that takes
// the verdict, so what waits for a disposition is a shortening standing
// unapproved and not an open event.
func TestShorteningsAwaitingADecisionAreWhatFactoryLists(t *testing.T) {
	ctx, pool, _, token := newTable(t)

	awaiting, err := factorysettings.ShorteningsAwaitingADecision(ctx, pool)
	if err != nil {
		t.Fatalf("ShorteningsAwaitingADecision: %v", err)
	}
	if len(awaiting) != 0 {
		t.Fatalf("a store with no shortening has %d awaiting a decision", len(awaiting))
	}

	var written factorysettings.Shortening
	inTx(t, ctx, pool, func(tx pgx.Tx) error {
		var err error
		written, err = factorysettings.InsertShortening(ctx, tx, token, owner, 30*24*3600)
		return err
	})

	awaiting, err = factorysettings.ShorteningsAwaitingADecision(ctx, pool)
	if err != nil {
		t.Fatalf("ShorteningsAwaitingADecision: %v", err)
	}
	if len(awaiting) != 1 || awaiting[0].ID != written.ID || awaiting[0].Seconds != 30*24*3600 {
		t.Fatalf("the shortenings awaiting a decision are %+v, want the one just written", awaiting)
	}
	if awaiting[0].Actor.Key != owner.Key {
		t.Errorf("the shortening awaiting a decision names %q, want the owner who authored the value",
			awaiting[0].Actor.Key)
	}

	inTx(t, ctx, pool, func(tx pgx.Tx) error {
		return factorysettings.ApproveShortening(ctx, tx, token, written.ID)
	})
	awaiting, err = factorysettings.ShorteningsAwaitingADecision(ctx, pool)
	if err != nil {
		t.Fatalf("ShorteningsAwaitingADecision: %v", err)
	}
	if len(awaiting) != 0 {
		t.Errorf("a shortening a row approved is still awaiting a decision: %+v", awaiting)
	}
}
