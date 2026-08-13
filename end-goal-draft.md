# End goal

A draft. Everything here is open to revision.

## What the factory does

A **fully autonomous software factory and operations**: it refines intent, produces the
software, deploys it, monitors it, finds issues, and fixes bugs, on its own. It also
runs itself: it decomposes intent into items, dispatches its own agents onto them, gates
its own output against a score it learns, and escalates what it cannot finish. The
factory is a product — each customer runs their own isolated, self-hosted setup.

**Tight integration is key.** One system, not a bundle of tools with connectors between
them. Intent, spec, change, gate decision, deploy, incident, and score are one graph.

**Traceability is key**, and is the testable form of it: every artifact walks back to the
intent that caused it and forward to what it produced, under the policy and score that
were in force at the time.

_Both to expand._

## What humans do

Non-exhaustive owner's list. The factory does everything else.

**Originate intent** — the factory cannot know what is wanted until told:

1. Request features.
2. Supply constraints: laws and regulations, and raw documents that refine the intent.
3. Sit for the factory's interview — grilled — until the intent is refined.

**Feed back as end users** — routine, in end-user terms, not engineering terms:

4. Report bugs.
5. Complain ("this button is too slow").

**Verify against intent** — at a gate, so as often as the score or a pin (9) says:

6. Confirm the acceptance criteria are the right ones. Unit tests are today's encoding of
   them; what a human is checking is the criteria, not the test code.
7. Perform UAT.

**Set the rules** — permanent, not shrinking:

8. Author gate policy and risk thresholds.
9. Pin a gate always-on for a stage, project, or area.
10. Veto after the fact — roll back a change the factory auto-approved.

**Backstop the factory** — only where it falls short, shrinking as it improves:

11. Help with spec generation when the factory cannot do it properly — up to creating
    the spec together with the AI.
12. Take over issues the factory cannot fix on its own.

## How humans do it

### One pipeline

An **item** is the unit the factory moves: one request-shaped thing, one thread, one
release. An **agent** is a worker the factory runs — a model in a role, with a scope.
Two agents on the same model share one authorship prior: the score is kept per model, not
per role.

Every item goes through the pipeline. A human-authored change, an AI-authored change, and
one the two write together take the same stages, the same gates, and the same score.
Authorship is an attribute of each stage, not a mode on the item: an item can have an AI
spec, a co-authored plan, and a human implementation, and it is still one thread.

Backstop duties (11, 12) are this and nothing more — the pen changes hands for a stage.
Taking over is not leaving the factory.

A bug the factory finds and fixes itself is an item like any other. It appears in Work,
takes the same stages, and is auto-passed only where the score allows. There is no
second, invisible path, and nothing ships that the trust number cannot see.

There is no bypass, including for incidents. A human standing at a gate is not a delay:
the emergency lever is approve now, not skip. A change that should not have shipped is
caught by the canary, not by a faster route around the pipeline.

Waiting for the UAT slot is the other place a queue forms, and its order is settable —
an urgent item goes to the front. That is not a bypass. Reordering a queue changes when
an item reaches the gates; it does not change which gates it passes through.

### Gates

A gate sits after every stage: spec, implementation plan, tasks, implementation, each
merge, and each deploy. The mechanism is permanent — it does not fade as the factory
improves.

The factory scores each change and auto-passes what it judges low risk. The same score
picks the rollout strategy: A/B, canary, blue-green, straight. Humans override by
pinning a gate always-on or pinning a strategy, and can veto after the fact.

A failing canary rolls back on its own — no human in the loop, no waiting. The rollback
is reported, not requested.

Veto after the fact assumes the change can still be undone, and that assumption decays
as later work builds on it. Reversibility is a scored dimension, and the veto window is
bounded by it. It decays that way and no other: with one item per release, a change is
never harder to undo because it happened to ship alongside nine others.

Actions available at each gate:

| Gate | Actions |
|---|---|
| Spec | Approve · Reject with feedback · Edit in place |
| Implementation plan | Approve · Reject with feedback · Edit in place |
| Tasks | Approve · Reject with feedback · Edit in place |
| Implementation | Approve · Reject with feedback |
| Merge to UAT branch | Approve · Reject with feedback |
| Deploy to UAT | Approve · Hold · Reject with feedback |
| Merge to master | Approve · Reject with feedback |
| Deploy to production | Approve · Hold · Pin strategy |

