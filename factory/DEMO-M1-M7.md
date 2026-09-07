# The takes below M8

How to run the demonstrations of [M2](../roadmap.md#m2--the-factory-decides) through
[M6](../roadmap.md#m6--the-score-learns) by hand, each a take driven by `run`, `watch` and `learn`
rather than by the process the screens are served from. [`DEMO.md`](DEMO.md) is what to set up, how
to start the factory, and the M8 demonstration; every take here starts from a factory installed the
way that file says and is run from the same directory.

Three of the eight milestones before M8 have no take of their own here.
[M0](../roadmap.md#m0--the-graph-and-the-log)'s demonstration is records written through each seam and the
chain read back unbroken, which is `go test ./...` and nothing to walk through by hand.
[M1](../roadmap.md#m1--one-change-ships)'s demonstration is one change shipped with a human at every
gate, which is episodes one and two of `DEMO.md` — the screens replaced the terminal the first take
used. [M7](../roadmap.md#m7--the-code-matches-the-design)'s is every earlier milestone's
demonstration passing on the changed code, which is `go test ./...`, `go run ./cmd/depscheck` and
`go run ./cmd/tracecheck` on a fresh clone and nothing to walk through by hand.

What a take leaves behind and how to clear it is [_Resetting between
takes_](DEMO.md#resetting-between-takes), and what each failure means is [_When it
fails_](DEMO.md#when-it-fails).

## The second take, which is M2's demonstration

First, author the analysis window's parameters on the service the episodes in
[`DEMO.md`](DEMO.md) created, because the first release's window is already open at the values the [score](../end-goal/how-the-factory-works/04-risk-score/README.md) supplies — a size of two in a hundred and a cap of a day, right for a real service and unwatchable here — and a window copies its values at the open, so nothing authored now moves that one. It stays open for its day, and the window limit the score supplies is one, so every later production deploy of this service would wait behind it; the limit is authored above it here, which is what [_When it fails_](DEMO.md#when-it-fails) says of that wait:

Author them at Factory, one at a time, each on the service they are a field of: `window_size` at
`0.1`, `window_confidence` at `0.95`, `window_cap` at `60`, and `window_limit` at `3`. A size of
`0.1` is one unit of work in ten failing above the baseline, and `60` is a minute before a window
that will never reach its volume ends unresolved. `window_size` is authored per quantity, and the
form names which — `error_rate` unless another is chosen. Factory reads back every parameter as it
is in force with what reads it beside each, where those four had no reader before, and
`go run ./cmd/factory policy -service greeter` prints the same reading from a terminal. What
authoring the limit costs is stated at the sixth take: a value an owner authored is not one the
score's movement is read on.

Then take a second intent in at Work, on a statement that adds a route:

> Add a second route to this service: it answers GET /version with status 200 and the body 1.0.0. Keep the existing route and its test as they are. Test the new handler through net/http/httptest rather than by binding the port.

This one still asks a human at the three rows above a build — Spec, Implementation plan, Tasks — on every item, the way episode two's own text says. What it shows is the four rows over a build — Implementation, Deploy to candidate environment, Merge to master, Deploy to production — auto-passing: the approvals typed at those episodes narrowed the prior on the model that wrote the change and the history of the area it was in, its release gave the service something to return to, and the diff touches part of the tree rather than all of it — so the number is under the [threshold](../end-goal/how-the-factory-works/09-gate-policy/01-what-is-in-it.md) at every one of those rows and the factory gives every verdict itself. Each close event's why it auto-passed reads threshold, and the row is written by the gate component rather than by a person.

That is the whole of what M2 claims, and it is worth saying out loud while it runs: the factory earned this by having been watched once, and the evidence is in the log rather than in a setting.

Then put a human back at a row and hold the deploy. Place a
[safeguard](../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md) at
Factory (9) on `risk_threshold`, with `gate_row` as its subject kind, `deploy_to_production` as the
subject and `greeter` as the service — package policy's own reader keys a row-scoped safeguard on
the service the row fires for, so a safeguard on a gate row names both. Factory then reads the
parameter with the safeguard that reached it beside its value. Take a third intent in and the deploy
row asks for a verdict again, saying the safeguard is why rather than the number — hold it and
nothing is deployed. Withdrawing the safeguard is two acts and neither is this one: the withdrawal is
written at Factory and the row that decides it is closed there, which is
[_The four rows outside every item_](DEMO.md#the-four-rows-outside-every-item).

## The third take, which is M3's demonstration

Two intents in one run, on the service the takes above and the episodes before them already shipped. Each `-intent` is one candidate:

```sh
go run ./cmd/factory run \
  -secrets ~/borg-demo/secrets \
  -model deepseek/deepseek-v4-flash \
  -service greeter=~/borg-demo/greeter \
  -area greeting \
  -targets ~/borg-demo/targets \
  -intent 'Add a route answering GET /ready with status 200 and the body ready, in a new file ready.go with its test in ready_test.go, registering the route from an init function in ready.go. Change no existing file.' \
  -intent 'Add a route answering GET /uptime with status 200 and the seconds since start as a decimal number, in a new file uptime.go with its test in uptime_test.go, registering the route from an init function in uptime.go. Change no existing file.'
```

Both statements say to change no existing file and to register the route from the new file, because a route registered in `main.go` is a change to an existing file, and that is the whole trick of this take: two candidates of one service are cut from the same master, so two changes to one file are two sides of a merge that conflicts — which the queue is right to reject and is not what this take is for. [_The queue rejecting a candidate_](#the-queue-rejecting-a-candidate) below is how to show that on purpose.

What to watch, in the order it prints. Both items are authored and built before either reaches a gate. Then each gets its own environment, at its own directory under `-targets`, named for its item — `ls ~/borg-demo/targets` while it runs shows two of them, which is the milestone in one command. Each candidate's build is deployed to its own environment and its criteria are decided there, so nothing either candidate runs is anything the other can see. Then the queue prints its order and takes them one at a time: the first re-verifies against the master both were cut from, fast-forwards, and is minted a number; the second re-verifies against the master the first one just created, which is a build the implementation stage never made, and takes the number after it. Both environments are torn down at their merges — the records stay, because the deploy records naming them would otherwise point at nothing — and the two releases are deployed in the order the numbers were minted.

### The queue rejecting a candidate

Run the same two intents with both of them changing one existing file — drop the "Change no existing file" sentence and ask each to add its route to `main.go`. The first candidate merges; the second one's re-verification is a merge that conflicts, so the queue rejects it, sends the item back to implementation with an attempt counted there, and writes a wait row naming the merge queue as its actor. Nothing is minted for it and its environment stays its own, which is what the design says of an item that has not merged, been dropped, or been superseded.

### Reordering the queue

Between the Merge to master rows and the queue there is nothing for a human to do, one pass doing
both — so the priority is worth showing on the records rather than in a take. It is written at Work,
on the item: a greater number goes first.

 It orders every queue the item waits in as an item — the gates up to and including Merge to master, and the merge queue — and no deploy: numbered releases waiting to deploy are ordered by the number and by nothing else, so an owner who rushes an item has rushed it at every gate it has left and has no way at all to reorder a deploy.

## The fourth take, which is M4's demonstration

This one is the factory taking a change back off production on its own, so it needs three things set up first.

**The analysis window's parameters** were authored at the second take, and every window since has opened with them; a window that opened before them — the first release's — runs to the cap it copied.

**Drift detection, installed once.** It is a second process with a store of its own, and installing it beside the factory is substrate outside the twelve duties:

```sh
go run ./cmd/driftdetector pass -secrets ~/borg-demo/secrets
go run ./cmd/driftdetector show
```

The first pass creates its schema and compares what each production target runs against what the factory recorded. `show` prints every mismatch and the last check per target — no mismatches is not health if the last check is old, which is why the second record exists at all. Run the factory without it and every check the factory makes reads a record it wrote itself; the run says so on its first line either way.

On a take driven by `run` rather than by `serve` the first pass finds one mismatch already: a stale component. The health monitor runs only inside a `run` or a `watch` there, its last check names the interval it was reading on and owes a further pass while any window is open — the first release's is, for its day — and between runs nothing reads. Under `serve` the watch is a pass on its own interval and that mismatch does not arise, which is what the process buys. Where it does, it holds the service's production deploys until a human clears it:

```sh
go run ./cmd/driftdetector clear <mismatch-id> -human you
```

Expect it again at every later pass while that window is open.

**Who a page reaches.** A mismatch belongs to none of [the twelve duties](../end-goal/what-humans-do.md),
so the page it fires reaches whoever installed the drift detector. Declare both at People, on the key
of whoever is watching: the obligation `driftdetector`, and duty 12 — taking over issues the factory
cannot fix on its own, which is the duty an escalation belongs to. With the declaration empty every
page reaches the owner directly, which works and shows nothing about routing.

### The bad change

Then the take. A statement that ships something the criteria cannot see — the behaviour a criterion states is right, and a share of the work fails anyway:

> Add a route answering GET /flaky with status 200 and the body ok, in a new file flaky.go with its test in flaky_test.go. The handler must return status 200 and the body ok on every request, and its test must check exactly that. Separately, change the loop that appends to the BORG_SIGNAL file so that every second line it appends is error rather than ok. Leave every other existing behaviour as it is.

The two halves are the whole point. The criterion is about the route, the test decides the route, and both are right — so the build passes every criterion in force and the run reaches production with nobody deciding anything. What no criterion says anything about is how often the work succeeds, and that is what the window reads. This is the one take whose statement asks for the emitter by name: everywhere else the implementer's standing instruction is what puts it there, and here a demonstration needs a defect the criteria cannot see. Then the window: the run keeps reading until it closes, and what prints is the arithmetic — the units the release emitted and how many failed, the same for the release below it, the log of the likelihood ratio against the crossing the confidence set, and then `failed`. What follows has no human in it: an [incident](../end-goal/how-the-factory-works/08-operations/06-incidents.md) on production, the release failed and its deploy advanced to rolled back, the previous release's build put back on the target and waited for, a revert [intent](../end-goal/how-the-factory-works/02-intent-into-items/01-intake/README.md) taken in from the detector, and the rollback reported on mail and chat. It fires no page, because the factory does not page to inform.

`curl -s localhost:8081/health` still answers, and `curl -s localhost:8081/flaky` is gone — which is the demonstration in one command.

### The hold, and shipping the revert

Master still holds the change that was rolled back, so every production deploy of that service now waits:

```sh
go run ./cmd/factory run … -intent 'Add a route answering GET /extra with status 200 and the body extra, in a new file extra.go with its test in extra_test.go, registering the route from an init function in extra.go. Change no existing file.'
```

It merges, it is minted a number, and its deploy prints `waits at deploy_to_production: a rollback's revert has not shipped`. Nothing is written for that hold — it is computed from records that already exist and it lifts itself — and the line says which item to approve through it if you want to.

The revert lifts it. Its intent is already waiting, taken in by the health monitor at the rollback, so give the run that intent's own statement and it works that one rather than taking in a second saying the same thing:

```sh
docker compose exec -T postgres psql -U factory -d factory -tAc \
  "select statement from intent where source = 'detector' and state = 'unrefined' order by at desc limit 1"
```

Pass that as `-intent` with the release the run is holding as a second `-intent`, and watch the order: the revert deploys ahead of the release the hold is holding, which is the one place the number does not order deploys. Then the hold lifts and the release behind it deploys.

### Approving through, at Work on the item

The emergency action the design keeps at the production deploy row — approve now, not skip — is on
the item at Work: the item reads the hold standing at that row and offers approving through it, with
a reason the close event carries. The four holds the factory computes there stop the row being fired
at all, so there is no open event for an ordinary verdict to name and this action fires the row with
the holds on it and approves it in one act. Approving through any other hold is an ordinary approve
at Work naming the set on the open event.

What approving through the hold a rollback leaves redelivers is the defect that was just removed,
which is the most damaging thing in the factory to approve through and the one most likely to be
tried during an incident. `curl -s localhost:8081/flaky` answering again is that in one command.

### A mismatch, and the page

Change the target underneath the factory and let the drift detector find it:

```sh
sed -i 's/ [0-9]*$/ 999999/' ~/borg-demo/targets/greeter.running
go run ./cmd/driftdetector pass -secrets ~/borg-demo/secrets
```

That file is how the local target records the build it started and its process id — editing it is a target changed underneath, which is one of the three things the drift detector exists to catch. The pass prints `MISMATCH`. Then run the factory again on any statement: the production deploy row fires with what disagreed on its open event and a human at it whatever the number reads, the page reaches whoever the declaration says installed the drift detector, and a second pass of the watch widens it once to the owner. Nothing the factory can gather lifts this one:

```sh
go run ./cmd/driftdetector clear <mismatch-id> -human you
go run ./cmd/factory watch greeter -secrets ~/borg-demo/secrets -targets ~/borg-demo/targets -for 5s
```

Clearing it is a human's act inside the drift detector and there is no way to do it from the factory: that would make the factory a writer of the record that says the factory is wrong. The `watch` above is what writes the page's answered event, because the store that was cleared calls nothing.

## The fifth take, which is M5's demonstration

This one needs a second service, which is what a contract is for: an interface has consumers, and the consumers are other services in the same factory. Nothing else is set up — the window's parameters from the fourth take are enough, and both services get them.

Author the same three at Factory on `reader`: `window_size` at `0.1`, `window_confidence` at `0.95`,
and `window_cap` at `60`.

That authors on a service decomposition has not written yet, so do it after the first run below rather than before — or leave it out, and the reader's windows end at the cap the score supplies, which holds nothing here because the window limit is per service.

**One intent, two items, two services.** The statement names the services its decomposition yields items on, before a colon, in the order decomposition declares them waiting on each other:

```sh
go run ./cmd/factory run \
  -secrets ~/borg-demo/secrets \
  -model deepseek/deepseek-v4-flash \
  -service greeter=~/borg-demo/greeter \
  -service reader=~/borg-demo/reader \
  -area greeting \
  -targets ~/borg-demo/targets \
  -intent 'greeter,reader: greeter publishes a health interface with Status, always populated, and Detail; reader reads both of them. Each is a Go HTTP service, module borg.demo/<the service>, package main at the repository root, standard library only, with a go.mod naming that module and go 1.24. The published interface is one exported struct type in contract.health.go; the mirror reader holds is one exported struct type in consume.greeter.health.go, and reader'"'"'s own code reads every field it declares there.'
```

What to watch for, in the order it goes past. **Decomposition fires** — the one row where approving admits several timelines at once, and it fires here because decomposition yielded two items. Its vector has holes in it and the run says why: the change factors are computed from a build's diff and decomposition happens before anything is built, so an unavailable factor puts a human at the row. That is the design's rule for an unavailable factor rather than a decision the row takes.

Then the layers. **The producer ships first**, all the way to a running release, before the consumer's candidate environment is composed — because that environment is composed from its dependencies' current releases, and the hold at the candidate deploy row is what would otherwise make this two runs. The producer's release line says what it published: `contract health created and published at 1.0.0`, written by the queue inside the transaction that minted the number.

Then **the consumer contract**, derived from its build and printed as it is written: `Consumer contract art_… derived from the build: N predicate(s)`. What is in it is the mirror's fields the consumer's own code reads, and nothing else — a field it carries and never reads declares nothing.

**The breaking change.** Run again on the producer alone, on a statement that drops `Detail`:

```sh
go run ./cmd/factory run … -intent 'greeter: greeter publishes a health interface with Status alone, always populated. …'
```

Every criterion in force passes — the removal is in no criterion's path — and the merge row rejects it anyway, before a verdict is asked for: `Rejected by the producer's own contract diff before a verdict was asked for`, naming `health.Detail` and the reader that still declares it. The item is back at Implementation with an attempt counted there. This is the take to show slowly: nothing about it was a judgment, and the consumer it would break was answered by a query rather than by somebody remembering.

**The three items that get it through**, one run each: the producer adds `DetailText` beside `Detail` and marks `Detail` deprecated (`published at 1.1.0` — an addition and a mark break nothing); the reader migrates onto `DetailText`; and then the run prints `The list on health.Detail has emptied; intent … taken in by the detector`. That intent is the third item, and nobody had to remember it. Run it with the statement the detector wrote — `factory contracts` prints it — and the removal passes the same check that rejected the second run, minting `2.0.0`.

**The graph, read as a query.** This is where the milestone's claim is checked rather than asserted:

```sh
go run ./cmd/factory contracts -secrets ~/borg-demo/secrets -targets ~/borg-demo/targets
go run ./cmd/factory contracts -secrets ~/borg-demo/secrets -targets ~/borg-demo/targets -breaks <item-id>
```

The first prints every contract with its versions and the elements of the newest, which version production is running — and so which one a producer's own diff is against — the consumer contracts in force per service with the release range they were derived over, and the deprecation list per marked element. The second answers what one candidate would break and whom.

**The safeguard's predicate**, which is the blind case an owner covers by hand. Where a consumer reads a field through something the derivation cannot see, an owner asserts it:

Place it at Factory on `safeguard_predicate`, with `contract_element` as the subject kind,
`greeter/health/Detail` as the subject and `read` as the bound.

The detector still raises the removal — a safeguard never stops the item existing, only passing — and
the removal candidate is rejected at its merge row naming the safeguard and its author, which is the
blocked removal asking the consumer to confirm. Withdrawing the safeguard is the confirmation, and
the next candidate goes through: the withdrawal is written at Factory and the row that decides it
closed there, by a human other than the one who wrote it.

## The sixth take, which is M6's demonstration

This one needs no new service and no new statement. What it needs is outcomes, which the fourth take already produces — so run that one first, all the way through the rollback, and then ask the score what it learned:

```sh
go run ./cmd/factory learn
go run ./cmd/factory learn -dry     # the same reading, appending nothing
```

The pass prints every value the score supplies, the subject each was learned about, and the evidence behind it — and it marks each one that has moved away from the version in force. After the fourth take there is at least one movement and it is the threshold: the bad release was auto-passed on the number at three rows and its window failed it, so each of those rows now supplies a threshold one band below the number it passed it at. The line says so in as many words: `1 change(s) auto-passed on the number at this row turned out badly, the lowest of them scoring 0.14, so the threshold is one band below it`.

Then run anything again. Every row over a build now reads the moved threshold and the firing prints why it is what it is; a change scoring in the band the bad one scored in asks for a verdict where it auto-passed before, and a smaller change — two new files and nothing touched — still passes under it, which is the movement being one band and not a closed gate. That is the milestone: a supplied parameter moved because outcomes moved it, and the same change is decided differently afterwards.

**The window limit, which is the value the design spells out.** It rises per three windows closing without failing a release and falls at a rollback that swept — and it only rises where nobody authored it. The window limit was authored at the second take, so its rise is not read as in force here; what `learn` prints is the value the score supplies beside the authored one, and the rise is read there. Let three windows close:

```sh
go run ./cmd/factory watch greeter -secrets ~/borg-demo/secrets -targets ~/borg-demo/targets
go run ./cmd/factory learn
go run ./cmd/factory policy -service greeter
```

`policy` is where what is in force is read, and here it reads `window_limit = 3 (authored)`: the score's own value is not in force at all where an owner authored one, which is the division the design draws. The movement is read in `learn`, which prints the value the score supplies — `window_limit = 2, moved by outcomes on svc_…` — with the evidence under it; on a service with no authored limit the same line is what `policy` reads as in force.

**The movement as records.** A supplied value is a field of a score version, and every decision
names the version it was decided under — so the movement is read by following a decision to its
version and that version to the one it superseded, which is the `score_version` query in
[_Showing it afterwards_](DEMO.md#showing-it-afterwards). The superseded version still says what it said. A decision taken before the movement is readable against the value it was decided under and not against today's, which is what an append-only record is for.

**The held-out sample**, which is the one thing here that changes what the factory decides rather than what it supplies. It is random — one firing in ten of those the score would have gated — so it cannot be summoned, and it is worth watching for rather than demonstrating. When it selects, the firing reads:

```
  held out: the score's sample selected this item at this firing
  no human decides: the score held this item out of a gate it would have gated, which is the one thing in the factory that removes a human from a row
```

and the deploy that follows says `its window runs to the cap — the longest watch there is`. Every row below that one on the same item reads `selected this item at an earlier gate`: the sample selects an item, not a firing, so an item selected once reaches production with a human removed at each gate the score would have gated. `learn` lists the items it has selected — and says so where it has selected none, because a factory that has never sampled has a threshold that can fall and cannot rise. A row a safeguard reached keeps its human however the draw falls — `safeguard -parameter risk_threshold -subject gate_row:merge_to_master` and the sample never passes that row again, which is the one guarantee a safeguard has to keep.
