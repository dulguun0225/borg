package wayin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/wayin"
)

// fakeStore is the composition's side of [wayin.Store], recording what
// reached it. It is what a test reads the submission off: the entrance
// writes nothing itself, so what it did is what the store was handed.
type fakeStore struct {
	notice      wayin.Notice
	noticeFor   []string
	submitted   []wayin.Submission
	collectedAt []time.Time
	result      wayin.Result
	err         error
}

func (f *fakeStore) NoticeInForce(_ context.Context, token string) (wayin.Notice, error) {
	f.noticeFor = append(f.noticeFor, token)
	return f.notice, f.err
}

func (f *fakeStore) Submit(_ context.Context, sub wayin.Submission, collectedAt time.Time) (wayin.Result, error) {
	f.submitted = append(f.submitted, sub)
	f.collectedAt = append(f.collectedAt, collectedAt)
	return f.result, f.err
}

// notice reads the notice at the open, as a way in does.
func notice(t *testing.T, entrance *wayin.Entrance, token string) (int, string) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, wayin.NoticePath, nil)
	if token != "" {
		r.Header.Set(wayin.TokenHeader, token)
	}
	w := httptest.NewRecorder()
	entrance.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

// submit posts one submission, as a way in does, and returns the status and
// the body a session is shown.
func submit(t *testing.T, entrance *wayin.Entrance, token, body string) (int, string) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, wayin.SubmitPath, strings.NewReader(body))
	if token != "" {
		r.Header.Set(wayin.TokenHeader, token)
	}
	w := httptest.NewRecorder()
	entrance.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

// derived is a source key in the shape the shipped way in sends one: the
// hexadecimal of a digest, and nothing else this entrance will forward.
const derived = "9f2c4a7b1e0d3856ac91bf4e27d0538a6b4c1f9e02d7a385cb61e4f70d29a8b3"

func TestTheNoticeRenders(t *testing.T) {
	store := &fakeStore{notice: wayin.Notice{ID: "con_1", Text: "what this channel is for"}}
	code, body := notice(t, wayin.NewEntrance(store), "token-1")

	if code != http.StatusOK {
		t.Fatalf("the notice answered %d, want %d: %s", code, http.StatusOK, body)
	}
	if !strings.Contains(body, "what this channel is for") || !strings.Contains(body, "con_1") {
		t.Errorf("the notice rendered %q, want the words and the record an owner authored", body)
	}
	if len(store.noticeFor) != 1 || store.noticeFor[0] != "token-1" {
		t.Errorf("the notice was read for %v, want the token the way in presented", store.noticeFor)
	}
}

func TestTheNoticeIsRefusedWithoutAToken(t *testing.T) {
	store := &fakeStore{}
	code, _ := notice(t, wayin.NewEntrance(store), "")

	if code != http.StatusBadRequest {
		t.Errorf("a call presenting no token answered %d, want %d", code, http.StatusBadRequest)
	}
	if len(store.noticeFor) != 0 {
		t.Errorf("a call presenting no token reached the store %d times, want none", len(store.noticeFor))
	}
}

