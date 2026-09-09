package reportstore_test

import (
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/reportstore"
)

// counted is the counter kept for one service, the empty service being the
// whole channel, and zeroes where no counter has been written.
func counted(t *testing.T, counts []reportstore.Count, serviceID string) reportstore.Count {
	t.Helper()
	for _, c := range counts {
		if c.ServiceID == serviceID {
			return c
		}
	}
	return reportstore.Count{ServiceID: serviceID}
}

// TestTheStoredReportNamesTheDeploysServiceAndNotTheSubmissions: which
// service a submission counts against is never what the submission says. The
// store is handed a submission carrying no service at all and the row names
// the one on the deploy record the token's digest found.
func TestTheStoredReportNamesTheDeploysServiceAndNotTheSubmissions(t *testing.T) {
	ctx, _, store, s := newStore(t)
	placed := deploy()
	s.place("tok", placed)

	collected := time.Now()
	written, err := store.Submit(ctx, submission("tok"), collected)
	if err != nil || !written.Accepted {
		t.Fatalf("Submit = %+v, %v; want an accepted report", written, err)
	}
	report := written.Report
	if report.DeployID != placed.ID || report.ServiceID != placed.ServiceID ||
		report.EnvironmentID != placed.EnvironmentID {
		t.Errorf("the report names %s %s %s, want the deploy record's %+v",
			report.DeployID, report.ServiceID, report.EnvironmentID, placed)
	}
	if report.ShippedBundleIdentity != "0.1.0" || report.NoticeID != "" ||
		report.CollectedAt == "" || report.IntentID != "" || report.AdmittedAt != "" {
		t.Errorf("the report is %+v, want the identity on the row, no notice, and ungrouped", report)
	}
}

// TestTheHarmMarkIsTheReportersAndNothingInfersIt: the field is written as
// the submission set it, and text describing harm sets nothing.
func TestTheHarmMarkIsTheReportersAndNothingInfersIt(t *testing.T) {
	ctx, _, store, s := newStore(t)
	s.place("tok", deploy())

	unmarked := submission("tok")
	unmarked.Text = "this is harming a person and someone is being hurt"
	written, err := store.Submit(ctx, unmarked, time.Now())
	if err != nil || !written.Accepted {
		t.Fatalf("Submit = %+v, %v", written, err)
	}
	if written.Report.HarmMarked {
		t.Error("a report whose words describe harm was marked; nothing infers the field")
	}

	marked := submission("tok")
	marked.HarmMarked = true
	marked.Text = "the page is slow"
	written, err = store.Submit(ctx, marked, time.Now())
	if err != nil || !written.Accepted {
		t.Fatalf("Submit: %+v, %v", written, err)
	}
	if !written.Report.HarmMarked {
		t.Error("the reporter's own mark was not written")
	}
}

// TestASubmissionNamingNoDeployIsCountedOnTheChannelAndNeverOnAService:
// a safeguard narrowing one service's rate is not evaded by submitting under
// another service's name, because the store never learns a service from a
// submission it cannot place.
func TestASubmissionNamingNoDeployIsCountedOnTheChannelAndNeverOnAService(t *testing.T) {
	ctx, _, store, s := newStore(t)
	placed := deploy()
	s.place("tok", placed)

	refused, err := store.Submit(ctx, submission("some other token"), time.Now())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if refused.Accepted || refused.Refusal != reportstore.RefusedNoDeploy {
		t.Errorf("Submit = %+v, want it refused for naming no deploy", refused)
	}
	counts, err := store.Counts(ctx)
	if err != nil {
		t.Fatalf("Counts: %v", err)
	}
	if got := counted(t, counts, "").Refusals; got != 1 {
		t.Errorf("the channel counted %d refusals, want 1", got)
	}
	if len(counts) != 1 {
		t.Errorf("the counters are %+v, want the channel's alone and nothing per service", counts)
	}
}

