// The grouper demonstrated: the reports that arrived through a deployed
// service's way in read once, the role dispatched over them, and what each
// group causes — an intent raised, a later report attached, a page for the one
// marking harm, and the two absences grouping is defined by.
package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/agentrun"
	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/safeguard"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/screens"
)

// The three reports the test submits. The first and the third are one problem
// and the second is another, which the fake model reads off the word each
// starts with: deciding whether two free-text reports are one problem is the
// model's work, so a test says which are by writing them that way.
const (
	firstReport  = "Saving the form hangs and never finishes"
	secondReport = "Exporting a list writes an empty file"
	thirdReport  = "Saving anything at all takes far longer than it used to"
)

// TestReportsBecomeIntents: two intents and not three, the first report of a
// group raising one and a later one attaching without rewriting it, the
// statement summarizing what the reports say, the source resolving a human onto
// the Spec row, the run record naming the project and the processing location,
// one page per intent for a report marking harm, and no gate row and no
// artifact version for any of it.
func TestReportsBecomeIntents(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	reports := newReports(t, ctx, &d)
	if _, err := run(ctx, d, of(theStatement)); err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}

	// The path that groups. run composed one of its own and returned nothing
	// but what it shipped, so this is a second composition over the same deps,
	// which is what every start of the process is.
	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path that groups: %v\n%s", err, out)
	}
	if p.grouper == nil {
		t.Fatal("a composition holding a report store composed no grouper")
	}
	ps := newPasses(p, nil, nil)

	// Nothing has arrived, so the pass reads no words and writes nothing.
	if moved, err := ps.Tick(ctx, passGrouper); err != nil {
		t.Fatalf("the grouper pass with no report: %v\n%s", err, out)
	} else if moved {
		t.Errorf("the grouper pass with no report reports that it moved something:\n%s", out)
	}

	socket := localtarget.WayInSocket(d.dir, theService)
	waitForTheWayIn(t, socket)
	client := overTheSocket(socket)

	decisions, versions := decisionsWritten(t, ctx, d), versionsWritten(t, ctx, d)
	reportThrough(t, client, firstReport, "bug", false)
	reportThrough(t, client, secondReport, "bug", false)

	if moved, err := ps.Tick(ctx, passGrouper); err != nil {
		t.Fatalf("the first grouper pass: %v\n%s", err, out)
	} else if !moved {
		t.Errorf("the grouper pass over two arrived reports announced nothing:\n%s", out)
	}

	// Two reports about two problems are two intents, each raised by the first
	// report of its group: nothing waited for a batch or a count.
	raised := reportIntents(t, ctx, d, p)
	if len(raised) != 2 {
		t.Fatalf("%d intent(s) were raised from two reports about two problems, want 2: %v\n%s",
			len(raised), raised, out)
	}
	ids := reports.ids(t, ctx)
	if len(ids) != 2 {
		t.Fatalf("the store holds %d report(s), want the two submitted", len(ids))
	}
	firstIntent := intentOfReport(t, ctx, d, ids[0])
	if firstIntent == "" || firstIntent == intentOfReport(t, ctx, d, ids[1]) {
		t.Fatalf("the two reports were grouped into one intent, want one each: %q", firstIntent)
	}

	// The statement summarizes the reports the intent was raised from, and the
	// intent names the project the reports arrived under.
	in := readIntent(t, ctx, d, firstIntent)
	if in.Source != intent.SourceReports {
		t.Errorf("the intent's source is %q, want %q", in.Source, intent.SourceReports)
	}
	if in.ProjectID != p.projectID {
		t.Errorf("the intent names project %q, want %q", in.ProjectID, p.projectID)
	}
	if !strings.Contains(in.Statement, firstReport) {
		t.Errorf("the statement does not summarize the report it was raised from: %q", in.Statement)
	}

	assertTheRunNamesTheProject(t, ctx, d, p)
	assertTheWordsLeftAReadEvent(t, ctx, d, ids[0])

	// Grouping takes no gate and writes no artifact version, so nothing scores
	// it and no per-author prior moves. Both are absences, and what holds them
	// is that nothing was written.
	if now := decisionsWritten(t, ctx, d); now != decisions {
		t.Errorf("grouping wrote %d decision row(s), and it takes no gate", now-decisions)
	}
	if now := versionsWritten(t, ctx, d); now != versions {
		t.Errorf("grouping wrote %d artifact version(s), and it authors none", now-versions)
	}

	// A later report attaches to the intent and never rewrites it, and the one
	// marking harm fires one page for the intent it landed in.
	statement, since := in.Statement, record.Now()
	reportThrough(t, client, thirdReport, "complaint", true)
	if moved, err := ps.Tick(ctx, passGrouper); err != nil {
		t.Fatalf("the second grouper pass: %v\n%s", err, out)
	} else if !moved {
		t.Errorf("the grouper pass over an arrived report announced nothing:\n%s", out)
	}
	if attached := reportIntents(t, ctx, d, p); len(attached) != 2 {
		t.Errorf("a third report made %d intent(s), want the same 2: a later report attaches",
			len(attached))
	}
	third := reports.ids(t, ctx)[2]
	if landed := intentOfReport(t, ctx, d, third); landed != firstIntent {
		t.Errorf("the later report was grouped into %q, want %q", landed, firstIntent)
	}
	if again := readIntent(t, ctx, d, firstIntent); again.Statement != statement {
		t.Errorf("the later report rewrote the statement:\n before %q\n after  %q",
			statement, again.Statement)
	}
	paged, err := notifier.PagedRowsSince(ctx, d.pool, reports.stored(t, ctx)[2].serviceID,
		notifier.KindHarmMarkedReport, since)
	if err != nil {
		t.Fatalf("counting the pages the harm mark fired: %v", err)
	}
	if paged != 1 {
		t.Errorf("the report marking harm fired %d page(s), want one per intent\n%s", paged, out)
	}

	assertTheSourceResolvesAtSpec(t, ctx, d, p, reports.stored(t, ctx)[0].serviceID, firstIntent)
}

