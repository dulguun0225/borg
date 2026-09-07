package screens_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/screens"
)

// TestAMatchingVersionReachesTheHandler: the version refusal is only for a
// mismatch — a call naming the server's own version is answered rather than
// refused.
func TestAMatchingVersionReachesTheHandler(t *testing.T) {
	called := false
	views := &fakeViews{
		home: func(context.Context, principal.Principal) (screens.Home, error) {
			called = true
			return screens.Home{}, nil
		},
	}
	s := screens.New(views, &fakeCalls{}, theVersion, noClient)
	rec := get(t, s, "/api/home", "hk_1")
	if rec.Code != http.StatusOK || !called {
		t.Errorf("status = %d, called = %v, want 200 and the view reached", rec.Code, called)
	}
}

// TestTheVersionRefusalTakesPrecedenceOverAMissingPrincipal: a call with
// both a wrong version and no principal is a version refusal, not a bad
// request — a client rendering the reload must never confuse the two.
func TestTheVersionRefusalTakesPrecedenceOverAMissingPrincipal(t *testing.T) {
	s := screens.New(&fakeViews{}, &fakeCalls{}, theVersion, noClient)
	r := httptest.NewRequest(http.MethodGet, "/api/home", nil)
	r.Header.Set("X-Factory-Version", "not-"+theVersion)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

// TestEveryAPIRouteIsChecked: the stream route and the call route are wrapped
// in the same middleware as a read, so a version mismatch refuses them too.
func TestEveryAPIRouteIsChecked(t *testing.T) {
	s := screens.New(&fakeViews{}, &fakeCalls{}, theVersion, noClient)
	for _, path := range []string{"/api/stream/item/it_1", "/api/call/acknowledge"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if path == "/api/call/acknowledge" {
			r.Method = http.MethodPost
		}
		r.Header.Set("X-Factory-Version", "not-"+theVersion)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		if rec.Code != http.StatusConflict {
			t.Errorf("%s: status = %d, want 409", path, rec.Code)
		}
	}
}

// TestTheStreamTakesBothValuesFromTheQueryString: a browser's EventSource
// sets no header, so the two values every call carries reach the stream route
// on the query string and are read there.
func TestTheStreamTakesBothValuesFromTheQueryString(t *testing.T) {
	server := screens.New(&fakeViews{}, &fakeCalls{}, theVersion, noClient)
	rec := httptest.NewRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet,
		"/api/stream/work/-?X-Factory-Version="+theVersion+"&X-Factory-Principal=owner", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() { server.ServeHTTP(rec, req); close(done) }()
	cancel()
	<-done
	if rec.Code != http.StatusOK {
		t.Fatalf("the stream answered %d with %q, want 200", rec.Code, rec.Body.String())
	}
}
