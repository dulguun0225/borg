package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/secretref"
)

// messagesURL is the one endpoint this implementation calls.
const messagesURL = "https://api.anthropic.com/v1/messages"

// anthropicVersion is the API version header the endpoint requires on every
// request.
const anthropicVersion = "2023-06-01"

// oauthBeta is the beta header an OAuth credential requires. The credential
// this package sends is the long-lived token `claude setup-token` mints against
// a Claude subscription, which the endpoint reads from the Authorization header
// and only under this beta — an API key's x-api-key header is a different
// scheme and this package does not send it. What one scheme costs: an install
// holding an API key rather than a subscription token cannot call the model
// until the header pair here is authored per credential, which is the fleet's
// job at M7 and not configuration M1 has anywhere to put.
const oauthBeta = "oauth-2025-04-20"

// maxTokens caps one reply. 8192 holds a spec or a small repository's worth
// of files, which is what an M1 reply is.
const maxTokens = 8192

// Anthropic is a [Model] answered by the Anthropic messages API, authenticated
// with a Claude subscription's long-lived OAuth token. The model name comes
// from configuration, and the credential is a name: the value is resolved
// inside [Anthropic.Complete], sent in a header, and stored in no field — this
// struct has nowhere to put it.
type Anthropic struct {
	// ModelName is the provider's model id, named in configuration.
	ModelName string
	// Credential names the subscription token the call authenticates with.
	// Resolver answers it.
	Credential secretref.Ref
	// Resolver is the one place the credential's value is read.
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
// request's body never contains the credential — it is sent in a header.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("agent: the model API answered %d: %s", e.Status, e.Body)
}

// ErrAnswer is returned for a 200 answer this package cannot read a reply out
// of: a body that is not the documented JSON, or one with no text block.
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

// response is the part of the documented answer this package reads: the text
// blocks and the token usage. A block's type is read because an answer carries
// kinds of block that are not text — a model with thinking on emits a thinking
// block, which has no text field at all — and only the text is a reply.
type response struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
}

// Complete sends one user message under one system prompt and returns the
// reply's text and its token spend, input and output together. No error it
// returns contains the credential's value: the resolver's errors carry no value
// by that package's own rule, a transport error names the method and the URL and
// no header, and a [StatusError] carries what the server sent back.
func (a Anthropic) Complete(ctx context.Context, system, user string) (Reply, error) {
	// The value exists from here to the header write and in nothing a caller
	// can reach afterwards.
	//
	// The principal recorded is this component's own: nothing yet carries the
	// dispatch's principal down to a [Model], so the resolver sees the client
	// asking for the credential and not who the running stage is.
	credential, err := a.Resolver.Resolve(principal.OfComponent("agent"), a.Credential)
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
	req.Header.Set("authorization", "Bearer "+credential)
	req.Header.Set("anthropic-beta", oauthBeta)
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
	// Every text block, joined in the order the answer carries them, and no
	// other kind of block. Reading the first block alone was what this did
	// until a model with thinking on put a thinking block in front of the
	// text: the reply came back empty and the role's parse refused it, which
	// is a defect the fake model in the end-to-end test cannot show. What
	// joining costs is that an answer split across two text blocks arrives
	// here as one string, which is what both roles parse anyway.
	var text strings.Builder
	for _, block := range parsed.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	if text.Len() == 0 {
		return Reply{}, fmt.Errorf("%w: it has no text block", ErrAnswer)
	}
	return Reply{
		Text:   text.String(),
		Tokens: parsed.Usage.InputTokens + parsed.Usage.OutputTokens,
	}, nil
}