// assertTheSourceResolvesAtSpec: an intent grouped from reports never
// auto-passes Spec. The source is a context factor the score resolves rather
// than weighs, so a human confirms the criteria whatever the rest of the vector
// says — and the held-out sample never selects past a resolved factor, which is
// what the score's own draw is refused on.
func assertTheSourceResolvesAtSpec(t *testing.T, ctx context.Context, d deps, p *path,
	serviceID, intentID string) {
	t.Helper()
	it, err := item.NewDecomposition(d.pool, d.token).Create(ctx, decompositionActor, item.New{
		IntentID: intentID, ServiceID: serviceID, AreaID: p.areaID, Branch: "candidate/reports",
	}, p.projectID, p.projectID, nil)
	if err != nil {
		t.Fatalf("writing an item of the intent grouped from reports: %v", err)
	}
	version, err := score.NewWriter(d.pool, d.token, marksOf(d.pool)).Ensure(ctx, scoreActor)
	if err != nil {
		t.Fatalf("reading the score version in force: %v", err)
	}
	assessed, err := score.New(score.Composition{
		Pool: d.pool, Version: version, Draw: score.NeverDraw{},
		Marks: marksOf(d.pool), Token: d.token,
	}).Assess(ctx, score.Change{
		ItemID: it.ID, ServiceID: serviceID, AreaID: p.areaID,
		FactorSet: score.SetAboveABuild, AtSpec: true,
	})
	if err != nil {
		t.Fatalf("scoring the Spec row of an item of a report-derived intent: %v", err)
	}
	for _, resolved := range assessed.Resolved {
		if resolved.Cause == score.CauseReportSourcedIntent {
			return
		}
	}
	t.Errorf("the Spec row of an item grouped from reports resolved nothing on its source: %v",
		assessed.Resolved)
}

