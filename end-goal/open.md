# Open

Nine questions, all from sitting 1 of the interview. Each is deleted from here when it is folded into the file that owns its subject.

**Are items written before or after the decomposition gate?** Does the cut write item records as it authors the set, with the gate then deciding over records that already exist, or does the cut decision hold a proposed set and items appear only on approval? Turns on: whether a rejected cut leaves item records to supersede or discard, and whether an item can exist that no gate approved.

**What says which stage an item is at, and who writes it?** [_Dispatch_](how-humans-do-it/02-intent-into-items.md#dispatch) matches the item's stage against the role, so something answers this — a field one component advances at each transition, or a fact derived from which artifacts exist and which gate decisions passed. Turns on: whether one component owns item state and every stage reports to it, or the stages are independent writers and an item's stage is a query over the graph.

**What does dispatch read off an item, and what does a pin read?** An item names one service. Are its area and its project fields of the item, or facts of the service reached through it? Turns on: whether a scope match at dispatch and a pin (9) over an area read the item alone, or must join to the service record.

**Is a candidate a record, or a name for an item plus a build?** Identity is item plus build, so a rebuild after a repair is a different pair while the branch and the environment persist across it. Is something written at an event — branch creation, build start, first successful build — or do the deploy, the criteria results, and the reject all point at the pair? Turns on: what a criteria result attaches to, and whether a rejected candidate leaves a record naming the rejection or only an attempt counted against [_the bound_](how-humans-do-it/03-gates.md#the-attempt-bound).

**Who writes the release record, and who mints the number?** Both happen at merge to master. Is [_The merge queue_](how-humans-do-it/05-environments.md#the-merge-queue) the writer of both, or does something else react to master moving? Turns on: whether release identity lives inside the queue's component, and where the per-service serialization sits that stops two merges taking one number.

**Which of the release record's links does it hold, and which are inbound edges?** It links the item, the build and commit, the gate decisions, the contract versions, and every deploy — the decisions written before it existed and the deploys after. Turns on: whether five components write one record, which is a seam that has to be declared, or the record holds what is known at merge and every reader traverses inbound edges.

**When is a deploy record written, and how many does one rollout produce?** At the start of the deploy or at the end of the rollout; one record updated as traffic shifts, or one per event. Turns on: what "which release runs where" answers while a release takes part of the traffic and its [_control_](how-humans-do-it/08-operations.md#the-health-signal) serves the rest, and what [_The reconciler_](how-humans-do-it/08-operations.md#the-reconciler) compares against.

**What is a deploy record keyed by?** Service and environment, and the production target too, given the reconciler reads what is actually running on each target. Turns on: whether current release can differ per target inside one environment, and whether a mismatch is raised per target or per environment.

**Is current release derived or stored, and which reading do its readers take?** A query over the service's production deploy records, or a field written at deploy completion. And while a rollout runs, does a dependent's candidate environment compose from the release taking part of the traffic or from the one its control runs — the same fact the dependency hold at [_Deploy to production_](how-humans-do-it/03-gates.md#deploy-to-production) reads. Turns on: whether the service record gains a writer on every deploy, and whether current means most recently deployed or most recently completed, which disagree exactly while a rollout runs.