Those eight rows are the default path, not the whole set. A gate sits after every deploy,
so a customer that defines more environments gets a row for each, carrying the actions
Deploy to UAT carries. It gets no more merge rows: two branches back the promotion path,
so the extra environments are deploy targets. Production stays the only deploy without
Reject.

At a gate, artifacts are editable by hand. Code is not: a gate approves or rejects an
implementation, it never hand-patches one. A human who wants different code authors it
upstream and sends it back through the pipeline.

Merge and deploy gates edit nothing at all — what they hold is an event, not a document.
Reject sends the change back up the pipeline; hold leaves it queued at the gate, for a
window or a dependency, with the change still good. The two are different answers and
have to stay distinguishable: the score learns from a reject and should learn nothing
from a hold.

A stage also carries an attempt bound, authored with the rest of gate policy (8). An item
that exceeds it stops being retried and stands in Inbox as an escalation (12) — the
factory saying it cannot do this one. Holds do not count against the bound; a hold is not
a failed attempt, for the same reason the score does not learn from one. The bound costs
something wherever it is set: low turns solvable work into human work, high burns spend
before anyone sees the item.

The Spec gate carries two duties. The interview (3) refines intent and the spec is what
it produces, so approving the spec ratifies that refinement — there is no interview gate
and none is missing. The spec also states the acceptance criteria, so approving it
confirms them (6). What is confirmed is the criteria; a test encoding them is downstream
of that approval, not the object of it.

Merge to master is the release event — where a candidate becomes a numbered release —
and it is also where the UAT verdict lands. Approving it is passing UAT; rejecting it is
failing UAT, which sends the item back up the pipeline and empties the UAT slot for
whatever is waiting. The verdict is a human's when the score or a pin puts one there and
the factory's own otherwise — UAT is scored like every gate around it.

Production deploy is the exception to reject, which is why that row does not offer it. By
the time a change stands there the merge has happened and the number is spent; hold is
the only way to stop it, and undoing it is a revert, which is a new item. That is veto
after the fact under another name.

The last two gates are the factory's own steady state. Both exist, both are scored, and
both are auto-passed by default, as is every gate above them. Pinning either one puts a
human back in prod's path without inventing a new mechanism, which is why they are gates
and not an exemption from gating.

### Risk score

A vector of named factors, reduced to one number by a published formula. Both halves
matter — the number is what a gate compares against a threshold, the vector is what a
human reads when they disagree with the number. A score nobody can argue with is a score
nobody will trust.

Factors, at least:

- **The change** — size, blast radius, area churn, test coverage, reversibility.
- **Authorship** — a prior, per human and per AI model, carried from that author's own
  history of vetoes and rollbacks. It starts wide for an author the factory has not seen
  and narrows with evidence, which is also how a new model version earns its way in.
- **Context** — what this change touches in this customer's business, and which sibling
  services consume what it publishes. The same diff is a different animal in a payments
  path than on a marketing page.

Likelihood and impact stay separate until the last step. They answer different questions
and drive different responses: likely-wrong but cheap to undo should ship and let
rollback handle it; unlikely but catastrophic should be gated regardless. This is also
why one score drives two decisions — the gate reads mostly likelihood against impact,
the rollout strategy reads mostly impact against reversibility and how fast a problem
would surface.

The score is learned, not fixed. Every bad call feeds back and refines it: an auto-passed
change that a human vetoes, a low-risk change whose canary rolled back, a gate the factory
would have passed but a human rejected. Outcome feedback is the sharpest signal but not
the only one — any source that improves the score is admissible, and the input set stays
open by design.

Scoring on authorship feeds itself if left alone: a distrusted author is gated more,
gated work draws more scrutiny, more scrutiny finds more faults, and the distrust is
confirmed. The factory holds out a random sample — occasionally auto-passing what it
would have gated, under canary protection — to keep unbiased signal on the authors and
areas it has stopped trusting.

### Environments

Environments are records, not names in code. Each carries its own gate policy, strategy
defaults, credentials, and history of deploys, incidents, and rollbacks. At least UAT and
prod exist everywhere; customers define more per project.

Two long-lived branches back each service's promotion path: a UAT branch and master.
Merging and deploying are separate events and so are separate gates — a merge admits a
change to a branch, a deploy puts a branch on an environment, and either can happen
without the other. A deploy can be rerun; a merge cannot be unmerged the same way.

