package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/dulguun0225/borg/factory/secretref"
)

// messagesURL is the one endpoint this implementation calls.
const messagesURL = "https://api.anthropic.com/v1/messages"

// anthropicVersion is the API version header the endpoint requires on every
// request.
const anthropicVersion = "2023-06-01"

// maxTokens caps one reply. 8192 holds a spec or a small repository's worth
// of files, which is what an M1 reply is.
const maxTokens = 8192

// Anthropic is a [Model] answered by the Anthropic messages API. The model
// name comes from configuration, and the credential is a name: the value is
// resolved inside [Anthropic.Complete], sent in a header, and stored in no
// field — this struct has nowhere to put it.
type Anthropic struct {
	// ModelName is the provider's model id, named in configuration.
	ModelName string
	// Key names the API credential. Resolver answers it.
	Key secretref.Ref
	// Resolver is the one place the key's value is read.
	Resolver *secretref.Resolver
	// Client is optional; nil means http.DefaultClient.
	Client *http.Client

	// url replaces messagesURL when it is not empty. Only a test sets it,
	// which is why it is not exported.
	url string
}

var _ Model = Anthropic{}

// StatusError is an API answer with a status other than 200. It carries the
// status and the response body: the body may quote the request back, and the
// request's body never contains the key — the key is sent in a header.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("agent: the model API answered %d: %s", e.Status, e.Body)
}

// ErrAnswer is returned for a 200 answer this package cannot read a reply out
// of: a body that is not the documented JSON, or one with no content block.
var ErrAnswer = errors.New("agent: the model API's answer is unreadable")

// request and message are the documented wire shape of a call, spelled out
// here because everything a static reader needs is in the text.
type request struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system"`
	Messages  []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// response is the part of the documented answer this package reads: the first
// content block's text and the token usage.
type response struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
}

// Complete sends one user message under one system prompt and returns the
// reply's text and its token spend, input and output together. No error it
// returns contains the key's value: the resolver's errors carry no value by
// that package's own rule, a transport error names the method and the URL and
// no header, and a [StatusError] carries what the server sent back.
func (a Anthropic) Complete(ctx context.Context, system, user string) (Reply, error) {
	// The value exists from here to the header write and in nothing a caller
	// can reach afterwards.
	key, err := a.Resolver.Resolve(a.Key)
	if err != nil {
		return Reply{}, fmt.Errorf("agent: resolving the model credential: %w", err)
	}

	body, err := json.Marshal(request{
		Model:     a.ModelName,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  []message{{Role: "user", Content: user}},
	})
	if err != nil {
		return Reply{}, fmt.Errorf("agent: encoding the request: %w", err)
	}

	url := a.url
	if url == "" {
		url = messagesURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Reply{}, fmt.Errorf("agent: building the request: %w", err)
	}
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", anthropicVersion)
	req.Header.Set("content-type", "application/json")

	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return Reply{}, fmt.Errorf("agent: calling the model API: %w", err)
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(resp.Body)
	if err != nil {
		return Reply{}, fmt.Errorf("agent: reading the answer: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Reply{}, &StatusError{Status: resp.StatusCode, Body: string(answer)}
	}

	var parsed response
	if err := json.Unmarshal(answer, &parsed); err != nil {
		return Reply{}, fmt.Errorf("%w: %v", ErrAnswer, err)
	}
	if len(parsed.Content) == 0 {
		return Reply{}, fmt.Errorf("%w: it has no content block", ErrAnswer)
	}
	return Reply{
		Text:   parsed.Content[0].Text,
		Tokens: parsed.Usage.InputTokens + parsed.Usage.OutputTokens,
	}, nil
}
