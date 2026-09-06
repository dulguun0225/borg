package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/secretref"
)

// chatCompletionsURL is the one endpoint this implementation calls.
const chatCompletionsURL = "https://openrouter.ai/api/v1/chat/completions"

// OpenRouter is a [Model] answered by OpenRouter's chat completions endpoint,
// authenticated with an OpenRouter API key. It is a second implementation
// beside [Anthropic] rather than a variant of it, because the two wire shapes
// differ where it matters: this endpoint carries the system prompt as the first
// message of the list, and Anthropic's carries it in a field of its own. The
// model name is the provider's namespaced id — `deepseek/deepseek-v4-flash`,
// `anthropic/claude-opus-4.8` — and it comes from configuration, as
// [Anthropic]'s does. An id prefixed `~` is this provider's floating alias for
// a family's newest member, and naming one is not what this factory does: the
// id is the author every artifact version records, and a per-author prior is
// kept per model version, so an id whose meaning changes underneath makes two
// versions recorded under one author that two models wrote. The credential is a name: the value is resolved inside
// [OpenRouter.Complete], sent in a header, and stored in no field.
//
// What a second provider costs: one more party between a role and the model, so
// a take's failure has one more place to come from — a status this package
// reads as the provider's may be the model's upstream, and [ErrUpstream] is
// that failure arriving as a 200.
type OpenRouter struct {
	// ModelName is the provider's namespaced model id, named in configuration.
	ModelName string
	// Credential names the API key the call authenticates with. Resolver
	// answers it.
	Credential secretref.Ref
	// Resolver is the one place the credential's value is read.
	Resolver *secretref.Resolver
	// Client is optional; nil means http.DefaultClient.
	Client *http.Client

	// url replaces chatCompletionsURL when it is not empty. Only a test sets
	// it, which is why it is not exported.
	url string
}

var _ Model = OpenRouter{}

// ErrUpstream is a 200 answer carrying an error object instead of a choice.
// This endpoint routes to a provider of its own, and a failure at that provider
// arrives here as a successful HTTP request whose body says what went wrong —
// a rate limit, a model that is not serving, a request the upstream refused. It
// is its own error rather than a [StatusError] because there is no status other
// than 200 to carry, and its own error rather than [ErrAnswer] because the body
// is exactly the documented shape: the answer is readable and says no.
var ErrUpstream = errors.New("agent: the model API answered 200 carrying an error")

// ErrRefused is a 200 answer whose choice carries a refusal instead of a reply.
// A model may decline a request on its provider's policy grounds, and this
// endpoint reports that as a finish reason of content_filter with the model's
// own sentence in a refusal field. It is its own error and not [ErrAnswer]
// because a refusal is not a malformed answer: nothing about the request's
// shape is wrong, the same request will be refused again, and a stage that
// retried it would spend its attempt limit on a verdict that has already been
// given.
//
// Measured on 2026-08-20 through this endpoint: anthropic/claude-opus-5 is
// served the spec stage and refuses the implementer's role prompt four times out of
// four under the cyber category, for a role prompt asking for a health-check HTTP
// handler — its own reasoning showing it part-way through writing that handler
// when the classifier stopped it. anthropic/claude-opus-4.8 and
// anthropic/claude-sonnet-5 author the same role prompt, as does
// deepseek/deepseek-v4-flash, which is what the runbook names. So a refusal
// here is as
// likely to be a false positive as a request worth refusing, which of the two
// it was is not something this package can tell, and the model named in
// configuration is what decides whether a take reaches an implementation at
// all.
var ErrRefused = errors.New("agent: the model refused the request")

// chatRequest, chatMessage and chatResponse are the documented wire shape of a
// call, spelled out here because everything a static reader needs is in the
// text. They are named apart from [Anthropic]'s request, message and response
// for the reason two files in one package cannot share a type name, and the
// duplication is what locality costs.
type chatRequest struct {
	Model     string        `json:"model"`
	MaxTokens int           `json:"max_tokens"`
	Messages  []chatMessage `json:"messages"`
	// Reasoning is this endpoint's field for the effort a fleet entry names,
	// and is left out where the entry names none. What the endpoint does with a
	// value it does not offer is its own answer, which is where an entry asking
	// for an effort nobody offers fails.
	Reasoning *chatReasoning `json:"reasoning,omitempty"`
}

