// The intent source factor against a real store: whatever raised the intent,
// a report the grouper has since grouped into it carries text the factory did
// not author, and the source resolves at Spec on that account whatever raised
// the intent first.
//
// It is in score_test for the reason report_test.go is: the fixture applies
// the whole factory schema through package postgres, which reaches this
// package back, so an internal test file importing postgres would make
// package score import itself. It does not skip when the database is
// unreachable.
package score_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
)

// TestADetectorIntentGroupedReportsResolvesTheSource: a detector-raised intent
// is asked about grouped reports like every other source, and a report the
// grouper has since grouped into it carries the same untrusted text, whatever
// raised the intent first.
func TestADetectorIntentGroupedReportsResolvesTheSource(t *testing.T) {
	ctx, pool, _, _, _, token := newReportStore(t)
	intake := intent.NewIntake(pool, token, intent.NoNotifier{})
	raised, err := intake.TakeIn(ctx, marking, intent.Arrival{
		Source: intent.SourceDetector, Statement: "a defect is live",
		Evidence: intent.Evidence{ServiceID: "svc_detected"},
	})
	if err != nil {
		t.Fatalf("TakeIn a detector-raised intent: %v", err)
	}
	it, err := item.NewDecomposition(pool, token, item.NoHolds{}).Create(ctx, marking, item.New{
		IntentID: raised.ID, ServiceID: "svc_detected", Branch: "item/detected",
		RequirementsAnswered: []string{record.NewID("rq")},
	}, "", "")
	if err != nil {
		t.Fatalf("writing the item: %v", err)
	}

	read := &groups{group: map[string]score.ReportGroup{raised.ID: {Reports: 2}}}
	scored := score.New(score.Composition{
		Pool: pool, Draw: score.NeverDraw{}, GroupedReports: read, Token: token,
	})
	assess := func(spec bool) score.Assessment {
		t.Helper()
		assessed, err := scored.Assess(ctx, score.Change{
			ItemID: it.ID, ServiceID: "svc_detected", FactorSet: score.SetAboveABuild, AtSpec: spec,
		})
		if err != nil {
			t.Fatalf("Assess at spec=%v: %v", spec, err)
		}
		return assessed
	}

	// Away from Spec, a report grouped into a detector-raised intent reads at
	// the top of the scale — the level away from Spec that every source
	// carrying untrusted text reads at — and not at the detector's own 0.4.
	above := assess(false)
	source, found := factorNamed(above, "context.intent_source")
	if !found {
		t.Fatalf("the vector carries no source factor: %+v", above.Vector)
	}
	if source.Level != 1.0 {
		t.Errorf("a detector intent with grouped reports read %v, want 1.0 away from Spec", source.Level)
	}

	// At Spec it resolves on the same account, whatever raised the intent.
	spec := assess(true)
	if !resolvedOn(spec, "context.intent_source") {
		t.Errorf("a detector intent with grouped reports did not resolve the source at Spec: %v", spec.Resolved)
	}
	for _, resolved := range spec.Resolved {
		if resolved.Factor == "context.intent_source" && resolved.Cause != score.CauseReportSourcedIntent {
			t.Errorf("the source resolved as %q, want %q", resolved.Cause, score.CauseReportSourcedIntent)
		}
	}

	if len(read.asked) == 0 || read.asked[0] != raised.ID {
		t.Errorf("the seam was not asked about the detector-raised intent: %v", read.asked)
	}

	// A detector intent with nothing grouped into it still reads its own
	// 0.4, which the fix must not disturb.
	read.group = map[string]score.ReportGroup{}
	quiet := assess(false)
	untouched, _ := factorNamed(quiet, "context.intent_source")
	if untouched.Level != 0.4 {
		t.Errorf("an unmarked detector intent read %v, want 0.4", untouched.Level)
	}
}
