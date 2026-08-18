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

const keyValue = "sk-the-key-nothing-else-may-see"

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
		ModelName: "claude-test-model",
		Key:       secretref.MustNew("model.anthropic"),
		Resolver:  newResolver(t),
		Client:    srv.Client(),
		url:       srv.URL,
	}
}

// TestCompleteResolvesTheKeyAtTheCall is the wire contract: the key header
// carries the resolved value, the version header is present, the body carries
// the model, the system prompt, and the user message, and a canned answer
// parses into a Reply whose Tokens is input plus output.
func TestCompleteResolvesTheKeyAtTheCall(t *testing.T) {
	var gotKey, gotVersion string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
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
	if gotKey != keyValue {
		t.Errorf("x-api-key = %q, want the resolved value", gotKey)
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

// TestCompleteRefusesAnUnreadableAnswer covers the 200 that is not the
// documented shape: not JSON, and JSON with no content block.
func TestCompleteRefusesAnUnreadableAnswer(t *testing.T) {
	for name, body := range map[string]string{
		"not json":         "an html error page",
		"no content block": `{"content":[],"usage":{"input_tokens":1,"output_tokens":1}}`,
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
		ModelName string
		Key       secretref.Ref
		Resolver  *secretref.Resolver
		Client    *http.Client
		url       string
	} = struct {
		ModelName string
		Key       secretref.Ref
		Resolver  *secretref.Resolver
		Client    *http.Client
		url       string
	}(a)
}
