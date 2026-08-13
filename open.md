# Open

## Should the Decomposition gate fan out?

One intent can land items in four services with four different holders, and the gate takes one verdict on the whole cut. Fanning it out — one verdict per service touched, each holder reading the whole intent and deciding only the items that land in theirs — is where a service's owner would get to say "not in ours, not that way" before anything is authored. What it costs is the shape [_Who owns a contract_](how-humans-do-it/07-contracts.md#who-owns-a-contract) refused once already: a producer-side block is not the consumer veto that section rejected, but four approvals standing at one gate rebuild the same wait, and the attempt bound turns a long enough wait into escalations (12).

Three things settle with it, and none is obvious. Whether an approval survives a re-cut that left that holder's slice untouched, or everyone decides again. Who takes the row where a service has no declared holder, People being declared and not enforced. And what bounds a holder who never answers — a silent one is closer to a hold than to a failed attempt, so the bound does not reach them.

## What bounds the interview?

The factory ends the interview when it has enough to author, so an intent an owner has stopped answering for waits indefinitely — the one wait in the factory that no bound touches. The attempt bound does not reach it: a question nobody answered is not a failed attempt, the same reason a hold is not one. What could bound it is a count of rounds, an elapsed time, or the factory drafting on what it has and letting the Spec gate catch the thinness. The first two turn an unanswered question into an escalation (12) an owner has to clear anyway; the third spends a full pipeline run to ask the question again in the shape of a bad spec.

## What sizes an item?

"It can ship by itself" is a floor, not a size. It admits an item that touches forty files and one that changes a string, and nothing in the cut says which the factory should prefer. Cutting small pays per item — an environment, a spec, four gates, a release number — and cutting large raises the odds of the attempt bound turning the item into an escalation (12), with everything already spent on it thrown away. The score reads size as a factor at every gate below the cut, so the cost of a bad size is visible after the fact; what is missing is the basis for choosing one before.

## Is a design system a contract, or only a standing constraint?

A design system the factory builds is a published thing with consumers inside the factory, and a token renamed or a spacing scale rescaled breaks them the way a schema change breaks a caller. That is the contract machinery exactly — a compatibility mode, a breaking diff caught at the merge gate, the three items of a migration, an old form carrying its own deprecation list. Calling it a contract costs one stretched word: _Two versioned things_ scopes consumers to other services and _Who owns a contract_ gives a contract to the service that publishes it, and a package of tokens is not obviously either. Leaving it a constraint (2) costs the enforcement — the factory checks nothing, and one token change breaks forty screens with no gate standing in front of it.

A design system supplied as a document rather than as code is the constraint case whichever way this settles, because there is no build to diff. What that in turn costs, and whether an owner should be pushed to supply code instead, is open with it.

## What bounds K?

Overlapping watch windows buy throughput on the high-frequency service and pay in how much a rollback takes with it. K = 1 is the serial factory and gives back the queue the built control was meant to dissolve; a large K makes a rollback a bundle in the one place the factory otherwise refuses one, and the size of that bundle is the number itself. Nothing in the document supplies a basis to pick it — the throughput gained is a property of the service's change rate and the loss is a property of how often a rollback fires, and those are not the same evidence.

Who picks it is settled and does not close this. An owner authors K and the score supplies it where they author nothing, so the missing basis is owed twice: once to the owner typing a number, once to the default standing in for them.

## What checks the factory against a fact it did not write?

Every check the factory makes reads a record the factory wrote. A service's [_current release_](how-humans-do-it/06-releases.md#the-number) is what its production deploy record names, the [_trust number_](how-humans-do-it/09-surfaces.md#the-trust-number) learns from the log the scored machine writes, and detection after a deploy that should not have happened rests on that same log. So a factory whose records are wrong reports itself healthy, and nothing in the document contradicts it. The seams in [_Deferred, but not designed out_](deferred.md) are what a correct factory is built from rather than a check on an incorrect one.

Three things could supply the outside fact, and each costs something not yet agreed to. Anchoring the log's chain head where the factory cannot rewrite it is nearly free in the record, but the product is self-hosted and single-tenant, so "outside" is the owner's own side and holding that head is a duty the twelve do not include. A reconciler outside the pipeline — reading what is actually running and comparing it to the record — is the direct answer, and the only candidate that does not share the factory's trust domain, but a second deployment with its own privilege is not a field on a record and fails the cheap-now test that admits a seam at all. Target-side attestation on the named operations of the agent-to-deploy seam spends that seam's "however it is implemented" early and lands the same custody duty anyway.

What turns on it is whether the human is the whole answer. Veto after the fact (10) is the one judgment the factory does not author, so it is a second path — but only if at least one fact the human reads is not self-reported, and today none is.
