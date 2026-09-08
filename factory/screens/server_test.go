package screens_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/screens"
)

const theVersion = "v_test"

// get is a GET request against s, carrying the version and the principal
// key every route requires.
func get(t *testing.T, s http.Handler, path, principalKey string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("X-Factory-Version", theVersion)
	if principalKey != "" {
		r.Header.Set("X-Factory-Principal", principalKey)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	return rec
}

var noClient = fstest.MapFS{}

// TestEveryReadRouteDecodesToItsOwnViewType: each of the nine reads answers
// the shape its own view type takes, so a client decoding the body gets the
// fields that screen's own view struct declares.
func TestEveryReadRouteDecodesToItsOwnViewType(t *testing.T) {
	views := &fakeViews{
		home: func(context.Context, principal.Principal) (screens.Home, error) {
			return screens.Home{Badge: screens.Badge{Total: 3}}, nil
		},
		work: func(context.Context, principal.Principal, screens.Filter) (screens.Work, error) {
			return screens.Work{Rows: []screens.WorkRow{{ItemID: "it_1"}}}, nil
		},
		item: func(context.Context, principal.Principal, string) (screens.Item, error) {
			return screens.Item{ID: "it_1"}, nil
		},
		decision: func(context.Context, principal.Principal, string) (screens.Decision, error) {
			return screens.Decision{OpenEventID: "oe_1"}, nil
		},
		ops: func(context.Context, principal.Principal) (screens.Ops, error) {
			return screens.Ops{Services: []screens.ServiceSummary{{ServiceID: "svc_1"}}}, nil
		},
		serviceOn: func(context.Context, principal.Principal, string, string) (screens.Service, error) {
			return screens.Service{ServiceID: "svc_1"}, nil
		},
		factory: func(context.Context, principal.Principal) (screens.Factory, error) {
			return screens.Factory{Projects: []screens.Project{{ID: "prj_1"}}}, nil
		},
		constraint: func(context.Context, principal.Principal, string) (screens.Constraint, error) {
			return screens.Constraint{ID: "con_1"}, nil
		},
		people: func(context.Context, principal.Principal) (screens.People, error) {
			return screens.People{Rows: []screens.PersonRow{{Key: "hk_1"}}}, nil
		},
	}
	s := screens.New(views, &fakeCalls{}, theVersion, noClient)

	for _, tc := range []struct {
		path string
		want string
	}{
		{"/api/home", `"total":3`},
		{"/api/work", `"it_1"`},
		{"/api/item/it_1", `"it_1"`},
		{"/api/decision/dec_1", `"oe_1"`},
		{"/api/ops", `"svc_1"`},
		{"/api/service/svc_1/on/env_1", `"svc_1"`},
		{"/api/factory", `"prj_1"`},
		{"/api/constraint/con_1", `"con_1"`},
		{"/api/people", `"hk_1"`},
	} {
		rec := get(t, s, tc.path, "hk_caller")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200, body %s", tc.path, rec.Code, rec.Body.String())
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatalf("%s: decode top level: %v", tc.path, err)
		}
	}
}

// TestTheItemViewCarriesTheReportsGroupedIntoItsIntent: the report reaches
// Work under the intent it was grouped into, on the item address and on no
// address of its own, with the fields a human decides an admission on.
func TestTheItemViewCarriesTheReportsGroupedIntoItsIntent(t *testing.T) {
	views := &fakeViews{
		item: func(context.Context, principal.Principal, string) (screens.Item, error) {
			return screens.Item{
				ID: "it_1", IntentID: "int_1",
				Reports: []screens.ReportSummary{{
					ID: "rep_1", Kind: "bug", HarmMarked: true,
					CollectedAt: "2026-09-04T07:00:00Z", NoticeID: "con_1",
					Text: "the export button does nothing",
				}},
			}, nil
		},
	}
	s := screens.New(views, &fakeCalls{}, theVersion, noClient)

	rec := get(t, s, "/api/item/it_1", "hk_caller")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", rec.Code, rec.Body.String())
	}
	var view screens.Item
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(view.Reports) != 1 {
		t.Fatalf("the item view carries %d reports, want the one grouped into its intent", len(view.Reports))
	}
	one := view.Reports[0]
	if one.ID != "rep_1" || one.Kind != "bug" || !one.HarmMarked || one.Admitted ||
		one.NoticeID != "con_1" || one.Text != "the export button does nothing" ||
		one.CollectedAt != "2026-09-04T07:00:00Z" {
		t.Errorf("the report reads %+v, want every field the view was given", one)
	}
}

