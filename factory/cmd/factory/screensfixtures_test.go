// The four screens as a test drives them: the real composition behind
// package screens' own handler, over HTTP, with both headers on every call.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dulguun0225/borg/factory/clientdist"
	"github.com/dulguun0225/borg/factory/screens"
)

// served is the composition behind the screens, the handler over it, and a
// test server serving it: the same three values [serveCommand] composes, so a
// test drives the views and the calls the process serves and not a second
// arrangement of them.
type screenServer struct {
	p    *path
	made *calls
	url  string
	// principal is what the X-Factory-Principal header carries: the
	// per-person key of the human the test says it is.
	principal string
}

// newScreens composes the path over d, the views and the calls over it, the
// handler over those, and a server serving it until the test ends.
func newScreens(t *testing.T, ctx context.Context, d deps, out *bytes.Buffer) *screenServer {
	t.Helper()
	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path: %v\n%s", err, out)
	}
	views := &views{p: p}
	made := &calls{p: p, v: views}
	handler := screens.New(views, made, factoryVersion, clientdist.Browser())
	made.server = handler
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &screenServer{p: p, made: made, url: server.URL, principal: p.human.Key}
}

// get reads one address and decodes the view into into, failing the test on
// any status but 200.
func (s *screenServer) get(t *testing.T, address string, into any) {
	t.Helper()
	request, err := http.NewRequest("GET", s.url+address, nil)
	if err != nil {
		t.Fatalf("building the request for %s: %v", address, err)
	}
	s.headers(request)
	answer, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET %s: %v", address, err)
	}
	defer answer.Body.Close()
	if answer.StatusCode != http.StatusOK {
		read := &bytes.Buffer{}
		_, _ = read.ReadFrom(answer.Body)
		t.Fatalf("GET %s answered %d: %s", address, answer.StatusCode, read)
	}
	if into == nil {
		return
	}
	if err := json.NewDecoder(answer.Body).Decode(into); err != nil {
		t.Fatalf("decoding %s: %v", address, err)
	}
}

// status is the status one read answered with, for a test about a refusal
// rather than about a view.
func (s *screenServer) status(t *testing.T, address string) int {
	t.Helper()
	request, err := http.NewRequest("GET", s.url+address, nil)
	if err != nil {
		t.Fatalf("building the request for %s: %v", address, err)
	}
	s.headers(request)
	answer, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET %s: %v", address, err)
	}
	defer answer.Body.Close()
	return answer.StatusCode
}

// call makes one call and returns its status and the body it answered with,
// which is the id where the call creates an address and the error otherwise.
func (s *screenServer) call(t *testing.T, name string, args any) (int, string) {
	t.Helper()
	body, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshalling the arguments of %s: %v", name, err)
	}
	request, err := http.NewRequest("POST", s.url+"/api/call/"+name, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("building the call %s: %v", name, err)
	}
	s.headers(request)
	answer, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST /api/call/%s: %v", name, err)
	}
	defer answer.Body.Close()
	read := &bytes.Buffer{}
	_, _ = read.ReadFrom(answer.Body)
	return answer.StatusCode, read.String()
}

// mustCall makes one call and fails the test where it was refused, answering
// with the id where the call creates an address.
func (s *screenServer) mustCall(t *testing.T, name string, args any) string {
	t.Helper()
	status, body := s.call(t, name, args)
	if status != http.StatusNoContent && status != http.StatusOK {
		t.Fatalf("POST /api/call/%s answered %d: %s", name, status, body)
	}
	if status == http.StatusNoContent {
		return ""
	}
	var created struct{ ID string }
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatalf("decoding the id %s answered with: %v", name, err)
	}
	return created.ID
}

// headers is the two every call carries: the factory version the client was
// built from, which is enforced, and the People key the human says they are,
// which is not.
func (s *screenServer) headers(request *http.Request) {
	request.Header.Set("X-Factory-Version", factoryVersion)
	request.Header.Set("X-Factory-Principal", s.principal)
}

// theFleetEntry is the entry episode one writes per role: this test's model at
// no effort, scoped to the whole factory, on the credential the owner lent.
func theFleetEntry(role string) screens.WriteFleetEntryArgs {
	return screens.WriteFleetEntryArgs{
		ModelVersion:              theModel,
		Role:                      role,
		Credential:                "model.fake",
		ProcessingLocation:        "fake",
		MaterialClasses:           []string{"intent_statement"},
		ReadAtOnceBound:           200_000,
		DispatchesBetweenEvalRuns: 50,
	}
}
