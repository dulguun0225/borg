package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/secretref"
)

const openRouterKeyValue = "sk-or-v1-nothing-else-may-see"

// newOpenRouterResolver writes a secrets file holding this provider's
// credential under the name the run resolves it by. It repeats the Anthropic
// helper rather than sharing one taking a name, which is the repetition
// locality is paid for in.
func newOpenRouterResolver(t *testing.T) *secretref.Resolver {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secrets")
	if err := os.WriteFile(path, []byte("model.openrouter="+openRouterKeyValue+"\n"), 0o600); err != nil {
		t.Fatalf("writing the secrets file: %v", err)
	}
	resolver, err := secretref.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return resolver
}

func newOpenRouter(t *testing.T, srv *httptest.Server) OpenRouter {
	t.Helper()
	return OpenRouter{
		ModelName:  "vendor/test-model",
		Credential: secretref.MustNew("model.openrouter"),
		Resolver:   newOpenRouterResolver(t),
		Client:     srv.Client(),
		url:        srv.URL,
	}
}

// TestOpenRouterCompleteResolvesTheCredentialAtTheCall is the wire contract:
// the Authorization header carries the resolved value as a bearer token, no
// x-api-key and neither Anthropic header is sent, the body carries the model,
// max_tokens, and the system prompt as the first of two messages, and a canned
// answer parses into a Reply whose Units carry the input and output counts
// apart, which is what the agent run record stores.
func TestOpenRouterCompleteResolvesTheCredentialAtTheCall(t *testing.T) {
	var gotAuth, gotAPIKey, gotBeta, gotVersion string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("authorization")
		gotAPIKey = r.Header.Get("x-api-key")
		gotBeta = r.Header.Get("anthropic-beta")
		gotVersion = r.Header.Get("anthropic-version")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("content-type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"QUESTION: which port?"}}],`+
			`"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107}}`)
	}))
	defer srv.Close()

	reply, err := newOpenRouter(t, srv).Complete(context.Background(), as(), Call{System: "the system prompt", User: "the user message"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if reply.Text != "QUESTION: which port?" {
		t.Errorf("Text = %q, want the first choice's content", reply.Text)
	}
	if reply.Units[UnitsInput] != 100 || reply.Units[UnitsOutput] != 7 {
		t.Errorf("Units = %v, want input 100 and output 7 counted apart", reply.Units)
	}
	if gotAuth != "Bearer "+openRouterKeyValue {
		t.Errorf("authorization = %q, want the resolved value as a bearer token", gotAuth)
	}
	if gotAPIKey != "" {
		t.Errorf("x-api-key = %q, want no such header — the credential is a bearer token", gotAPIKey)
	}
	if gotBeta != "" || gotVersion != "" {
		t.Errorf("anthropic-beta = %q and anthropic-version = %q, want neither — this is not that endpoint", gotBeta, gotVersion)
	}

	var sent chatRequest
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("unmarshalling the sent body: %v", err)
	}
	if sent.Model != "vendor/test-model" || sent.MaxTokens != maxTokens {
		t.Errorf("sent %+v, want the configured model and max_tokens %d", sent, maxTokens)
	}
	want := []chatMessage{
		{Role: "system", Content: "the system prompt"},
		{Role: "user", Content: "the user message"},
	}
	if len(sent.Messages) != 2 || sent.Messages[0] != want[0] || sent.Messages[1] != want[1] {
		t.Errorf("Messages = %+v, want the system prompt first and the user message second", sent.Messages)
	}
}

// TestOpenRouterSendsTheEffortTheEntryNames: the effort a fleet entry names is
// asked of the provider in the field this endpoint has for it, and a call that
// names none sends none — the factory does not check that the provider offers
// what the entry asked for, so an effort nobody offers fails at the provider's
// own answer.
func TestOpenRouterSendsTheEffortTheEntryNames(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("content-type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}],`+
			`"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	}))
	defer srv.Close()

	if _, err := newOpenRouter(t, srv).Complete(context.Background(), as(),
		Call{System: "s", User: "u", Effort: "high"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	var sent chatRequest
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("unmarshalling the sent body: %v", err)
	}
	if sent.Reasoning == nil || sent.Reasoning.Effort != "high" {
		t.Errorf("the request carries %+v, want the effort the entry named", sent.Reasoning)
	}

	if _, err := newOpenRouter(t, srv).Complete(context.Background(), as(), Call{System: "s", User: "u"}); err != nil {
		t.Fatalf("Complete with no effort: %v", err)
	}
	if strings.Contains(string(gotBody), "reasoning") {
		t.Errorf("the request carries %s, and an entry naming no effort asks for none", gotBody)
	}
}

// TestOpenRouterCompleteReturnsStatusAndBodyAndNoKey is the error contract: a
// non-200 answer is a StatusError carrying the status and the server's body,
// and no error path renders the key's value.
func TestOpenRouterCompleteReturnsStatusAndBodyAndNoKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"code":401,"message":"No auth credentials found"}}`)
	}))
	defer srv.Close()

	_, err := newOpenRouter(t, srv).Complete(context.Background(), as(), Call{System: "s", User: "u"})
	var status *StatusError
	if !errors.As(err, &status) {
		t.Fatalf("Complete = %v, want a StatusError", err)
	}
	if status.Status != http.StatusUnauthorized {
		t.Errorf("Status = %d, want 401", status.Status)
	}
	if !strings.Contains(status.Body, "No auth credentials found") {
		t.Errorf("Body = %q, want the server's body", status.Body)
	}
	if strings.Contains(err.Error(), openRouterKeyValue) {
		t.Errorf("the error contains the key's value: %v", err)
	}
}

