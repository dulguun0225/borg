# Next

The work list, not a section of the document. Each entry carries what has already been decided about it, so the next session starts from the decision instead of rediscovering it.

## Decided, not yet written

### The cut into items is gated

One intent can become four items in four services, and nothing stands in front of that cut. The Spec gate is per item, so approving one item's spec never ratifies the decomposition that produced four — a wrong cut surfaces several specs later, and re-cutting means abandoning items. Decomposition becomes a stage with a gate of its own and a row in the actions table, scored like every other gate. That is what gives [_No single item may break a contract_](how-humans-do-it/06-contracts.md#no-single-item-may-break-a-contract) — "an item that cannot ship by itself was cut wrong" — a place to be caught. The cost is a new stage and a new row. (Owner decision, 2026-08-13.)

### The interview is factory-driven and ends at the spec

The factory asks until it has enough to draft a spec that passes the criteria form. The questions land in Inbox (3), the spec is the artifact, and what ends the interview is the factory having enough to author rather than a human declaring done. This is what [_Where a gate sits, and what decides it_](how-humans-do-it/02-gates.md#where-a-gate-sits-and-what-decides-it) already assumes when it says approving the spec ratifies that refinement and no interview gate is missing. (Owner decision, 2026-08-13.)

### A merge-queue failure is a rejection and burns an attempt

[_The merge queue_](how-humans-do-it/04-environments.md#the-merge-queue) says a candidate "fast-forwards only if that passes" and never says what happens to the one that fails. It matters because the item has already passed its merge gate, so no verdict path is left for it.

Candidates behind a failure re-verify for free — they failed on someone else's account, and the section already says so. A candidate that fails its own re-verification against the master it will actually land on failed on its merits: it goes back up the pipeline, burns an attempt against the bound, and the score learns from it the way it learns from a reject and not from a hold. [_The attempt bound_](how-humans-do-it/02-gates.md#the-attempt-bound) already presumes this shape when it says an item that exceeds the bound "stops being retried".

### There is no tenancy

Each organisation runs its own self-hosted setup, which [_What the factory does_](what-the-factory-does.md) already states in full. There is no multi-tenant model, no shared install, and nothing further to design. Recorded so the absence is not read as a gap. (Owner, 2026-08-13.)

## Still to write

**The front of the pipeline.** [_What the factory does_](what-the-factory-does.md) promises the factory "decomposes intent into items, dispatches its own agents onto them", and no section owns cutting, sizing, or dispatch. With the cut now gated, this is the section that has to describe what the gate decides. Duty (1) is cited nowhere in the tree and (2) once, against (9) nine times and (12) seven — the intake half of the duty list has almost no mechanism attached to it. Contracts and Operations run to roughly 4,000 words between them; everything from intent through spec is about 1,000.

**Tasks.** The only stage in the pipeline with no prose anywhere, though it has a gate row offering Edit in place and appears on the Work timeline. What a task is, who authors it, and how it relates to the implementation plan are all unstated. It cannot be a unit that ships — one item is one release — so it is an internal step, and saying that is most of the work.

**The gate subsections that are missing.** [_What particular gates carry_](how-humans-do-it/02-gates.md#what-particular-gates-carry) covers Spec, Merge to master, and Deploy to production. Implementation plan, Tasks, Implementation, and Deploy to candidate environment have none, and the new decomposition gate will need one. The Implementation gate is where the UI state machine is enforced and where Edit in place is absent by design — both facts currently live in other sections' prose.

**What gate policy (8) contains.** Enumerated only by accretion across four files: the attempt bound, the predicate catalog, K, and the watch window's size, confidence, and cap. Duty 8 reads far smaller than it is, and an owner cannot see what authoring it involves.

**`page`.** An Ops action and a push channel, named twice, with no on-call concept behind it — no target, no rotation, no statement of what earns one.

**`what-the-factory-does.md` ends in `_Both to expand._`** 142 words at the entry point to the tree, with tight integration and traceability each getting one sentence.
