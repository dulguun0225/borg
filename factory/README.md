# factory

The factory's code. What it is for and what it is built toward are in [`../end-goal/`](../end-goal/README.md); the order it is built in is [`../roadmap.md`](../roadmap.md); the rules it is written under are the _Code_ section of [`../CLAUDE.md`](../CLAUDE.md#code). This file is the map those rules require to ship with the code: every package, what it owns, the edges between them, and how to run the thing.

What exists is milestone M0, [_The graph and the log_](../roadmap.md#m0--the-graph-and-the-log): the four seams of [_Deferred, but not designed out_](../end-goal/deferred.md#security-comes-last) — an actor on every record, one append-only decision log whose rows chain their predecessors, secrets by reference, and a named seam between agents and deploy targets — and nothing else. There is no pipeline, no gate, no agent, and no user-facing anything. M0 exists because none of the four can be added to a history written without them.

## The packages

| Package | What it owns |
|---|---|
| [`record`](record) | The graph's conventions: the actor — kind `human` or `component`, plus a name, neither empty — identifier generation, the timestamp format, and the SQL text every record table composes for its columns and their constraints. It owns no table. |
| [`decisionlog`](decisionlog) | The chained log: one table, one writer with three append methods for the three shapes — a decision, a page event, a wait — and, as functions over the pool rather than methods on the writer, `Read` and a `Verify` that walks the chain and names the first row that breaks it. Reading the log is not a reason to hold the thing that appends to it. |
| [`secretref`](secretref) | The reference type every other package uses in place of a secret, and the one resolver that reads a value. A `Ref` has one field and it is the name, so nothing that renders one can render a value. |
| [`targetseam`](targetseam) | The named operations an agent reaches a deploy target through — `Deploy`, `Stop`, `ReadRunning` — as an interface, plus a fake that records what was called on it. Nothing real is behind it. |
| [`postgres`](postgres) | Opening the pool, and applying each package's DDL from one ordered list written in the source. No ORM, no migration framework. |
| [`cmd/depscheck`](cmd/depscheck) | Failing the build on an import between two packages of this module that [`deps.txt`](deps.txt) does not allow. |

Each package's `doc.go` says what it owns, who may write what, and which `end-goal/` section defines the concept it implements. Read that before the code.

## The allowed edges

```
record       <- decisionlog
secretref    <- targetseam
decisionlog  <- postgres
```

`record` and `secretref` import nothing inside this module, and `cmd/depscheck` imports nothing inside it either. One edge exists only in test code, and [`deps.txt`](deps.txt) states it as `test decisionlog -> postgres secretref`: `postgres` imports `decisionlog` to apply its DDL, so `decisionlog` cannot import `postgres`, and its database tests are in the external test package `decisionlog_test`, which the compiler treats as a separate package.

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

## What the tests demonstrate

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
