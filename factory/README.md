# factory

The factory's code. What it is for and what it is built toward are in [`../end-goal/`](../end-goal/README.md); the order it is built in is [`../roadmap.md`](../roadmap.md); the rules it is written under are the _Code_ section of [`../CLAUDE.md`](../CLAUDE.md#code). This file is the map those rules require to ship with the code: every package, what it owns, the edges between them, and how to run the thing.

What exists is milestone M0, [_The graph and the log_](../roadmap.md#m0--the-graph-and-the-log), plus milestone M1, [_One change ships_](../roadmap.md#m1--one-change-ships). M0 put the four seams of [_Deferred, but not designed out_](../end-goal/deferred.md#security-comes-last) into the record graph from the first record — an actor on every record, one append-only decision log whose rows chain their predecessors, secrets by reference, and a named seam between agents and deploy targets — and shipped no pipeline, no gate, no agent, and no user-facing anything. M1 built the thinnest path through the whole loop on top of that graph: an intent taken in, an item cut, a spec of one criterion authored by an agent, an implementation authored by another, the Merge to master gate deciding with a human, a release minted, and a straight deploy against a target that runs the software — one change followed end to end, with `cmd/factory` as the crude interface it runs through until the four surfaces of M7 replace it.

## The packages

| Package | What it owns |
|---|---|
| [`record`](record) | The graph's conventions: the actor — kind `human` or `component`, plus a name, neither empty — identifier generation, the timestamp format, and the SQL text every record table composes for its columns and their constraints. It owns no table. |
| [`decisionlog`](decisionlog) | The chained log: one table, one writer with four append methods — a decision is two rows, an opening row appended when a gate fires and a closing row naming it and carrying the verdict, plus one method each for a page event and a wait — and, as functions over the pool rather than methods on the writer, `Read` and a `Verify` that walks the chain and names the first row that breaks it. Reading the log is not a reason to hold the thing that appends to it. |
| [`secretref`](secretref) | The reference type every other package uses in place of a secret, and the one resolver that reads a value. A `Ref` has one field and it is the name, so nothing that renders one can render a value. |
| [`targetseam`](targetseam) | The named operations an agent reaches a deploy target through — `Deploy`, `Stop`, `ReadRunning` — as an interface. `Fake` records what was called on it and reaches nothing; `localtarget` is the implementation the M1 demonstration deploys against. |
| [`postgres`](postgres) | Opening the pool, and applying each package's DDL from one ordered list written in the source. No ORM, no migration framework. |
| [`cmd/depscheck`](cmd/depscheck) | Failing the build on an import between two packages of this module that [`deps.txt`](deps.txt) does not allow. |
| [`intent`](intent) | The intent — its source, its statement, its refinement state, its round count — and the questions attached to it, `Intake` being the one writer of both. |
| [`service`](service) | A service's identity and its repository, written at the cut and by nothing else. |
| [`item`](item) | The item — its intent, service, and branch — written once by the cut, and the stage, attempts, and spend `Dispatch` writes after. |
| [`artifact`](artifact) | The artifact store: an artifact's version chain, its authorship, and the one call a spec version and the criteria it introduces are submitted in. |
| [`criterion`](criterion) | A criterion of a service: its stable id, its pattern, the query for which criteria are in force for a build, and the encoding checks that tie a criterion to the test code deciding it. |
| [`gate`](gate) | The gate component: firing the one row M1 builds — Merge to master — and opening and closing a decision through `decisionlog`, behind a `Score` interface the stub always answers "a human decides." |
| [`build`](build) | A build record, one per commit built, naming the item it was built for and the commit it was made from. |
| [`release`](release) | The release record and the number, minted per service at the fast-forward. |
| [`deploy`](deploy) | The deploy record and the straight rollout, reaching a target through `targetseam`. |
| [`agent`](agent) | The two authoring roles — `SpecAuthor`, `Implementer` — the `Model` interface both call, and `Anthropic`, the one implementation, reaching its credential through `secretref`. |
| [`localtarget`](localtarget) | A `targetseam.Target` that runs the software as a local process, one per service — the implementation M1's demonstration deploys against. |
| [`cmd/factory`](cmd/factory) | The crude interface: `run` walks the whole path once and `walk <deploy-id>` follows the links back. It owns no table of its own; every record it causes to exist is written by the package that owns it. |

Each package's `doc.go` says what it owns, who may write what, and which `end-goal/` section defines the concept it implements. Read that before the code.

## The allowed edges

```
record
decisionlog -> record
secretref
targetseam -> secretref
intent -> record
service -> record
item -> record
criterion -> record
artifact -> criterion record
build -> record
release -> record
deploy -> record secretref targetseam
gate -> decisionlog record
agent -> secretref
localtarget -> secretref targetseam
postgres -> artifact build criterion decisionlog deploy intent item release service
cmd/depscheck
cmd/factory -> agent artifact build criterion decisionlog deploy gate intent item
                localtarget postgres record release secretref service targetseam
```

An arrow reads as "imports": `decisionlog -> record` is `decisionlog` importing `record`. `record`, `secretref`, and `cmd/depscheck` import nothing inside this module. Two edges deserve their reason stated: `artifact -> criterion` is the one import between two record packages, because the artifact store is the criterion's one writer, so a spec version and the criteria it introduces are written in one transaction; `cmd/factory` importing nearly everything is because it is the crude interface M1 defers the four surfaces with, the one place the whole path is composed. Every record package that owns a table has a database test in its external test package importing `postgres` to open the pool and apply the schema, an edge [`deps.txt`](deps.txt) states once per package as `test <package> -> postgres`; `decisionlog`'s also names `secretref`, for the one test that resolves a secret and writes to the log at once.

[`deps.txt`](deps.txt) is that list, and `cmd/depscheck` is what makes it binding. A package the file does not list is an error, an edge it does not allow is an error, and a line naming a package that does not exist is an error. The compiler already refuses a cycle; this refuses an edge that would compile.

## Running it

The toolchain is pinned by [`../mise.toml`](../mise.toml). The dev database is [`docker-compose.yml`](docker-compose.yml), on port 5433 so a PostgreSQL installed on the machine keeps 5432.

```sh
docker compose up -d           # the database
go vet ./...
go run ./cmd/depscheck
go test -count=1 ./...
```

The database tests read `DATABASE_URL` and fall back to `postgres://factory:factory@localhost:5433/factory`. They do not skip when the database is unreachable — a silent skip is how a green run comes to mean nothing — so an unreachable database fails them. `-count=1` is what keeps that promise: `go test` caches a result against the test binary and what the test read, and a database is neither, so a re-run with the database stopped reports the cached `ok` without opening a socket. Each database test creates a PostgreSQL schema of its own, applies the DDL inside it, and drops it when it ends, so a rerun on a database a previous run left dirty starts clean.

[`../.github/workflows/factory.yml`](../.github/workflows/factory.yml) runs the same three commands against a `postgres:17` service container.

M1's demonstration is the end-to-end test in `cmd/factory`, and it runs under `go test` above along with everything else — no separate step is needed to see it pass. To walk the path against a real model and a real process, run it directly:

```sh
go run ./cmd/factory run -secrets <file> -model <name> -repo <dir> -service <name> -targets <dir>
go run ./cmd/factory walk <deploy-id>
```

`run` reads two secrets from `<file>` by the names `model.anthropic`, the Anthropic API key the model call resolves and sends in a header, and `deploy.local`, the credential `targetseam` requires on every operation and `localtarget` never reads. `-repo` is the service's git repository, created when absent; `-targets` is the directory `localtarget` runs releases from. `-human` names the deciding human (default `owner`) and `-intent` supplies the intent's statement, prompted for on standard input when absent. `walk <deploy-id>` follows the links from an existing deploy record back to its intent, printing each stored field it crosses.

## What the tests demonstrate

### M0

M0 is done when these pass, which is what [`../roadmap.md`](../roadmap.md#m0--the-graph-and-the-log) sets as the milestone's demonstration:

- all three shapes appended and the chain read back unbroken, every row carrying an actor and a timestamp the writer wrote;
- a row edited by raw SQL inside the chain, after which `Verify` names that row and says its stored hash is not the hash of its fields; and a row removed from inside the chain, after which `Verify` names the row that followed it;
- a row removed from the *end* of the chain, after which `Verify` returns nil — and an ordinary append then writing a replacement over it that also verifies clean. That is a limit the tests record rather than a defect they found: nothing anchors the head, so `Verify` proves that the rows present are one unbroken history and does not prove that they are all of it. [_Deferred, but not designed out_](../end-goal/deferred.md#security-comes-last) defers exactly this — "Where the head is anchored and who reads it can wait" — and anchoring the head is what closes it;
- a page event or a wait naming a policy version or a score version refused by the method, and refused again by a CHECK constraint when it is inserted around the methods;
- an empty or unknown actor refused by the writer, and refused again by a CHECK constraint;
- a timestamp that is not the one fixed-width RFC 3339 UTC layout refused by a CHECK constraint, which is what the format is worth to the next package that composes `record`'s columns and brings its own writer;
- a secret resolved through `secretref` whose value is in no byte of any row — the assertion casts each stored row to text and searches it, rather than trusting the Go values the test held;
- the fake target recording each named operation, with the credential present as a reference and no value anywhere in what it recorded;
- eight goroutines appending at once and the chain still whole, which is the advisory lock doing the work the one-writer rule needs.

### M1

M1 is done when these pass too, which is what [`../roadmap.md`](../roadmap.md#m1--one-change-ships) sets as the milestone's demonstration — one change followed end to end on a real repository:

- an intent taken in, one round of interview, and one item cut from it;
- a spec of one criterion, in one of the six patterns, and an implementation authored by an agent against it;
- every criterion in force for the service encoded, checked in both directions, and run wherever the build was made — a criterion in force with no encoding naming it is refused, and so is an encoding naming a criterion not in force. That is a set and not one criterion, which is what makes a second change on a service shippable: its candidate branch is based on master, so the previous items' encodings are in the tree, and the implementation role is told every criterion it has to keep passing;
- a human deciding at the one gate this milestone builds, Merge to master, with the decision readable in the log as two chained rows — an opening row naming the policy version and score version and carrying the factor vector, and a closing row naming it and carrying the verdict;
- on approval: a release numbered 1, master created by the first fast-forward, and a straight deploy through `targetseam` against `localtarget`, a target that runs the software as a local process;
- the link walk from the deploy record back through the release, the build, the item, to the intent, with every step a stored field and none reconstructed;
- `Verify` still clean over the whole log after all of it; and
- the reject path: a human rejecting at the gate instead, after which the run stops with no release minted, no deploy recorded, and the item left at the implementation stage.
