# Open

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