The UAT branch is a slot, not a queue. It is reset to master, takes exactly one item,
gets deployed and tested, merges, and resets. A second item that is ready waits. A reject
empties the slot at once and the item rejoins the queue: a candidate being repaired must
not hold the branch shut behind it.

The slot is per service, so a twelve-service project can have twelve items in UAT at
once. That is only safe because no item may break a contract, and it settles what a
candidate is tested against — the current releases of its dependencies, never another
service's candidate. A UAT environment is composed for the candidate standing in it.

The graph is not uniform. Up to UAT, deploys are plain and what moves is a candidate.
UAT is production-like, and it is where the candidate is tested (7). Passing UAT is where
a candidate becomes releasable; merge to master is where it becomes a release and gets
its number. Everything from there is machine: numbering, strategy selection, rollout,
monitoring, rollback.

UAT is score-gated like every other gate, so it is not the last human touchpoint and
there is no last one: the same score decides at each gate whether a human stands there,
and a pin (9) puts one back. Where it auto-passes, the verdict on the candidate is the
factory's own, taken on a production-like environment against acceptance criteria a human
already confirmed (6).

The alternative was to split UAT by origin — permanent for human-originated features,
auto-passable for factory-originated fixes — and that is the second, invisible path the
pipeline forbids, sorted by a worse predictor than the score's own factors. Scoring it
costs this: a change can reach production with nobody having watched it run. What then
catches a factory that built the wrong thing is the criteria confirmed at the Spec gate,
the canary, and veto after the fact (10) — and an owner who wants more buys it back per
service or per area with a pin.

### Releases

**One item per release. Always, at every stage, permanently.** The single thread of an
item never forks: rollout stays item-scoped like everything before it, and a veto is the
rollback of exactly one item rather than a surgical extraction from a bundle of ten. The
cost is one trip through the UAT slot per item, taken serially within a service. Where
the score or a pin puts a human in that slot, the service moves at one human UAT per
item — a ceiling that is bought rather than structural. A dev/alpha channel added later
inherits the rule rather than renegotiating it.

A release is a record, and it is where the graph joins. It holds the item that caused it,
the build and commit it is made of, the gate decisions that let it through, the contract
versions it publishes, and every deploy of it to every environment. Ask anything about a
shipped change — from what intent, on whose approval, under which policy, running where,
rolled back when — and the answer is a walk out from the release record. Traceability is
not bolted to this; it is what the record is for.

Before it is a release it is a **candidate**, identified by its item and its build. That
is identity enough to deploy, test, and reject, and a rejected candidate never needed a
number. A build wears one label and no more: on the UAT branch it is **beta**, on master
it is a **release**. A customer who defines five pre-prod environments still has two
labels and one build collecting five deploy records — maturity does not multiply with
places to stand.

The number is an ordinal, per service, assigned at merge to master. It orders builds and
names rollback targets, and that is the whole of its job; compatibility is the contract's
business, not the release's. Numbers are never reused. A release that is rolled back
keeps its number, and the fix that follows takes the next one.

A numbered release that has never run anywhere is normal, not an anomaly. The number is
minted at merge, one gate before production, so it records that a change was accepted —
not that it is live. A hold at the production deploy gate produces exactly this. Where a
release is running is a deploy record and never the number.

Because master's only inbound path is the UAT branch, and the branch holds one item,
master cannot move while an item is in UAT. The merge is therefore always a fast-forward
and the commit that passed UAT is the commit on master. What was tested is what ships —
a structural property of the slot, not a discipline anyone has to keep.

Rollback is a deploy event, not a version event: it puts the previous release build back
on the environment and writes a deploy record, minting and retiring nothing. That the old
build still runs is not luck: no item may break the store it stands on, and that rule runs
in both directions — what the newer release wrote while it was live is still readable by
the one coming back. Undoing a release that has already shipped is not that. Master
keeps it, and the correction is a revert — a new item, its own thread, its own number.
That is what veto after the fact (10) actually costs.

### Contracts

A project's software is one or more services, and they reach each other through published
interfaces — REST under OpenAPI, gRPC under protobuf, Kafka under topic schemas. Those
interfaces have consumers, and the consumers are other services in the same factory.

So there are two versioned things and they must not be collapsed into one. A release
number orders the builds of a single service. A contract version is a compatibility
promise to whoever calls it, and semver is precisely what that is for: major means a
consumer breaks. One service can publish several contracts — an API, a gRPC service,
three topics — and they move at their own rates. A single number per service cannot carry
several independent promises, and a promise that moves for unrelated reasons stops being
read. Most releases publish no new contract version at all.

