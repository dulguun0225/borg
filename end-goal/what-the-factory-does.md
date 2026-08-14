# What the factory does

A **fully autonomous software factory and operations**: it refines intent, produces the software, deploys it, monitors it, finds issues, and fixes bugs, on its own. It also runs itself: it decomposes intent into items, dispatches its own agents onto them, gates its own output against a score it learns, and escalates what it cannot finish. The factory is a product — each customer runs their own isolated, self-hosted setup. There is no tenancy: no shared install, no multi-tenant model, and nothing about one customer's data that another's factory could reach. The absence is a decision, not a gap left to fill.

## Tight integration

**Tight integration is key.** One system, not a bundle of tools with connectors between them. Intent, spec, change, gate decision, deploy, incident, and score are records in one graph, linked to each other.

Because they are linked, a question that connectors answer by matching records across systems is answered here by following links. Which services consume a contract, so [what a change breaks](how-humans-do-it/07-contracts.md#enforcement) is a query rather than an estimate. Which items came from one request, so [work that spans services](how-humans-do-it/07-contracts.md#work-that-spans-services) needs no record type of its own. Which release each service is running, so a candidate's environment is composed from the [current releases](how-humans-do-it/06-releases.md#the-number) of its dependencies.

A bundle of tools can report all of that. What one graph does is act on it, with no human copying a fact from one system into another: what a consumer assumes is [derived from its build](how-humans-do-it/07-contracts.md#what-a-consumer-declares) rather than entered by hand, a comparison that finds a problem [after its window closed](how-humans-do-it/08-operations.md#after-the-watch-window) writes an intent at the start of the pipeline, and the score learns from the same log that serves as [the audit trail](deferred.md).

The downside is that one graph is one trust domain. Almost every check the factory makes reads a record the factory wrote, so a record that is wrong is read as true by every check downstream of it. [_One pipeline_](how-humans-do-it/01-one-pipeline.md) accepts the same exposure for the path — a defect in it reaches every item that goes through — and one graph is that exposure over the records. The one exception is [_The reconciler_](how-humans-do-it/08-operations.md#the-reconciler), which reads what is actually running rather than what was recorded, and compares the two.

## Traceability

**Traceability is key**, and is the testable form of it: every artifact links back to the intent that caused it and forward to what it produced, under the policy and score that were in force at the time.

Testable is what keeps it from being a discipline anyone has to keep: it holds where a question is answered by following links, and fails where an answer has to be reconstructed. Two records supply those links — the [intent](how-humans-do-it/02-intent-into-items.md#intake) every item points back to, and the [release record](how-humans-do-it/06-releases.md#the-release-record) linking the item, the build, the gate decisions, the contract versions, and every deploy. An [incident](how-humans-do-it/08-operations.md#incidents) is the same set of links followed from the other end.

_In force at the time_ is the harder part, because policy and score both change: an owner re-authors [gate policy](how-humans-do-it/09-gate-policy.md), and the score is [learned from every outcome that arrives](how-humans-do-it/04-risk-score.md#how-it-learns). A decision read back against today's values is not the decision that was made, so each decision record names the policy version and the score version it was decided under, next to [the actor](deferred.md) and for the same reason — no such field can be added to a history written without it.

What traceability rules out is any action that skips a record. A human who wants different code [authors it upstream](how-humans-do-it/03-gates.md#what-a-gate-may-change) rather than patching one at a gate, an emergency is [approve now rather than skip](how-humans-do-it/01-one-pipeline.md), and undoing something already shipped is a rollback while its control is still running or a [revert item](how-humans-do-it/06-releases.md#rollback) after. Every shortcut is an item, and that is what keeps the links unbroken.