// TestAMissingOrWrongVersionIsRefused: every /api/ route refuses a call
// whose version does not match the server's, with the reload_required
// shape and never the answer.
func TestAMissingOrWrongVersionIsRefused(t *testing.T) {
	s := screens.New(&fakeViews{}, &fakeCalls{}, theVersion, noClient)
	for _, version := range []string{"", "not-" + theVersion} {
		r := httptest.NewRequest(http.MethodGet, "/api/home", nil)
		if version != "" {
			r.Header.Set("X-Factory-Version", version)
		}
		r.Header.Set("X-Factory-Principal", "hk_1")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		if rec.Code != http.StatusConflict {
			t.Fatalf("version %q: status = %d, want 409", version, rec.Code)
		}
		var body struct {
			ReloadRequired bool   `json:"reload_required"`
			Expected       string `json:"expected"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !body.ReloadRequired || body.Expected != theVersion {
			t.Errorf("version %q: body = %+v, want reload_required true and expected %q", version, body, theVersion)
		}
	}
}

// TestAnEmptyPrincipalIs400: the People key is required on every call.
func TestAnEmptyPrincipalIs400(t *testing.T) {
	s := screens.New(&fakeViews{}, &fakeCalls{}, theVersion, noClient)
	rec := get(t, s, "/api/home", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body %s", rec.Code, rec.Body.String())
	}
}

// TestThePrincipalReachesTheFakeAsClaimed: the header's key becomes a
// principal.OfHuman with a claimed basis, which is what every caller gets
// until seam 5 is built.
func TestThePrincipalReachesTheFakeAsClaimed(t *testing.T) {
	var got principal.Principal
	views := &fakeViews{
		home: func(_ context.Context, p principal.Principal) (screens.Home, error) {
			got = p
			return screens.Home{}, nil
		},
	}
	s := screens.New(views, &fakeCalls{}, theVersion, noClient)
	get(t, s, "/api/home", "hk_caller")

	want := principal.OfHuman("hk_caller", record.BasisClaimed)
	if got != want {
		t.Errorf("principal = %s, want %s", got, want)
	}
}

// TestClientServing: a client holding index.html serves it under a screen
// route, and a client holding none answers 503 naming where to build it.
func TestClientServing(t *testing.T) {
	client := fstest.MapFS{
		"index.html": {Data: []byte("<html>the client</html>")},
		"main.js":    {Data: []byte("console.log('hi')")},
	}
	s := screens.New(&fakeViews{}, &fakeCalls{}, theVersion, client)

	rec := get(t, s, "/work/abc", "hk_1")
	if rec.Code != http.StatusOK || string(rec.Body.Bytes()) != "<html>the client</html>" {
		t.Errorf("/work/abc: status %d, body %q, want 200 and index.html", rec.Code, rec.Body.String())
	}

	rec = get(t, s, "/main.js", "hk_1")
	if rec.Code != http.StatusOK || string(rec.Body.Bytes()) != "console.log('hi')" {
		t.Errorf("/main.js: status %d, body %q, want 200 and the static file", rec.Code, rec.Body.String())
	}

	empty := screens.New(&fakeViews{}, &fakeCalls{}, theVersion, noClient)
	rec = get(t, empty, "/", "hk_1")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("empty client at /: status = %d, want 503", rec.Code)
	}
}
