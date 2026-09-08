// The one way into the factory from outside it, demonstrated end to end: a
// change shipped to production, the way in the factory injected into that
// service's build listening on the target, and a report arriving through it.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/constraint"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/reportstore"
	"github.com/dulguun0225/borg/factory/screens"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/wayin"
)

// TestAReportArrives is the channel from the outside: run ships one change to
// production, so the target holds a live process the factory built with the
// way in inside it and started with the token, the entrance and the socket.
// The test is that process's reporter — it dials the socket and nothing else,
// the way somebody using the software would.
func TestAReportArrives(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	reports := newReports(t, ctx, &d)

	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	c := only(t, res)
	if c.deployID == "" {
		t.Fatalf("the run deployed nothing to production:\n%s", out)
	}

	// What the seam carried to the target: the token this deploy minted and
	// the entrance it is presented at. The record holds a digest of the token
	// and never the token, so this is the one place it can be read.
	placed := reports.at(t, d.dir, theService)
	if placed.token == "" || placed.address != reports.url {
		t.Fatalf("the production deploy carried token %q and address %q, want a token and %s",
			placed.token, placed.address, reports.url)
	}

	socket := localtarget.WayInSocket(d.dir, theService)
	waitForTheWayIn(t, socket)
	client := overTheSocket(socket)

	// The notice at the open, where an owner has authored none: no notice and
	// no words, which is what the way in shows then.
	shown := openSession(t, client)
	if shown.NoticeID != "" || shown.Text != "" {
		t.Errorf("the way in showed %+v where no notice is authored, want none", shown)
	}
	if shown.Session == "" {
		t.Fatal("the way in showed no session, and a submission carries one back")
	}

	// The notice an owner authored, read from the store at every open: the
	// deployed service does not build again for it.
	svc, err := service.Get(ctx, d.pool, res.serviceID)
	if err != nil {
		t.Fatalf("reading the service: %v", err)
	}
	const theNotice = "what you write here reaches the people who make this software"
	authored, err := constraint.NewWriter(d.pool, d.token).Arrive(ctx,
		owner(t, ctx, d.pool, d.token, d.human), constraint.New{
			Kind: constraint.KindNotice, Reach: constraint.ReachProject,
			SubjectID: svc.ProjectID, Statement: theNotice,
		})
	if err != nil {
		t.Fatalf("authoring the notice: %v", err)
	}
	shown = openSession(t, client)
	if shown.NoticeID != authored.ID || shown.Text != theNotice {
		t.Errorf("the way in showed %+v, want the notice the owner authored, %s", shown, authored.ID)
	}

	// The report, and its result in the same call.
	result := submit(t, client, `{"kind":"bug","text":"the save button does nothing",`+
		`"session":"`+shown.Session+`","notice_id":"`+shown.NoticeID+`"}`)
	if !result.Accepted || result.Refusal != "" {
		t.Fatalf("the way in rendered %+v, want the submission accepted", result)
	}

	// The row names the deploy record's service and environment. Nothing the
	// submission carried could have said either: the token is what resolves
	// them, and the way in presents it in the header of a request whose body
	// has no field for a service at all.
	rows := reports.stored(t, ctx)
	if len(rows) != 1 {
		t.Fatalf("the store holds %d reports, want the one that was submitted: %+v", len(rows), rows)
	}
	row := rows[0]
	if row.serviceID != res.serviceID || row.environmentID != res.environmentID {
		t.Errorf("the report names service %s in environment %s, want %s in %s",
			row.serviceID, row.environmentID, res.serviceID, res.environmentID)
	}
	if row.deployID != c.deployID {
		t.Errorf("the report names deploy %s, the production deploy was %s", row.deployID, c.deployID)
	}
	if row.kind != string(reportstore.KindBug) || row.text != "the save button does nothing" {
		t.Errorf("the report reads %q of kind %q, want the words the reporter wrote", row.text, row.kind)
	}
	if row.identity != factoryVersion {
		t.Errorf("the report names shipped-bundle identity %q, want %q — the release that built the way in",
			row.identity, factoryVersion)
	}
	if row.noticeID != authored.ID {
		t.Errorf("the report names notice %q, want the one in force when it arrived, %s", row.noticeID, authored.ID)
	}
	if row.sourceKey == "" || row.sourceKey == shown.Session {
		t.Errorf("the report's source key is %q, want one derived from the session and not the session", row.sourceKey)
	}
	if row.harmMarked {
		t.Error("the report is marked as describing harm, and the reporter marked nothing")
	}

	// A submission naming no deploy this factory placed a way in at is
	// refused, and counted on the whole channel and never on a service — so
	// narrowing one service's rate cannot be evaded by submitting under
	// another's name. It reaches the entrance directly, which is what a way in
	// the factory did not deploy would do.
	refused := toTheEntrance(t, reports.url, "no-such-token", wayin.Shape, "from nowhere")
	if refused.Accepted || !strings.Contains(refused.Refusal, "no deploy") {
		t.Errorf("a submission under an unknown token rendered %+v, want a refusal naming the deploy", refused)
	}
	if channel := reports.counts(t, ctx, ""); channel.Refusals != 1 {
		t.Errorf("the channel counted %d refusals, want the one it refused", channel.Refusals)
	}
	if onService := reports.counts(t, ctx, res.serviceID); onService.Refusals != 0 {
		t.Errorf("a submission naming no deploy counted %d refusals against %s, and it names no service",
			onService.Refusals, res.serviceID)
	}

	// A submission written under a shape this factory version does not read is
	// counted per service, which is the loss the refused counter cannot see: a
	// service serving a way in from a release this one is behind.
	unread := toTheEntrance(t, reports.url, placed.token, "submission/999",
		"from a way in this version cannot read")
	if unread.Accepted || !strings.Contains(unread.Refusal, "shape") {
		t.Errorf("a submission under an unknown shape rendered %+v, want a refusal naming the shape", unread)
	}
	onService := reports.counts(t, ctx, res.serviceID)
	if onService.UnreadableShape != 1 {
		t.Errorf("%s counted %d unreadable submissions, want one", res.serviceID, onService.UnreadableShape)
	}
	if onService.Refusals != 0 {
		t.Errorf("an unreadable submission was counted as a refusal against %s as well", res.serviceID)
	}

	// The factory-wide rate authored to zero closes the channel: the way in
	// still answers, and what it renders is the bound that refused.
	if _, err := policy.NewFactory(d.pool, d.token).AuthorReportChannelRate(ctx,
		owner(t, ctx, d.pool, d.token, d.human), 0); err != nil {
		t.Fatalf("authoring the channel's rate: %v", err)
	}
	closed := submit(t, client, `{"kind":"complaint","text":"and this one arrives nowhere",`+
		`"session":"`+shown.Session+`"}`)
	if closed.Accepted || !strings.Contains(closed.Refusal, "the whole factory") {
		t.Errorf("a submission with the channel closed rendered %+v, want the rate as the reason", closed)
	}
	if rows := reports.stored(t, ctx); len(rows) != 1 {
		t.Errorf("the store holds %d reports, and the channel was closed after the first", len(rows))
	}
	if channel := reports.counts(t, ctx, ""); channel.Refusals != 2 {
		t.Errorf("the channel counted %d refusals, want the unknown token and the closed channel", channel.Refusals)
	}
	if onService := reports.counts(t, ctx, res.serviceID); onService.Refusals != 1 {
		t.Errorf("%s counted %d refusals, want the one the closed channel refused under its token",
			res.serviceID, onService.Refusals)
	}
}