**No single item may break a contract.** A breaking change is three items: the producer
adds the new form beside the old, each consumer migrates, the producer removes the old.
Each ships alone, passes its own UAT, and is independently reversible. This is the same
discipline no-batching already forces — an item that cannot ship by itself was cut
wrong — and the two rules hold each other up. Where a change genuinely cannot be
decomposed, that is an escalation (12), not a licence to batch.

The rule is mechanical rather than a judgment call. The factory diffs the contract a
candidate publishes against the one in production: a diff the contract's mode allows
passes, and a breaking diff without the migration already in front of it is a rejection
at the merge gate. The same graph answers who is affected — the factory knows which
services consume which contracts, so "what does this break" is a query rather than an
estimate, and it feeds the context factor of the risk score directly.

Every contract carries a **compatibility mode**, declared by the service that publishes
it and enforced on every diff after that. Backward — the new build reads what the old one
wrote — is the default, and is what a consumer of an interface needs. Forward is the
opposite promise, and it is what a rollback rests on: the release coming back has to read
what the release being pulled wrote while it ran. Full is both, and costs both. The mode
belongs to each contract rather than to the factory, because an interface nobody rolls
back and a store every rollback reads do not need the same promise.

A contract belongs to the service that publishes it, and changes only inside that
service's items. It gets no thread of its own: a promise is only real when something
serves it, so the authority to accept a change to one belongs where there is a build to
check it against. What a consumer owns is the mode, declared once, not a seat at every
gate the producer stands at. The alternative was a per-change consumer veto, and it
deadlocks — in a graph where nearly every service is somebody's consumer, items wait on
each other until the attempt bound turns the pile into escalations (12), which is the
human load the factory exists to remove. An objection raised after the fact is an item
against the producer, joined to the original by intent, like any bug, and an owner who
wants a human on a particular contract buys one with a pin (9).

Deprecation is an obligation the contract carries, not a note someone leaves. When the
producer adds the new form, the old one is marked with its consumers attached; as each
migrates the list shortens, and when it empties the factory raises the removal item
itself. Nobody has to remember step three.

A service's store is a contract too, and its consumer is the service's own past — the
release that was running a minute ago, which a rollback can put back. That consumer is
why a store's mode is **full**: every rollback reads across the change in the direction
backward alone does not cover. The rule holds there unchanged: **no single item may break
the store.** A breaking schema change is three items — the store gains the new form beside
the old, the code migrates onto it, the old form is dropped — and while it stands, the old
form carries the same deprecation obligation an old interface carries.

Enforcement costs nothing new: the factory diffs the schema a candidate carries against
the one in production, and a destructive diff without the migration already in front of
it is a rejection at the merge gate. The cost lands on small work — renaming a column is
three items and three trips through the slot. What it buys is the rollback in Releases,
which is otherwise a promise about code made over data that has already moved on.

Work that spans services needs no new noun. One request can produce four items in four
services, and the intent is what joins them — every item already walks back to the intent
that caused it, so "everything that came from this request" is a query that works today.
The three items of a contract migration are the same shape: one intent, three items,
three releases.

### Operations

A deploy record says which release runs where, so the factory always knows what it is
looking at. It watches that release against the one it replaced — error rate, latency,
throughput, on comparable traffic — and that comparison is the health signal. Nothing has
to be authored for a new service to have one.

A canary fails when the comparison crosses the line, and the rollback follows on its own.
The baseline is only as good as the release it is drawn from, which is the case for
pinning: an owner can pin explicit thresholds for a service the way they pin a gate or a
strategy, and a service whose normal behaviour is already bad is where that earns its
keep.

The comparison keeps running after the rollout finishes. What it finds then is not a
rollback candidate — the change has been live for a week and the build it replaced is
long gone. It is an unrefined item in Work, the same shape as an end-user complaint
(4, 5), taking the same stages and the same gates. That is the whole of "finds issues
and fixes bugs": detection writes an item, and the pipeline does the rest.

An incident is a record on the environment. It points at the deploy, the deploy at the
release, the release at its item and its intent — so what caused an incident is a walk
out of it, the same walk the release record answers from the other end.