// TestASubmissionUnderAShapeTheStoreDoesNotReadIsCountedAsARefusal: a
// submission of a shape this store does not read is itself a refusal, and is
// counted on the service and on the whole channel the way any other is —
// which bound refused it is on the result and not on the counter.
func TestASubmissionUnderAShapeTheStoreDoesNotReadIsCountedAsARefusal(t *testing.T) {
	ctx, _, store, s := newStore(t)
	placed := deploy()
	s.place("tok", placed)

	ahead := submission("tok")
	ahead.Shape = "submission/9"
	ahead.Kind = ""
	ahead.Text = ""
	refused, err := store.Submit(ctx, ahead, time.Now())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if refused.Accepted || refused.Refusal != reportstore.RefusedShape {
		t.Errorf("Submit = %+v, want it refused for its shape", refused)
	}
	counts, err := store.Counts(ctx)
	if err != nil {
		t.Fatalf("Counts: %v", err)
	}
	if got := counted(t, counts, placed.ServiceID).Refusals; got != 1 {
		t.Errorf("the service counted %d refusals, want 1", got)
	}
	if got := counted(t, counts, "").Refusals; got != 1 {
		t.Errorf("the channel counted %d refusals, want the one counted on it too", got)
	}
}

// TestAChannelRateOfZeroClosesTheChannel: the factory-wide rate authored to
// zero refuses everything, a report marking harm included, and each refusal
// is counted on the channel and on the service.
func TestAChannelRateOfZeroClosesTheChannel(t *testing.T) {
	ctx, _, store, s := newStore(t)
	placed := deploy()
	s.place("tok", placed)
	s.channel = reportstore.Rate{Reports: 0, Authored: true}

	for _, marked := range []bool{false, true} {
		sub := submission("tok")
		sub.HarmMarked = marked
		refused, err := store.Submit(ctx, sub, time.Now())
		if err != nil {
			t.Fatalf("Submit: %v", err)
		}
		if refused.Accepted || refused.Refusal != reportstore.RefusedChannelRate {
			t.Errorf("Submit with harm marked %v = %+v, want it refused by the channel's rate", marked, refused)
		}
	}
	counts, err := store.Counts(ctx)
	if err != nil {
		t.Fatalf("Counts: %v", err)
	}
	if counted(t, counts, "").Refusals != 2 || counted(t, counts, placed.ServiceID).Refusals != 2 {
		t.Errorf("the counters are %+v, want both refusals on the channel and on the service", counts)
	}
}

// TestAServiceRateOfZeroClosesOneServicesWayIn: a safeguard narrowing one
// service's rate to zero closes that service and leaves every other open.
func TestAServiceRateOfZeroClosesOneServicesWayIn(t *testing.T) {
	ctx, _, store, s := newStore(t)
	closed, open := deploy(), deploy()
	s.place("closed", closed)
	s.place("open", open)
	s.services[closed.ServiceID] = reportstore.Rate{Reports: 0, Authored: true}

	refused, err := store.Submit(ctx, submission("closed"), time.Now())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if refused.Accepted || refused.Refusal != reportstore.RefusedServiceRate {
		t.Errorf("Submit = %+v, want it refused by the service's rate", refused)
	}
	written, err := store.Submit(ctx, submission("open"), time.Now())
	if err != nil || !written.Accepted {
		t.Errorf("Submit against the open service = %+v, %v; want it accepted", written, err)
	}
}

