// The fields of the factory-wide settings record beside the attempt limit and
// the list of allowed predicate kinds: the sample rates, the advisory severity
// and its remediation period, retention, the report channel's rates and the harm
// mark's page cap, and whether seam 5 is enforced. The harness is db_test.go's.
package factorysettings_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/factorysettings"
)

// TestTheTwoSampleRates: the held-out rate is one field of the record, the sample
// being one formula's and no service's, and the review sample rate is per duty,
// a duty being the factory's own the way a stage is.
func TestTheTwoSampleRates(t *testing.T) {
	ctx, pool, w := newTable(t)
	settings, err := w.Ensure(ctx, owner)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if settings.HeldOutSampleRate.Present {
		t.Errorf("a freshly created record carries a held-out sample rate: %+v", settings.HeldOutSampleRate)
	}

	inTx(t, ctx, pool, func(tx pgx.Tx) error {
		return factorysettings.SetHeldOutSampleRate(ctx, tx, settings.ID, 0.1)
	})
	inTx(t, ctx, pool, func(tx pgx.Tx) error {
		return factorysettings.SetReviewSampleRate(ctx, tx, owner, settings.ID, 10, 0.25)
	})

	read, err := factorysettings.Get(ctx, pool)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !read.HeldOutSampleRate.Present || read.HeldOutSampleRate.Number != 0.1 {
		t.Errorf("the held-out sample rate reads back as %+v, want 0.1 present", read.HeldOutSampleRate)
	}
	rate, err := factorysettings.ReviewSampleRate(ctx, pool, settings.ID, 10)
	if err != nil {
		t.Fatalf("ReviewSampleRate: %v", err)
	}
	if !rate.Present || rate.Number != 0.25 {
		t.Errorf("duty 10's review sample rate reads back as %+v, want 0.25 present", rate)
	}
	unauthored, err := factorysettings.ReviewSampleRate(ctx, pool, settings.ID, 9)
	if err != nil || unauthored.Present {
		t.Errorf("duty 9's unauthored rate reads back as %+v, %v", unauthored, err)
	}

	tx := begin(t, ctx, pool)
	if err := factorysettings.SetHeldOutSampleRate(ctx, tx, settings.ID, 1.5); !errors.Is(err, factorysettings.ErrRateOutOfRange) {
		t.Errorf("a held-out rate above one = %v, want ErrRateOutOfRange", err)
	}
	if err := factorysettings.SetReviewSampleRate(ctx, tx, owner, settings.ID, 13, 0.2); !errors.Is(err, factorysettings.ErrDutyOutOfRange) {
		t.Errorf("a review sample rate on a thirteenth duty = %v, want ErrDutyOutOfRange", err)
	}
	if err := factorysettings.SetReviewSampleRate(ctx, tx, owner, settings.ID, 4, -0.1); !errors.Is(err, factorysettings.ErrRateOutOfRange) {
		t.Errorf("a review sample rate below nothing = %v, want ErrRateOutOfRange", err)
	}
}

// TestTheAdvisorySeverityAndItsRemediationPeriod: the severity is one field of
// the record, one pass over one feed reaching every project at once, and the
// period is per severity and authored outright.
func TestTheAdvisorySeverityAndItsRemediationPeriod(t *testing.T) {
	ctx, pool, w := newTable(t)
	settings, err := w.Ensure(ctx, owner)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	inTx(t, ctx, pool, func(tx pgx.Tx) error {
		return factorysettings.SetAdvisorySeverity(ctx, tx, settings.ID, 7)
	})
	inTx(t, ctx, pool, func(tx pgx.Tx) error {
		return factorysettings.SetRemediationPeriod(ctx, tx, owner, settings.ID, 7, 72*60*60)
	})

	read, err := factorysettings.Get(ctx, pool)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !read.AdvisorySeverity.Present || read.AdvisorySeverity.Number != 7 {
		t.Errorf("the advisory severity reads back as %+v, want 7 present", read.AdvisorySeverity)
	}
	period, err := factorysettings.RemediationPeriod(ctx, pool, settings.ID, 7)
	if err != nil {
		t.Fatalf("RemediationPeriod: %v", err)
	}
	if !period.Present || period.Number != 72*60*60 {
		t.Errorf("the remediation period at severity 7 reads back as %+v", period)
	}

	tx := begin(t, ctx, pool)
	if err := factorysettings.SetAdvisorySeverity(ctx, tx, settings.ID, -1); !errors.Is(err, factorysettings.ErrSeverityNegative) {
		t.Errorf("a severity below nothing = %v, want ErrSeverityNegative", err)
	}
	if err := factorysettings.SetRemediationPeriod(ctx, tx, owner, settings.ID, 7, 0); !errors.Is(err, factorysettings.ErrPeriodNotPositive) {
		t.Errorf("a remediation period of nothing = %v, want ErrPeriodNotPositive", err)
	}
}