// TestASubmissionReachesTheStoreWithTheTokenAndNeverAPerson sends everything
// a session's own client would carry beside the report — an address, a
// cookie, an agent, a name — and asserts that the token is what reached the
// store and that none of the rest reached it or was rendered back.
func TestASubmissionReachesTheStoreWithTheTokenAndNeverAPerson(t *testing.T) {
	store := &fakeStore{
		notice: wayin.Notice{ID: "con_1"},
		result: wayin.Result{Accepted: true},
	}
	entrance := wayin.NewEntrance(store)

	body := `{"shape":"submission/1","shipped_bundle_identity":"factory/3","kind":"bug",` +
		`"text":"the save button does nothing","harm_marked":true,"source_key":"` + derived + `",` +
		`"notice_id":"con_1"}`
	r := httptest.NewRequest(http.MethodPost, wayin.SubmitPath, strings.NewReader(body))
	r.Header.Set(wayin.TokenHeader, "token-1")
	r.Header.Set("Cookie", "who=zoe-lovelace")
	r.Header.Set("User-Agent", "zoe-browser")
	r.Header.Set("Authorization", "Bearer zoe-secret")
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	r.RemoteAddr = "203.0.113.7:51000"
	w := httptest.NewRecorder()
	entrance.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("the submission answered %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if len(store.submitted) != 1 {
		t.Fatalf("the store took %d submissions, want one", len(store.submitted))
	}
	sub := store.submitted[0]
	if sub.Token != "token-1" {
		t.Errorf("the store was handed the token %q, want the one the way in presented", sub.Token)
	}
	if sub.Shape != "submission/1" || sub.ShippedBundleIdentity != "factory/3" {
		t.Errorf("the store was handed shape %q and identity %q, want what the way in wrote",
			sub.Shape, sub.ShippedBundleIdentity)
	}
	if sub.Kind != "bug" || sub.Text != "the save button does nothing" || !sub.HarmMarked ||
		sub.NoticeID != "con_1" {
		t.Errorf("the store was handed %+v, want what the session submitted", sub)
	}
	if sub.SourceKey != derived {
		t.Errorf("the source key is %q, want the one the way in derived", sub.SourceKey)
	}
	fields := strings.Join([]string{sub.Shape, sub.ShippedBundleIdentity, sub.Token, sub.Kind,
		sub.Text, sub.SourceKey, sub.NoticeID}, " ")
	// Each value carries a character no hexadecimal digest can, so a
	// derived key matching one is a value that reached it and never chance.
	for _, carried := range []string{"zoe-lovelace", "203.0.113.7", "zoe-browser", "zoe-secret"} {
		if strings.Contains(fields, carried) {
			t.Errorf("%q reached the store, and nothing about a person may", carried)
		}
		if strings.Contains(w.Body.String(), carried) {
			t.Errorf("%q was rendered back, and the render carries no identity", carried)
		}
	}
	if len(store.collectedAt) != 1 || store.collectedAt[0].IsZero() {
		t.Errorf("the store was handed %v as the instant, want the entrance's own", store.collectedAt)
	}
}

func TestARefusalRendersAsTheSubmitResult(t *testing.T) {
	refusal := "the report channel is over the rate an owner authored for the whole factory"
	store := &fakeStore{result: wayin.Result{Refusal: refusal}}

	code, body := submit(t, wayin.NewEntrance(store), "token-1",
		`{"kind":"bug","text":"the save button does nothing","source_key":"`+derived+`","notice_id":""}`)

	if code != http.StatusOK {
		t.Fatalf("a refused submission answered %d, want %d: %s", code, http.StatusOK, body)
	}
	var result struct {
		Accepted bool   `json:"accepted"`
		Refusal  string `json:"refusal"`
	}
	if err := json.NewDecoder(bytes.NewReader([]byte(body))).Decode(&result); err != nil {
		t.Fatalf("reading the submit result %q: %v", body, err)
	}
	if result.Accepted {
		t.Errorf("the result reads accepted, want refused")
	}
	if result.Refusal != refusal {
		t.Errorf("the result renders %q, want the bound that refused it", result.Refusal)
	}
}

func TestASubmissionIsRefusedWithoutAToken(t *testing.T) {
	store := &fakeStore{}
	code, _ := submit(t, wayin.NewEntrance(store), "", `{"kind":"bug","text":"words"}`)

	if code != http.StatusBadRequest {
		t.Errorf("a submission presenting no token answered %d, want %d", code, http.StatusBadRequest)
	}
	if len(store.submitted) != 0 {
		t.Errorf("a submission presenting no token reached the store, and it names no deploy")
	}
}

