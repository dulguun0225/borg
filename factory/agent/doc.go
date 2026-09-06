// Package agent owns the roles an agent is put on — [Interviewer], which runs
// on an intent, and the four authoring roles [SpecAuthor], [Planner],
// [TaskAuthor] and [Implementer] — the [Model] interface every role calls, and
// the two implementations of it, [OpenRouter] and [Anthropic]. The words the
// product ships per role are exported constants,
// [ShippedInterviewerPrompt], [ShippedDecomposerPrompt],
// [ShippedSpecAuthorPrompt], [ShippedPlannerPrompt],
// [ShippedTaskAuthorPrompt] and [ShippedImplementerPrompt] — one per role, the
// way the design closes the set of prompts with the set of roles — and the four
// rules they all name are [Rules], one constant included in each so they cannot
// drift apart.
//
// # The files
//
// model.go is [Model] — one call, a principal, a system prompt and a user
// message in, a [Reply] out — the unit kinds [UnitsInput], [UnitsOutput] and
// [UnitsCachedInput], and [ErrReply]. rules.go is [Rules]. criterion.go is
// [Criterion] and the writer that puts the criteria in force into a prompt.
//
// interviewer.go is [ShippedInterviewerPrompt], [Interviewer], the
// [Interviewing] it takes, and the [Reading] it returns — a question or the
// statements the requester confirms — with the parse of the reply.
// decomposer.go is [ShippedDecomposerPrompt] and nothing else.
// specauthor.go is [ShippedSpecAuthorPrompt], [SpecAuthor], the [Refining] it
// takes with its [Question], [Requirement], [Constraint], [Hazard] and
// [Returned] values, the [Refined] it returns with its [DraftCriterion] values,
// [ErrNoPrompt], and the parse of the reply. specauthorscreen.go is the screen
// half of that reply — [ScreenMachine], [ScreenTransition], [ScreenInForce] and
// the parse of the three lines that carry a machine — split out so that neither
// file passes the length a file is held to.
// planner.go is [ShippedPlannerPrompt], [Planner], the [Planning] it takes,
// the [Plan] it returns, and parseBlock, the one-header protocol the plan and
// the tasks share. taskauthor.go is [ShippedTaskAuthorPrompt], [TaskAuthor],
// the [Dividing] it takes and the [Tasks] it returns. implementer.go is
// [ShippedImplementerPrompt], [Implementer], the [Implementing] it takes, and
// the [Change] of [File] values it returns, with the parse of the reply beside
// it.
//
// openrouter.go is [OpenRouter], the chat completions request and response it
// is written against, [ErrUpstream] and [ErrRefused]. anthropic.go is
// [Anthropic], the messages request and response, [StatusError] and
// [ErrAnswer]. paced.go is [Paced] and [NewPaced].
//
// The tests are one file per file of code, and none of them reaches a
// database, this package writing no record.
//
// # What a role is
//
// A role is a struct over a [Model] and the role prompt version in force,
// which the component that dispatches it read off the artifact store's chain
// for that role and handed over. The words a run reads are that version's, not
// the shipped constant's: the constants are what the factory enters at its
// first start, and a role whose Prompt is empty refuses with [ErrNoPrompt]
// rather than falling back on them, because a role with no version in force is
// a hold dispatch writes and a run it does not make.
//
// [Interviewer.Interview] takes an [Interviewing] and returns a [Reading]: one
// question the role cannot proceed without, or the reading as statements, one
// behaviour each, which intake writes as the intent's requirements when the
// requester confirms them. It names no service and no item — the interview
// happens before decomposition — and it authors no criterion.
//
// [SpecAuthor.Refine] takes a [Refining] and returns a [Refined] — the spec,
// the criteria it introduces each naming the requirement it answers and the
// provenance it was drafted under, the criteria it withdraws, and the screen's
// state machine where the item has a user interface. The provenance is read at
// introduction and written by the artifact store in the same call, so the role
// is handed the constraints in force and the item's area where it is graded
// irreversible; a role handed neither names neither, and every criterion it
// authors is factory-drafted. [Planner.Plan] takes a [Planning] and returns a [Plan];
// [TaskAuthor.Divide] takes a [Dividing] and returns [Tasks];
// [Implementer.Implement] takes an [Implementing] and returns a [Change] of
// [File] values. Every role takes a [Returned] — the reject or the rework
// request that sent the item back to it, with its reason and the version it
// was decided over — and every one of them is told the criteria in force as
// [Criterion] values carrying the id and the sentence and nothing else.
//
// Each implementation takes its model name from configuration and names its
// credential as a [secretref.Ref]: the value is resolved at the moment of the
// HTTP call under the caller's own principal, sent in a header, stored in no
// field of any struct here, and rendered in no error this package returns.
// [OpenRouter] posts to OpenRouter's chat completions endpoint with an
// OpenRouter API key; [Anthropic] posts to Anthropic's messages endpoint with
// the OAuth token `claude setup-token` mints against a Claude subscription,
// which is served claude-haiku-4-5 and refused every model above it. The
// caller selects one where the model is constructed and this package
// dispatches on nothing.
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
// Each role states its reply protocol in its role prompt and parses the reply
// strictly: a reply in neither of its forms is [ErrReply], an agent asserting a
// verdict is a parse failure, and text that reads as an instruction anywhere in
// a reply or an input is content nothing here acts on.
//
// [ShippedImplementerPrompt] carries, beside the four rules, the place an
// encoding declares — the build or the candidate environment, written as the
// suffix package criterion's extractor matches — the standing instruction that
// the program appends one line per unit of work to the file its environment
// names, with the count of the area's hazardous operation where one is named,
// the marker a driver names its screen and state by and the file a screen's own
// transition function is written in, both of which package
// screenstatemachine's extractor matches, and the file-name conventions a
// contract is derived from: a published interface is one exported struct type
// in a file named for it, an interface this service reads is a mirror in a file
// named for its producer, and the unit goes in a field's own name rather than
// in a tag.
//
// # Which callers are not built
//
// The decomposer has no type here and only its words: what would run them is a
// stage that decides a decomposition, and the factory is told what to
// decompose. The words ship because there is one role prompt per role, and a
// dispatch on that role with no version in force is a hold rather than a run.
//
// The gate's mechanical rejection of a build whose emission does not count the
// area's hazardous operation is not built: the prompt asks for the count and
// nothing reads it back off the build. The drivers and the screen's transition
// function are asked for and read back — package screenstatemachine derives
// both from the build and rejects in both directions over them — but the gate
// that fires those checks is not built either.
//
// There is no constraint record in the factory, so what a caller can put in
// [Refining.Constraints] is whatever constraint arrived with the request, by id
// and with no text. The field is the seam, and a fuller list arrives when that
// record does.
//
// Who may write what: this package writes no record. The units a reply
// carries, and the artifact the role produced, are recorded and submitted by
// the component that dispatched the role, not here. There is no fleet record
// behind these roles: the model name is configuration and the scope is
// wherever the caller points the role.
//
// What defines it: a role — what an agent is put on, naming the work of one
// stage or put on an intent before there is an item — and the material a stage
// hands an agent, the reject or rework request among it, are
// ../../end-goal/how-the-factory-works/01-one-pipeline.md. The interview the
// interviewer runs, its rounds, and the reading written as a set of statements
// in the six patterns are
// ../../end-goal/how-the-factory-works/02-intent-into-items/02-the-interview.md,
// and what the decomposer would cut is
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md. The role prompt as
// a versioned record the factory enters what shipped into is
// ../../end-goal/how-the-factory-works/10-fleet/03-what-an-agent-is-told/README.md,
// and what a version may be authored from is 01-what-a-version-is-authored-from.md
// beside it. The units a provider returns per kind are
// ../../end-goal/how-the-factory-works/10-fleet/01-what-an-agent-runs-on.md.
// What the spec author authors — several criteria, the withdrawals, and the
// requirement each answers — is
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/README.md;
// a criterion's provenance, which the drafting stage names and the artifact
// store writes in the same call, is 01-the-record.md beside it; and the screen's
// state machine, with the transition that leaves the screen, is
// 04-the-screen-state-machine.md.
// The plan is
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/03-implementation-plan.md
// and the tasks are 04-tasks.md. The encoding's place, the emission, and the
// Implementation gate rejecting in both directions are
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/05-implementation/02-the-encoding-and-the-emission.md.
// The fleet behind the roles is
// ../../end-goal/how-the-factory-works/10-fleet/README.md.
package agent