// TestNeitherAnAuthoredValueNorASafeguardGoesUnderTheRetentionFloor: the floor
// bounds decision-log retention, and the refusal is in the writer and in the
// store around it. The other three retentions are fields with no floor.
func TestNeitherAnAuthoredValueNorASafeguardGoesUnderTheRetentionFloor(t *testing.T) {
	ctx, pool, w := newTable(t)
	settings, err := w.Ensure(ctx, owner)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	inTx(t, ctx, pool, func(tx pgx.Tx) error {
		return factorysettings.SetRetentionFloor(ctx, tx, settings.ID, 90*24*60*60)
	})
	inTx(t, ctx, pool, func(tx pgx.Tx) error {
		return factorysettings.SetDecisionLogRetention(ctx, tx, settings.ID, 365*24*60*60)
	})
	inTx(t, ctx, pool, func(tx pgx.Tx) error {
		return factorysettings.SetReportRetention(ctx, tx, settings.ID, 30*24*60*60)
	})
	inTx(t, ctx, pool, func(tx pgx.Tx) error {
		return factorysettings.SetBackupRetention(ctx, tx, settings.ID, 14*24*60*60)
	})

	read, err := factorysettings.Get(ctx, pool)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.RetentionFloorSeconds.Number != 90*24*60*60 ||
		read.DecisionLogRetentionSeconds.Number != 365*24*60*60 ||
		read.ReportRetentionSeconds.Number != 30*24*60*60 ||
		read.BackupRetentionSeconds.Number != 14*24*60*60 {
		t.Errorf("the retentions read back as %+v", read)
	}

	tx := begin(t, ctx, pool)
	if err := factorysettings.SetDecisionLogRetention(ctx, tx, settings.ID, 24*60*60); !errors.Is(err, factorysettings.ErrUnderTheRetentionFloor) {
		t.Errorf("a decision-log retention under the floor = %v, want ErrUnderTheRetentionFloor", err)
	}
	if err := factorysettings.SetReportRetention(ctx, tx, settings.ID, 0); !errors.Is(err, factorysettings.ErrRetentionNotPositive) {
		t.Errorf("a report retention of nothing = %v, want ErrRetentionNotPositive", err)
	}
	if _, err := pool.Exec(ctx, `update `+factorysettings.Table+`
		set decision_log_retention_seconds = 60 where id = $1`, settings.ID); err == nil {
		t.Error("the store accepted a decision-log retention written under the floor around the writer")
	}
}