// TestAShareOfTheRateIsReservedForAReportMarkingHarm: a surge drops an
// ordinary report before it drops a marked one — the store refuses an
// unmarked report once the ordinary share is spent and a marked one only once
// the whole rate is.
func TestAShareOfTheRateIsReservedForAReportMarkingHarm(t *testing.T) {
	ctx, _, store, s := newStore(t)
	s.place("tok", deploy())
	// Four reports an hour, of which one is reserved: the three ordinary ones
	// arrive, the fourth unmarked is refused, and a marked one still arrives.
	s.channel = reportstore.Rate{Reports: 4, Authored: true}

	ordinary := submission("tok")
	ordinary.SourceKey = "" // the source's own share is a bound of its own
	for n := range 3 {
		written, err := store.Submit(ctx, ordinary, time.Now())
		if err != nil || !written.Accepted {
			t.Fatalf("Submit %d = %+v, %v; want it under the ordinary share", n, written, err)
		}
	}
	refused, err := store.Submit(ctx, ordinary, time.Now())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if refused.Accepted || refused.Refusal != reportstore.RefusedChannelRate {
		t.Errorf("the fourth unmarked report = %+v, want it refused once the ordinary share is spent", refused)
	}

	marked := ordinary
	marked.HarmMarked = true
	written, err := store.Submit(ctx, marked, time.Now())
	if err != nil || !written.Accepted {
		t.Fatalf("the marked report = %+v, %v; want the reserved share to admit it", written, err)
	}
	if refused, err = store.Submit(ctx, marked, time.Now()); err != nil || refused.Accepted {
		t.Errorf("a marked report past the whole rate = %+v, %v; want it refused too", refused, err)
	}
}

// TestASourceSpendsItsOwnShareAndNoMore: the store rates a source by the
// opaque key, so a source that fills its share leaves the rest of the rate to
// everyone else. A fresh session is a fresh key, so what this bounds is a
// session and no longer.
func TestASourceSpendsItsOwnShareAndNoMore(t *testing.T) {
	ctx, _, store, s := newStore(t)
	s.place("tok", deploy())
	// Eight an hour leaves one source two of them.
	s.channel = reportstore.Rate{Reports: 8, Authored: true}

	one := submission("tok")
	one.SourceKey = "src_one"
	for n := range 2 {
		written, err := store.Submit(ctx, one, time.Now())
		if err != nil || !written.Accepted {
			t.Fatalf("Submit %d from one source = %+v, %v", n, written, err)
		}
	}
	refused, err := store.Submit(ctx, one, time.Now())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if refused.Accepted || refused.Refusal != reportstore.RefusedSourceRate {
		t.Errorf("the third report from one source = %+v, want it refused by the source's share", refused)
	}

	two := submission("tok")
	two.SourceKey = "src_two"
	written, err := store.Submit(ctx, two, time.Now())
	if err != nil || !written.Accepted {
		t.Errorf("a second source = %+v, %v; want the rest of the rate left to it", written, err)
	}
}

// TestNoRateAuthoredIsUnbounded: where an owner authored neither rate,
// arrival is bounded by nothing here — no outcome teaches a rate, so there is
// nothing to supply where one was not authored.
func TestNoRateAuthoredIsUnbounded(t *testing.T) {
	ctx, _, store, s := newStore(t)
	s.place("tok", deploy())

	for n := range 12 {
		written, err := store.Submit(ctx, submission("tok"), time.Now())
		if err != nil || !written.Accepted {
			t.Fatalf("Submit %d = %+v, %v; want no bound where none is authored", n, written, err)
		}
	}
}

// TestASubmissionOutsideTheRatePeriodDoesNotSpendIt: a rate is counted over
// [reportstore.RatePeriod], so what arrived before it is spent.
func TestASubmissionOutsideTheRatePeriodDoesNotSpendIt(t *testing.T) {
	ctx, _, store, s := newStore(t)
	s.place("tok", deploy())
	s.channel = reportstore.Rate{Reports: 2, Authored: true}

	old := submission("tok")
	old.SourceKey = ""
	if written, err := store.Submit(ctx, old, time.Now().Add(-2*reportstore.RatePeriod)); err != nil || !written.Accepted {
		t.Fatalf("the earlier report = %+v, %v", written, err)
	}
	written, err := store.Submit(ctx, old, time.Now())
	if err != nil || !written.Accepted {
		t.Errorf("Submit = %+v, %v; want the report outside the period not to have spent the rate", written, err)
	}
}