// TestAStoreFailureRendersNothingOfItsOwn holds the one address outside the
// factory can reach to answering that it failed and never with what failed.
func TestAStoreFailureRendersNothingOfItsOwn(t *testing.T) {
	store := &fakeStore{err: errors.New("reportstore: dial tcp 10.0.0.4:5432: connection refused")}
	entrance := wayin.NewEntrance(store)

	for _, answer := range []string{
		mustBody(notice(t, entrance, "token-1")),
		mustBody(submit(t, entrance, "token-1", `{"kind":"bug","text":"words","notice_id":""}`)),
	} {
		if strings.Contains(answer, "10.0.0.4") || strings.Contains(answer, "reportstore:") {
			t.Errorf("a failure rendered %q, want nothing of the store's own", answer)
		}
	}
}

func mustBody(_ int, body string) string { return body }

// TestTheEntranceForwardsTheKeyAsReceived holds this side to deriving
// nothing: the key is the deployed software's, and the session it came from
// never reaches here. What the entrance does with what arrives under that
// name is hold it to the one shape a derived key has, so that no report
// carries a field a person could be read out of.
func TestTheEntranceForwardsTheKeyAsReceived(t *testing.T) {
	for _, c := range []struct {
		name, sent, want string
	}{
		{"a derived key", derived, derived},
		{"no key at all", "", ""},
		{"a name", "ada@example.com", ""},
		{"an address", "203.0.113.7", ""},
		{"a key of the right length that is not one", strings.Repeat("z", 64), ""},
		{"a key one digit short", derived[:63], ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			store := &fakeStore{result: wayin.Result{Accepted: true}}
			submit(t, wayin.NewEntrance(store), "token-1",
				`{"kind":"bug","text":"the save button does nothing","source_key":"`+c.sent+`","notice_id":""}`)

			if len(store.submitted) != 1 {
				t.Fatalf("the store took %d submissions, want one", len(store.submitted))
			}
			if store.submitted[0].SourceKey != c.want {
				t.Errorf("%s reached the store as %q, want %q",
					c.name, store.submitted[0].SourceKey, c.want)
			}
		})
	}
}

// TestASubmissionNamingNoSessionIsRefused: a submission whose notice_id key
// is absent altogether — never opened, or the shipped source's own bug —
// names no session at all, and is refused at the entrance itself, before the
// store is ever reached to check it against anything.
func TestASubmissionNamingNoSessionIsRefused(t *testing.T) {
	store := &fakeStore{result: wayin.Result{Accepted: true}}

	code, body := submit(t, wayin.NewEntrance(store), "token-1",
		`{"kind":"bug","text":"the save button does nothing"}`)

	if code != http.StatusOK {
		t.Fatalf("a submission naming no session answered %d, want %d: %s", code, http.StatusOK, body)
	}
	var result struct {
		Accepted bool   `json:"accepted"`
		Refusal  string `json:"refusal"`
	}
	if err := json.NewDecoder(bytes.NewReader([]byte(body))).Decode(&result); err != nil {
		t.Fatalf("reading the submit result %q: %v", body, err)
	}
	if result.Accepted || result.Refusal != wayin.RefusedNoSession {
		t.Errorf("a submission naming no session rendered %+v, want %q", result, wayin.RefusedNoSession)
	}
	if len(store.noticeFor) != 0 || len(store.submitted) != 0 {
		t.Errorf("the store was reached %d times for the notice and %d times to submit, want neither",
			len(store.noticeFor), len(store.submitted))
	}
}

