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

const keyValue = "sk-ant-oat01-nothing-else-may-see"

func newResolver(t *testing.T) *secretref.Resolver {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secrets")
	if err := os.WriteFile(path, []byte("model.anthropic="+keyValue+"\n"), 0o600); err != nil {
		t.Fatalf("writing the secrets file: %v", err)
	}
	resolver, err := secretref.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return resolver
}

func newAnthropic(t *testing.T, srv *httptest.Server) Anthropic {
	t.Helper()
	return Anthropic{
		ModelName:  "claude-test-model",
		Credential: secretref.MustNew("model.anthropic"),
		Resolver:   newResolver(t),
		Client:     srv.Client(),
		url:        srv.URL,
	}
}

// TestCompleteResolvesTheCredentialAtTheCall is the wire contract: the
// Authorization header carries the resolved value as a bearer token, the OAuth
// beta and version headers are present, no x-api-key header is sent, the body
// carries the model, the system prompt, and the user message, and a canned
// answer parses into a Reply whose Tokens is input plus output.
func TestCompleteResolvesTheCredentialAtTheCall(t *testing.T) {
	var gotAuth, gotBeta, gotAPIKey, gotVersion string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("authorization")
		gotBeta = r.Header.Get("anthropic-beta")
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("content-type", "application/json")
		fmt.Fprint(w, `{"content":[{"type":"text","text":"QUESTION: which port?"}],"usage":{"input_tokens":100,"output_tokens":7}}`)
	}))
	defer srv.Close()

	reply, err := newAnthropic(t, srv).Complete(context.Background(), "the system prompt", "the user message")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if reply.Text != "QUESTION: which port?" {
		t.Errorf("Text = %q, want the content block's text", reply.Text)
	}
	if reply.Tokens != 107 {
		t.Errorf("Tokens = %d, want input+output = 107", reply.Tokens)
	}
	if gotAuth != "Bearer "+keyValue {
		t.Errorf("authorization = %q, want the resolved value as a bearer token", gotAuth)
	}
	if gotBeta != oauthBeta {
		t.Errorf("anthropic-beta = %q, want %q", gotBeta, oauthBeta)
	}
	if gotAPIKey != "" {
		t.Errorf("x-api-key = %q, want no such header — the credential is a bearer token", gotAPIKey)
	}
	if gotVersion != "2023-06-01" {
		t.Errorf("anthropic-version = %q, want 2023-06-01", gotVersion)
	}

	var sent request
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("unmarshalling the sent body: %v", err)
	}
	if sent.Model != "claude-test-model" || sent.System != "the system prompt" || sent.MaxTokens != maxTokens {
		t.Errorf("sent %+v, want the configured model, the system prompt, and max_tokens %d", sent, maxTokens)
	}
	if len(sent.Messages) != 1 || sent.Messages[0].Role != "user" || sent.Messages[0].Content != "the user message" {
		t.Errorf("Messages = %+v, want one user message", sent.Messages)
	}
}

// TestCompleteReturnsStatusAndBodyAndNoKey is the error contract: a non-200
// answer is a StatusError carrying the status and the server's body, and no
// error path renders the key's value.
func TestCompleteReturnsStatusAndBodyAndNoKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"type":"rate_limit_error"}}`)
	}))
	defer srv.Close()

	_, err := newAnthropic(t, srv).Complete(context.Background(), "s", "u")
	var status *StatusError
	if !errors.As(err, &status) {
		t.Fatalf("Complete = %v, want a StatusError", err)
	}
	if status.Status != http.StatusTooManyRequests {
		t.Errorf("Status = %d, want 429", status.Status)
	}
	if !strings.Contains(status.Body, "rate_limit_error") {
		t.Errorf("Body = %q, want the server's body", status.Body)
	}
	if strings.Contains(err.Error(), keyValue) {
		t.Errorf("the error contains the key's value: %v", err)
	}
}

// TestCompleteReadsPastABlockThatIsNotText is the answer a model with thinking
// on sends: a thinking block, which has no text field, before the text. Reading
// the first block alone returned an empty reply, and an empty reply is refused
// by both roles' parses — so the whole path failed against a real model while
// the end-to-end test's fake model passed.
func TestCompleteReadsPastABlockThatIsNotText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"content":[{"type":"thinking","thinking":"","signature":"abc"},`+
			`{"type":"text","text":"=== FILE main.go ==="},{"type":"text","text":"\npackage main"}],`+
			`"usage":{"input_tokens":2,"output_tokens":3}}`)
	}))
	defer srv.Close()

	reply, err := newAnthropic(t, srv).Complete(context.Background(), "s", "u")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if reply.Text != "=== FILE main.go ===\npackage main" {
		t.Errorf("Text = %q, want the text blocks joined and the thinking block skipped", reply.Text)
	}
}

// TestCompleteRefusesAnUnreadableAnswer covers the 200 that is not the
// documented shape: not JSON, no content block at all, and blocks of which
// none is text.
func TestCompleteRefusesAnUnreadableAnswer(t *testing.T) {
	for name, body := range map[string]string{
		"not json":         "an html error page",
		"no content block": `{"content":[],"usage":{"input_tokens":1,"output_tokens":1}}`,
		"no text block":    `{"content":[{"type":"thinking","thinking":""}],"usage":{"input_tokens":1,"output_tokens":1}}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, body)
			}))
			defer srv.Close()
			_, err := newAnthropic(t, srv).Complete(context.Background(), "s", "u")
			if !errors.Is(err, ErrAnswer) {
				t.Fatalf("Complete = %v, want ErrAnswer", err)
			}
			if strings.Contains(err.Error(), keyValue) {
				t.Errorf("the error contains the key's value: %v", err)
			}
		})
	}
}

// TestAnthropicHasNoFieldForTheValue fails if a field is ever added that a
// resolved value could be stored in between calls: the struct is the
// configuration and the test URL, nothing else.
func TestAnthropicHasNoFieldForTheValue(t *testing.T) {
	var a Anthropic
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
	}(a)
}
