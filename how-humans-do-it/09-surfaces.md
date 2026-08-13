# Surfaces

One product, five surfaces. They are split by what a human is trying to do, not by whether the data is configuration or observation — a number is only worth showing next to the control it should change.

## Inbox, Work, Ops, Factory, People

- **Inbox** — everything waiting on a human, in one queue: pending gates, UAT assignments (7), the factory's interview questions (3), and escalations where the factory admits it is stuck (11, 12). Carries the badge count. This is the home screen, because answering the factory is the daily job.
- **Work** — one item is one thread. Intent, spec, plan, tasks, implementation, rollout, and the numbered release it ends in, on a single timeline, with each gate shown inline at the point it sits. The cut is the one gate above a thread rather than on it: an intent that became four items shows one decomposition decision with four threads under it, and shows as partial where those threads did not all reach a release. Features and bugs are the same kind of item. A project is a grouping of work, not a separate place. Board and list views answer "where is it stuck" — which now includes the merge queue and the windows a service has open: who holds each, what is under watch, and what waits where the windows are all open, a rollback is holding, or a human is.
- **Ops** — deployed software per environment: which release of each service is running, what contracts it publishes, health, incidents, in-flight rollouts and the control each is measured against, where the strategy paid for one. An acting surface, not watch-only: exercise veto after the fact (10) — rolling back while a control stands, raising the revert after — and page.
- **Factory** — the machine itself. Gate and risk policy, thresholds, strategy pins, environments, agent fleet — and the same page carries the readout: throughput, rework rate, gate rejection rate, cost per feature, what each agent is doing and how well. Not stage definition: the stages are the factory's own.
- **People** — humans, roles, who gates what, who does UAT. Declared, not enforced: the model routes work today, and is the seam authentication attaches to later.

## Three properties the surfaces have to carry

**Two audiences.** Everything above serves whoever holds a duty from the owner's list, however many people that is. Duties 4 and 5 — report a bug, complain — belong to end users, who never open this product. Their intake is thin and embedded in the deployed software; what they send lands in Work as an unrefined intent.

**Designed for silence.** When the factory is working, there is nothing to do and the screens are empty. Empty must not read as dead: Inbox at zero shows a digest of what the factory shipped, decided, and auto-approved while nobody was looking.

**Push, not poll.** Gates and escalations leave the product — mail, chat, page. Otherwise the factory's speed is capped by how often a human remembers to check.

## The trust number

Factory carries one number that governs trust: how much the factory auto-approved and how often that was later vetoed or rolled back. Humans decide how much rope to give from that number, and it is the same signal the risk score learns from.
