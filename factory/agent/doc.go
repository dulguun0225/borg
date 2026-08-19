// Package agent is M1's two authoring roles — [SpecAuthor] and [Implementer]
// — the [Model] interface both roles call, and [Anthropic], the one
// implementation of it. The system prompts are exported constants —
// [SpecAuthorSystemPrompt], [ImplementerSystemPrompt] — because roadmap M1
// makes what each agent is told part of the milestone rather than a detail of
// it, and the four rules it names are [Rules], one constant included in both
// prompts so the two cannot drift apart.
//
// # A model, a role, a credential
//
// A role is a struct over a [Model], and a Model is one call: a system
// prompt, a user message, a [Reply]. [Anthropic] takes its model name from
// configuration and names its credential as a [secretref.Ref]: the value is
// resolved at the moment of the HTTP call, sent in a header, stored in no
// field of any struct here, and rendered in no error this package returns —
// an API error carries the status and the response body, and the request that
// body may quote carries the credential in a header, not in the body.
//
// [Paced] wraps a Model to leave an interval between two call starts, so the
// factory sends nothing in rapid succession however many calls a stage makes —
// a retry after a refused reply being the one place two requests would
// otherwise have nothing between them. It is composed around [Anthropic] by the
// caller rather than being a field on it, Anthropic being a value with nowhere
// to keep the time of the last call.
//
// The credential is the long-lived OAuth token `claude setup-token` mints
// against a Claude subscription, sent as a bearer token under the beta header
// that scheme requires. One scheme and not two: an API key is a different
// header and this package sends neither conditionally, so an install holding
// one cannot call the model until a credential kind is something configuration
// names — which is the fleet's at M7, there being no fleet record behind these
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
// introduced. What that costs: the brief grows with the service, and M1 sends
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
package agent