// assertTheWordsLeftAReadEvent: every read that answers with a report's words
// appends a read event naming who read them, which is what makes who had
// already read them answerable after a redaction. The grouper's pass is such a
// read, so the log holds one naming the report it read.
func assertTheWordsLeftAReadEvent(t *testing.T, ctx context.Context, d deps, reportID string) {
	t.Helper()
	for _, row := range readLog(t, ctx, d) {
		if row.Shape == decisionlog.ShapeReadEvent && strings.Contains(row.Payload, reportID) {
			return
		}
	}
	t.Errorf("no read event names %s, and a read of a report's words appends one", reportID)
}

// assertTheRunNamesTheProject: the grouper's own run record names the project
// it was put on and the processing location its credential resolved to, so the
// providers that received a stranger's words are enumerable. It names neither
// an item nor an intent — the role is put on a project and runs before there is
// an intent at all.
func assertTheRunNamesTheProject(t *testing.T, ctx context.Context, d deps, p *path) {
	t.Helper()
	runs, err := agentrun.ByAuthorModel(ctx, d.pool, theModel)
	if err != nil {
		t.Fatalf("reading the runs of %s: %v", theModel, err)
	}
	for _, run := range runs {
		if run.ProjectID != p.projectID {
			continue
		}
		if run.ItemID != "" || run.IntentID != "" {
			t.Errorf("the grouper's run names item %q and intent %q, and it is put on a project",
				run.ItemID, run.IntentID)
		}
		if run.ProcessingLocation == "" || run.CredentialName == "" {
			t.Errorf("the grouper's run names processing location %q through credential %q",
				run.ProcessingLocation, run.CredentialName)
		}
		return
	}
	t.Errorf("no agent run record names the project the grouper was put on: %d run(s)", len(runs))
}

// reportThrough submits one report through the deployed service's own way in, in a
// session of its own, which is what a person using the software does.
func reportThrough(t *testing.T, client *http.Client, text, kind string, harmMarked bool) {
	t.Helper()
	shown := openSession(t, client)
	if shown.Session == "" {
		t.Fatal("the way in showed no session, and a submission carries one back")
	}
	marked := "false"
	if harmMarked {
		marked = "true"
	}
	result := submit(t, client, `{"kind":"`+kind+`","text":"`+text+`","harm_marked":`+marked+
		`,"session":"`+shown.Session+`","notice_id":"`+shown.NoticeID+`"}`)
	if !result.Accepted {
		t.Fatalf("the way in refused %q: %s", text, result.Refusal)
	}
}

// intentOfReport is the intent one report was grouped into, read back off the
// row, and empty where it is still ungrouped.
func intentOfReport(t *testing.T, ctx context.Context, d deps, reportID string) string {
	t.Helper()
	report, err := d.reports.Get(ctx, grouperPrincipal, reportID)
	if err != nil {
		t.Fatalf("reading %s: %v", reportID, err)
	}
	return report.IntentID
}

// reportIntents is every intent of the project whose source is reports.
func reportIntents(t *testing.T, ctx context.Context, d deps, p *path) []string {
	t.Helper()
	all, err := intent.InProject(ctx, d.pool, p.projectID)
	if err != nil {
		t.Fatalf("reading the intents of %s: %v", p.projectID, err)
	}
	var grouped []string
	for _, one := range all {
		if one.Source == intent.SourceReports {
			grouped = append(grouped, one.ID)
		}
	}
	return grouped
}

func readIntent(t *testing.T, ctx context.Context, d deps, intentID string) intent.Intent {
	t.Helper()
	in, err := intent.Get(ctx, d.pool, intentID)
	if err != nil {
		t.Fatalf("reading %s: %v", intentID, err)
	}
	return in
}