// shownAtTheOpen is what a session is given at the open: the notice in force,
// and the session this way in minted for it.
type shownAtTheOpen struct {
	NoticeID string `json:"notice_id"`
	Text     string `json:"text"`
	Session  string `json:"session"`
}

// submitted is what one submission did, rendered in the session that made it.
type submitted struct {
	Accepted bool   `json:"accepted"`
	Refusal  string `json:"refusal"`
}

// openSession reads the notice from the deployed service's way in, which is
// what mints the session a submission carries back.
func openSession(t *testing.T, client *http.Client) shownAtTheOpen {
	t.Helper()
	response, err := client.Get("http://way-in/")
	if err != nil {
		t.Fatalf("opening the way in: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the way in answered the open with %d", response.StatusCode)
	}
	var shown shownAtTheOpen
	if err := json.NewDecoder(response.Body).Decode(&shown); err != nil {
		t.Fatalf("reading what the way in showed: %v", err)
	}
	return shown
}

// submit posts one report to the deployed service's way in and returns what it
// rendered in the same call.
func submit(t *testing.T, client *http.Client, body string) submitted {
	t.Helper()
	response, err := client.Post("http://way-in/", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("submitting a report: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the way in answered the submission with %d", response.StatusCode)
	}
	var result submitted
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("reading the submit result: %v", err)
	}
	return result
}

