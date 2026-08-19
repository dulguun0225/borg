# factory

The factory's code. What it is for and what it is built toward are in [`../end-goal/`](../end-goal/README.md); the order it is built in is [`../roadmap.md`](../roadmap.md); the rules it is written under are the _Code_ section of [`../CLAUDE.md`](../CLAUDE.md#code). This file is the map those rules require to ship with the code: every package, what it owns, the edges between them, and how to run the thing.

What exists is milestone M0, [_The graph and the log_](../roadmap.md#m0--the-graph-and-the-log), milestone M1, [_One change ships_](../roadmap.md#m1--one-change-ships), milestone M2, [_The factory decides_](../roadmap.md#m2--the-factory-decides), and milestone M3, [_A candidate gets an environment_](../roadmap.md#m3--a-candidate-gets-an-environment). M0 put the four seams of [_Deferred, but not designed out_](../end-goal/deferred.md#security-comes-last) into the record graph from the first record — an actor on every record, one append-only decision log whose rows chain their predecessors, secrets by reference, and a named seam between agents and deploy targets — and shipped no pipeline, no gate, no agent, and no user-facing anything. M1 built the thinnest path through the whole loop on top of that graph: an intent taken in, an item cut, a spec of one criterion authored by an agent, an implementation authored by another, the Merge to master gate deciding with a human, a release minted, and a straight deploy against a target that runs the software — one change followed end to end, with `cmd/factory` as the crude interface it runs through until the four surfaces of M7 replace it.

M2 made the score real and gave the owner something to author. The score reads eight factors off the graph, reduces them by a published formula to one number, and names the version it did that under; the gate compares the number against the threshold in force and decides for itself where no human is put there; and an owner authors any of [gate policy](../end-goal/how-humans-do-it/09-gate-policy.md)'s seven parameters or places a [pin](../end-goal/how-humans-do-it/09-gate-policy.md#one-shape-across-all-of-them), each write appending a policy version. A second gate row arrived with it — Deploy to production, which is where [hold](../end-goal/how-humans-do-it/03-gates.md#what-a-gate-may-change) is an action — and so did the records an owner authors on: the area, the environment, and the factory policy record.

M3 gave every candidate an environment of its own and put a queue in front of master. A third gate row creates that environment, the candidate's build runs on it, and the [criteria](../end-goal/how-humans-do-it/03-gates.md#spec) are decided there by two runs, a criterion whose runs disagree being [undecided](../end-goal/how-humans-do-it/05-environments.md#what-the-candidate-environment-decides) rather than passed. The [merge queue](../end-goal/how-humans-do-it/05-environments.md#the-merge-queue) is then the one inbound path to master: it orders its members by the priority an owner set and the time of their merge approval, re-verifies each against the master it will actually merge into, fast-forwards and mints the release, or rejects a candidate that failed on its own merits and sends the item back with an attempt counted where it goes. Two candidates of one service now proceed at once, neither reading the other's environment.

## The packages

| Package | What it owns |
|---|---|
| [`record`](record) | The graph's conventions: the actor — kind `human` or `component`, plus a name, neither empty — identifier generation, the timestamp format, and the SQL text every record table composes for its columns and their constraints. It owns no table. |
| [`decisionlog`](decisionlog) | The chained log: one table, one writer with four append methods — a decision is two rows, an opening row appended when a gate fires and a closing row naming it and carrying the verdict, plus one method each for a page event and a wait — and, as functions over the pool rather than methods on the writer, `Read` and a `Verify` that walks the chain and names the first row that breaks it. Reading the log is not a reason to hold the thing that appends to it. |
| [`secretref`](secretref) | The reference type every other package uses in place of a secret, and the one resolver that reads a value. A `Ref` has one field and it is the name, so nothing that renders one can render a value. |
| [`targetseam`](targetseam) | The named operations an agent reaches a deploy target through — `Deploy`, `Stop`, `ReadRunning` — as an interface. `Fake` records what was called on it and reaches nothing; `localtarget` is the implementation the demonstrations deploy against. |
| [`postgres`](postgres) | Opening the pool, and applying each package's DDL from one ordered list written in the source. No ORM, no migration framework. |
| [`cmd/depscheck`](cmd/depscheck) | Failing the build on an import between two packages of this module that [`deps.txt`](deps.txt) does not allow. |
| [`intent`](intent) | The intent — its source, its statement, its refinement state, its round count — and the questions attached to it, `Intake` being the one writer of both. |
| [`service`](service) | A service's identity and its repository, written at the cut, and the four parameters an owner authors on it. Two writers, and the seam between them is the field. |
| [`item`](item) | The item — its intent, service, area, branch, and what it waits on — written once by the cut, and the stage, attempts, spend, and priority `Dispatch` writes after. It moves both ways: one stage forward, or back to the stage it is at or one above it with an attempt counted where it lands. |
| [`artifact`](artifact) | The artifact store: an artifact's version chain, its authorship and its author — the model version an authorship prior is kept on — and the one call a spec version and the criteria it introduces are submitted in. |
| [`criterion`](criterion) | A criterion of a service: its stable id, its pattern, the query for which criteria are in force for one build — a build being a set of items — the encoding checks that tie a criterion to the test code deciding it, and what deciding one against a build produced, undecided among the three outcomes. |
| [`gate`](gate) | The gate component: firing the three rows built so far — Deploy to candidate environment, Merge to master, and Deploy to production — deciding whether a human decides, and opening and closing a decision through `decisionlog`, the verdict a human's where one is put at the row and the factory's own otherwise. It also owns the vocabulary of the factory's own hold, and the read of when each item's merge approval closed, both payloads' shape being its. |
| [`build`](build) | A build record, one per commit built, naming the item it was built for and the commit it was made from. |
| [`release`](release) | The release record and the number, minted per service at the fast-forward, and master's head as a query — the commit of the service's highest-numbered release. |
| [`deploy`](deploy) | The deploy record and the straight rollout, reaching a target through `targetseam`. The environment it names is an environment record's id, and what it names as deployed is the build it put there and, where it is deploying one, the release. |
| [`agent`](agent) | The two authoring roles — `SpecAuthor`, `Implementer` — the `Model` interface both call, `Anthropic`, the one implementation, reaching its credential — a Claude subscription's token — through `secretref`, and `Paced`, which wraps a model so two calls never start closer together than an interval. |
| [`localtarget`](localtarget) | A `targetseam.Target` that runs the software as a local process, one per service, in one directory — the implementation the demonstrations deploy against. There is one target per environment, because an address on this substrate is a directory. A substrate that moves a process rather than traffic, so every deploy is straight and there is no strategy to pin. |
| [`gatepolicy`](gatepolicy) | The vocabulary of everything an owner authors: the eight parameters of gate policy's seven rows, what each value means, the record its scope names, and the direction a pin on it points. It owns no table, the way `record` owns none. |
| [`score`](score) | The risk score: the score version — append-only, naming the published formula, the factor set, and the values the score supplies — the eight factors of the design's three groups, and the assessment a gate fires with. |
| [`policy`](policy) | Factory's writer: the one component every authored value and every pin goes through, the policy version it appends in the same transaction, and the value in force as a read of three things — authored, supplied, clamped by a pin. |
| [`pin`](pin) | The pin record — its subject, the parameter it binds, its direction, its bound, and whether it has been withdrawn — and the query by subject every mechanism a pin binds runs. |
| [`area`](area) | The area record: an owner's grouping, the area it lies inside, and the item-size target authored on it. |
| [`environment`](environment) | The environment record — production and a candidate's — its targets, its credential reference, the gate thresholds authored on it per row, and, for a candidate's, the item it belongs to, what it was composed from, and the teardown that keeps the row. Two writers, and the kind is the seam. |
| [`factorypolicy`](factorypolicy) | The factory policy record: the attempt bound per stage, the predicate catalog, and the threshold the brief-or-skill gate row reads. |
| [`mergequeue`](mergequeue) | The merge queue: its membership as the items whose stage says Merge to master approved them, its order as the priority and then the approval's time in the log, the fast-forward and the release it mints with it, and the rejection of a candidate that failed its own re-verification. It owns no table, and what it does to a repository and to a candidate environment is behind an interface its caller implements. |
| [`cmd/factory`](cmd/factory) | The crude interface, and the deploy agent: `run` walks the whole path once per intent it is given, `walk <deploy-id>` follows the links back, and `area`, `author`, `pin`, `policy`, and `priority` are duty 8 and duty 9 until a surface exists. It owns no table of its own; every record it causes to exist is written by the package that owns it. |

Each package's `doc.go` says what it owns, who may write what, and which `end-goal/` section defines the concept it implements. Read that before the code.

## The allowed edges

```
record
gatepolicy
decisionlog -> record
secretref
targetseam -> secretref
intent -> record
service -> record gatepolicy
item -> record
criterion -> record
artifact -> criterion record
build -> record
release -> record
deploy -> record secretref targetseam
area -> record gatepolicy
environment -> record gatepolicy secretref
factorypolicy -> record gatepolicy item
pin -> record gatepolicy
score -> record gatepolicy artifact decisionlog item release
policy -> record gatepolicy area environment factorypolicy item pin score
                secretref service
gate -> criterion decisionlog policy record score
mergequeue -> decisionlog gate item record release
agent -> secretref
localtarget -> secretref targetseam
postgres -> area artifact build criterion decisionlog deploy environment
                factorypolicy intent item pin policy release score service
cmd/depscheck
cmd/factory -> agent area artifact build criterion decisionlog deploy environment
                factorypolicy gate gatepolicy intent item localtarget mergequeue
                pin policy postgres record release score secretref service
                targetseam
```

An arrow reads as "imports": `decisionlog -> record` is `decisionlog` importing `record`. `record`, `gatepolicy`, `secretref`, and `cmd/depscheck` import nothing inside this module. Seven edges deserve their reason stated, and [`deps.txt`](deps.txt) states each of them beside the list:

- `artifact -> criterion` is the one import between two record packages that both own tables, because the artifact store is the criterion's one writer, so a spec version and the criteria it introduces are written in one transaction.
- `gatepolicy` is imported by everything that holds an authored value and imports nothing itself: it owns no table and is the one place the parameters, their units, and the direction each one's pin points are written down.
- `score` imports the record packages the factors are computed from, because the score reads the graph. What it does not import is a repository: the one input that is not a record is the build's diff, measured by the component that built and handed to it.
- `policy` imports every record an owner authors a value on and calls the transaction-taking write each of them exposes, appending the policy version inside the same transaction, and it imports `score` for the value the score supplies where an owner authored nothing.
- `gate -> criterion` is one type: what deciding a criterion against a build produced, which is what the merge gate reads about the item's own behaviour. The vocabulary of an outcome belongs to the package that owns the criterion, so the gate names it rather than keeping a second spelling of it.
- `mergequeue -> gate` is the shape of a decision's two payloads. The queue's order is the time of the merge approval in the log, and a queue that unmarshalled those payloads itself would be a second place naming the same JSON fields; it asks `gate` for the approval times instead. What it does not import is anything that reaches a repository or a deploy target.
- `cmd/factory` importing nearly everything is because it is the crude interface the four surfaces are deferred with — the one place the whole path is composed, the one place an owner can author a parameter or place a pin, and the deploy agent the merge queue reaches its repository and its candidate environments through.

Every record package that owns a table has a database test in its external test package importing `postgres` to open the pool and apply the schema, an edge `deps.txt` states once per package as `test <package> -> postgres`. Three test lines name more. `decisionlog`'s names `secretref`, for the one test that resolves a secret and writes to the log at once. `score`'s names `criterion`, `gate`, and `policy`, because the outcomes the score counts are the decisions a gate wrote and a test that wrote those rows by hand would prove only that the score can read what the test writes. `mergequeue`'s names every package it writes through, its tests reaching the whole schema through `postgres.Apply` and driving the queue over a fake repository — what a re-verification does to a repository and to a candidate environment is the crude interface's own demonstration.

[`deps.txt`](deps.txt) is that list, and `cmd/depscheck` is what makes it binding. A package the file does not list is an error, an edge it does not allow is an error, and a line naming a package that does not exist is an error. The compiler already refuses a cycle; this refuses an edge that would compile.

## Running it

The toolchain is pinned by [`../mise.toml`](../mise.toml). The dev database is [`docker-compose.yml`](docker-compose.yml), on port 5433 so a PostgreSQL installed on the machine keeps 5432.

```sh
docker compose up -d           # the database
go vet ./...
go run ./cmd/depscheck
go test -count=1 ./...
```

Each milestone so far has added columns to tables an earlier one wrote, and `create table if not exists` does not alter a table that already exists. M2 added `item.area_id`, `artifact.author`, and `deploy.environment_id`, the last a rename; M3 added `item.waits_on` and `item.priority`, `criterion.item_id`, `deploy.build_id`, three columns to `environment`, and the `criterion_result` table. So a database an earlier milestone wrote does not become an M3 database by running against it: drop the schema and let it be applied again, which for the dev database is `drop schema public cascade; create schema public;`. That is the forward promise this store does not have yet, stated where somebody meets it; [`postgres/doc.go`](postgres/doc.go) says whose question it is and when it is due.

The database tests read `DATABASE_URL` and fall back to `postgres://factory:factory@localhost:5433/factory`. They do not skip when the database is unreachable — a silent skip is how a green run comes to mean nothing — so an unreachable database fails them. `-count=1` is what keeps that promise: `go test` caches a result against the test binary and what the test read, and a database is neither, so a re-run with the database stopped reports the cached `ok` without opening a socket. Each database test creates a PostgreSQL schema of its own, applies the DDL inside it, and drops it when it ends, so a rerun on a database a previous run left dirty starts clean.

[`../.github/workflows/factory.yml`](../.github/workflows/factory.yml) runs the same three commands against a `postgres:17` service container.

A fourth command runs the same demonstration against the model API instead of a fake, and it is the only thing that tests the stretch between a role and the provider — the credential's header, the shape of a real answer, a reply the protocol refuses, an encoding named the way a model chose to name it. Three defects have sat on that stretch while everything above was green, so it is run whenever something on it changes and before a milestone is called done:

```sh
FACTORY_MODEL=claude-opus-5 go test -tags realmodel -count=1 -v -run RealModel ./cmd/factory/
```

The credential is `model.anthropic` in `secrets.local`, which [`../.gitignore`](../.gitignore) refuses to track; `claude setup-token` mints one. The test fails rather than skips when the file holds no token, for the reason the database tests do not skip, and it keeps its git repository on a failure so what the model wrote can be read. The build tag is what keeps it out of `go test ./...` and out of CI, neither of which has a credential to spend — and what the tag costs is that this one file is not compiled in the default build, so `go vet -tags realmodel ./cmd/factory/` is how a change to it is checked.

Both milestones' demonstrations are end-to-end tests in `cmd/factory`, and they run under `go test` above along with everything else — no separate step is needed to see them pass. To walk the path against a real model and a real process, run it directly — [`DEMO.md`](DEMO.md) is that run as a runbook, with what to set up, what to type at each prompt, what to show afterwards, and what the run deliberately does not cover:

```sh
go run ./cmd/factory run -secrets <file> -model <name> -repo <dir> -service <name> -area <name> -targets <dir> -intent <statement>
go run ./cmd/factory walk <deploy-id>
```

`run` reads two secrets from `<file>` by the names `model.anthropic`, the Claude subscription token `claude setup-token` mints, which the model call resolves and sends as a bearer token, and `deploy.local`, the credential `targetseam` requires on every operation and `localtarget` never reads. `-repo` is the service's git repository, created when absent; `-targets` is the directory `localtarget` runs releases from, and production's environment record is created naming it. `-human` names the deciding human, who is also the owner every authoring write is made as (default `owner`). `-intent` supplies an intent's statement and is given once per candidate — two of them is two items authored, two candidate environments live at once, and both merges ordered by the queue — and one is prompted for on standard input where the flag is absent. `-candidate-environments` is how many candidate environments this substrate has room for at once (default 8): a candidate that meets it waits, and that wait is written into the log, being neither a record nor a parameter of an owner's. `-area` names the area the item is in, declared where it does not exist — without one the score can read neither context factor and a human decides every gate of that item. `-pace` is the least time between two model calls, two seconds by default, which is what stops a stage's retry following its refusal with nothing in between; `0` sends them back to back. `walk <deploy-id>` follows the links from an existing deploy record back to its intent, printing each stored field it crosses and then every decision the item's gates left in the log.

Five more subcommands are duty 8 and duty 9 — everything an owner authors, the pins, and the priority a queue is reordered with — which have no surface until the four of M7 arrive:

```sh
go run ./cmd/factory area <name> [-inside <name>]
go run ./cmd/factory author -parameter <name> -value <v> [-service <name>] [-area <name>] [-gate <row>] [-stage <stage>]
go run ./cmd/factory pin -parameter <name> -subject <kind>:<name> [-bound <v>]
go run ./cmd/factory pin -withdraw <pin-id>
go run ./cmd/factory policy [-service <name>] [-area <name>] [-gate <row>] [-stage <stage>]
go run ./cmd/factory priority <item-id> -priority <n>
```

`author` reads the subject flags the parameter needs and no others, the record a parameter is a field of being a fact of the parameter rather than a choice: a threshold is authored on an environment for one gate row, an attempt bound on the factory policy record for one stage, an item-size target on an area, and the watch window's four on a service. `pin` takes no direction, because the direction differs per parameter and points the same way in each — an owner chooses the subject and the bound. `priority` writes the one field that orders a queue: a greater number goes first, and it orders every queue the item waits in as an item — the gates up to and including Merge to master, and the merge queue — and no deploy. `policy` prints every parameter as it is in force, where its value came from, the pins that reached it, and what reads it at this milestone, which is where an owner sees that four of the eight are read by nothing yet.

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
- every criterion in force for the build encoded, checked in both directions, and run wherever there is a run to observe — a criterion in force with no encoding naming it is refused, and so is an encoding naming a criterion not in force. That is a set and not one criterion, which is what makes a second change on a service shippable: its candidate branch is based on master, so the merged items' encodings are in the tree, and the implementation role is told every criterion it has to keep passing;
- a human deciding at Merge to master, with the decision readable in the log as two chained rows — an opening row naming the policy version and score version and carrying the factor vector, and a closing row naming it and carrying the verdict;
- on approval: a release numbered 1, master created by the first fast-forward, and a straight deploy through `targetseam` against `localtarget`, a target that runs the software as a local process — the deploy record naming the build that runs and the release it is of;
- the link walk from the deploy record back through the release, the build, the item, to the intent, with every step a stored field and none reconstructed;
- `Verify` still clean over the whole log after all of it; and
- the reject path: a human rejecting at the gate instead, after which the run stops with no release minted, no deploy recorded, and the item back at the implementation stage with an attempt counted there.

### M2

M2 is done when these pass too, which is what [`../roadmap.md`](../roadmap.md#m2--the-factory-decides) sets as the milestone's demonstration — three runs of the path against one service, and what each is for. The two rows M2 read at are three from M3 on, and the assertions read every one of them:

- the first run, which is M1's under a real score: every one of the eight factors computed, none of them unavailable, and the number over the threshold at every gate row — no earlier release to return to, an author nobody has approved, an area with no history, and every file in the tree touched — so a human decides at each and every decision names a score version and a policy version that are records rather than names;
- the second run, which is the milestone's own: the same path on the same service, with the prior on the model and the history of the area narrowed by the first run's verdicts and a release to return to, so the number is under the threshold at every row, every closing row is written by the gate component saying it was auto-passed by the threshold, every opening row waits on nobody, and the second run's scripted input is empty — a run that stopped to ask anybody anything would fail on a reader with nothing in it;
- the third run, which is the pin: an owner places one on the production deploy row, the row the score would have passed puts a human there and says the pin is why, the human holds, and the run stops with the release minted, nothing deployed, no attempt counted, and the item where it was — then withdrawing the pin leaves the row the score's again;
- the score's own arithmetic: the published formula stating every breakpoint the source applies, the weights summing to one within each half, an unlikely catastrophe still gated, a change that is cheap to undo reading lower than one that is not, and a vector with any factor unavailable reducing to the top of the scale;
- what the score reads and what it refuses to read: a hold teaching it nothing, an auto-pass not counting as evidence about the author, a reject counting against one, and a measurement that could not be taken leaving size and reach unavailable with the reason on each;
- the value in force as a read of three things, for all seven rows: what an owner authored where they authored one, what the score supplies where they did not, and a pin clamping either — with a pinned ceiling of five over an authored two leaving the two, because a pin is a bound and not a precedence;
- one writer for everything an owner authors: a component refused, a failed write appending no policy version, and every write that lands appending one that names the parameter, the subject, and the actor; and
- `Verify` still clean over a log holding both rows of every decision the three runs fired.

### M3

M3 is done when these pass too, which is what [`../roadmap.md`](../roadmap.md#m3--a-candidate-gets-an-environment) sets as the milestone's demonstration — three runs on one service, and what each is for:

- a candidate's own environment: composed at the approval of Deploy to candidate environment, named for the item so two candidates of one service cannot collide, its target a directory of its own, composed from nothing because the cut declared no dependency, and torn down at the merge with the record kept — the deploy records naming it would otherwise point at nothing;
- what the candidate deploy provides, which is the criteria: the build deployed there with the deploy record naming that build and no release, the encodings run twice on it, and one result per criterion written against the build by the deploy agent. Nothing is current on a candidate environment, `Current` reading the records that name a release;
- two candidates in one run, which is the milestone's own: two items cut on the same master, two environments live at once with different directories and different deploy records, each criteria run attaching to its own build, both merge gates approving, and the queue merging them in its order — the second re-verifying against the master the first created, so its release names a build the implementation stage never made, and both releases deployed in the order the numbers were minted;
- the queue's rejection: two candidates changing one file differently, so the second's re-verification is a merge that conflicts. The item goes back to Implementation with an attempt counted there, no release is minted for it, its environment stays its own, and the log holds a wait row naming the merge queue as caller and actor — no gate fired, the merge gate's own having closed as an approval;
- the settable order: an owner writes a priority through dispatch between the merge gate and the queue, the queue takes that candidate first, and the one behind it is now the one whose merge conflicts — reordering changes when a candidate re-verifies and never what it has to pass;
- the two holds the factory sets at a deploy row, and how they differ. A substrate with no room for another environment is written into the log as a wait with the deploy agent as its actor, that condition being neither a record nor a parameter of an owner's; a declared dependency that is not its service's current release writes nothing at all and is recomputed at every firing;
- the third outcome: an encoding whose two runs disagree leaves the criterion undecided for that build, which the merge gate reads the way it reads a failure, and the score is told it as a failure too;
- in force is per build: a criterion introduced by an item that has not merged is not in force for another item's build, which is what lets a candidate cut in parallel with another one build at all — and both criteria are records of the service all the same, nothing here withdrawing one;
- the queue's membership being the service's and not the run's: a run whose input ends after one merge gate approved leaves that item queued, and the next run finishes it — merges it, tears its environment down, and deploys its release beside its own — rather than failing on it after it has spent its own model calls;
- one merge minting one number: a member that already has a release is finished rather than re-verified, so an advance that failed is repaired and the second number a re-verification would have minted is never minted — nothing in the store refuses one, a release being unique on the service and the number and not on the item; and
- `Verify` still clean over a log holding a wait row beside the decisions.