// decisionsWritten is how many rows of the decision shape the log holds, which
// is what says whether a gate fired; versionsWritten is the same for the
// artifact store. Both are read before the pass and again after it: grouping
// takes no gate and authors no version, and an absence is demonstrated by
// nothing having been written rather than by code.
func decisionsWritten(t *testing.T, ctx context.Context, d deps) int {
	t.Helper()
	var n int
	if err := d.pool.QueryRow(ctx, `select count(*) from `+decisionlog.Table+` where shape = $1`,
		string(decisionlog.ShapeDecision)).Scan(&n); err != nil {
		t.Fatalf("counting the decision rows: %v", err)
	}
	return n
}

func versionsWritten(t *testing.T, ctx context.Context, d deps) int {
	t.Helper()
	var n int
	if err := d.pool.QueryRow(ctx, `select count(*) from `+artifact.Table).Scan(&n); err != nil {
		t.Fatalf("counting the artifact versions: %v", err)
	}
	return n
}

// TestTheTwoAdmissionsHoldWhatArrives: an owner who wants a human before the
// pipeline spends anything on a report places a safeguard whose subject is the
// report store, and each of its two bounds holds one thing.
//
// With the first, an arrived report waits ungrouped: the grouper does not read
// it, it is a row on Work's home view among what waits on a human, and the
// admission there is what lets the grouper read it. With the second, the intent
// that grouping raises waits: dispatch puts no agent on it and no interview
// round runs, it is a row on the same view, and the admission there is one
// action over the group the intent already is.
func TestTheTwoAdmissionsHoldWhatArrives(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	newReports(t, ctx, &d)
	if _, err := run(ctx, d, of(theStatement)); err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path that groups: %v\n%s", err, out)
	}
	ps := newPasses(p, nil, nil)
	acting := owner(t, ctx, d.pool, d.token, d.human)
	view := &views{p: p}
	made := &calls{p: p, v: view}

	socket := localtarget.WayInSocket(d.dir, theService)
	waitForTheWayIn(t, socket)
	client := overTheSocket(socket)

	// The first safeguard: an arrived report waits ungrouped until a human
	// admits it, so only an admitted report reaches the grouper.
	placeAdmission(t, ctx, p, acting, gatepolicy.ReportAdmission)
	reportThrough(t, client, firstReport, "bug", false)
	if moved, err := ps.Tick(ctx, passGrouper); err != nil {
		t.Fatalf("the grouper pass over a report awaiting admission: %v\n%s", err, out)
	} else if moved {
		t.Errorf("the grouper grouped a report no human had admitted:\n%s", out)
	}
	if raised := reportIntents(t, ctx, d, p); len(raised) != 0 {
		t.Fatalf("%d intent(s) were raised from a report awaiting admission", len(raised))
	}

	home := readHome(t, ctx, view, acting)
	if len(home.Awaiting.Reports) != 1 || home.Awaiting.Reports[0].Text != firstReport {
		t.Fatalf("the home view shows %+v awaiting admission, want the arrived report", home.Awaiting.Reports)
	}
	if home.Badge.Admissions != 1 || home.Badge.Total < 1 {
		t.Errorf("the badge counts %d admission(s) in a total of %d, want the wait counted",
			home.Badge.Admissions, home.Badge.Total)
	}

	// The human's admission at Work, through the call a screen makes.
	if err := made.AdmitReport(ctx, asPrincipal(acting), screens.AdmitReportArgs{
		ReportID: home.Awaiting.Reports[0].ID,
	}); err != nil {
		t.Fatalf("AdmitReport: %v", err)
	}
	if moved, err := ps.Tick(ctx, passGrouper); err != nil {
		t.Fatalf("the grouper pass after the admission: %v\n%s", err, out)
	} else if !moved {
		t.Errorf("the grouper read nothing after the report was admitted:\n%s", out)
	}
	raised := reportIntents(t, ctx, d, p)
	if len(raised) != 1 {
		t.Fatalf("%d intent(s) after the admission, want the one the admitted report raised", len(raised))
	}
	if after := readHome(t, ctx, view, acting); len(after.Awaiting.Reports) != 0 {
		t.Errorf("the admitted report is still shown as waiting: %+v", after.Awaiting.Reports)
	}

	// The second safeguard: the intent grouping raised waits before an agent is
	// put on it. The one already raised was raised before the safeguard was
	// placed and waits too, the safeguard being read in force and not marked at
	// the arrival.
	placeAdmission(t, ctx, p, acting, gatepolicy.ReportDerivedIntentAdmission)
	waiting := readHome(t, ctx, view, acting)
	if len(waiting.Awaiting.Intents) != 1 || waiting.Awaiting.Intents[0].IntentID != raised[0] {
		t.Fatalf("the home view shows %+v awaiting admission, want the intent grouping raised",
			waiting.Awaiting.Intents)
	}
	if held := waiting.Awaiting.Intents[0]; held.Reports != 1 || held.Statement == "" {
		t.Errorf("the waiting intent's row is %+v, want the group's size and its statement", held)
	}

	// Dispatch puts no agent on it: the interview is the first role a
	// report-derived intent meets, and it is held rather than run, so nothing
	// is spent refining what a human would never admit.
	onTheIntent := dispatch.On{IntentID: raised[0], ProjectID: p.projectID, CountedSoFar: 0}
	_, run, err := p.dispatch.Interviewer(ctx, onTheIntent, nil,
		agent.Interviewing{Statement: waiting.Awaiting.Intents[0].Statement})
	if !errors.Is(err, dispatch.ErrHeld) || run.Held != dispatch.HoldIntentAwaitsAdmission {
		t.Fatalf("the interviewer on an intent awaiting admission = %v, held %q", err, run.Held)
	}
	if held := readIntent(t, ctx, d, raised[0]); held.State != intent.StateUnrefined || held.Rounds != 0 {
		t.Errorf("the intent is %s after %d round(s), want it unrefined with none run",
			held.State, held.Rounds)
	}

	// The admission at Work, one action over the group the intent already is,
	// and the same dispatch runs.
	if err := made.AdmitIntent(ctx, asPrincipal(acting), screens.AdmitIntentArgs{
		IntentID: raised[0],
	}); err != nil {
		t.Fatalf("AdmitIntent: %v", err)
	}
	admitted := readIntent(t, ctx, d, raised[0])
	if admitted.AdmittedAt == "" {
		t.Fatal("the admitted intent carries no admission")
	}
	if after := readHome(t, ctx, view, acting); len(after.Awaiting.Intents) != 0 {
		t.Errorf("the admitted intent is still shown as waiting: %+v", after.Awaiting.Intents)
	}
	read, run, err := p.dispatch.Interviewer(ctx, onTheIntent, nil,
		agent.Interviewing{Statement: admitted.Statement})
	if err != nil || run.Held != "" {
		t.Fatalf("the interviewer after the admission = %v, held %q\n%s", err, run.Held, out)
	}
	if len(read.Requirements) == 0 {
		t.Errorf("the round that ran after the admission stated nothing: %+v", read)
	}
}

// placeAdmission places one of the two safeguards on the report store, which is
// the write an owner makes at Factory. The subject is the factory-wide settings
// record's id, the store having no record of its own.
func placeAdmission(t *testing.T, ctx context.Context, p *path, acting record.Actor,
	parameter gatepolicy.Parameter) {
	t.Helper()
	settings, err := factorysettings.Get(ctx, p.d.pool)
	if err != nil {
		t.Fatalf("reading the factory-wide settings record: %v", err)
	}
	if _, _, err := p.factory.AddSafeguard(ctx, acting, parameter,
		safeguard.Subject{Kind: safeguard.SubjectReportStore, ID: settings.ID},
		safeguard.Bound{}, safeguard.Routing{}); err != nil {
		t.Fatalf("placing the safeguard on %s: %v", parameter, err)
	}
}

// readHome is Work's home view as the human at the screen reads it.
func readHome(t *testing.T, ctx context.Context, v *views, acting record.Actor) screens.Home {
	t.Helper()
	home, err := v.Home(ctx, asPrincipal(acting))
	if err != nil {
		t.Fatalf("reading the home view: %v", err)
	}
	return home
}
