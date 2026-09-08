package reportstore_test

import (
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/principal"
)

// TestWhereNoRetentionIsAuthoredNothingIsRemoved: no outcome teaches a
// retention period, so where an owner authored none reports are kept for the
// life of the install.
func TestWhereNoRetentionIsAuthoredNothingIsRemoved(t *testing.T) {
	ctx, _, store, s := newStore(t)
	s.place("tok", deploy())

	if _, err := store.Submit(ctx, submission("tok"), time.Now().Add(-10000*time.Hour)); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	retired, err := store.Retire(ctx, time.Now())
	if err != nil {
		t.Fatalf("Retire: %v", err)
	}
	if retired.Removed != 0 || retired.Held != nil {
		t.Errorf("Retire = %+v, want nothing removed where nothing is authored", retired)
	}
	if ungrouped, err := store.Ungrouped(ctx); err != nil || ungrouped != 1 {
		t.Errorf("Ungrouped = %d, %v; want the report still there", ungrouped, err)
	}
}

// TestRetentionRemovesWhatIsPastItAndKeepsTheRest: the retention an owner
// authored is what removes a report, and grouping never does.
func TestRetentionRemovesWhatIsPastItAndKeepsTheRest(t *testing.T) {
	ctx, _, store, s := newStore(t)
	s.place("tok", deploy())
	s.retention, s.authored = 24*time.Hour, true

	now := time.Now()
	expired, err := store.Submit(ctx, submission("tok"), now.Add(-48*time.Hour))
	if err != nil || !expired.Accepted {
		t.Fatalf("Submit: %+v, %v", expired, err)
	}
	kept, err := store.Submit(ctx, submission("tok"), now.Add(-time.Hour))
	if err != nil || !kept.Accepted {
		t.Fatalf("Submit: %+v, %v", kept, err)
	}

	retired, err := store.Retire(ctx, now)
	if err != nil {
		t.Fatalf("Retire: %v", err)
	}
	if retired.Removed != 1 || retired.Held != nil {
		t.Errorf("Retire = %+v, want the one expired report removed", retired)
	}
	if _, err := store.Get(ctx, principal.OfComponent("factory"), expired.Report.ID); err == nil {
		t.Error("the expired report is still readable")
	}
	if _, err := store.Get(ctx, principal.OfComponent("factory"), kept.Report.ID); err != nil {
		t.Errorf("the report inside the retention is gone: %v", err)
	}
}

// TestALegalHoldSuspendsOneServicesRemovalsAndNotThePass: a hold standing
// over a service keeps that service's reports until it is withdrawn, and the
// pass goes on to the services no hold reaches.
func TestALegalHoldSuspendsOneServicesRemovalsAndNotThePass(t *testing.T) {
	ctx, _, store, s := newStore(t)
	held, free := deploy(), deploy()
	s.place("held", held)
	s.place("free", free)
	s.retention, s.authored = 24*time.Hour, true
	s.held[held.ServiceID] = true

	now := time.Now()
	if _, err := store.Submit(ctx, submission("held"), now.Add(-48*time.Hour)); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := store.Submit(ctx, submission("free"), now.Add(-48*time.Hour)); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	retired, err := store.Retire(ctx, now)
	if err != nil {
		t.Fatalf("Retire: %v", err)
	}
	if retired.Removed != 1 {
		t.Errorf("Retire removed %d, want the report of the service no hold reaches", retired.Removed)
	}
	if len(retired.Held) != 1 || retired.Held[0] != held.ServiceID {
		t.Errorf("Retire held %v, want the held service named so the refusal is recorded", retired.Held)
	}
	if ungrouped, err := store.Ungrouped(ctx); err != nil || ungrouped != 1 {
		t.Errorf("Ungrouped = %d, %v; want the held service's report still there", ungrouped, err)
	}

	// The hold is withdrawn and the same pass removes what it was suspending.
	s.held[held.ServiceID] = false
	retired, err = store.Retire(ctx, now)
	if err != nil {
		t.Fatalf("Retire after the withdrawal: %v", err)
	}
	if retired.Removed != 1 || retired.Held != nil {
		t.Errorf("Retire = %+v, want the removal the hold suspended", retired)
	}
}