// TestOpenRouterRefusesAnErrorCarriedAtTwoHundred is the failure this endpoint
// has and Anthropic's does not: the upstream provider refused and the endpoint
// reports it at 200 with an error object and no choice. It is ErrUpstream and
// not ErrAnswer, and the message the body carried is in the error so a reader
// learns which provider said what.
func TestOpenRouterRefusesAnErrorCarriedAtTwoHundred(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"error":{"code":429,"message":"upstream rate-limited"}}`)
	}))
	defer srv.Close()

	_, err := newOpenRouter(t, srv).Complete(context.Background(), as(), Call{System: "s", User: "u"})
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("Complete = %v, want ErrUpstream", err)
	}
	if !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "upstream rate-limited") {
		t.Errorf("error = %v, want the code and the message the body carried", err)
	}
	if strings.Contains(err.Error(), openRouterKeyValue) {
		t.Errorf("the error contains the key's value: %v", err)
	}
}

// TestOpenRouterNamesARefusalAsOne is the answer a model gives when it declines
// on policy grounds: a 200 whose choice carries a refusal and no content. It is
// ErrRefused and not ErrAnswer, because a stage that read it as a malformed
// answer would retry a request that will be refused again and spend its attempt
// limit doing it.
func TestOpenRouterNamesARefusalAsOne(t *testing.T) {
	for name, body := range map[string]string{
		"a refusal field": `{"choices":[{"finish_reason":"content_filter","message":{"content":"",` +
			`"refusal":"This request triggered restrictions on violative cyber content."}}],` +
			`"usage":{"prompt_tokens":98,"completion_tokens":74}}`,
		"the finish reason alone": `{"choices":[{"finish_reason":"content_filter","message":{"content":""}}],` +
			`"usage":{"prompt_tokens":98,"completion_tokens":74}}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, body)
			}))
			defer srv.Close()
			_, err := newOpenRouter(t, srv).Complete(context.Background(), as(), Call{System: "s", User: "u"})
			if !errors.Is(err, ErrRefused) {
				t.Fatalf("Complete = %v, want ErrRefused", err)
			}
			if errors.Is(err, ErrAnswer) {
				t.Error("a refusal must not also read as an unreadable answer")
			}
			if strings.Contains(err.Error(), openRouterKeyValue) {
				t.Errorf("the error contains the key's value: %v", err)
			}
		})
	}
}

// TestOpenRouterNamesTheTokenCap is the empty content that is neither a refusal
// nor a malformed answer: a model that spent the whole cap on reasoning. The
// error names the cap, because raising it is what a reader does about it.
func TestOpenRouterNamesTheTokenCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"finish_reason":"length","message":{"content":""}}],`+
			`"usage":{"prompt_tokens":10,"completion_tokens":8192}}`)
	}))
	defer srv.Close()
	_, err := newOpenRouter(t, srv).Complete(context.Background(), as(), Call{System: "s", User: "u"})
	if !errors.Is(err, ErrAnswer) {
		t.Fatalf("Complete = %v, want ErrAnswer", err)
	}
	if !strings.Contains(err.Error(), "cap") {
		t.Errorf("error = %v, want the cap named", err)
	}
}

// TestOpenRouterRefusesAReplyCutAtTheCap: a reply with content that finished
// as length is missing whatever followed the cut, so it is refused as outside
// the protocol with the cap named, and its spend comes back with the error.
func TestOpenRouterRefusesAReplyCutAtTheCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"finish_reason":"length","message":{"content":"=== main.go ===\npackage main"}}],`+
			`"usage":{"prompt_tokens":10,"completion_tokens":8192}}`)
	}))
	defer srv.Close()
	reply, err := newOpenRouter(t, srv).Complete(context.Background(), as(), Call{System: "s", User: "u"})
	if !errors.Is(err, ErrReply) {
		t.Fatalf("Complete = %v, want ErrReply", err)
	}
	if !strings.Contains(err.Error(), "cap") {
		t.Errorf("error = %v, want the cap named", err)
	}
	if reply.Units[UnitsOutput] != 8192 {
		t.Errorf("the refused reply's spend is %v, want the units the provider reported", reply.Units)
	}
}

// TestOpenRouterRefusesAnUnreadableAnswer covers the 200 that is not the
// documented shape: not JSON, no choice at all, and a first choice whose
// content is empty — which is what a model that put its whole reply in a
// reasoning field of its own sends, and which both roles' parses refuse.
func TestOpenRouterRefusesAnUnreadableAnswer(t *testing.T) {
	for name, body := range map[string]string{
		"not json":   "an html error page",
		"no choice":  `{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
		"no content": `{"choices":[{"finish_reason":"stop","message":{"content":""}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
		"no content field": `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","reasoning":"thought about it"}}],` +
			`"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, body)
			}))
			defer srv.Close()
			_, err := newOpenRouter(t, srv).Complete(context.Background(), as(), Call{System: "s", User: "u"})
			if !errors.Is(err, ErrAnswer) {
				t.Fatalf("Complete = %v, want ErrAnswer", err)
			}
			if strings.Contains(err.Error(), openRouterKeyValue) {
				t.Errorf("the error contains the key's value: %v", err)
			}
		})
	}
}

// TestOpenRouterHasNoFieldForTheValue fails if a field is ever added that a
// resolved value could be stored in between calls: the struct is the
// configuration and the test URL, nothing else.
func TestOpenRouterHasNoFieldForTheValue(t *testing.T) {
	var o OpenRouter
	var _ struct {
		ModelName  string
		Credential secretref.Ref
		Resolver   *secretref.Resolver
		Client     *http.Client
		url        string
	} = struct {
		ModelName  string
		Credential secretref.Ref
		Resolver   *secretref.Resolver
		Client     *http.Client
		url        string
	}(o)
}
