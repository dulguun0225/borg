package wayin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// bodyLimit bounds what one submission may send. The channel's own bound is
// the two rates the store enforces; this bounds one request's hold on this
// process, which is the factory's own resource and not the channel's.
const bodyLimit = 64 << 10

// Notice is the notice in force over the project the token's deploy lies in,
// as a session at the way in is shown it: the record it was collected under
// and the words an owner authored. Both are empty where an owner authored
// none, which is what the way in shows then.
type Notice struct {
	ID   string
	Text string
}

// Submission is one report as it arrives at the entrance. Nothing on it says
// which service it counts against: Token does, through the deploy record its
// digest finds, and the store is what resolves it.
type Submission struct {
	// Shape is the shape the way in wrote this submission under, and
	// ShippedBundleIdentity the release that built that way in. Both are read
	// before anything else on the submission is.
	Shape                 string
	ShippedBundleIdentity string
	// Token is the way-in token the deployer minted at the deploy that placed
	// the way in, presented on every call it makes.
	Token      string
	Kind       string
	Text       string
	HarmMarked bool
	// SourceKey is the opaque key the deployed software derived from its own
	// session and its own salt, forwarded as received. Nothing joins it to a
	// person, and it is empty where the way in supplied none or supplied
	// something that is no key.
	SourceKey string
	NoticeID  string
}

// Result is what one submission did, and the whole of what a session is
// shown: accepted, or refused with the bound that refused it as the reason.
// It names no report and carries nothing about whoever submitted.
type Result struct {
	Accepted bool
	Refusal  string
}

// Store is what the entrance reaches, implemented by the composition over the
// report store. The entrance holds no store of its own and writes nothing.
type Store interface {
	// NoticeInForce is the notice in force over the project the way-in token
	// resolves to, read at every open so that a notice moves without the
	// service building again. A token naming no deploy this factory knows
	// answers with an empty notice rather than an error: what a session is
	// shown at the open says nothing about which deploy it opened.
	NoticeInForce(ctx context.Context, token string) (Notice, error)
	// Submit is what the store did with one submission, refusal included.
	// collectedAt is when this entrance received it: the instant the way in
	// names is a deployed service's clock, and a rate counted over one could
	// be spent by a service whose clock is wrong.
	Submit(ctx context.Context, sub Submission, collectedAt time.Time) (Result, error)
}

// Entrance is the factory side of the way in: the notice at the open and the
// submission, over [Store] and nothing else. It is the one entrance into the
// factory from outside it, and it writes no record about a person, reads
// none, and keeps nothing about a session beyond the response it answers with.
type Entrance struct {
	store Store
	mux   *http.ServeMux
}

// NewEntrance composes an entrance over store.
func NewEntrance(store Store) *Entrance {
	e := &Entrance{store: store}
	e.mux = http.NewServeMux()
	e.mux.Handle("GET "+NoticePath, http.HandlerFunc(e.handleNotice))
	e.mux.Handle("POST "+SubmitPath, http.HandlerFunc(e.handleSubmit))
	return e
}

// ServeHTTP makes Entrance an [http.Handler].
func (e *Entrance) ServeHTTP(w http.ResponseWriter, r *http.Request) { e.mux.ServeHTTP(w, r) }

// noticeBody is the notice on the wire, in the field names the shipped source
// reads it under.
type noticeBody struct {
	NoticeID string `json:"notice_id"`
	Text     string `json:"text"`
}

// submissionBody is a submission on the wire, in the field names the shipped
// source writes. The token is not among them: it is presented in the header,
// which is where the way in puts it and where nothing renders it back. Nor is
// the session the source key was derived from: that is the deployed
// software's own and never leaves it.
type submissionBody struct {
	Shape                 string `json:"shape"`
	ShippedBundleIdentity string `json:"shipped_bundle_identity"`
	Kind                  string `json:"kind"`
	Text                  string `json:"text"`
	HarmMarked            bool   `json:"harm_marked"`
	SourceKey             string `json:"source_key"`
	NoticeID              string `json:"notice_id"`
}

// resultBody is the submit result on the wire.
type resultBody struct {
	Accepted bool   `json:"accepted"`
	Refusal  string `json:"refusal,omitempty"`
}

func (e *Entrance) handleNotice(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get(TokenHeader)
	if token == "" {
		plain(w, http.StatusBadRequest, "the call presents no way-in token")
		return
	}
	notice, err := e.store.NoticeInForce(r.Context(), token)
	if err != nil {
		plain(w, http.StatusServiceUnavailable, "the report store could not be reached")
		return
	}
	writeJSON(w, noticeBody{NoticeID: notice.ID, Text: notice.Text})
}

func (e *Entrance) handleSubmit(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get(TokenHeader)
	if token == "" {
		plain(w, http.StatusBadRequest, "the call presents no way-in token")
		return
	}
	var body submissionBody
	if err := json.NewDecoder(io.LimitReader(r.Body, bodyLimit)).Decode(&body); err != nil {
		plain(w, http.StatusBadRequest, "the submission could not be read")
		return
	}
	// What reaches the store is these fields and nothing else the request
	// carried: no address, no header beyond the token, and no field a person
	// could be recovered from.
	sub := Submission{
		Shape:                 body.Shape,
		ShippedBundleIdentity: body.ShippedBundleIdentity,
		Token:                 token,
		Kind:                  body.Kind,
		Text:                  body.Text,
		HarmMarked:            body.HarmMarked,
		SourceKey:             opaqueKey(body.SourceKey),
		NoticeID:              body.NoticeID,
	}
	result, err := e.store.Submit(r.Context(), sub, time.Now())
	if err != nil {
		// The store's own words say what it holds and who reached it, and
		// this is the one address outside the factory can reach, so what a
		// failure renders is that it failed.
		plain(w, http.StatusServiceUnavailable, "the report store could not be reached")
		return
	}
	writeJSON(w, resultBody{Accepted: result.Accepted, Refusal: result.Refusal})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func plain(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, message+"\n")
}
