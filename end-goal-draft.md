# End goal

A draft. Everything here is open to revision.

## What the factory does

A **fully autonomous software factory and operations**: it refines intent, produces the
software, deploys it, monitors it, finds issues, and fixes bugs, on its own. The factory
is a product — each customer runs their own isolated, self-hosted setup.

## What humans do

Non-exhaustive owner's list. The factory does everything else.

**Originate intent** — the factory cannot know what is wanted until told:

1. Request features.
2. Supply constraints: laws and regulations, and raw documents that refine the intent.
3. Sit for the factory's interview — grilled — until the intent is refined.

**Feed back as end users** — routine, in end-user terms, not engineering terms:

4. Report bugs.
5. Complain ("this button is too slow").

**Verify against intent** — the candidate permanent touchpoints:

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

Everything goes through it. A human-authored change, an AI-authored change, and one the
two write together take the same stages, the same gates, and the same score. Authorship
is an attribute of each stage, not a mode on the item: an item can have an AI spec, a
co-authored plan, and a human implementation, and it is still one thread.

Backstop duties (11, 12) are this and nothing more — the pen changes hands for a stage.
Taking over is not leaving the factory.

A bug the factory finds and fixes itself is an item like any other. It appears in Work,
takes the same stages, and is auto-passed only where the score allows. There is no
second, invisible path, and nothing ships that the trust number cannot see.

There is no bypass, including for incidents. A human standing at a gate is not a delay:
the emergency lever is approve now, not skip. A change that should not have shipped is
caught by the canary, not by a faster route around the pipeline.

### Gates

A gate sits after every stage: spec, implementation plan, tasks, implementation, and
each promotion between environments. The mechanism is permanent — it does not fade as
the factory improves.

The factory scores each change and auto-passes what it judges low risk. The same score
picks the rollout strategy: A/B, canary, blue-green, straight. Humans override by
pinning a gate always-on or pinning a strategy, and can veto after the fact.

A failing canary rolls back on its own — no human in the loop, no waiting. The rollback
is reported, not requested.

Veto after the fact assumes the change can still be undone, and that assumption decays
as later work builds on it. Reversibility is a scored dimension, and the veto window is
bounded by it.

Actions available at each gate:

| Gate | Actions |
|---|---|
| Spec | Approve · Reject with feedback · Edit in place |
| Implementation plan | Approve · Reject with feedback · Edit in place |
| Tasks | Approve · Reject with feedback · Edit in place |
| Implementation | Approve · Reject with feedback |

At a gate, artifacts are editable by hand. Code is not: a gate approves or rejects an
implementation, it never hand-patches one. A human who wants different code authors it
upstream and sends it back through the pipeline.

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
- **Context** — what this change touches in this customer's business. The same diff is a
  different animal in a payments path than on a marketing page.

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

The graph is not uniform. Up to UAT, deploys are plain and what moves is a candidate.
UAT is production-like, and it is where human UAT (7) happens. Passing UAT is where a
change becomes a version, and everything past that point is machine: versioning, strategy
selection, rollout, monitoring, rollback.

So UAT is the last human touchpoint before the factory runs unattended.

### Surfaces

One product, five surfaces. They are split by what a human is trying to do, not by
whether the data is configuration or observation — a number is only worth showing next
to the control it should change.

- **Inbox** — everything waiting on a human, in one queue: pending gates, UAT
  assignments (7), the factory's interview questions (3), and escalations where the
  factory admits it is stuck (11, 12). Carries the badge count. This is the home screen,
  because answering the factory is the daily job.
- **Work** — one item is one thread. Intent, spec, plan, tasks, implementation, and
  rollout on a single timeline, with each gate shown inline at the point it sits.
  Features and bugs are the same kind of item. A project is a grouping of work, not a
  separate place. Board and list views answer "where is it stuck".
- **Ops** — deployed software per environment: health, incidents, in-flight rollouts.
  An acting surface, not watch-only: roll back, page, and exercise veto after the
  fact (10).
- **Factory** — the machine itself. Gate and risk policy, thresholds, strategy pins,
  environments, agent fleet — and the same page carries the readout: throughput, rework
  rate, gate rejection rate, cost per feature, what each agent is doing and how well.
  Not stage definition: the stages are the factory's own.
- **People** — humans, roles, who gates what, who does UAT.

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

- Does a version hold one work item or several? If UAT batches, rollout is version-scoped
  while everything before it is item-scoped — the single thread forks, and vetoing one
  change out of ten that shipped together becomes the expensive case. One item per version
  keeps the thread intact and costs a UAT per item.
- Is UAT permanently human? If it is, nothing reaches prod unattended — including the bugs
  the factory finds and fixes itself — and throughput is capped by human testing. The
  alternatives: score-gate UAT like every other gate, or split it, permanent for
  human-originated features and auto-passable for factory-originated fixes.
