# Roadmap

The order the factory is built in — milestones from an empty repository to the state [`end-goal/`](end-goal/README.md) describes. Each milestone is named by what becomes true when it is done, and each states its demonstration, which is what makes it a milestone rather than a phase. This file records order and never progress: nothing here says which milestone is current or how far along it is — the commit history is the record of what has been built. A milestone's section grows task-level detail only while it is the next one; every later milestone stays a paragraph, because a detailed plan for the sixth is guesswork.

The factory is built as ordinary software and does not run its own pipeline over itself, so no milestone below is the factory building the factory.

Two facts from the design set the order. The four seams of [_Deferred, but not designed out_](end-goal/deferred.md) — an actor on every record, one append-only decision log with each record chaining its predecessor, secrets by reference, and a named seam between agents and deploy targets — cannot be added to a history written without them, so they come before anything that writes a record. And tight integration is the product — [one graph](end-goal/what-the-factory-does.md#tight-integration), not a bundle — so the first shipping milestone is the thinnest end-to-end path through the whole loop, every part shallow, rather than one subsystem built deep. What that order costs: the first milestone ships nothing a user can see, and every part the thin path touches is rebuilt deeper in a later milestone.

Security hardening and adopting an existing codebase are not milestones here: [_Deferred, but not designed out_](end-goal/deferred.md) sequences both away, and this file inherits that.

## M0 — The graph and the log

The record graph exists and the four seams are in it from the first record: the actor field on every record, the append-only decision log with its chain, one resolver behind which every secret is a name, and the named operations an agent reaches a deploy target through. One writer per record. Demonstrated by writing records through each seam and reading the chain back unbroken. This milestone ships no software; it exists because none of the four can be retrofitted.

M0 is the next milestone, so its section carries the task-level detail; the sections below it stay a paragraph each.

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

Decisions inside `decisionlog`, taken so the demonstration holds: the payload column is text rather than JSONB, because JSONB normalises what it stores and the chain hashes exact bytes; the timestamp is text for the same reason — RFC 3339 UTC, written by the writer, because a hashed field has to read back byte-identical. A decision row names the policy version and score version it was decided under, the other two shapes refuse both, and a CHECK constraint enforces it in the store as well as in the methods. Appends serialise on one advisory lock, which is the one-writer rule enforced rather than assumed. The question [_Open_](end-goal/open.md) holds — how a verdict joins the chain — is not decided here: M0 appends whole records, and the decision's two-write shape stays open for the owner.

The demonstration is `go test ./...` against the compose database: append all three shapes and read the chain back unbroken; tamper with a middle row by raw SQL and `Verify` names the broken link; every record carries an actor; a resolved secret's value appears in no record; the fake target records the named operations called on it. CI (`.github/workflows/factory.yml`) runs `go vet`, `depscheck`, and the tests against a PostgreSQL service. M0 is done when all of that passes on a fresh clone.

## M1 — One change ships

The thinnest end-to-end path through the whole loop, every part the shallowest version that completes it. An [intent](end-goal/how-humans-do-it/02-intent-into-items.md#intake) is taken in, a minimal [interview](end-goal/how-humans-do-it/02-intent-into-items.md#the-interview) refines it, [the cut](end-goal/how-humans-do-it/02-intent-into-items.md#the-cut) yields one [item](end-goal/how-humans-do-it/01-one-pipeline.md), an agent produces the change against a [spec](end-goal/how-humans-do-it/03-gates.md#spec) of one criterion, one [gate](end-goal/how-humans-do-it/03-gates.md#where-a-gate-is-and-what-decides-it) fires with a human deciding — the [score](end-goal/how-humans-do-it/04-risk-score.md) is a stub whose answer is always that a human decides — merge assigns the [release](end-goal/how-humans-do-it/06-releases.md#what-a-build-is-called-and-when) its ordinal, a [straight](end-goal/how-humans-do-it/03-gates.md#the-rollout-strategy) deploy ships it, and the [release record](end-goal/how-humans-do-it/06-releases.md#the-release-record) links all of it. Demonstrated by following the links from the deploy back to the intent with no step reconstructed. The interface the human decides through is whatever is cheapest — the four surfaces come at M7, and a crude interface until then is what deferring them costs.

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