// chatReasoning is the effort as this endpoint takes it.
type chatReasoning struct {
	Effort string `json:"effort"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatResponse is the part of the documented answer this package reads: the
// first choice's text, the token usage, and the error object a 200 may carry
// instead of a choice. Only the first choice is read because the request asks
// for one completion and this endpoint returns one choice for it.
type chatResponse struct {
	Choices []struct {
		// FinishReason is why generation stopped. It is read because an
		// empty content means something different under each value, and a
		// reader given "no content" learns none of it.
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content string `json:"content"`
			// Refusal is the model's own sentence when it declined. A
			// refusal may arrive with this field set, or with only the
			// finish reason saying so.
			Refusal string `json:"refusal"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
		// PromptTokensDetails carries the part of the prompt the provider
		// served from its own cache, which it prices apart from the rest.
		PromptTokensDetails struct {
			CachedTokens int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Complete sends one user message under one system prompt and returns the
// reply's text and the units the provider returned per kind. No error it
// returns contains the credential's value: the resolver's errors carry no value
// by that package's own rule, a transport error names the method and the URL and
// no header, and a [StatusError] carries what the server sent back.
func (o OpenRouter) Complete(ctx context.Context, p principal.Principal, call Call) (Reply, error) {
	// The value exists from here to the header write and in nothing a caller
	// can reach afterwards. The principal recorded beside the credential's name
	// is the caller's, for the reason [Anthropic.Complete] states.
	credential, err := o.Resolver.Resolve(p, o.Credential)
	if err != nil {
		return Reply{}, fmt.Errorf("agent: resolving the model credential: %w", err)
	}

	// The system prompt is the first message and not a field, which is this
	// endpoint's shape rather than a choice made here.
	request := chatRequest{
		Model:     o.ModelName,
		MaxTokens: maxTokens,
		Messages: []chatMessage{
			{Role: "system", Content: call.System},
			{Role: "user", Content: call.User},
		},
	}
	if call.Effort != "" {
		request.Reasoning = &chatReasoning{Effort: call.Effort}
	}
	body, err := json.Marshal(request)
	if err != nil {
		return Reply{}, fmt.Errorf("agent: encoding the request: %w", err)
	}

	url := o.url
	if url == "" {
		url = chatCompletionsURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Reply{}, fmt.Errorf("agent: building the request: %w", err)
	}
	// Two headers and no more. The endpoint documents HTTP-Referer and X-Title
	// as optional attribution on a public listing of what calls it, which is
	// not something a factory install has a value for, so neither is sent.
	req.Header.Set("authorization", "Bearer "+credential)
	req.Header.Set("content-type", "application/json")

	client := o.Client
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

	var parsed chatResponse
	if err := json.Unmarshal(answer, &parsed); err != nil {
		return Reply{}, fmt.Errorf("%w: %v", ErrAnswer, err)
	}
	// Read before the choices, because this is the answer that has none: an
	// upstream failure the endpoint reports at 200. Reading the choices first
	// would report it as an unreadable answer and lose what the body said.
	if parsed.Error.Message != "" {
		return Reply{}, fmt.Errorf("%w: %d %s", ErrUpstream, parsed.Error.Code, parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return Reply{}, fmt.Errorf("%w: it has no choice", ErrAnswer)
	}
	choice := parsed.Choices[0]
	if choice.Message.Refusal != "" {
		return Reply{}, fmt.Errorf("%w: %s", ErrRefused, choice.Message.Refusal)
	}
	// An empty reply is refused by both roles' parses, so it is refused here,
	// where why the answer is empty can still be named. Each reason is a
	// different thing for a reader to do about it: a refusal is not retried, a
	// cap is raised, and anything else is the answer not being what this
	// package reads.
	text := choice.Message.Content
	if text == "" {
		switch choice.FinishReason {
		case "content_filter":
			return Reply{}, fmt.Errorf("%w: it carried no reason", ErrRefused)
		case "length":
			return Reply{}, fmt.Errorf("%w: it stopped at the %d-token cap having written no content, which is a model that spent the cap on reasoning", ErrAnswer, maxTokens)
		default:
			return Reply{}, fmt.Errorf("%w: its first choice has no content and finished as %q", ErrAnswer, choice.FinishReason)
		}
	}
	// The cached part of the prompt is reported inside the prompt count, so it
	// is subtracted out here: the two kinds are priced apart and a kind counted
	// twice would be a spend the provider never charged.
	units := map[string]int64{
		UnitsInput:  parsed.Usage.PromptTokens - parsed.Usage.PromptTokensDetails.CachedTokens,
		UnitsOutput: parsed.Usage.CompletionTokens,
	}
	if cached := parsed.Usage.PromptTokensDetails.CachedTokens; cached > 0 {
		units[UnitsCachedInput] = cached
	}
	// A reply cut at the cap with content is outside every role's protocol —
	// what follows the cut is missing — and it is refused here so the reason
	// named is the cap and not the marker the cut removed. The spend goes back
	// with the error, the way a refused reply's does.
	if choice.FinishReason == "length" {
		return Reply{Units: units}, fmt.Errorf("%w: the reply stopped at the %d-token cap mid-reply, so what follows the cut is missing", ErrReply, maxTokens)
	}
	return Reply{Text: text, Units: units}, nil
}
