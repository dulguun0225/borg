# Deploy to production

The default path's exception to reject, which is why [that row](../03-actions-at-each-gate.md) does not offer it. By the time a change reaches it, the merge has happened and the release number is already assigned, so hold is the only way to stop it. Once it deploys, all that is left is a human's undo of a shipped change (10) — a rollback while the build it would return to is still running, a revert after, which is a new item.

Its hold also fires on the factory's own account, like the one a rollback leaves in place. A dependency declared at decomposition that is not its service's [_current release_](../../06-releases/05-the-deploy-record/README.md) when this gate fires holds the deploy. Current means marked complete on every production target, so a producer deployed to three targets of four holds its consumer until the fourth lands. The check runs at the moment of firing, the same rule the [two contract baselines](../../07-contracts/06-what-a-consumer-declares.md) keep, because a producer that was live when its consumer verified can be rolled back before that consumer deploys. Nothing is decided and no [_page_](../../08-operations/07-pages.md) fires; the hold lifts when the dependency is current again. A human can approve through it, as through every hold the factory sets, and what approving accepts is the break the hold was preventing. The verdict names it among every hold standing, [_What a gate may change_](../04-what-a-gate-may-change.md)'s rule for all of them, which keeps this hold's approve distinguishable from the [rollback hold](../../08-operations/03-overlapping-windows.md)'s. An [error budget](../../08-operations/05-service-level-objectives.md) exhausted holds the deploy the same way and lifts itself the same way, and two items pass it rather than waiting: a revert, and an item the health monitor raised on that service. A service its owner has not marked [provisioned](../../02-intent-into-items/03-decomposition/README.md) holds it the same way too, since the store this deploy would apply a schema change to is one nobody has said exists. A service already at its [maximum concurrent kept fleets](../../09-gate-policy/03-what-is-not-in-it/01-authored-and-not-among-the-eleven.md) holds the same way. A kept fleet is [the instances of a replaced release kept while a window could return to it](../../08-operations/03-overlapping-windows.md); deploying further would tear one down that a window could still call back, so the deploy waits rather than losing that recovery, and the hold lifts as a kept fleet's window closes.

An [advisory](../../02-intent-into-items/01-intake/03-advisories.md) match holds this gate too, on the declared-dependency hold's shape: where the release's own [resolved set](../../05-environments/01-records-and-one-long-lived-branch.md) holds a package matching an advisory at or above the [advisory severity](../../09-gate-policy/01-what-is-in-it.md), nothing is decided and no page fires, and the hold lifts when the intent the advisory raised ships its clearing version. A human can approve through it the same way, and what approving accepts is the vulnerability the hold was blocking rather than the break a dependency hold blocks.

The hold [_Drift detection_](../../08-operations/08-drift-detection.md) sets is the other kind and the only one the factory sets: what the factory recorded about this service is not what is running, so nothing here can be decided on the record, and no evidence the factory can gather lifts it. That one pages, for the reason the dependency hold does not — it waits on a human and on nothing else. Approving through is still offered, and here it is the human saying the record is wrong and the deploy should proceed anyway.

Two more holds here are an owner's rather than the factory's. A [change
freeze](../../09-gate-policy/04-stopping-the-factory.md) holds this service's deploys for
the period the owner authored and lifts itself when that period passes, taking the two
exceptions the error budget hold takes and approved through the same way. A
[halt](../../09-gate-policy/04-stopping-the-factory.md) holds every service's and takes
those same two exceptions, and it is the other hold here that no evidence the factory can
gather lifts. It is also the one hold at this row no approve passes, because approving
through it would end it at a deploy gate rather than at the row that decides it ends.

A service missing [a target the deployer reaches, instances the platform can replace, a
rollback path, or an emission the health monitor can
read](../../02-intent-into-items/03-decomposition/README.md) cannot auto-pass this gate,
whatever the score computes. The measurement those fields exist for cannot be read without
them, so a human decides in their place, the same way [a resolved
factor](../../04-risk-score/01-factors-at-least.md) already does.

What it does not cover is the consumer already in production when its producer rolls back. A deploy gate stops deploys, and by then there is nothing left to stop — what catches that one is the consumer's own error rate raising an item, the same answer the factory gives to every consumer break it cannot see from the producer's side.
