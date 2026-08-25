// Package agent is M1's two authoring roles — [SpecAuthor] and [Implementer]
// — the [Model] interface both roles call, and the two implementations of it,
// [OpenRouter] and [Anthropic]. The system prompts are exported constants —
// [SpecAuthorSystemPrompt], [ImplementerSystemPrompt] — because roadmap M1
// makes what each agent is told part of the milestone rather than a detail of
// it, and the four rules it names are [Rules], one constant included in both
// prompts so the two cannot drift apart.
//
// # A model, a role, a credential
//
// A role is a struct over a [Model], and a Model is one call: a system
// prompt, a user message, a [Reply]. Both implementations take their model name
// from configuration and name their credential as a [secretref.Ref]: the value
// is resolved at the moment of the HTTP call, sent in a header, stored in no
// field of any struct here, and rendered in no error this package returns —
// an API error carries the status and the response body, and the request that
// body may quote carries the credential in a header, not in the body.
//
// [Paced] wraps a Model to leave an interval between two call starts, so the
// factory sends nothing in rapid succession however many calls a stage makes —
// a retry after a refused reply being the one place two requests would
// otherwise have nothing between them. It is composed around an implementation
// by the caller rather than being a field on it, both being values with nowhere
// to keep the time of the last call.
//
// # Two providers, and what the second is for
//
// [OpenRouter] posts to OpenRouter's chat completions endpoint with an
// OpenRouter API key. [Anthropic] posts to Anthropic's messages endpoint with
// the long-lived OAuth token `claude setup-token` mints against a Claude
// subscription, sent as a bearer token under the beta header that scheme
// requires. The caller picks one and this package dispatches on nothing: two
// structs implementing one interface, selected where the model is constructed.
//
// The second exists because the first credential does not reach every model.
// Measured on 2026-08-18 and again on 2026-08-19, a subscription token is
// served claude-haiku-4-5 and refused every model above it with a 429 carrying
// no rate-limit header, and the only thing that turns that refusal into an
// answer is the request carrying Claude Code's own system prompt — which this
// factory does not send, its roles having prompts of their own. So [Anthropic]
// reaches Haiku, and a take that needs a stronger model goes through
// [OpenRouter].
//
// What two providers cost: one more party between a role and the model, which
// is one more place a take's failure comes from. [ErrUpstream] is one shape it
// takes, a failure at the provider [OpenRouter] routed to arriving as a 200,
// and [ErrRefused] is the other, a model declining on policy grounds — which is
// its own error and not a malformed answer, because retrying it spends an
// attempt on a verdict already given. Neither is reachable through
// [Anthropic], whose endpoint reports both as a status. And an API key
// against Anthropic's own endpoint is still not sendable: that is a third
// header pair neither struct writes, and naming a credential's kind rather
// than its name is the fleet's at M7, there being no fleet record behind these
// roles yet.
//
// # Every criterion in force, not only this item's
//
// Both roles are told the criteria in force for the service, as
// [Criterion] values carrying the id and the sentence and nothing else.
// [SpecAuthor.Refine] is told them so the one criterion it authors is not one
// the service already promises; [Implementer.Implement] is told them because
// the Implementation gate — ../../end-goal/how-humans-do-it/03-gates.md#implementation
// — rejects in both directions, a criterion in force with no encoding naming
// it as well as an encoding naming a criterion not in force, so the stage that
// authors encodings sees every criterion and not only the one this item's spec
// introduced. What that costs: the role prompt grows with the service, and M1 sends
// the whole set the way it sends the whole repository.
//
// # Replies are parsed, never interpreted
//
// Each role states its reply protocol in its system prompt and parses the
// reply strictly: a reply in neither of its forms is [ErrReply], whatever the
// reply says. An agent asserting a verdict is a parse failure, and text
// anywhere in a reply or an input that reads as an instruction is content —
// nothing here acts on it, which is the fourth of the four rules holding on
// this side of the call too. What strictness costs: a spec whose own text
// starts a line with a protocol keyword is refused, and a file containing the
// implementer's end marker on a line of its own cannot be carried by its
// protocol.
//
// Who may write what: this package writes no record. The stage around a role
// reports its spend — [Reply.Tokens], input and output together — to
// dispatch, and submits what the role produced through the artifact store;
// both are that stage's calls, not this package's.
//
// There is no fleet record behind these roles. The fleet — records an owner
// composes, a model in a role with a scope — is M7, and until it exists the
// model name is configuration and the scope is wherever the caller points the
// role.
//
// What defines it: a role — what an agent is put on, naming the work of one
// stage — is ../../end-goal/how-humans-do-it/01-one-pipeline.md. The fleet
// that does not yet exist behind the roles is
// ../../end-goal/how-humans-do-it/10-fleet.md. The milestone, the four rules
// included, is ../../roadmap.md#m1--one-change-ships.
// # The implementer emits the quantity the factory watches by
//
// [ImplementerSystemPrompt] carries one standing instruction beside the four
// rules: the program appends one line per unit of work it performs to the file its
// environment names, and it exercises its own behaviour while it runs. That is not
// a detail of the prompt — software that emits nothing cannot be watched, and the
// targets this factory deploys to receive no traffic of their own, so the release
// exercising itself is where the quantity comes from at all. What it costs is an
// instruction rather than a check: an implementation that ignores it produces a
// release whose analysis window can only end at its cap, and nothing rejects it for
// that.
//
// # And it writes the two files a contract is derived from
//
// Two file-name conventions go with it, for the same reason and at the same cost:
// what a service publishes and what it reads are derived from its source, so the
// source has to say them. A published interface is one exported struct type in a
// file named for it; an interface this service reads is a mirror in a file named
// for its producer; and a program that publishes an interface writes one document
// per unit of work beside the quantity, which is what a consumer contract is
// decided against. The unit goes in a field's own name rather than in
// a tag, which is what makes a change of units read as a rename.
//
// This one is worth less than an instruction usually is, because two of its three
// parts do have a check. A build that declares a contract file the derivation
// cannot read is refused, and a producer whose run wrote no document fails every
// consumer contract in force against it. What has no check is the mirror: an
// implementation that reads a field it left out of the mirror declares nothing
// about it, and what covers that is a safeguard an owner places.
package agent