// TestASubmissionWhoseNoticeMovedIsShownAgain: a session naming a notice no
// longer the one in force is not refused — the fresh notice is rendered in
// its place, and the store is never reached to submit under a stale one.
func TestASubmissionWhoseNoticeMovedIsShownAgain(t *testing.T) {
	store := &fakeStore{notice: wayin.Notice{ID: "con_2", Text: "a second notice an owner authored"}}

	code, body := submit(t, wayin.NewEntrance(store), "token-1",
		`{"kind":"bug","text":"the save button does nothing","notice_id":"con_1"}`)

	if code != http.StatusOK {
		t.Fatalf("a submission under a stale notice answered %d, want %d: %s", code, http.StatusOK, body)
	}
	var result struct {
		Accepted bool   `json:"accepted"`
		Refusal  string `json:"refusal"`
		NoticeID string `json:"notice_id"`
		Text     string `json:"text"`
	}
	if err := json.NewDecoder(bytes.NewReader([]byte(body))).Decode(&result); err != nil {
		t.Fatalf("reading the submit result %q: %v", body, err)
	}
	if result.Accepted || result.Refusal != "" {
		t.Errorf("a submission under a stale notice rendered %+v, want neither accepted nor refused", result)
	}
	if result.NoticeID != "con_2" || result.Text != "a second notice an owner authored" {
		t.Errorf("the moved notice rendered %+v, want the one now in force", result)
	}
	if len(store.submitted) != 0 {
		t.Errorf("a submission under a stale notice reached the store to submit, and it never should")
	}
}

// TestTheSessionDigestsTheNoticeWhereItCarriesNoID: a notice with words but no
// id of its own is still given a session, and two such notices are given
// different ones — a digest of the words and not an empty string shared by
// both.
func TestTheSessionDigestsTheNoticeWhereItCarriesNoID(t *testing.T) {
	first := &fakeStore{notice: wayin.Notice{Text: "words but no id of its own"}}
	_, firstBody := notice(t, wayin.NewEntrance(first), "token-1")
	var firstShown struct {
		NoticeID string `json:"notice_id"`
	}
	if err := json.NewDecoder(bytes.NewReader([]byte(firstBody))).Decode(&firstShown); err != nil {
		t.Fatalf("reading the notice %q: %v", firstBody, err)
	}
	if firstShown.NoticeID == "" || len(firstShown.NoticeID) != 64 {
		t.Errorf("a notice with words and no id showed session %q, want a 64-character digest", firstShown.NoticeID)
	}

	second := &fakeStore{notice: wayin.Notice{Text: "different words and no id either"}}
	_, secondBody := notice(t, wayin.NewEntrance(second), "token-1")
	var secondShown struct {
		NoticeID string `json:"notice_id"`
	}
	if err := json.NewDecoder(bytes.NewReader([]byte(secondBody))).Decode(&secondShown); err != nil {
		t.Fatalf("reading the notice %q: %v", secondBody, err)
	}
	if secondShown.NoticeID == firstShown.NoticeID {
		t.Errorf("two notices of different words showed the same session %q", firstShown.NoticeID)
	}
}

// TestASubmissionMatchingADigestedIdentityReachesTheStore: a session shown a
// notice with words and no id names that digest back, the entrance confirms
// it against the notice again in force, and what is recorded on the
// submission is the notice's own id — empty, here, because this notice
// carries none — and never the digest the session was compared by.
func TestASubmissionMatchingADigestedIdentityReachesTheStore(t *testing.T) {
	store := &fakeStore{
		notice: wayin.Notice{Text: "words but no id of its own"},
		result: wayin.Result{Accepted: true},
	}
	entrance := wayin.NewEntrance(store)
	_, shownBody := notice(t, entrance, "token-1")
	var shown struct {
		NoticeID string `json:"notice_id"`
	}
	if err := json.NewDecoder(bytes.NewReader([]byte(shownBody))).Decode(&shown); err != nil {
		t.Fatalf("reading the notice %q: %v", shownBody, err)
	}

	code, body := submit(t, entrance, "token-1",
		`{"kind":"bug","text":"the save button does nothing","notice_id":"`+shown.NoticeID+`"}`)
	if code != http.StatusOK {
		t.Fatalf("the submission answered %d, want %d: %s", code, http.StatusOK, body)
	}
	if len(store.submitted) != 1 {
		t.Fatalf("the store took %d submissions, want one", len(store.submitted))
	}
	if store.submitted[0].NoticeID != "" {
		t.Errorf("the recorded notice id is %q, want the notice's own empty id and not the digest",
			store.submitted[0].NoticeID)
	}
}
