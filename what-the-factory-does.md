# What the factory does

A **fully autonomous software factory and operations**: it refines intent, produces the software, deploys it, monitors it, finds issues, and fixes bugs, on its own. It also runs itself: it decomposes intent into items, dispatches its own agents onto them, gates its own output against a score it learns, and escalates what it cannot finish. The factory is a product — each customer runs their own isolated, self-hosted setup. There is no tenancy: no shared install, no multi-tenant model, and nothing about one customer's data that another's factory could reach. The absence is a decision, not a gap left to fill.

## Tight integration

**Tight integration is key.** One system, not a bundle of tools with connectors between them. Intent, spec, change, gate decision, deploy, incident, and score are one graph.

What one graph buys is that the questions connectors answer by reconciliation are walks instead. Which services consume a contract, so [what a change breaks](how-humans-do-it/07-contracts.md#enforcement) is a query rather than an estimate. Which items came from one request, so [work that spans services](how-humans-do-it/07-contracts.md#work-that-spans-services) needs no noun of its own. Which release each service is running, so a candidate's environment stands on the [current releases](how-humans-do-it/06-releases.md#the-number) of its dependencies.

A bundle of tools can report all of that. What one graph does is act on it, with no human carrying a fact from one system into another: what a consumer assumes is [derived from its build](how-humans-do-it/07-contracts.md#what-a-consumer-declares) rather than filed, a comparison that finds something [after its window closed](how-humans-do-it/08-operations.md#after-the-watch-window) writes an intent into the front of the pipeline, and the score learns from the same log that is [the audit trail](deferred.md).

The cost is that one graph is one trust domain. Every check the factory makes reads a record the factory wrote: [_One pipeline_](how-humans-do-it/01-one-pipeline.md) is a single blast radius in the path, and one graph is that radius over the records. What could supply a fact the factory did not write is [_Open_](open.md).

## Traceability

**Traceability is key**, and is the testable form of it: every artifact walks back to the intent that caused it and forward to what it produced, under the policy and score that were in force at the time.

Testable is what keeps it from being a discipline anyone has to keep: it holds where a record answers by being walked, and fails where an answer has to be reconstructed. Two records carry the joins — the [intent](how-humans-do-it/02-intent-into-items.md#intake) every item walks back to, and the [release record](how-humans-do-it/06-releases.md#the-release-record) holding the item, the build, the gate decisions, the contract versions, and every deploy. An [incident](how-humans-do-it/08-operations.md#incidents) is the same walk from the other end.

_In force at the time_ is the hard half, because policy and score both move: an owner re-authors [gate policy](how-humans-do-it/09-gate-policy.md), and the score is [learned from every outcome that arrives](how-humans-do-it/04-risk-score.md#how-it-learns). A decision read back against today's values is not the decision that was made, so each decision record names the policy and the score version it was decided under, beside [the actor](deferred.md) and for that field's reason — neither can be added to a history written without it.

What traceability costs is that there is nowhere to put an action that skips a record. A human who wants different code [authors it upstream](how-humans-do-it/03-gates.md#what-a-gate-may-change) rather than patching one at a gate, an emergency is [approve now rather than skip](how-humans-do-it/01-one-pipeline.md), and undoing something already shipped is a rollback while its control stands or a [revert item](how-humans-do-it/06-releases.md#rollback) after. Every shortcut is an item, and that is the price of the walk staying unbroken.
