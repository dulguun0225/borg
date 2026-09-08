package wayin

import (
	"fmt"
	"strconv"
	"strings"
)

// FileName is the name the shipped source takes in a service's checkout. It
// is the path the overlay replaces and never a file written there: nothing
// in this package writes into a checkout.
const FileName = "borg_wayin.go"

// identityPoint is the one substitution point in [ShippedSource]: the quoted
// shipped-bundle identity, which [Source] replaces with the identity its
// caller was given. It is quoted in the constant so the unfilled source is
// itself valid Go, which is what lets the tests parse and format it.
const identityPoint = `"shipped-bundle-identity"`

// Shape is the shape the shipped way in writes a submission under, and the
// same string ShippedSource carries. A shape once shipped is added to and
// never changed under its identity, so a second shape is a second constant
// and a second shipped source beside this one.
const Shape = "submission/1"

// The two routes the entrance serves and the header a submission presents
// the way-in token on. ShippedSource carries the same three strings: it is
// text this package ships rather than code that can import a constant, so
// the two spellings are one search away from each other.
const (
	NoticePath  = "/way-in/notice"
	SubmitPath  = "/way-in/submit"
	TokenHeader = "X-Borg-Way-In"
)

// Source is the shipped source with identity filled in, which is what
// [Overlay] writes and a service's build compiles. An empty identity is an
// error: the identity is what says which release built the way in, and a
// submission naming none is refused at the store.
func Source(identity string) (string, error) {
	if identity == "" {
		return "", fmt.Errorf("wayin: the shipped source names no shipped-bundle identity")
	}
	if n := strings.Count(ShippedSource, identityPoint); n != 1 {
		return "", fmt.Errorf("wayin: the shipped source has %d substitution points and not one", n)
	}
	return strings.Replace(ShippedSource, identityPoint, strconv.Quote(identity), 1), nil
}