// toTheEntrance submits to the factory's own entrance rather than through a
// deployed service's way in, under the token and the shape the caller names.
// It is what a way in this factory did not deploy reaches, which is the one
// thing a session at a deployed service cannot be made to do.
func toTheEntrance(t *testing.T, address, token, shape, text string) submitted {
	t.Helper()
	// The fields the way in writes, which the entrance reads: the shape and
	// the identity first, and what the reporter wrote after them.
	body, err := json.Marshal(struct {
		Shape                 string `json:"shape"`
		ShippedBundleIdentity string `json:"shipped_bundle_identity"`
		Kind                  string `json:"kind"`
		Text                  string `json:"text"`
	}{shape, factoryVersion, string(reportstore.KindBug), text})
	if err != nil {
		t.Fatalf("marshalling the submission: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, address+wayin.SubmitPath, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("building the submission: %v", err)
	}
	request.Header.Set(wayin.TokenHeader, token)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("submitting to the entrance: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the entrance answered the submission with %d", response.StatusCode)
	}
	var result submitted
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("reading the submit result: %v", err)
	}
	return result
}

// TestServedMountsTheWayIn is what makes the entrance reachable at all: the
// process serves it at the way in's own two paths, beside the screens and
// beside /healthz, and the shipped source calls exactly those two.
func TestServedMountsTheWayIn(t *testing.T) {
	handler := served(nil, wayin.NewEntrance(nowhere{}))
	for _, route := range []struct {
		method, path string
	}{
		{http.MethodGet, wayin.NoticePath},
		{http.MethodPost, wayin.SubmitPath},
	} {
		recorded := httptest.NewRecorder()
		handler.ServeHTTP(recorded, httptest.NewRequest(route.method, route.path, nil))
		// The entrance refuses a call presenting no token, which is what says
		// the entrance answered rather than a screen or the mux.
		if recorded.Code != http.StatusBadRequest || !strings.Contains(recorded.Body.String(), "way-in token") {
			t.Errorf("%s %s answered %d: %s", route.method, route.path, recorded.Code, recorded.Body)
		}
	}
}

// nowhere is a [wayin.Store] that is never reached: the routes above are
// refused before the entrance asks it anything.
type nowhere struct{}

func (nowhere) NoticeInForce(context.Context, string) (wayin.Notice, error) {
	return wayin.Notice{}, nil
}

func (nowhere) Submit(context.Context, wayin.Submission, time.Time) (wayin.Result, error) {
	return wayin.Result{}, nil
}