The factory works the item it raised under the attempt bound like any other. Hitting that
bound turns it into an escalation (12), the same Inbox row a stuck feature produces: a
bug the factory cannot fix is not a different kind of stuck.

### Surfaces

One product, five surfaces. They are split by what a human is trying to do, not by
whether the data is configuration or observation — a number is only worth showing next
to the control it should change.

- **Inbox** — everything waiting on a human, in one queue: pending gates, UAT
  assignments (7), the factory's interview questions (3), and escalations where the
  factory admits it is stuck (11, 12). Carries the badge count. This is the home screen,
  because answering the factory is the daily job.
- **Work** — one item is one thread. Intent, spec, plan, tasks, implementation, rollout,
  and the numbered release it ends in, on a single timeline, with each gate shown inline
  at the point it sits. Features and bugs are the same kind of item. A project is a
  grouping of work, not a separate place. Board and list views answer "where is it
  stuck" — which now includes the UAT slot: who holds it, who is waiting, in what order.
- **Ops** — deployed software per environment: which release of each service is running,
  what contracts it publishes, health, incidents, in-flight rollouts. An acting surface,
  not watch-only: roll back, page, and exercise veto after the fact (10).
- **Factory** — the machine itself. Gate and risk policy, thresholds, strategy pins,
  environments, agent fleet — and the same page carries the readout: throughput, rework
  rate, gate rejection rate, cost per feature, what each agent is doing and how well.
  Not stage definition: the stages are the factory's own.
- **People** — humans, roles, who gates what, who does UAT. Declared, not enforced: the
  model routes work today, and is the seam authentication attaches to later.

Three properties the surfaces have to carry:

**Two audiences.** Everything above serves the owner. Duties 4 and 5 — report a bug,
complain — belong to end users, who never open this product. Their intake is thin and
embedded in the deployed software; what they send lands in Work as an unrefined item.

**Designed for silence.** When the factory is working, there is nothing to do and the
screens are empty. Empty must not read as dead: Inbox at zero shows a digest of what the
factory shipped, decided, and auto-approved while nobody was looking.

**Push, not poll.** Gates and escalations leave the product — mail, chat, page.
Otherwise the factory's speed is capped by how often a human remembers to check.

Factory carries one number that governs trust: how much the factory auto-approved and
how often that was later vetoed or rolled back. Humans decide how much rope to give from
that number, and it is the same signal the risk score learns from.

## Deferred, but not designed out

Security comes last. The factory should be free and easy to play with at the start and
tighten as the human world demands it. That is a sequencing decision, not permission to
build something that cannot be secured later.

Four seams are nearly free now and expensive to retrofit:

1. **An actor on every record** — every gate decision, edit, approval, veto. No
   authentication, no enforcement, just the field, always populated. Identity cannot be
   added to a history that was written without it.
2. **One append-only decision log.** It is the audit trail and the risk score's training
   data at once, and it must not become two systems.
3. **Secrets by reference.** Artifacts and specs carry names, never values — they get
   copied, diffed, and handed to agents. The resolver can be a file on disk today.
4. **A named seam between agents and deploy targets.** However it is implemented, an
   agent reaches an environment through a small set of named operations. That seam is
   where policy attaches later; without it, prod access is diffused through the codebase.

One pipeline is the strongest of these and was chosen for coherence rather than safety:
a single path is a single place to put policy.

## Open

- What proves nothing breaks, when a diff cannot see meaning? A diff proves the shape
  still holds, not that callers survive — a field that quietly stops being populated, or
  that keeps its name and its type and changes its units, breaks a consumer without
  breaking a contract. No mode catches this: a mode is checked against the schema, and the
  damage is in the semantics. The honest version is consumer-driven — replay each known
  consumer's actual usage against the candidate before it releases — and that needs every
  consumer's usage recorded as a first-class thing, which is a large build and is not
  costed here. Recorded usage also covers only what has run: a consumer on a monthly path
  falls outside any window worth keeping.
- How long must a canary watch? The factory leans on it three times — as what catches a
  change that should not have shipped, as what it bought by scoring UAT instead of pinning
  it, and as the protection under the random sample it auto-passes to keep the score
  unbiased. A producer's own faults surface in the comparison quickly; a consumer's
  surface on the consumer's schedule, and a service that runs nightly proves nothing an
  hour after the rollout finished. Whether the factory can learn the interval per contract
  from its own consumer graph, or whether it is a floor an owner sets like a threshold, is
  undecided.
