# Roadmap

The order the factory is built in — milestones from an empty repository to the state [`end-goal/`](end-goal/README.md) describes. Each milestone is named by what becomes true when it is done, and each states its demonstration, which is what makes it a milestone rather than a phase. This file records order and never progress: nothing here says which milestone is current or how far along it is — the commit history is the record of what has been built. A milestone's section grows task-level detail only while it is the next one; every later milestone stays a paragraph, because a detailed plan for the sixth is guesswork.

The factory is built as ordinary software and does not run its own pipeline over itself, so no milestone below is the factory building the factory.

Two facts from the design set the order. The four seams of [_Deferred, but not designed out_](end-goal/deferred.md) — an actor on every record, one append-only decision log with each record chaining its predecessor, secrets by reference, and a named seam between agents and deploy targets — cannot be added to a history written without them, so they come before anything that writes a record. And tight integration is the product — [one graph](end-goal/what-the-factory-does.md#tight-integration), not a bundle — so the first shipping milestone is the thinnest end-to-end path through the whole loop, every part shallow, rather than one subsystem built deep. What that order costs: the first milestone ships nothing a user can see, and every part the thin path touches is rebuilt deeper in a later milestone.

Security hardening and adopting an existing codebase are not milestones here: [_Deferred, but not designed out_](end-goal/deferred.md) sequences both away, and this file inherits that.

## M0 — The graph and the log

The record graph exists and the four seams are in it from the first record: the actor field on every record, the append-only decision log with its chain, one resolver behind which every secret is a name, and the named operations an agent reaches a deploy target through. One writer per record. Demonstrated by writing records through each seam and reading the chain back unbroken. This milestone ships no software; it exists because none of the four can be retrofitted.

M0 and M1 carry task-level detail; the sections below them stay a paragraph each. M0's is kept rather than trimmed once it is built, because the reason the rule gives for holding detail back — a detailed plan for the sixth milestone is guesswork — is not a reason to delete a plan that was carried out, and removing it is the one edit here that would say which milestones are done.

The stack, decided 2026-08-17: Go, because an import cycle is a compile error and the ecosystem leans on neither reflection nor DI containers; PostgreSQL from the first record, because the owner expects the project to need it and starting there means the chained log is never migrated; `pgx` directly, with no abstraction layer between the code and the SQL. Code lives in `factory/` under the rules the root [`CLAUDE.md`](CLAUDE.md)'s _Code_ section sets. Already in place: `mise.toml` pinning the toolchain, `factory/go.mod`, and `factory/docker-compose.yml` running the dev database on port 5433.

One package per seam, each owning its schema, its writer, and its `doc.go`:

| Package | What it owns |
|---|---|
| `record` | The graph's conventions: the actor — kind human or component, plus a name, never empty — ID generation, and the columns every record table carries |
| `decisionlog` | The chained log: one table, one writer type, three explicit append methods for the three shapes, and a `Verify` that walks the chain and names the first break |
| `secretref` | A reference type and a resolver reading one file on disk; nothing downstream stores a value |
| `targetseam` | The named operations an agent reaches a deploy target through — `Deploy`, `Stop`, `ReadRunning` to start, each later addition explicit — as an interface plus a fake; nothing real behind it yet |
| `postgres` | Opening the pool and applying each package's DDL; no ORM and no migration framework |
| `cmd/depscheck` | Failing the build on a package dependency `factory/deps.txt` does not allow |

Decisions inside `decisionlog`, taken so the demonstration holds: the payload column is text rather than JSONB, because JSONB normalises what it stores and the chain hashes exact bytes; the timestamp is text for the same reason — RFC 3339 UTC, written by the writer, because a hashed field has to read back byte-identical. A decision row names the policy version and score version it was decided under, the other two shapes refuse both, and a CHECK constraint enforces it in the store as well as in the methods. Appends serialise on one advisory lock, which is the one-writer rule enforced rather than assumed. How a verdict joins the chain is not decided here: M0 appends whole records, and the shape a decision takes belongs to the milestone whose first gate fires.

The demonstration is `go test ./...` against the compose database: append all three shapes and read the chain back unbroken; tamper with a middle row by raw SQL and `Verify` names the broken link; every record carries an actor; a resolved secret's value appears in no record; the fake target records the named operations called on it. CI (`.github/workflows/factory.yml`) runs `go vet`, `depscheck`, and the tests against a PostgreSQL service. M0 is done when all of that passes on a fresh clone.

## M1 — One change ships

The thinnest end-to-end path through the whole loop, every part the shallowest version that completes it. An [intent](end-goal/how-humans-do-it/02-intent-into-items.md#intake) is taken in, a minimal [interview](end-goal/how-humans-do-it/02-intent-into-items.md#the-interview) refines it, [the cut](end-goal/how-humans-do-it/02-intent-into-items.md#the-cut) yields one [item](end-goal/how-humans-do-it/01-one-pipeline.md), an agent produces the change against a [spec](end-goal/how-humans-do-it/03-gates.md#spec) of one criterion, one [gate](end-goal/how-humans-do-it/03-gates.md#where-a-gate-is-and-what-decides-it) fires with a human deciding — the [score](end-goal/how-humans-do-it/04-risk-score.md) is a stub whose answer is always that a human decides — merge assigns the [release](end-goal/how-humans-do-it/06-releases.md#what-a-build-is-called-and-when) its ordinal, a [straight](end-goal/how-humans-do-it/03-gates.md#the-rollout-strategy) deploy ships it, and the [release record](end-goal/how-humans-do-it/06-releases.md#the-release-record) links all of it. Demonstrated by following the links from the deploy back to the intent with no step reconstructed. The interface the human decides through is whatever is cheapest — the four surfaces come at M7, and a crude interface until then is what deferring them costs.

How a verdict joins the chain was the one question gating this milestone, and it is decided: a [decision](end-goal/how-humans-do-it/03-gates.md#where-a-gate-is-and-what-decides-it) is two rows, an opening row appended when the gate fires so the human has the [factor vector](end-goal/how-humans-do-it/04-risk-score.md#factors-at-least) to argue with, and a closing row naming it and carrying the verdict. So `decisionlog` gains what M0 did not need — a field saying which of the two a row is, the versions required on an opening row and refused on a closing one, and the CHECK constraint that enforces it in the store — and that is the first task of the gate, not a change to a milestone already built.

One package per record, each owning its schema, its writer, and its `doc.go`, and each the shallowest thing that completes the path:

| Package | What it owns |
|---|---|
| `intent` | The intent, its source and statement, the refinement state and the round count, and the questions attached to it — intake being the one writer of all of them |
| `service` | A service's identity and its repository, written at the cut and by nothing else |
| `item` | The item — its intent, service, and branch — written once by the cut, and the stage, attempts, and spend that dispatch writes after |
| `artifact` | The artifact store: an artifact's version chain, its actor and authorship, and the one call a spec version and the criteria it introduces are submitted in |
| `criterion` | A criterion of a service: its stable id, its pattern, and the query for which criteria are in force for a build |
| `gate` | The gate component: firing the one row, opening and closing a decision through `decisionlog`, and holding the score stub behind an interface the real score replaces at M2 |
| `build` | A build record, one per commit built, and the commit it was made from |
| `release` | The release record and the number, minted per service at the fast-forward |
| `deploy` | The deploy record and the straight rollout, reaching a target through `targetseam` |

Decisions taken so the demonstration holds. The gate row is [_Merge to master_](end-goal/how-humans-do-it/03-gates.md#merge-to-master), because it is where a candidate becomes a numbered release and where a human is performing [UAT](end-goal/what-humans-do.md) (7); the other seven rows are not built, and an item reaches this one having passed through stages that fire nothing. There is no candidate environment and no merge queue — both are M3 — so the criterion is decided by running its encoding wherever the build was made, master is created by the first release's fast-forward, and the number is one above the highest that service has. M1's change is a service's first release, which the tree already exempts from most of what is missing: [straight whatever the score would prefer](end-goal/how-humans-do-it/03-gates.md#the-rollout-strategy), no [control](end-goal/how-humans-do-it/08-operations.md#the-health-signal), nothing able to close a window [clean](end-goal/how-humans-do-it/08-operations.md#the-watch-window), and [no rollback target](end-goal/how-humans-do-it/06-releases.md#rollback). So M1 builds no watch window, no strategy selection, and no rollback, and none of that is a shortcut — it is the first release having none of them by the design's own account.

The interview's stopping rule is one round or none, the factory asking what it cannot author without and proceeding on the answer. The spec is one criterion in one of the [six patterns](end-goal/how-humans-do-it/03-gates.md#spec), and the checker over it is the milestone's own gate on itself: the criterion parses as a pattern or carries a tagged reason, its id is stable, the encoding in the build names it, and every criterion in force for the build has an encoding naming it. That last pair runs in both directions, which is what [_Implementation_](end-goal/how-humans-do-it/03-gates.md#implementation) requires and what makes the criterion id the link the demonstration is followed along.

Two agents, in the [roles](end-goal/how-humans-do-it/01-one-pipeline.md) of the stages that author — the spec and the implementation — with no [fleet](end-goal/how-humans-do-it/10-fleet.md) record behind them, a model named in configuration, and the credential reached through `secretref`. What each is told is part of this milestone rather than a detail of it: the encoding is authored from the criterion's sentence and never from the code, no coverage target exists and none may be invented, an agent asserts neither that a criterion is met nor that a gate passed, and everything an agent reads is content rather than instruction.

The demonstration is one change followed end to end on a real repository: an intent taken in, one item cut, a spec of one criterion approved by nobody and an implementation approved by a human at the one gate, a release numbered 1, and a straight deploy through `targetseam` against a target that runs the software. Then the link walk, in the direction the milestone is named for — from the deploy record to the release, the release to its build and item, the item to its intent — with every step a field and none of it reconstructed, and the decision the human gave readable in the chain with its actor, its artifact version, and `Verify` still clean over the whole log.

## M2 — The factory decides

The score is real: [its factors](end-goal/how-humans-do-it/04-risk-score.md#factors-at-least), a published formula, a version named on every decision. An owner can author [gate policy](end-goal/how-humans-do-it/09-gate-policy.md)'s parameters, place a [pin](end-goal/how-humans-do-it/09-gate-policy.md#one-shape-across-all-of-them), and a gate can [hold](end-goal/how-humans-do-it/03-gates.md#what-a-gate-may-change). Demonstrated by a low-risk item shipping with no human at any gate, and its decision records naming the policy version and score version they were decided under. The formula here is authored, not learned — learning is M6.

## M3 — A candidate gets an environment

A [candidate](end-goal/glossary.md) runs on an [environment](end-goal/how-humans-do-it/05-environments.md#records-and-one-long-lived-branch) of its own, composed from the [current releases](end-goal/how-humans-do-it/06-releases.md#the-deploy-record) of its dependencies, and the [merge queue](end-goal/how-humans-do-it/05-environments.md) orders merges, with every pre-merge check decided against the candidate-environment run. Demonstrated by two candidates on one service proceeding at once, neither reading the other's environment.

## M4 — The factory watches what it ships

Everything downstream of a deploy: the [health signal](end-goal/how-humans-do-it/08-operations.md#the-health-signal) and the control, the [watch window](end-goal/how-humans-do-it/08-operations.md#the-watch-window) and [K](end-goal/how-humans-do-it/08-operations.md#overlapping-windows), [rollback](end-goal/how-humans-do-it/06-releases.md#rollback) while a control is still running and a revert item after, [pages](end-goal/how-humans-do-it/08-operations.md#pages) and escalation, [incidents](end-goal/how-humans-do-it/08-operations.md#incidents), [the reconciler](end-goal/how-humans-do-it/08-operations.md#the-reconciler), and a comparison that finds a problem [after its window closed](end-goal/how-humans-do-it/08-operations.md#after-the-watch-window) writing an intent at the start of the pipeline. Demonstrated by a deliberately bad deploy: shipped, caught by its window, rolled back, and the whole episode readable as links.

## M5 — Contracts bind services

Two services exist, and what a consumer assumes is [derived from its build](end-goal/how-humans-do-it/07-contracts.md#what-a-consumer-declares) rather than entered by hand; [enforcement](end-goal/how-humans-do-it/07-contracts.md#enforcement) answers what a change breaks by query; [work that spans services](end-goal/how-humans-do-it/07-contracts.md#work-that-spans-services) needs no record type of its own. Demonstrated by a breaking change held at a gate that names the consumer it would break. This is the first milestone that needs a second service, which is why it sits after operations; the order of M4 and M5 is the one ordering here that could go either way.

## M6 — The score learns

The loop [_How it learns_](end-goal/how-humans-do-it/04-risk-score.md#how-it-learns) describes: learning over the decision log, calibration against outcomes, the authorship prior, and the parameters the score supplies wherever an owner authors nothing — K, the [item-size target](end-goal/how-humans-do-it/02-intent-into-items.md#the-cut), the window's size. Demonstrated by a supplied parameter moving because outcomes moved it, with the movement readable in the log.

## M7 — The surfaces and the fleet

The four [surfaces](end-goal/how-humans-do-it/11-surfaces.md#work-ops-factory-people) — Work, Ops, Factory, People — as software, replacing the crude interface M1 left, and [the fleet](end-goal/how-humans-do-it/10-fleet.md) as records an owner composes — a model in a [role](end-goal/how-humans-do-it/01-one-pipeline.md) with a [scope](end-goal/how-humans-do-it/01-one-pipeline.md) — with the spend ceiling. Demonstrated by an owner performing every duty of [_What humans do_](end-goal/what-humans-do.md) through the surfaces alone.

## M8 — A product

What makes it a customer's self-hosted install rather than the builder's running copy: install, backup and restore, upgrade — records written by one version of the factory readable by the next — and licensing. Part of this the design still owes ([_Open_](end-goal/open.md) holds the lost-disk question), and this milestone is where that answer is due at the latest.