// ShippedSource is the way in itself: a package main file the factory ships
// and versions with itself, injected into every service's build and written
// into no repository. It reads the three names the factory hands the service
// it was deployed to, serves the notice and takes a submission on a socket
// in that service's own directory, and presents the way-in token to the
// entrance on both calls.
//
// It uses the standard library and nothing else, because it is compiled
// inside a module the factory does not author and cannot add a requirement
// to. Every name in it is prefixed so that none can collide with the
// service's own, and its one func init is the departure doc.go states.
const ShippedSource = `package main

// This file is the factory's way in. The factory injects it into this
// service's build and writes it into no repository, so it is not the
// service's own code and no commit here carries it.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

// The three names the factory hands the service it deployed. Nothing here
// runs unless all three are set, so a service started outside the factory is
// unaffected by the file being in its build.
const (
	borgWayInTokenEnv  = "BORG_WAY_IN"
	borgWayInStoreEnv  = "BORG_WAY_IN_STORE"
	borgWayInListenEnv = "BORG_WAY_IN_LISTEN"
)

// borgWayInShippedBundleIdentity is the release of the factory that built
// this way in. The factory fills it at the build, so it moves only when the
// factory is upgraded and this service builds again.
const borgWayInShippedBundleIdentity = "shipped-bundle-identity"

// borgWayInShape is the shape this way in writes a submission under. The
// store reads every shape any release ever shipped, so a submission this
// factory version cannot read is counted rather than read as something else.
const borgWayInShape = "submission/1"

// The two routes at the entrance and the header the token is presented on.
const (
	borgWayInNoticePath  = "/way-in/notice"
	borgWayInSubmitPath  = "/way-in/submit"
	borgWayInTokenHeader = "X-Borg-Way-In"
)

// borgWayInBodyLimit bounds what one submission may send, and
// borgWayInTimeout bounds both the session on the socket and the call to the
// entrance. The channel's own bound is the rates the store enforces; these
// two bound one session's hold on this service's memory and goroutines,
// which is the service's own resource and not the channel's.
const (
	borgWayInBodyLimit = 64 << 10
	borgWayInTimeout   = 20 * time.Second
)

// init starts the way in. It is an init because this file is compiled into a
// main the factory did not write, where nothing calls a start function.
func init() {
	token := os.Getenv(borgWayInTokenEnv)
	store := os.Getenv(borgWayInStoreEnv)
	listen := os.Getenv(borgWayInListenEnv)
	if token == "" || store == "" || listen == "" {
		log.Printf("borg way in: not started: %s, %s and %s are not all set",
			borgWayInTokenEnv, borgWayInStoreEnv, borgWayInListenEnv)
		return
	}
	// The socket is this way in's own path in the target's directory, so one
	// left behind by a process that ended is removed rather than read as
	// another way in still listening.
	_ = os.Remove(listen)
	listener, err := net.Listen("unix", listen)
	if err != nil {
		log.Printf("borg way in: not started: listening on %s: %v", listen, err)
		return
	}
	w := &borgWayIn{
		token:  token,
		store:  store,
		salt:   borgWayInRandom(),
		client: &http.Client{Timeout: borgWayInTimeout},
	}
	server := &http.Server{Handler: w, ReadTimeout: borgWayInTimeout, WriteTimeout: borgWayInTimeout}
	go func() { _ = server.Serve(listener) }()
}

// ServeHTTP is the whole surface: the notice at the open, and a submission.
// It reads the method itself rather than registering a method pattern on a
// ServeMux, because this file is compiled inside a module the factory does
// not author and a module declaring a go directive below 1.22 gets the
// routing of that release, where a method pattern is a literal path and
// matches nothing.
func (w *borgWayIn) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.borgWayInNotice(rw, r)
	case http.MethodPost:
		w.borgWayInSubmit(rw, r)
	default:
		rw.Header().Set("Allow", "GET, POST")
		borgWayInPlain(rw, http.StatusMethodNotAllowed, "the way in shows a notice and takes a report")
	}
}

// borgWayIn is the way in as it runs: the token this deploy was minted, the
// entrance to reach, the salt every source key here is derived under, and the
// client that reaches the entrance.
//
// The salt is minted once, in this process, and written nowhere: it is what
// makes a source key one this deployed software supplied rather than a name
// for a session anything else could compute, and a process that starts again
// re-keys every session it sees.
type borgWayIn struct {
	token  string
	store  string
	salt   string
	client *http.Client
}

// borgWayInNoticeBody is what a session is shown at the open: the notice in
// force over this project, read from the store at every open so that it
// moves without this service building again, and the session key this way in
// mints for that session and nothing else.
type borgWayInNoticeBody struct {
	NoticeID string ` + "`" + `json:"notice_id"` + "`" + `
	Text     string ` + "`" + `json:"text"` + "`" + `
	Session  string ` + "`" + `json:"session"` + "`" + `
}

// borgWayInSubmissionBody is what a session submits: the two fields the
// reporter authors, the field the reporter sets themselves, and the notice
// and the session it was shown at the open.
type borgWayInSubmissionBody struct {
	Kind       string ` + "`" + `json:"kind"` + "`" + `
	Text       string ` + "`" + `json:"text"` + "`" + `
	HarmMarked bool   ` + "`" + `json:"harm_marked"` + "`" + `
	Session    string ` + "`" + `json:"session"` + "`" + `
	NoticeID   string ` + "`" + `json:"notice_id"` + "`" + `
}

// borgWayInForwarded is what reaches the entrance. It carries what the
// session sent plus what this way in is: the shape, the identity of the
// release that built it, and the source key this way in derived. The token
// travels in the header and never here, and the session the key was derived
// from never leaves this process.
type borgWayInForwarded struct {
	Shape                 string ` + "`" + `json:"shape"` + "`" + `
	ShippedBundleIdentity string ` + "`" + `json:"shipped_bundle_identity"` + "`" + `
	Kind                  string ` + "`" + `json:"kind"` + "`" + `
	Text                  string ` + "`" + `json:"text"` + "`" + `
	HarmMarked            bool   ` + "`" + `json:"harm_marked"` + "`" + `
	SourceKey             string ` + "`" + `json:"source_key"` + "`" + `
	NoticeID              string ` + "`" + `json:"notice_id"` + "`" + `
}

// borgWayInResult is the submit result, rendered in the session that
// submitted: accepted, or refused with the bound that refused it as the
// reason. Nothing else is rendered and nothing about the session outlives it.
type borgWayInResult struct {
	Accepted bool   ` + "`" + `json:"accepted"` + "`" + `
	Refusal  string ` + "`" + `json:"refusal,omitempty"` + "`" + `
}

// borgWayInNotice shows the notice in force and mints the session key the
// submission carries back.
func (w *borgWayIn) borgWayInNotice(rw http.ResponseWriter, _ *http.Request) {
	var notice borgWayInNoticeBody
	if !w.borgWayInCall(http.MethodGet, borgWayInNoticePath, nil, &notice) {
		borgWayInPlain(rw, http.StatusServiceUnavailable, "the way in cannot reach the factory")
		return
	}
	notice.Session = borgWayInRandom()
	borgWayInJSON(rw, http.StatusOK, notice)
}

// borgWayInSubmit takes one submission, forwards it with the token, and
// renders what the store did with it in the same response.
func (w *borgWayIn) borgWayInSubmit(rw http.ResponseWriter, r *http.Request) {
	var body borgWayInSubmissionBody
	if err := json.NewDecoder(io.LimitReader(r.Body, borgWayInBodyLimit)).Decode(&body); err != nil {
		borgWayInPlain(rw, http.StatusBadRequest, "the submission could not be read")
		return
	}
	if body.Text == "" {
		borgWayInPlain(rw, http.StatusBadRequest, "a report is words, and this one carries none")
		return
	}
	forwarded := borgWayInForwarded{
		Shape:                 borgWayInShape,
		ShippedBundleIdentity: borgWayInShippedBundleIdentity,
		Kind:                  body.Kind,
		Text:                  body.Text,
		HarmMarked:            body.HarmMarked,
		SourceKey:             w.borgWayInSourceKey(body.Session),
		NoticeID:              body.NoticeID,
	}
	var result borgWayInResult
	if !w.borgWayInCall(http.MethodPost, borgWayInSubmitPath, forwarded, &result) {
		borgWayInPlain(rw, http.StatusServiceUnavailable, "the way in cannot reach the factory")
		return
	}
	borgWayInJSON(rw, http.StatusOK, result)
}

// borgWayInCall makes one call to the entrance under the way-in token, which
// is how this way in calls as the deploy that placed it. It builds a request
// of its own and forwards nothing the session sent beyond the body above, so
// nothing about whoever is submitting reaches the factory. It reports
// whether the entrance answered.
//
// The call is bounded by its own timeout and not by the submitting session:
// a report already sent has to land whether or not the session is still open
// to be shown the result, and a session that closes the moment it submits is
// how a report is submitted from a page somebody is leaving.
// It is written interface{} rather than any, and every other name here is
// held to the same bound: this file is compiled under the go directive of a
// module the factory does not author, and any is a spelling that release
// does not have.
func (w *borgWayIn) borgWayInCall(method, path string, send, into interface{}) bool {
	ctx, done := context.WithTimeout(context.Background(), borgWayInTimeout)
	defer done()
	var body io.Reader
	if send != nil {
		encoded, err := json.Marshal(send)
		if err != nil {
			return false
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, w.store+path, body)
	if err != nil {
		return false
	}
	request.Header.Set(borgWayInTokenHeader, w.token)
	if send != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := w.client.Do(request)
	if err != nil {
		return false
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return false
	}
	return json.NewDecoder(io.LimitReader(response.Body, borgWayInBodyLimit)).Decode(into) == nil
}

// borgWayInSourceKey is the opaque key one session's reports are marked
// with: the session this way in minted at the open, salted with this
// process's own salt and digested here, so that what leaves the service is
// the key and never the session. It carries nobody's identity — the session
// it is derived from was minted here and joined to nothing.
//
// A session this way in never minted keys the same way as one it did: what
// the key marks is a session and not a caller, and nothing here decides
// anything on it. Where there is no session, or no salt to derive under,
// there is no key at all, which is what the store reads as a report carrying
// none — never a weak key.
func (w *borgWayIn) borgWayInSourceKey(session string) string {
	if session == "" || w.salt == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(w.salt + "\x00" + session))
	return hex.EncodeToString(sum[:])
}

// borgWayInRandom is a fresh opaque value: the salt at start, and a session
// at every open. It carries nothing about whoever is given one, and where it
// cannot be minted it is empty rather than guessable.
func borgWayInRandom() string {
	var minted [16]byte
	if _, err := rand.Read(minted[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(minted[:])
}

func borgWayInJSON(rw http.ResponseWriter, status int, v interface{}) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	_ = json.NewEncoder(rw).Encode(v)
}

func borgWayInPlain(rw http.ResponseWriter, status int, message string) {
	rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
	rw.WriteHeader(status)
	_, _ = io.WriteString(rw, message+"\n")
}
`
