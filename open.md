# Open

## What checks the factory against a fact it did not write?

Every check the factory makes reads a record the factory wrote. A service's [_current release_](how-humans-do-it/06-releases.md#the-number) is what its production deploy record names, the [_trust number_](how-humans-do-it/09-surfaces.md#the-trust-number) learns from the log the scored machine writes, and detection after a deploy that should not have happened rests on that same log. So a factory whose records are wrong reports itself healthy, and nothing in the document contradicts it. The seams in [_Deferred, but not designed out_](deferred.md) are what a correct factory is built from rather than a check on an incorrect one.

Three things could supply the outside fact, and each costs something not yet agreed to. Anchoring the log's chain head where the factory cannot rewrite it is nearly free in the record, but the product is self-hosted and single-tenant, so "outside" is the owner's own side and holding that head is a duty the twelve do not include. A reconciler outside the pipeline — reading what is actually running and comparing it to the record — is the direct answer, and the only candidate that does not share the factory's trust domain, but a second deployment with its own privilege is not a field on a record and fails the cheap-now test that admits a seam at all. Target-side attestation on the named operations of the agent-to-deploy seam spends that seam's "however it is implemented" early and lands the same custody duty anyway.

What turns on it is whether the human is the whole answer. Veto after the fact (10) is the one judgment the factory does not author, so it is a second path — but only if at least one fact the human reads is not self-reported, and today none is.