// TestTheReportChannelsRatesAndTheHarmMarksPageCap: two rates, per service and
// factory-wide; a cap per service with the interval it is counted over, shipped
// with a default; and the field an owner who will not be woken by a stranger
// turns off.
func TestTheReportChannelsRatesAndTheHarmMarksPageCap(t *testing.T) {
	ctx, pool, w := newTable(t)
	settings, err := w.Ensure(ctx, owner)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !settings.HarmMarkPages {
		t.Error("the harm mark's page ships off, and it ships on")
	}

	shipped, err := factorysettings.HarmMarkPageCap(ctx, pool, settings.ID, "svc_a")
	if err != nil {
		t.Fatalf("HarmMarkPageCap: %v", err)
	}
	if shipped.Authored || shipped.Cap != factorysettings.DefaultHarmMarkPageCap ||
		shipped.IntervalSeconds != factorysettings.DefaultHarmMarkPageInterval {
		t.Errorf("the unauthored cap reads back as %+v, want the shipped default", shipped)
	}

	inTx(t, ctx, pool, func(tx pgx.Tx) error {
		return factorysettings.SetReportChannelRate(ctx, tx, settings.ID, 100)
	})
	inTx(t, ctx, pool, func(tx pgx.Tx) error {
		return factorysettings.SetServiceReportChannelRate(ctx, tx, owner, settings.ID, "svc_a", 0)
	})
	inTx(t, ctx, pool, func(tx pgx.Tx) error {
		return factorysettings.SetHarmMarkPageCap(ctx, tx, owner, settings.ID, "svc_a", 1, 60*60)
	})
	inTx(t, ctx, pool, func(tx pgx.Tx) error {
		return factorysettings.SetHarmMarkPages(ctx, tx, settings.ID, false)
	})

	read, err := factorysettings.Get(ctx, pool)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !read.ReportChannelRate.Present || read.ReportChannelRate.Number != 100 || read.HarmMarkPages {
		t.Errorf("the record reads back as %+v, want the factory-wide rate 100 and the page off", read)
	}
	// Zero is a rate an owner may mean: it closes that service's way in.
	rate, err := factorysettings.ReportChannelRate(ctx, pool, settings.ID, "svc_a")
	if err != nil {
		t.Fatalf("ReportChannelRate: %v", err)
	}
	if !rate.Present || rate.Number != 0 {
		t.Errorf("the closed service's rate reads back as %+v, want 0 present", rate)
	}
	cap, err := factorysettings.HarmMarkPageCap(ctx, pool, settings.ID, "svc_a")
	if err != nil {
		t.Fatalf("HarmMarkPageCap: %v", err)
	}
	if !cap.Authored || cap.Cap != 1 || cap.IntervalSeconds != 60*60 {
		t.Errorf("the authored cap reads back as %+v", cap)
	}

	tx := begin(t, ctx, pool)
	if err := factorysettings.SetServiceReportChannelRate(ctx, tx, owner, settings.ID, "", 5); !errors.Is(err, factorysettings.ErrServiceEmpty) {
		t.Errorf("a per-service rate naming no service = %v, want ErrServiceEmpty", err)
	}
	if err := factorysettings.SetHarmMarkPageCap(ctx, tx, owner, settings.ID, "svc_a", -1, 60); !errors.Is(err, factorysettings.ErrPageCapNegative) {
		t.Errorf("a cap below nothing = %v, want ErrPageCapNegative", err)
	}
	if err := factorysettings.SetHarmMarkPageCap(ctx, tx, owner, settings.ID, "svc_a", 1, 0); !errors.Is(err, factorysettings.ErrIntervalNotPositive) {
		t.Errorf("a cap over no interval = %v, want ErrIntervalNotPositive", err)
	}
}

// TestSeam5IsTurnedOnOnceAndNeverOff: it is off at install, an owner turns it on,
// and the writer refuses turning it off again.
func TestSeam5IsTurnedOnOnceAndNeverOff(t *testing.T) {
	ctx, pool, w := newTable(t)
	settings, err := w.Ensure(ctx, owner)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if settings.Seam5Enforced {
		t.Error("seam 5 is enforced at install, and it is off there")
	}

	inTx(t, ctx, pool, func(tx pgx.Tx) error {
		return factorysettings.SetSeam5Enforced(ctx, tx, settings.ID, true)
	})
	read, err := factorysettings.Get(ctx, pool)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !read.Seam5Enforced {
		t.Error("seam 5 was turned on and reads back off")
	}

	tx := begin(t, ctx, pool)
	if err := factorysettings.SetSeam5Enforced(ctx, tx, settings.ID, false); !errors.Is(err, factorysettings.ErrSeam5NotTurnedOff) {
		t.Errorf("turning seam 5 off = %v, want ErrSeam5NotTurnedOff", err)
	}
}
