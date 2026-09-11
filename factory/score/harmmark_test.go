// The harm mark on the vector: a report grouped into the item's intent saying a
// person is being harmed by the software, read beside the source and taking the
// source's own treatment at Spec.
//
// It is in score_test for the reason report_test.go is: the fixture applies the
// whole factory schema through package postgres, which reaches this package
// back, so an internal test file importing postgres would make package score
// import itself. It does not skip when the database is unreachable.
package score_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
)

// groups is [score.GroupedReports] a test sets: how many reports are grouped
// into an intent and the ids of the ones marking harm, and the failure a
// reader that could not answer leaves.
type groups struct {
	group map[string]score.ReportGroup
	fails bool
	asked []string
}

func (g *groups) Grouped(_ context.Context, intentID string) (score.ReportGroup, error) {
	g.asked = append(g.asked, intentID)
	if g.fails {
		return score.ReportGroup{}, errors.New("the report store could not be reached")
	}
	return g.group[intentID], nil
}

// TestAHarmMarkedReportResolvesAtSpecBesideTheSource: the mark is recorded on
// the vector beside the source and takes the source's own treatment there — a
// human confirms the criteria whatever the rest of the vector says.
//
// It adds no gate a report did not already meet: the source of the same intent
// resolves that row too, so the row was a human's before the mark was read, and
// what the mark adds is that the human sees which one is marked. Away from Spec
// it is a level and resolves nothing, the way the source is.
func TestAHarmMarkedReportResolvesAtSpecBesideTheSource(t *testing.T) {
	ctx, pool, _, _, _, token := newReportStore(t)
	intake := intent.NewIntake(pool, token, intent.NoNotifier{})
	grouped, err := intake.TakeIn(ctx, marking, intent.Arrival{
		Source: intent.SourceReports, Statement: "2 end-user report(s) grouped as one problem",
	})
	if err != nil {
		t.Fatalf("TakeIn a report-derived intent: %v", err)
	}
	it, err := item.NewDecomposition(pool, token, item.NoHolds{}).Create(ctx, marking, item.New{
		IntentID: grouped.ID, ServiceID: "svc_marked", Branch: "item/marked",
		RequirementsAnswered: []string{record.NewID("rq")},
	}, "", "")
	if err != nil {
		t.Fatalf("writing the item: %v", err)
	}

	read := &groups{group: map[string]score.ReportGroup{grouped.ID: {Reports: 1, Marked: []string{"rep_1"}}}}
	scored := score.New(score.Composition{
		Pool: pool, Draw: score.NeverDraw{}, GroupedReports: read, Token: token,
	})
	at := func(spec bool) score.Assessment {
		t.Helper()
		assessed, err := scored.Assess(ctx, score.Change{
			ItemID: it.ID, ServiceID: "svc_marked", FactorSet: score.SetAboveABuild, AtSpec: spec,
		})
		if err != nil {
			t.Fatalf("Assess at spec=%v: %v", spec, err)
		}
		return assessed
	}

	// At Spec the mark is a resolution beside the source's, and the vector
	// carries both so the human deciding sees which one is marked.
	spec := at(true)
	causes := map[score.Cause]string{}
	for _, resolved := range spec.Resolved {
		causes[resolved.Cause] = resolved.Factor
	}
	if causes[score.CauseHarmMarkedReport] != "context.harm_marked_report" {
		t.Errorf("the Spec row resolved %v, want the harm mark among them", spec.Resolved)
	}
	if causes[score.CauseReportSourcedIntent] != "context.intent_source" {
		t.Errorf("the Spec row resolved %v, want the source beside the mark", spec.Resolved)
	}
	marked, found := factorNamed(spec, "context.harm_marked_report")
	if !found {
		t.Fatalf("the vector carries no harm mark: %+v", spec.Vector)
	}
	if marked.Resolved == "" || marked.Reading == "" {
		t.Errorf("the mark on the vector is %+v, want it resolved and readable", marked)
	}
	if marked.Weight != 0 {
		t.Errorf("the mark is weighed at %v, and it adds no gate a report did not already meet", marked.Weight)
	}

	// Away from Spec it is a level and resolves nothing, the way the source is.
	if above := at(false); resolvedOn(above, "context.harm_marked_report") {
		t.Errorf("the mark resolved away from Spec: %v", above.Resolved)
	}

	// An intent no report of which marks harm reads as nothing marked and not
	// as a factor that could not be computed: no report is not an unreadable
	// report.
	read.group = map[string]score.ReportGroup{}
	unmarked := at(true)
	if resolvedOn(unmarked, "context.harm_marked_report") {
		t.Errorf("an unmarked group resolved the mark: %v", unmarked.Resolved)
	}
	quiet, _ := factorNamed(unmarked, "context.harm_marked_report")
	if quiet.Level != 0 || quiet.Reading == "" {
		t.Errorf("an unmarked group reads %+v, want a level of nothing with words", quiet)
	}
	if len(read.asked) == 0 || read.asked[0] != grouped.ID {
		t.Errorf("the seam was asked about %v, want the intent the item answers", read.asked)
	}

	// A reader that could not answer is an unavailable input, which resolves on
	// that account and not on the mark's.
	read.fails = true
	down := at(true)
	if !resolvedOn(down, "context.harm_marked_report") {
		t.Error("a reader that could not answer left the mark weighed")
	}
	for _, resolved := range down.Resolved {
		if resolved.Factor == "context.harm_marked_report" && resolved.Cause != score.CauseUnavailable {
			t.Errorf("a reader that could not answer resolved as %q, want unavailable", resolved.Cause)
		}
	}
}

// marking is the actor these fixtures write as.
var marking = record.Actor{Kind: record.KindComponent, Key: "intake", Basis: record.BasisClaimed}

// factorNamed is one factor of an assessment's vector.
func factorNamed(a score.Assessment, name string) (score.Factor, bool) {
	for _, f := range a.Vector {
		if f.Name == name {
			return f, true
		}
	}
	return score.Factor{}, false
}

// resolvedOn reports whether the assessment resolved one factor.
func resolvedOn(a score.Assessment, name string) bool {
	for _, resolved := range a.Resolved {
		if resolved.Factor == name {
			return true
		}
	}
	return false
}
