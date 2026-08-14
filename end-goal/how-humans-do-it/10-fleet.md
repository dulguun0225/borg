# The fleet

The **fleet** is the set of agents the factory runs. [_One pipeline_](01-one-pipeline.md) says what one is — a model in a role, with a scope — and that the authorship prior is kept per model rather than per role. What is left is what stands behind the model: an account at a provider, reached through a credential, owned by somebody.

## What stands behind an agent

An agent reaches its model through a **provider account**, and the factory holds a reference to that account's credential rather than the credential itself — seam 3 of [_Deferred, but not designed out_](../deferred.md), which the fleet spends before any artifact does. The resolver is a file on disk today. No provider is named anywhere in the factory: a fleet entry carries a model, a role, a scope, and the name of a credential.

The account may be an organisation's or a person's own, and the factory takes either, because a reference resolves the same way whichever it is. The early factory is where that matters. An owner standing one up before it has proved anything has a personal subscription and no organisation agreement to spend against, and a design that made them wait for procurement to watch the factory run once would be charging the unproven case the price of the proven one. So a personal account attaches through no second mechanism: same field, same resolver, same record.

What differs is downstream of the account and not in the factory at all — its quota, who may revoke it, and whose money it spends. Three things follow, and each is a cost the personal account carries rather than a feature of it: whose name ends up on the work, what a spent account does, and what happens when one is taken away.

## Paid for is not authored by

Seam 1 of [_Deferred, but not designed out_](../deferred.md) puts an actor on every record, and an artifact's author is the model that wrote it. Whose account paid for the tokens is a third fact, carried on the fleet entry and not on the artifact. A human who lends the factory their subscription has authored nothing.

Collapsing the two is the failure worth naming, because a human's name is in the record either way. The [_authorship prior_](04-risk-score.md#factors-at-least) is per model and stays per model: two fleet entries on one model share one prior however many accounts they run on, and moving an entry from a personal account to an organisation's resets nothing. A prior that travelled with the credential would make a model earn its way in twice and put a machine's record on a person.

## An account that runs out is a hold

A personal account carries a quota an organisation's agreement usually does not, so the fleet has a failure nothing else in the factory meets: an agent stops part-way through a stage because the account behind it is spent.

That is not the model failing, and it is a **hold**. The stage waits and is picked up again when there is a credential to reach, burning nothing against [_the attempt bound_](03-gates.md#the-attempt-bound) and teaching the prior nothing — the score learns from a reject and not from a hold, which [_What a gate may change_](03-gates.md#what-a-gate-may-change) settles for gates and [_The watch window_](08-operations.md#the-watch-window) keeps again when it counts only a rollback traceable to the health signal as evidence. What an author is worth has to be read off that author's work.

No [_page_](08-operations.md#pages) fires. Nothing deployed is worse for a spent account, which is the bar, so it is an Inbox row and [_Work_](11-surfaces.md#inbox-work-ops-factory-people) shows the item stopped on it like anything else that is stuck. The cost is that the factory cannot see the balance it is spending against: a provider's remaining quota is not a fact the fleet entry holds, so exhaustion is discovered by arriving at it and never predicted.

## Withdrawal

A personal credential can be taken away, and an organisation's cannot in the same sense — the person who lent it leaves, revokes it, or simply stops. The factory finds out the way it finds out about a spent account, by an agent failing to reach its model, and it does the same thing.

What separates them is that this hold does not lift itself. A spent account is topped up or it is not; a withdrawn one is gone, and the fleet entry stands empty until a human attaches another credential. Neither is the reconciler's shape of unliftable hold — nothing here is decided on a record that proved wrong, and no production deploy is held — so it stays an Inbox row and earns no page for the reason above.

The prior survives it. The score's evidence is about the model, and the credential was never the author, so a fleet rebuilt on new accounts keeps every bit of its history. That is the practical half of keeping the prior per model rather than per fleet entry.

The cost is availability, and it is the price of starting before there is an organisation agreement: a fleet standing on personal accounts can be halved by one resignation, and the factory has no claim on any of it. Nothing about it is unsafe — a credential that is gone stops work rather than shipping any — but a factory whose throughput rests on goodwill is bounded by something nobody authored.

## What the fleet is not

**It is not a duty.** Supplying a credential is not work done with the factory — it is the substrate the factory runs on, like the host the owner already provides, and it stays outside the twelve for the reason standing up [_The reconciler_](08-operations.md#the-reconciler) does. Routing needs a holder rather than a duty number: the row reaches whoever People records as having lent the credential, and the owner where that is who withdrew it or where nobody has lent one yet, which is where a [_page_](08-operations.md#pages) already widens to. What it costs is that nothing scores the obligation — the availability [_Withdrawal_](#withdrawal) prices rests on goodwill, and no duty and no parameter reaches it.

**It is not gate policy.** A credential reference rides on the fleet entry the way a strategy default rides on the environment record, and [_Gate policy_](09-gate-policy.md#what-is-not-in-it) stays at its eight rows. Who lent which credential is a People declaration, enforced by nothing, like the rotation a [_page_](08-operations.md#pages) follows.

**It is not a spend ceiling.** Two of gate policy's parameters state their cost in spend and Factory reads cost per feature off what the factory spent, but neither bounds anything. What actually stops an agent on a personal account is that account's own quota, set at the provider by whoever owns it — a number the factory neither sets nor reads.