// TestAReportAppearsUnderItsIntentAndIsAdmittedThere is the other end of the
// channel: what a stranger wrote reaches Work under the intent it was grouped
// into and under no address of its own, and the two admissions a safeguard on
// the report store makes an owner perform are made from there — one report,
// and one action over the group the intent already is.
func TestAReportAppearsUnderItsIntentAndIsAdmittedThere(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	reports := newReports(t, ctx, &d)

	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	svc, err := service.Get(ctx, d.pool, res.serviceID)
	if err != nil {
		t.Fatalf("reading the service: %v", err)
	}
	const theNotice = "what you write here reaches the people who make this software"
	authored, err := constraint.NewWriter(d.pool, d.token).Arrive(ctx,
		owner(t, ctx, d.pool, d.token, d.human), constraint.New{
			Kind: constraint.KindNotice, Reach: constraint.ReachProject,
			SubjectID: svc.ProjectID, Statement: theNotice,
		})
	if err != nil {
		t.Fatalf("authoring the notice: %v", err)
	}

	// Two reports of one problem, both through the way in the deploy placed.
	socket := localtarget.WayInSocket(d.dir, theService)
	waitForTheWayIn(t, socket)
	client := overTheSocket(socket)
	shown := openSession(t, client)
	for _, words := range []string{"the export button does nothing", "exporting invoices fails"} {
		result := submit(t, client, `{"kind":"bug","harm_marked":true,"text":"`+words+`",`+
			`"session":"`+shown.Session+`","notice_id":"`+shown.NoticeID+`"}`)
		if !result.Accepted {
			t.Fatalf("the way in rendered %+v for %q, want it accepted", result, words)
		}
	}
	arrived := reports.ids(t, ctx)
	if len(arrived) != 2 {
		t.Fatalf("the store holds %d reports, want the two that were submitted", len(arrived))
	}

	// The intent the grouper raises from them, decomposed into the item whose
	// timeline they are read under. The grouping is written here because the
	// grouper is its own step; what this test is about is the screen.
	s := newScreens(t, ctx, d, out)
	raised, err := s.p.intake.TakeIn(ctx, owner(t, ctx, d.pool, d.token, d.human), intent.Arrival{
		Source: intent.SourceReports, ProjectID: svc.ProjectID,
		Statement: theService + ": " + theSecondStatement,
	})
	if err != nil {
		t.Fatalf("raising the intent the reports were grouped into: %v", err)
	}
	_, candidates, err := s.p.authorIntent(ctx, asked{
		statement: theSecondStatement, services: []string{theService},
		resumeIntentID: raised.ID,
	}, "grouped from reports")
	if err != nil || len(candidates) != 1 {
		t.Fatalf("decomposing the report-derived intent = %d items, %v\n%s", len(candidates), err, out)
	}
	itemID := candidates[0].itemID
	for _, id := range arrived {
		if err := reports.channel.store.Link(ctx, id, raised.ID); err != nil {
			t.Fatalf("linking %s to %s: %v", id, raised.ID, err)
		}
	}

	// The timeline carries both, oldest first, each waiting on a human.
	var view screens.Item
	s.get(t, "/api/item/"+itemID, &view)
	if view.IntentID != raised.ID {
		t.Errorf("the item view names intent %q, want %s", view.IntentID, raised.ID)
	}
	if len(view.Reports) != 2 {
		t.Fatalf("the item view carries %d reports, want the two grouped into its intent: %+v", len(view.Reports), view.Reports)
	}
	first := view.Reports[0]
	if first.ID != arrived[0] || first.Kind != string(reportstore.KindBug) || !first.HarmMarked ||
		first.NoticeID != authored.ID || first.CollectedAt == "" || first.Admitted ||
		first.Text != "the export button does nothing" {
		t.Errorf("the first report reads %+v, want the words, the mark, the notice and no admission", first)
	}

	// One report admitted on its own, which is what the safeguard holding a
	// report before the grouper reads it makes a human do. It is one report at
	// a time: such a report is ungrouped and belongs to no intent, so there is
	// no group for one action to cover.
	s.mustCall(t, "admitReport", screens.AdmitReportArgs{ReportID: arrived[0]})
	s.get(t, "/api/item/"+itemID, &view)
	if !view.Reports[0].Admitted || view.Reports[1].Admitted {
		t.Errorf("after admitting %s the reports read %+v, want that one admitted and no other", arrived[0], view.Reports)
	}

	// The intent's own admission, which the other safeguard makes a human write
	// and which is one action over the group the intent already is. It is the
	// intent record's field and moves no report: the two admissions are
	// separate, and a group whose every report is admitted may still be an
	// intent nobody has admitted.
	s.mustCall(t, "admitIntent", screens.AdmitIntentArgs{IntentID: raised.ID})
	s.get(t, "/api/item/"+itemID, &view)
	if !view.Reports[0].Admitted || view.Reports[1].Admitted {
		t.Errorf("admitting the intent moved a report's own admission: %+v", view.Reports)
	}
	admitted, err := intent.Get(ctx, d.pool, raised.ID)
	if err != nil {
		t.Fatalf("reading the admitted intent: %v", err)
	}
	if admitted.AdmittedAt == "" {
		t.Error("the admitted intent carries no admission")
	}

	// A report id naming nothing is a 404 and not a fault of the server's own,
	// and so is an intent id naming nothing.
	if status, _ := s.call(t, "admitReport", screens.AdmitReportArgs{ReportID: "rep_nothing"}); status != http.StatusNotFound {
		t.Errorf("admitting a report the store does not hold answered %d, want 404", status)
	}
	if status, _ := s.call(t, "admitIntent", screens.AdmitIntentArgs{IntentID: "in_nothing"}); status != http.StatusNotFound {
		t.Errorf("admitting an intent the store does not hold answered %d, want 404", status)
	}
}
