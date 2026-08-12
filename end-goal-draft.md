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

6. Check that the unit tests conform to the functional requirements.
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

### Gates

A gate sits after every stage: spec, implementation plan, tasks, implementation, and
each promotion between environments. The mechanism is permanent — it does not fade as
the factory improves.

The factory scores each change and auto-passes what it judges low risk. The same score
picks the rollout strategy: A/B, canary, blue-green, straight. Humans override by
pinning a gate always-on or pinning a strategy, and can veto after the fact.

A failing canary rolls back on its own — no human in the loop, no waiting. The rollback
is reported, not requested.

The score is learned, not fixed. Every bad call feeds back and refines it: an auto-passed
change that a human vetoes, a low-risk change whose canary rolled back, a gate the factory
would have passed but a human rejected. Outcome feedback is the sharpest signal but not
the only one — any source that improves the score is admissible, and the input set stays
open by design.

Actions available at each gate:

| Gate | Actions |
|---|---|
| Spec | Approve · Reject with feedback · Edit in place |
| Implementation plan | Approve · Reject with feedback · Edit in place |
| Tasks | Approve · Reject with feedback · Edit in place |
| Implementation | Approve · Reject with feedback |

Artifacts are editable by hand. Code is not.

**Deployments** — at least UAT and prod, more definable per project. UAT is where human
UAT (7) happens.

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

## Open

- What shape does the risk score take — one number, or per-dimension (blast radius,
  reversibility, test coverage, area churn)?
