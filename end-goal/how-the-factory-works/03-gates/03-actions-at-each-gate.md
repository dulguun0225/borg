# Actions at each gate

What may be done at each of the eight rows, and nothing about what decides it. Refer is on every row because it is about the human and not the event: a holder who cannot judge what they were shown passes the row to another holder, on the terms [_Where a gate is, and what decides it_](01-where-a-gate-is-and-what-decides-it.md) states.

| Gate | Actions |
|---|---|
| Decomposition | Approve · Reject with feedback · Edit in place · Refer |
| Spec | Approve · Reject with feedback · Edit in place · Refer |
| Implementation plan | Approve · Reject with feedback · Edit in place · Refer |
| Tasks | Approve · Reject with feedback · Edit in place · Refer |
| Implementation | Approve · Reject with feedback · Refer |
| Deploy to candidate environment | Approve · Hold · Reject with feedback · Refer |
| Merge to master | Approve · Reject with feedback · Refer |
| Deploy to production | Approve · Hold · Safeguard the strategy · Refer |

Those eight rows are the default path, not the whole set. There is a gate before every deploy, so a customer that defines more environments gets a row for each. It gets no more merge rows: one long-lived branch is the promotion path, so an extra environment is deployed into and never merged through.

Five further rows are the factory's own and belong to no item: [_A role prompt or a skill_](07-what-particular-gates-decide/09-a-role-prompt-or-a-skill.md), where a version of what an agent is told is decided, [_A safeguard's withdrawal_](07-what-particular-gates-decide/10-a-safeguards-withdrawal.md), where the record that removes a human from a gate is decided, and [_A halt's withdrawal_](07-what-particular-gates-decide/11-a-halts-withdrawal.md), where the record that ends a [halt](../09-gate-policy/04-stopping-the-factory.md) is. Two more take [_A safeguard's withdrawal_](07-what-particular-gates-decide/10-a-safeguards-withdrawal.md)'s shape (Approve and Reject with feedback, a human always, routed away from the author) and are stated beside the record each decides: the row that decides shortening decision-log retention, at [_Retention_](../09-gate-policy/03-what-is-not-in-it/02-retention.md), and the row that ends a legal hold, at [_A legal hold_](../09-gate-policy/03-what-is-not-in-it/03-a-legal-hold.md). Each is outside the eight because everything the eight share is an item: a stage to be at, a build to point at, a release to reach. Those five rows have none of them.

They are fed from master, and the candidate-fed row stays the factory's own. A customer environment persists, and a persistent one fed from candidate builds is a slot two candidates take turns on — the shared environment [_An environment per candidate_](../05-environments/02-an-environment-per-candidate/README.md) exists to remove, which delays everything behind whichever item is using it. A human who wants to see a pre-merge change sees it on that candidate's own environment, where a safeguard (9) puts them, and the delay is that item's alone. The drawback: no long-lived environment keeps a candidate running.

What actions a new row has follows from what feeds it, not from which row it was copied from. Fed from master, every new row has Approve and Hold for the reason Deploy to production has them: the merge has happened and the [release number](../06-releases/04-the-release-number.md) is already assigned, so hold is the only stop and what is left is a human's undo of a shipped change (10). Reject is available up to the merge to master and nowhere after it.

That is the actions and only the actions: the [_strategy_](02-the-rollout-strategy.md) and the [_analysis window_](../08-operations/02-the-analysis-window.md) follow production rather than master, so a new row fed from master gets neither — the release number is already assigned, and what makes a control worth running is organic traffic the row does not have.
