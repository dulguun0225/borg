// Package agent owns the two authoring roles — [SpecAuthor] and [Implementer] —
// the [Model] interface both roles call, and the two implementations of it,
// [OpenRouter] and [Anthropic]. The system prompts are exported constants,
// [SpecAuthorSystemPrompt] and [ImplementerSystemPrompt], and the four rules
// they both name are [Rules], one constant included in both so the two cannot
// drift apart.
//
// # The files
//
// model.go is [Model] — one call, a system prompt and a user message in, a
// [Reply] out — and [ErrReply]. rules.go is [Rules]. criterion.go is
// [Criterion] and the writer that puts the criteria in force into a prompt.
// specauthor.go is [SpecAuthorSystemPrompt], [SpecAuthor], the [Refining] it
// takes with its [QA] values, the [Refined] it returns, and the parse of the
// reply. implementer.go is [ImplementerSystemPrompt], [Implementer], the
// [Implementing] it takes, and the [Change] of [File] values it returns, with
// the parse of the reply beside it.
//
// openrouter.go is [OpenRouter], the chat completions request and response it
// is written against, [ErrUpstream] and [ErrRefused]. anthropic.go is
// [Anthropic], the messages request and response, [StatusError] and
// [ErrAnswer]. paced.go is [Paced] and [NewPaced].
//
// The tests are one file per file of code — anthropic_test.go,
// implementer_test.go, openrouter_test.go, paced_test.go, specauthor_test.go —
// and none of them reaches a database, this package writing no record.
//
// A role is a struct over a [Model], and a Model is one call: a system prompt,
// a user message, a [Reply]. [SpecAuthor.Refine] takes a [Refining] with the
// intent's statement and any [QA] and returns a [Refined];
// [Implementer.Implement] takes an [Implementing] and returns a [Change] of
// [File] values. Both roles are told the criteria in force for the service as
// [Criterion] values carrying the id and the sentence and nothing else — the
// implementer because the Implementation gate rejects a criterion with no
// encoding as well as an encoding naming a criterion not in force.
//
// Each implementation takes its model name from configuration and names its
// credential as a [secretref.Ref]: the value is resolved at the moment of the
// HTTP call, sent in a header, stored in no field of any struct here, and
// rendered in no error this package returns. [OpenRouter] posts to OpenRouter's
// chat completions endpoint with an OpenRouter API key; [Anthropic] posts to
// Anthropic's messages endpoint with the OAuth token `claude setup-token` mints
// against a Claude subscription, which is served claude-haiku-4-5 and refused
// every model above it. The caller selects one where the model is constructed
// and this package dispatches on nothing.
//
// [ErrUpstream] is a failure at the provider [OpenRouter] routed to, arriving
// as a 200, and [ErrRefused] is a model declining on policy grounds — its own
// error, because retrying it spends an attempt on a verdict already given.
// Neither is reachable through [Anthropic], whose endpoint reports both as a
// [StatusError]; an answer of neither shape is [ErrAnswer].
//
// [NewPaced] wraps a Model so two calls never start closer together than an
// interval. [Paced] is composed around an implementation by the caller rather
// than being a field on it, both implementations being values with nowhere to
// keep the time of the last call.
//
// Each role states its reply protocol in its system prompt and parses the reply
// strictly: a reply in neither of its forms is [ErrReply], an agent asserting a
// verdict is a parse failure, and text that reads as an instruction anywhere in
// a reply or an input is content nothing here acts on.
//
// [ImplementerSystemPrompt] carries, beside the four rules, the standing
// instruction that the program appends one line per unit of work to the file
// its environment names and exercises its own behaviour while it runs, and the
// file-name conventions a contract is derived from — a published interface is
// one exported struct type in a file named for it, an interface this service
// reads is a mirror in a file named for its producer, and the unit goes in a
// field's own name rather than in a tag.
//
// Who may write what: this package writes no record. The stage around a role
// reports its spend — [Reply.Tokens], input and output together — to dispatch,
// and submits what the role produced through the artifact store; both are that
// stage's calls, not this package's. There is no fleet record behind these
// roles: the model name is configuration and the scope is wherever the caller
// points the role.
//
// What defines it: a role — what an agent is put on, naming the work of one
// stage — is ../../end-goal/how-the-factory-works/01-one-pipeline.md. The
// Implementation gate rejecting in both directions is
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/05-implementation/README.md.
// The fleet behind the roles is
// ../../end-goal/how-the-factory-works/10-fleet/README.md.
package agent
