package screens

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	"github.com/dulguun0225/borg/factory/principal"
)

// The two headers every call carries: the factory version the client was
// built from, and the People key the human at the screen says they are.
// version.go's middleware checks both before any handler below runs.
const (
	headerVersion   = "X-Factory-Version"
	headerPrincipal = "X-Factory-Principal"
)

// screenRoots is where the client's own router takes over: the four screen
// routes and everything under them get index.html rather than a static
// file, so a link into an item or a decision opens the client at that
// address rather than a 404 from the file system.
var screenRoots = [...]string{"/work", "/ops", "/factory", "/people"}

// Server is the server side of the four screens: the reads [Views] answers,
// the writes [Calls] performs, the stream a client subscribes to, and the
// embedded client itself. It owns no table: every record it reads and every
// writer it reaches is behind Views and Calls, which whatever composes it
// implements.
type Server struct {
	views   Views
	calls   Calls
	version string
	client  fs.FS
	streams *streams
	mux     *http.ServeMux
}

// New composes a Server: views answers every read, calls performs every
// write, version is what every call is checked against, and client is the
// directory the built client serves from — clientdist.Browser() once the
// client's build has run, and an empty fs.FS on a fresh clone that has not.
func New(views Views, calls Calls, version string, client fs.FS) *Server {
	s := &Server{views: views, calls: calls, version: version, client: client, streams: newStreams()}
	s.mux = http.NewServeMux()
	s.routes()
	return s
}

// ServeHTTP makes Server an [http.Handler].
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) routes() {
	api := func(h http.HandlerFunc) http.Handler { return s.checked(h) }
	s.mux.Handle("GET /api/home", api(s.handleHome))
	s.mux.Handle("GET /api/work", api(s.handleWork))
	s.mux.Handle("GET /api/ops", api(s.handleOps))
	s.mux.Handle("GET /api/factory", api(s.handleFactory))
	s.mux.Handle("GET /api/people", api(s.handlePeople))
	s.mux.Handle("GET /api/item/{id}", api(s.handleItem))
	s.mux.Handle("GET /api/decision/{id}", api(s.handleDecision))
	s.mux.Handle("GET /api/service/{serviceID}/on/{environmentID}", api(s.handleServiceOn))
	s.mux.Handle("GET /api/constraint/{id}", api(s.handleConstraint))
	s.mux.Handle("POST /api/call/{name}", api(s.handleCall))
	s.mux.Handle("GET /api/stream/{kind}/{id}", api(s.handleStream))
	s.mux.Handle("GET /", http.HandlerFunc(s.handleClient))
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	v, err := s.views.Home(r.Context(), principalFrom(r.Context()))
	writeView(w, v, err)
}

func (s *Server) handleWork(w http.ResponseWriter, r *http.Request) {
	filter := Filter{WaitingOnAHuman: r.URL.Query().Get("waiting_on_a_human") == "true"}
	v, err := s.views.Work(r.Context(), principalFrom(r.Context()), filter)
	writeView(w, v, err)
}

func (s *Server) handleOps(w http.ResponseWriter, r *http.Request) {
	v, err := s.views.Ops(r.Context(), principalFrom(r.Context()))
	writeView(w, v, err)
}

func (s *Server) handleFactory(w http.ResponseWriter, r *http.Request) {
	v, err := s.views.Factory(r.Context(), principalFrom(r.Context()))
	writeView(w, v, err)
}

func (s *Server) handlePeople(w http.ResponseWriter, r *http.Request) {
	v, err := s.views.People(r.Context(), principalFrom(r.Context()))
	writeView(w, v, err)
}

func (s *Server) handleItem(w http.ResponseWriter, r *http.Request) {
	v, err := s.views.Item(r.Context(), principalFrom(r.Context()), r.PathValue("id"))
	writeView(w, v, err)
}

func (s *Server) handleDecision(w http.ResponseWriter, r *http.Request) {
	v, err := s.views.Decision(r.Context(), principalFrom(r.Context()), r.PathValue("id"))
	writeView(w, v, err)
}

func (s *Server) handleServiceOn(w http.ResponseWriter, r *http.Request) {
	v, err := s.views.ServiceOn(r.Context(), principalFrom(r.Context()), r.PathValue("serviceID"), r.PathValue("environmentID"))
	writeView(w, v, err)
}

func (s *Server) handleConstraint(w http.ResponseWriter, r *http.Request) {
	v, err := s.views.Constraint(r.Context(), principalFrom(r.Context()), r.PathValue("id"))
	writeView(w, v, err)
}

// handleClient serves the embedded client: every screen route and anything
// under it gets index.html, so the client's own router takes over from
// whichever address a link opened at, and everything else is served as a
// static file out of client. A client directory with no index.html is a
// fresh clone whose client has not been built yet.
func (s *Server) handleClient(w http.ResponseWriter, r *http.Request) {
	if _, err := fs.Stat(s.client, "index.html"); err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintln(w, "the client is not built: build it under factory/client")
		return
	}
	if isScreenRoute(r.URL.Path) {
		http.ServeFileFS(w, r, s.client, "index.html")
		return
	}
	http.FileServerFS(s.client).ServeHTTP(w, r)
}

func isScreenRoute(path string) bool {
	for _, root := range screenRoots {
		if path == root || strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}

// writeJSON writes v as the response body.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes the {"error": "..."} shape every failure but the version
// refusal takes.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// statusFor is 404 for an address or a referenced record ErrNotFound names,
// and 500 for anything else a view or a call returned.
func statusFor(err error) int {
	if errors.Is(err, ErrNotFound) {
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}

// writeView answers a read: the view as JSON, or its error.
func writeView(w http.ResponseWriter, v any, err error) {
	if err != nil {
		writeError(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, v)
}

// ctxKey is the type of the one value this package stores on a request's
// context: the principal version.go's middleware resolved.
type ctxKey int

const principalKey ctxKey = 0

func withPrincipal(ctx context.Context, p principal.Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

func principalFrom(ctx context.Context) principal.Principal {
	p, _ := ctx.Value(principalKey).(principal.Principal)
	return p
}
