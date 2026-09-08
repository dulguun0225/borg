// The admission a report-derived intent waits for, which only a safeguard on
// the report store makes it wait for. Split from db_test.go by subject at the
// length a file is held to, sharing its fixtures and package.
package intent_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/record"
)

// TestAReportDerivedIntentIsAdmittedOnceByAHuman: an intent grouped from
// reports carries the admission a safeguard on the report store makes it wait
// for. It arrives unadmitted whether or not that safeguard stands — nothing
// here reads one — and the write is a human's at Work, once: admitting one
// already admitted leaves the first admission's instant where it is.
//
// No other source takes one. An owner's request and a detector's intent arrive
// through no channel a stranger writes into, so an admission on either would
// record a human act nobody ever asked for.
func TestAReportDerivedIntentIsAdmittedOnceByAHuman(t *testing.T) {
	ctx, pool, in := newIntake(t)

	grouped, err := in.TakeIn(ctx, intake, intent.Arrival{
		Source: intent.SourceReports, Statement: "2 end-user report(s) grouped as one problem",
	})
	if err != nil {
		t.Fatalf("TakeIn a report-derived intent: %v", err)
	}
	if grouped.AdmittedAt != "" {
		t.Errorf("the intent arrived admitted at %q, want it waiting", grouped.AdmittedAt)
	}

	if err := in.Admit(ctx, owner, grouped.ID); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	admitted, err := intent.Get(ctx, pool, grouped.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if admitted.AdmittedAt == "" {
		t.Fatal("the admitted intent carries no admission")
	}
	if _, err := record.ParseTime(admitted.AdmittedAt); err != nil {
		t.Errorf("the admission reads %q: %v", admitted.AdmittedAt, err)
	}

	// Admitting one already admitted changes nothing, so the instant on the row
	// is the first admission's and not the last caller's.
	if err := in.Admit(ctx, owner, grouped.ID); err != nil {
		t.Fatalf("Admit an intent already admitted: %v", err)
	}
	again, err := intent.Get(ctx, pool, grouped.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if again.AdmittedAt != admitted.AdmittedAt {
		t.Errorf("the second admission moved the instant from %q to %q",
			admitted.AdmittedAt, again.AdmittedAt)
	}

	// Neither of the other two sources is admitted, in the writer and again in
	// the store.
	asked := requested(t, ctx, in, "let a customer export their invoices")
	if err := in.Admit(ctx, owner, asked.ID); !errors.Is(err, intent.ErrAdmissionNotFromReports) {
		t.Errorf("Admit an owner's request = %v, want ErrAdmissionNotFromReports", err)
	}
	found := raised(t, ctx, in, crossing, "the checkout service is failing its window")
	if err := in.Admit(ctx, owner, found.ID); !errors.Is(err, intent.ErrAdmissionNotFromReports) {
		t.Errorf("Admit a detector's intent = %v, want ErrAdmissionNotFromReports", err)
	}
	if _, err := pool.Exec(ctx, `update `+intent.Table+` set admitted_at = $1 where id = $2`,
		record.Now(), asked.ID); err == nil {
		t.Error("the store admitted an owner's request, and only a report-derived intent takes one")
	}
	if _, err := pool.Exec(ctx, `update `+intent.Table+` set admitted_at = 'yesterday' where id = $1`,
		grouped.ID); err == nil {
		t.Error("the store took an admission that is no instant")
	}
}
