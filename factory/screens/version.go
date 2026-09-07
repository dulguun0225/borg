package screens

import (
	"encoding/json"
	"net/http"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
)

// checked is the wrapper on every /api/ route: the factory version the
// client was built from must equal this server's, on every call including a
// read, or the call is refused with a required reload rather than answered
// or half-answered; and the People key the human says they are becomes the
// principal.Principal every Views and Calls method carries. Nothing here
// verifies the key: seam 5 of "Security comes last" is where a principal is
// checked, and this milestone attaches none, so any key a caller sends is
// carried as principal.OfHuman with a claimed basis.
//
// Both values are read from the header, and where the header is absent from
// the query string under the same name: a browser's EventSource sets no
// request header, so the stream a client subscribes to carries them there.
func (s *Server) checked(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if carried(r, headerVersion) != s.version {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"reload_required": true,
				"expected":        s.version,
			})
			return
		}
		key := carried(r, headerPrincipal)
		if key == "" {
			writeError(w, http.StatusBadRequest, "screens: "+headerPrincipal+" is required")
			return
		}
		p := principal.OfHuman(key, record.BasisClaimed)
		next(w, r.WithContext(withPrincipal(r.Context(), p)))
	})
}

// carried is the value under name in the request's header, or in its query
// string where the header is absent.
func carried(r *http.Request, name string) string {
	if value := r.Header.Get(name); value != "" {
		return value
	}
	return r.URL.Query().Get(name)
}
