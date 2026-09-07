package screens_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/screens"
)

// post is a POST /api/call/{name} request against s, carrying the version
// and the principal key every route requires.
func post(t *testing.T, s http.Handler, name string, body any) *httptest.ResponseRecorder {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/call/"+name, bytes.NewReader(buf))
	r.Header.Set("X-Factory-Version", theVersion)
	r.Header.Set("X-Factory-Principal", "hk_caller")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	return rec
}

// TestDecideReachesTheDecodedStruct: POST /api/call/decide reaches Calls.Decide
// with the body decoded into screens.DecideArgs and the caller's principal.
func TestDecideReachesTheDecodedStruct(t *testing.T) {
	var gotArgs screens.DecideArgs
	var gotPrincipal principal.Principal
	calls := &fakeCalls{
		decide: func(_ context.Context, p principal.Principal, a screens.DecideArgs) error {
			gotArgs, gotPrincipal = a, p
			return nil
		},
	}
	s := screens.New(&fakeViews{}, calls, theVersion, noClient)

	rec := post(t, s, "decide", screens.DecideArgs{
		OpenEventID:    "oe_1",
		Verdict:        "approve",
		OpenedInWorkAt: "2026-09-07T00:00:00Z",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body %s", rec.Code, rec.Body.String())
	}
	if gotArgs.OpenEventID != "oe_1" || gotArgs.Verdict != "approve" || gotArgs.OpenedInWorkAt != "2026-09-07T00:00:00Z" {
		t.Errorf("args = %+v, want the decoded body", gotArgs)
	}
	if gotPrincipal.Actor.Key != "hk_caller" {
		t.Errorf("principal = %s, want the caller's key", gotPrincipal)
	}
}

// TestApproveThroughHoldReachesTheDecodedStruct: POST
// /api/call/approveThroughHold reaches Calls.ApproveThroughHold with the body
// decoded into screens.ApproveThroughHoldArgs. It names the item and no open
// event, the hold it approves through being what stops the row being fired.
func TestApproveThroughHoldReachesTheDecodedStruct(t *testing.T) {
	var gotArgs screens.ApproveThroughHoldArgs
	var gotPrincipal principal.Principal
	calls := &fakeCalls{
		approveThroughHold: func(_ context.Context, p principal.Principal, a screens.ApproveThroughHoldArgs) error {
			gotArgs, gotPrincipal = a, p
			return nil
		},
	}
	s := screens.New(&fakeViews{}, calls, theVersion, noClient)

	rec := post(t, s, "approveThroughHold", screens.ApproveThroughHoldArgs{
		ItemID:         "it_1",
		Reason:         "the incident is worse than the defect",
		OpenedInWorkAt: "2026-09-07T00:00:00Z",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body %s", rec.Code, rec.Body.String())
	}
	if gotArgs.ItemID != "it_1" || gotArgs.Reason != "the incident is worse than the defect" ||
		gotArgs.OpenedInWorkAt != "2026-09-07T00:00:00Z" {
		t.Errorf("args = %+v, want the decoded body", gotArgs)
	}
	if gotPrincipal.Actor.Key != "hk_caller" {
		t.Errorf("principal = %s, want the caller's key", gotPrincipal)
	}
}

// TestEditRecordRowReachesTheDecodedStruct: POST /api/call/editRecordRow
// reaches Calls.EditRecordRow with the body decoded into
// screens.EditRecordRowArgs.
func TestEditRecordRowReachesTheDecodedStruct(t *testing.T) {
	var gotArgs screens.EditRecordRowArgs
	calls := &fakeCalls{
		editRecordRow: func(_ context.Context, _ principal.Principal, a screens.EditRecordRowArgs) error {
			gotArgs = a
			return nil
		},
	}
	s := screens.New(&fakeViews{}, calls, theVersion, noClient)

	rec := post(t, s, "editRecordRow", screens.EditRecordRowArgs{
		Row:            "role_prompt",
		RecordID:       "rp_1",
		Version:        "v2",
		OpenedInWorkAt: "2026-09-07T00:00:00Z",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body %s", rec.Code, rec.Body.String())
	}
	if gotArgs.Row != "role_prompt" || gotArgs.RecordID != "rp_1" || gotArgs.Version != "v2" || gotArgs.OpenedInWorkAt != "2026-09-07T00:00:00Z" {
		t.Errorf("args = %+v, want the decoded body", gotArgs)
	}
}

// TestRetireServiceReachesTheDecodedStruct: POST /api/call/retireService
// reaches Calls.RetireService with the body decoded, the environment name
// among it — a retirement naming one performs the removal for that
// environment alone and writes nothing on the service record.
func TestRetireServiceReachesTheDecodedStruct(t *testing.T) {
	var gotArgs screens.RetireServiceArgs
	calls := &fakeCalls{
		retireService: func(_ context.Context, _ principal.Principal, a screens.RetireServiceArgs) error {
			gotArgs = a
			return nil
		},
	}
	s := screens.New(&fakeViews{}, calls, theVersion, noClient)

	rec := post(t, s, "retireService", screens.RetireServiceArgs{
		ServiceID: "svc_1", EnvironmentName: "production",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body %s", rec.Code, rec.Body.String())
	}
	if gotArgs.ServiceID != "svc_1" || gotArgs.EnvironmentName != "production" {
		t.Errorf("args = %+v, want the decoded body", gotArgs)
	}
}

// TestEndProjectReachesTheDecodedStruct: POST /api/call/endProject reaches
// Calls.EndProject with the project it names.
func TestEndProjectReachesTheDecodedStruct(t *testing.T) {
	var gotArgs screens.EndProjectArgs
	calls := &fakeCalls{
		endProject: func(_ context.Context, _ principal.Principal, a screens.EndProjectArgs) error {
			gotArgs = a
			return nil
		},
	}
	s := screens.New(&fakeViews{}, calls, theVersion, noClient)

	rec := post(t, s, "endProject", screens.EndProjectArgs{ProjectName: "default"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body %s", rec.Code, rec.Body.String())
	}
	if gotArgs.ProjectName != "default" {
		t.Errorf("args = %+v, want the decoded body", gotArgs)
	}
}

// TestACreatingCallReturnsItsID: a call that creates an address answers its
// id as JSON.
func TestACreatingCallReturnsItsID(t *testing.T) {
	calls := &fakeCalls{
		supplyIntent: func(context.Context, principal.Principal, screens.SupplyIntentArgs) (string, error) {
			return "int_1", nil
		},
	}
	s := screens.New(&fakeViews{}, calls, theVersion, noClient)
	rec := post(t, s, "supplyIntent", screens.SupplyIntentArgs{Statement: "add a health check"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["id"] != "int_1" {
		t.Errorf("id = %q, want int_1", body["id"])
	}
}

// TestAnUnknownCallNameIs404: a name handleCall's switch does not name is
// refused rather than silently ignored.
func TestAnUnknownCallNameIs404(t *testing.T) {
	s := screens.New(&fakeViews{}, &fakeCalls{}, theVersion, noClient)
	rec := post(t, s, "notARealCall", map[string]string{})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404, body %s", rec.Code, rec.Body.String())
	}
}

// TestABadBodyIs400: a body handleCall cannot decode into the call's own
// argument struct is a bad request and never reaches Calls.
func TestABadBodyIs400(t *testing.T) {
	called := false
	calls := &fakeCalls{
		acknowledge: func(context.Context, principal.Principal, screens.AcknowledgeArgs) error {
			called = true
			return nil
		},
	}
	s := screens.New(&fakeViews{}, calls, theVersion, noClient)
	r := httptest.NewRequest(http.MethodPost, "/api/call/acknowledge", bytes.NewReader([]byte("not json")))
	r.Header.Set("X-Factory-Version", theVersion)
	r.Header.Set("X-Factory-Principal", "hk_caller")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400, body %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Error("Acknowledge was called with an undecodable body")
	}
}
