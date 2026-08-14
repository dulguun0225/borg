# The fleet

The **fleet** is the set of agents the factory runs. [_One pipeline_](01-one-pipeline.md) says what one is — a model in a role, with a scope — and that the authorship prior is kept per model rather than per role. What is left is what the model runs on: an account at a provider, reached through a credential, owned by somebody.

## What an agent runs on

An agent reaches its model through a **provider account**, and the factory stores a reference to that account's credential rather than the credential itself — seam 3 of [_Deferred, but not designed out_](../deferred.md), which the fleet is the first thing to use. The resolver is a file on disk today. No provider is named anywhere in the factory: a fleet entry has a model, a role, a scope, and the name of a credential.

The account may be an organisation's or a person's own, and the factory takes either, because a reference resolves the same way whichever it is. The early factory is where that matters. An owner installing one before it has proved anything has a personal subscription and no organisation agreement to spend against, and a design that made them wait for procurement in order to watch the factory run once would impose the proven case's requirements on the unproven one. So a personal account attaches through no second mechanism: same field, same resolver, same record.

What differs is downstream of the account and not in the factory at all — its quota, who may revoke it, and whose money it spends. Three things follow, and each is a drawback of the personal account rather than a feature of it: whose name ends up on the work, what an exhausted account does, and what happens when one is taken away.

## Paid for is not authored by

Seam 1 of [_Deferred, but not designed out_](../deferred.md) puts an actor on every record, and an artifact's author is the model that wrote it. Whose account paid for the tokens is a third fact, recorded on the fleet entry and not on the artifact. A human who lends the factory their subscription has authored nothing.

Combining the two is the failure worth naming, because a human's name is in the record either way. The [_authorship prior_](04-risk-score.md#factors-at-least) is per model and stays per model: two fleet entries on one model share one prior however many accounts they run on, and moving an entry from a personal account to an organisation's resets nothing. A prior that moved with the credential would make a model build up its evidence twice and attach a machine's record to a person.

## An account that runs out is a hold

A personal account has a quota an organisation's agreement usually does not, so the fleet has a failure nothing else in the factory has: an agent stops part-way through a stage because the account it runs on is exhausted.

That is not the model failing, and it is a **hold**. The stage waits and resumes when there is a credential to reach, counting nothing against [_the attempt bound_](03-gates.md#the-attempt-bound) and teaching the prior nothing — the score learns from a reject and not from a hold, which [_What a gate may change_](03-gates.md#what-a-gate-may-change) settles for gates and [_The watch window_](08-operations.md#the-watch-window) keeps when it counts only a rollback traceable to the health signal as evidence. How good an author is has to be measured from that author's work.

No [_page_](08-operations.md#pages) fires. Nothing deployed is worse for an exhausted account, which is the condition, so it waits in [_Work_](11-surfaces.md#work-ops-factory-people), which shows the item stopped on it like anything else that is stuck. The drawback is that the factory cannot see the balance it is spending against: a provider's remaining quota is not a field on the fleet entry, so exhaustion is discovered by reaching it and never predicted.

## Withdrawal

A personal credential can be taken away, and an organisation's cannot in the same sense — the person who lent it leaves, revokes it, or simply stops. The factory finds out the way it finds out about an exhausted account, by an agent failing to reach its model, and it does the same thing.

What separates them is that this hold does not lift itself. An exhausted account is topped up or it is not; a withdrawn one is gone, and the fleet entry has no credential until a human attaches another. Neither is the reconciler's kind of unliftable hold — nothing here is decided on a record that proved wrong, and no production deploy is held — so it stays a row in Work and gets no page for the reason above.

The prior is unaffected. The score's evidence is about the model, and the credential was never the author, so a fleet rebuilt on new accounts keeps every bit of its history. That is the practical half of keeping the prior per model rather than per fleet entry.

The drawback is availability, and it is what starting before there is an organisation agreement costs: a fleet running on personal accounts can be halved by one resignation, and the factory has no claim on any of it. Nothing about it is unsafe — a credential that is gone stops work rather than shipping any — but a factory whose throughput depends on goodwill is limited by something nobody authored.

## What the fleet is not

**It is not a duty.** Supplying a credential is not work done with the factory — it is the substrate the factory runs on, like the host the owner already provides, and it stays outside the twelve for the same reason installing [_The reconciler_](08-operations.md#the-reconciler) does. Routing needs a named human rather than a duty number: the row reaches whoever [_People_](11-surfaces.md#work-ops-factory-people) records as having lent the credential, and the owner where that person is the one who withdrew it or where nobody has lent one yet, which is where a [_page_](08-operations.md#pages) already widens to. The drawback is that nothing scores the obligation — the availability [_Withdrawal_](#withdrawal) describes depends on goodwill, and no duty and no parameter reaches it.

**It is not gate policy.** A credential reference is a field on the fleet entry the way a strategy default is a field on the environment record, and [_Gate policy_](09-gate-policy.md#what-is-not-in-it) stays at its seven rows. Who lent which credential is a People declaration, enforced by nothing, like the duties a [_page_](08-operations.md#pages) routes on.

**It is not a spend ceiling.** Gate policy's attempt bound states its cost in spend and [_Factory_](11-surfaces.md#work-ops-factory-people) computes cost per feature from what the factory spent, but neither limits anything. What actually stops an agent on a personal account is that account's own quota, set at the provider by whoever owns it — a number the factory neither sets nor reads.

That is a decision and not an omission, and two arguments support it. A ceiling the factory enforced would stop by reaching a limit it never saw, because the fleet stores a reference and not a balance — [_An account that runs out is a hold_](#an-account-that-runs-out-is-a-hold) states that blindness already, and a ceiling built on it is the quota again, one trust domain further from the money that pays. And it fails two of the three claims [_One shape across all of them_](09-gate-policy.md#one-shape-across-all-of-them) makes of a parameter: nothing in the score's evidence teaches its default, since the score learns from vetoes and rollbacks while a ceiling's evidence is cost per feature, and scope has three candidates limiting different things — an entry, a stage, an area — where every other parameter has the one its mechanism picks.

What refusing it costs is a number on the factory's own screens that nothing in the factory limits, and it shows where [_Factory_](11-surfaces.md#work-ops-factory-people) reports cost per feature beside the fleet entry naming the account: the control is at the provider, and an owner who wants a tighter one sets it there. The only thing between an item going in circles and unbounded cost is [_the attempt bound_](03-gates.md#the-attempt-bound), which counts attempts and not money — so what an attempt costs still varies with the model behind it and the size of the item, and no parameter in the tree reaches that.
