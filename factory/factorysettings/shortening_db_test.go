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
