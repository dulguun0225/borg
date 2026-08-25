# Demoing one change end to end

How to run milestone M1's demonstration, [_One change ships_](../roadmap.md#m1--one-change-ships), M2's, [_The factory decides_](../roadmap.md#m2--the-factory-decides), M3's, [_A candidate gets an environment_](../roadmap.md#m3--a-candidate-gets-an-environment), and M4's, [_The factory watches what it ships_](../roadmap.md#m4--the-factory-watches-what-it-ships), by hand: a real model authoring against a real git repository, a candidate running on an [environment](../end-goal/how-humans-do-it/05-environments.md#an-environment-per-candidate) of its own, a human deciding at the three [gate](../end-goal/how-humans-do-it/03-gates/01-where-a-gate-is-and-what-decides-it.md) rows built so far, and a [release](../end-goal/how-humans-do-it/06-releases.md#the-release-record) left running as a local process — then a second change on the same service that ships with nobody deciding anything, which is what M2 is for, then two changes at once whose merges the [queue](../end-goal/how-humans-do-it/05-environments.md#the-merge-queue) orders, which is what M3 is for, and then a change that ships and is taken back off production by the factory itself, which is what M4 is for. The same paths run under `go test` as end-to-end tests in `cmd/factory`, with a fake model and scripted answers; this is the version with nothing faked, which is what there is to show somebody. [`README.md`](README.md) is the map of the code underneath it.

Everything below is run from this directory.

## What it needs

| What | Why, and what goes wrong without it |
|---|---|
| The dev database | [`docker-compose.yml`](docker-compose.yml) on port 5433. `docker compose up -d`. Every record the run writes goes there, so an unreachable database stops it at the first write. |
| An OpenRouter API key | [openrouter.ai/keys](https://openrouter.ai/keys) mints one, and the [agent](../end-goal/how-humans-do-it/10-fleet.md) sends it as a bearer token. This is the default provider because it reaches every model; the alternative, `-provider anthropic` with the token `claude setup-token` mints, is served `claude-haiku-4-5` and refused above it — see [_When it fails_](#when-it-fails) for what was measured. An API key against Anthropic's own endpoint goes in a header this code does not write and would answer 401. |
| A directory outside this repository | The secrets file, the service's repository, and the directory releases run from — a candidate environment gets a directory of its own under that one. Nothing the demo creates belongs in `end-goal/`. |
| Go and git | The build is `go build` in the service's repository and the encodings run as `go test` there, so the demo's service is a Go program. |

## Setting it up

Once, anywhere outside the repository — `~/borg-demo` below:

```sh
mkdir -p ~/borg-demo/targets
cat > ~/borg-demo/secrets <<'EOF'
model.openrouter=PASTE_THE_KEY_HERE
deploy.local=demo-credential
EOF
chmod 600 ~/borg-demo/secrets
```

Two secrets, by the names the run resolves. `model.openrouter` is the API key, read inside the model call and stored in no record — a run under `-provider anthropic` reads `model.anthropic` instead and never this one, so a file holding both is a file whose second line nothing resolves. `deploy.local` is the credential the [seam between the deployer and a deploy target](../end-goal/deferred.md#security-comes-last) requires on every operation and `localtarget` never reads — any value will do, and its being required is the point.

## The run

```sh
go run ./cmd/factory run \
  -secrets ~/borg-demo/secrets \
  -model deepseek/deepseek-v4-flash \
  -service greeter=~/borg-demo/greeter \
  -area greeting \
  -targets ~/borg-demo/targets
```

`-service` is a service as `<name>=<path>`, the path being its git repository and created when absent, and it is given once per service the install knows — one is every take up to M4's, and M5's needs two. `-provider` is which provider answers — `openrouter` by default, reading `model.openrouter`, or `anthropic`, reading `model.anthropic` — and it selects an implementation rather than configuring one, the two endpoints differing in their wire shape as well as their credential. `-model` is that provider's model id and has no default, because M1 requires the model named in configuration; OpenRouter's ids are namespaced — `deepseek/deepseek-v4-flash`, `anthropic/claude-opus-4.8` — and Anthropic's are not, so `-provider` and `-model` are set together or neither is. An id prefixed `~` is that provider's floating alias for a family's newest member; do not name one, because `-model` is the author every version records and a per-author prior is kept per model version, so an id that changes meaning underneath makes two versions recorded under one author that two models wrote. Not every model authors an implementation: `anthropic/claude-opus-5` refused the implementer's role prompt four times out of four on 2026-08-20, and `deepseek/deepseek-v4-flash` drove the whole path — spec, implementation, three gate rows, a release, a deploy without a control, and a clean chain walked back — in one attempt per stage on 2026-08-20, which is why it is named above; it is also the author every version this run writes names, a [per-author prior](../end-goal/how-humans-do-it/04-risk-score/01-factors-at-least.md) being kept per model version. `-area` names the [area](../end-goal/how-humans-do-it/02-intent-into-items/03-decomposition/02-what-an-item-names.md) the item is in and declares it where it does not exist — leave it out and the [score](../end-goal/how-humans-do-it/04-risk-score/README.md) can read neither of its context factors, which puts a human at every gate of that item and makes M2's second take impossible. `-human` names the deciding human, who is also the owner every authoring write is made as, and defaults to `owner`. `-pace` holds the model calls at least two seconds apart, so a take never sends requests in rapid succession — raise it if a provider is objecting, and leave it alone otherwise. `-candidate-environments` is how many candidate environments this substrate has room for at once and defaults to eight; a candidate that meets it waits, and the wait is written into the log. `-watch` is how long the run keeps reading its own [analysis windows](../end-goal/how-humans-do-it/08-operations.md#the-analysis-window) before leaving what is still open, open — a minute by default, and `-watch-every` is how often it reads. A window's duration is measured and never set, so a run cannot know in advance how long to wait: what it gives up on, `factory watch <service>` continues.

The first run also installs what an owner authors on: the [factory-wide settings](../end-goal/how-humans-do-it/09-gate-policy.md#one-shape-across-all-of-them) record, which exists before any project does, and production's [environment](../end-goal/how-humans-do-it/05-environments.md#records-and-one-long-lived-branch) record, which an owner does not choose because production exists everywhere. Both creations append a [policy version](../end-goal/what-the-factory-does.md#traceability), so the first line the run prints is the two versions in force — the policy's and the score's.

It prompts for the [intent](../end-goal/how-humans-do-it/02-intent-into-items/01-intake/README.md)'s statement. This one is written to survive a live run: it names the module, keeps the change inside the standard library, and asks for an encoding that does not bind the port, so a release still running from an earlier take cannot fail the next one's tests.

> A Go HTTP service, module borg.demo/greeter, package main in main.go at the repository root, standard library only, with a go.mod. It answers GET /health with status 200 and the body ok, on port 8081. Test the handler through net/http/httptest rather than by binding the port.

[_Statements that work_](#statements-that-work) below has three more and says what each part of one is for.

Then four prompts on the first take, and nothing else waits on a human:

| Prompt | What to type |
|---|---|
| `The spec author asks: …` | One line, any answer. A blank line is asked again — [the interview](../end-goal/how-humans-do-it/02-intent-into-items/02-the-interview.md) is one round or none, and this is what the round is spent on. Some runs are not asked anything. |
| `Verdict (approve, hold, reject <feedback>): ` at `deploy_to_candidate_environment` | `approve`, which is what creates the candidate's own environment and puts the build on it. `hold <a note>` leaves the item with no environment and nothing running; `reject <feedback>` sends the [item](../end-goal/how-humans-do-it/01-one-pipeline.md) back to implementation with an attempt counted there. |
| `Verdict (approve, reject <feedback>): ` at `merge_to_master` | `approve`, which admits the candidate to the merge queue. Or `reject <feedback>`, which stops the path with no release minted, no deploy recorded, and the item back at implementation with an attempt counted there — worth showing once, because a gate that cannot stop anything is not a gate. |
| `Verdict (approve, hold): ` at `deploy_to_production` | `approve`. Or `hold <a note>`, which leaves the release minted and nothing deployed, with the event queued and the change still good — no attempt counted and nothing taught to the score, which is what separates a hold from a reject. |

Every verdict is asked for on a first take because the score puts a human at each of the three rows, and it says why at each: a service's first release has no earlier release to return to, its author has never been approved, its area has no history, and the diff touches every file in the tree. Nothing about that is a shortcut — it is the design's own account of a first release, arriving as a number.

What prints between the prompts is the demonstration, in order: the two versions in force, the area, the intent taken in and refined, the service and the item [decomposed](../end-goal/how-humans-do-it/02-intent-into-items/03-decomposition/README.md) with its branch, the spec version and the [criterion](../end-goal/how-humans-do-it/03-gates/07-what-particular-gates-decide/02-spec.md) it introduces with its id and pattern, the implementation's commit, the build, then the candidate deploy row firing — the number against the threshold it was compared against and where that threshold came from, every factor with the quantity it was read from, and whether a human decides and why — the candidate environment composed with its directory and what it was composed from, the deploy onto it, the encodings checked in both directions and run twice there, the merge row firing with each criterion's outcome, the queue's order and its re-verification, master fast-forwarded, release number 1, the environment torn down, the deploy [without a control](../end-goal/how-humans-do-it/03-gates/02-the-rollout-strategy.md) to production, the analysis window opened over it — which on a service's first release says clean was never available to it — the health monitor reading until that window ends at its cap, and the walk from the deploy record back to the intent with every decision it crossed.

## The second take, which is M2's demonstration

Run the path again with the same `-service` and `-area`, on a statement that adds a route:

> Add a second route to this service: it answers GET /version with status 200 and the body 1.0.0. Keep the existing route and its test as they are. Test the new handler through net/http/httptest rather than by binding the port.

This one is asked nothing after the interview. The first take's approvals narrowed the prior on the model that wrote the change and the history of the area it was in, its release gave the service something to return to, and the diff touches part of the tree rather than all of it — so the number is under the [threshold](../end-goal/how-humans-do-it/09-gate-policy.md#what-is-in-it) at every row and the factory gives every verdict itself. Each close event's why it auto-passed reads threshold, and the row is written by the gate component rather than by a person.

That is the whole of what M2 claims, and it is worth saying out loud while it runs: the factory earned this by having been watched once, and the evidence is in the log rather than in a setting.

Then put a human back at a row and hold the deploy:

```sh
go run ./cmd/factory safeguard -parameter risk_threshold -subject gate_row:deploy_to_production
go run ./cmd/factory policy -service greeter -area greeting -gate deploy_to_production
```

The first places a [safeguard](../end-goal/how-humans-do-it/09-gate-policy.md#one-shape-across-all-of-them) (9) and prints the policy version it appended; the second prints every parameter as it is in force, where its value came from, and the safeguards that reach it. Run the path a third time and the deploy row asks for a verdict again, saying the safeguard is why rather than the number — type `hold waiting to watch it` and nothing is deployed. `go run ./cmd/factory safeguard -withdraw <safeguard-id>` puts the row back in the score's hands.

## The third take, which is M3's demonstration

Two intents in one run, on the service the takes above already shipped. Each `-intent` is one candidate:

```sh
go run ./cmd/factory run \
  -secrets ~/borg-demo/secrets \
  -model deepseek/deepseek-v4-flash \
  -service greeter=~/borg-demo/greeter \
  -area greeting \
  -targets ~/borg-demo/targets \
  -intent 'Add a route answering GET /ready with status 200 and the body ready, in a new file ready.go with its test in ready_test.go. Change no existing file.' \
  -intent 'Add a route answering GET /uptime with status 200 and the seconds since start as a decimal number, in a new file uptime.go with its test in uptime_test.go. Change no existing file.'
```

Both statements say to change no existing file, and that is the whole trick of this take: two candidates of one service are cut from the same master, so two changes to one file are two sides of a merge that conflicts — which the queue is right to reject and is not what this take is for. [_The queue rejecting a candidate_](#the-queue-rejecting-a-candidate) below is how to show that on purpose.

What to watch, in the order it prints. Both items are authored and built before either reaches a gate. Then each gets its own environment, at its own directory under `-targets`, named for its item — `ls ~/borg-demo/targets` while it runs shows two of them, which is the milestone in one command. Each candidate's build is deployed to its own environment and its criteria are decided there, so nothing either candidate runs is anything the other can see. Then the queue prints its order and takes them one at a time: the first re-verifies against the master both were cut from, fast-forwards, and is minted a number; the second re-verifies against the master the first one just created, which is a build the implementation stage never made, and takes the number after it. Both environments are torn down at their merges — the records stay, because the deploy records naming them would otherwise point at nothing — and the two releases are deployed in the order the numbers were minted.

### The queue rejecting a candidate

Run the same two intents with both of them changing one existing file — drop the "Change no existing file" sentence and ask each to add its route to `main.go`. The first candidate merges; the second one's re-verification is a merge that conflicts, so the queue rejects it, sends the item back to implementation with an attempt counted there, and writes a wait row naming the merge queue as its actor. Nothing is minted for it and its environment stays its own, which is what the design says of an item that has not merged, been dropped, or been superseded.

### Reordering the queue

Between the Merge to master gates and the queue there is nothing to type, the run doing both in one call — so the priority is worth showing on the records rather than in a take:

```sh
go run ./cmd/factory priority <item-id> -priority 5
```

A greater number goes first. It orders every queue the item waits in as an item — the gates up to and including Merge to master, and the merge queue — and no deploy: numbered releases waiting to deploy are ordered by the number and by nothing else, so an owner who rushes an item has rushed it at every gate it has left and has no way at all to reorder a deploy.

## The fourth take, which is M4's demonstration

This one is the factory taking a change back off production on its own, so it needs three things set up first.

**The analysis window's parameters, authored before the first window opens.** A window copies the size, the confidence, and the cap onto itself at the open, so authoring afterwards does not move one already open — and the values the [score](../end-goal/how-humans-do-it/04-risk-score/README.md) supplies are a size of two in a hundred and a cap of a day, which is right for a real service and unwatchable in a demonstration. Author a coarse size and a short cap on the service the takes above created:

```sh
go run ./cmd/factory author -parameter window_size -value 0.1 -service greeter
go run ./cmd/factory author -parameter window_confidence -value 0.95 -service greeter
go run ./cmd/factory author -parameter window_cap -value 60 -service greeter
go run ./cmd/factory policy -service greeter
```

A size of `0.1` is one unit of work in ten failing above the baseline, and `60` is a minute before a window that will never reach its volume ends unresolved. What `policy` prints now is those four with a reader beside each, where it said nothing read them before.

**Drift detection, installed once.** It is a second process with a store of its own, and installing it beside the factory is substrate outside the twelve duties:

```sh
go run ./cmd/driftdetector pass -secrets ~/borg-demo/secrets
go run ./cmd/driftdetector show
```

The first pass creates its schema and compares what each production target runs against what the factory recorded. `show` prints every mismatch and the last check per target — no mismatches is not health if the last check is old, which is why the second record exists at all. Run the factory without it and every check the factory makes reads a record it wrote itself; the run says so on its first line either way.

**Who a page reaches.** A mismatch belongs to none of [the twelve duties](../end-goal/what-humans-do.md), so the page it fires reaches whoever installed the drift detector:

```sh
go run ./cmd/factory people you -obligation driftdetector
go run ./cmd/factory people you -duty 12
go run ./cmd/factory people
```

Duty 12 is taking over issues the factory cannot fix on its own, which is the duty an escalation belongs to. With the declaration empty every page reaches the owner directly, which works and shows nothing about routing.

### The bad change

Then the take. A statement that ships something the criteria cannot see — the behaviour a criterion states is right, and a share of the work fails anyway:

> Add a route answering GET /flaky with status 200 and the body ok, in a new file flaky.go with its test in flaky_test.go. The handler must return status 200 and the body ok on every request, and its test must check exactly that. Separately, change the loop that appends to the BORG_SIGNAL file so that every second line it appends is error rather than ok. Leave every other existing behaviour and every existing test as they are.

The two halves are the whole point. The criterion is about the route, the test decides the route, and both are right — so the build passes every criterion in force and the run reaches production with nobody deciding anything. What no criterion says anything about is how often the work succeeds, and that is what the window reads. This is the one take whose statement asks for the emitter by name: everywhere else the implementer's standing instruction is what puts it there, and here a demonstration needs a defect the criteria cannot see. Then the window: the run keeps reading until it closes, and what prints is the arithmetic — the units the release emitted and how many failed, the same for the release below it, the log of the likelihood ratio against the crossing the confidence set, and then `harm`. What follows has no human in it: an [incident](../end-goal/how-humans-do-it/08-operations.md#incidents) on production, the release failed and its deploy advanced to rolled back, the previous release's build put back on the target and waited for, a revert [intent](../end-goal/how-humans-do-it/02-intent-into-items/01-intake/README.md) taken in from the detector, and the rollback reported on mail and chat. It fires no page, because the factory does not page to inform.

`curl -s localhost:8081/health` still answers, and `curl -s localhost:8081/flaky` is gone — which is the demonstration in one command.

### The hold, and shipping the revert

Master still holds the change that was rolled back, so every production deploy of that service now waits:

```sh
go run ./cmd/factory run … -intent 'Add a route answering GET /extra with status 200 and the body extra, in a new file extra.go with its test in extra_test.go. Change no existing file.'
```

It merges, it is minted a number, and its deploy prints `waits at deploy_to_production: a rollback's revert has not shipped`. Nothing is written for that hold — it is computed from records that already exist and it lifts itself — and the line says which item to approve through it if you want to.

The revert lifts it. Its intent is already waiting, taken in by the health monitor at the rollback, so give the run that intent's own statement and it works that one rather than taking in a second saying the same thing:

```sh
docker compose exec -T postgres psql -U factory -d factory -tAc \
  "select statement from intent where source = 'detector' and state = 'unrefined' order by at desc limit 1"
```

Pass that as `-intent` with the release the run is holding as a second `-intent`, and watch the order: the revert deploys ahead of the release the hold is holding, which is the one place the number does not order deploys. Then the hold lifts and the release behind it deploys.

### Approving through, which is the one to show carefully

Before the revert ships, push the held release through by hand:

```sh
go run ./cmd/factory approve <item-id> \
  -secrets ~/borg-demo/secrets -targets ~/borg-demo/targets \
  -reason 'the incident is worse than the defect'
```

The row fires with the hold on its open event, the human's verdict and reason close it, and the deploy happens. What it accepts is the defect that was just removed — so the window that opens over it fails it again, and the run says so. It is the most damaging thing in the factory to approve through and the one most likely to be tried during an incident, which is the whole reason for showing it.

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

```sh
go run ./cmd/factory author -parameter window_size -value 0.1 -service reader
go run ./cmd/factory author -parameter window_confidence -value 0.95 -service reader
go run ./cmd/factory author -parameter window_cap -value 60 -service reader
```

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

```sh
go run ./cmd/factory safeguard -parameter safeguard_predicate -subject contract_element:greeter/health/Detail -bound read
```

The detector still raises the removal — a safeguard never stops the item existing, only passing — and the removal candidate is rejected at its merge row naming the safeguard and its author, which is the blocked removal asking the consumer to confirm. `safeguard -withdraw <safeguard-id>` is the confirmation, and the next candidate goes through.

## The sixth take, which is M6's demonstration

This one needs no new service and no new statement. What it needs is outcomes, which the fourth take already produces — so run that one first, all the way through the rollback, and then ask the score what it learned:

```sh
go run ./cmd/factory learn
go run ./cmd/factory learn -dry     # the same reading, appending nothing
```

The pass prints every value the score supplies, the subject each was learned about, and the evidence behind it — and it marks each one that has moved away from the version in force. After the fourth take there is at least one movement and it is the threshold: the bad release was auto-passed on the number at three rows and its window failed it, so each of those rows now supplies a threshold one band below the number it passed it at. The line says so in as many words: `1 change(s) auto-passed on the number at this row turned out badly, the lowest of them scoring 0.14, so the threshold is one band below it`.

Then run anything again. The rows that auto-passed before the rollback now ask for a verdict, and the firing prints why the threshold it was compared against is what it is. That is the milestone: a supplied parameter moved because outcomes moved it, and the same change is decided differently afterwards.

**The window limit, which is the value the design spells out.** It rises per three windows closing without failing a release and falls at a rollback that swept — and it only rises where nobody authored it, so leave the window limit the fourth take authored out and let three windows close:

```sh
go run ./cmd/factory watch greeter -secrets ~/borg-demo/secrets -targets ~/borg-demo/targets
go run ./cmd/factory learn
go run ./cmd/factory policy -service greeter
```

`policy` is where the movement is read as what is in force: `window_limit = 2 (supplied), moved by outcomes on svc_…`, with the evidence under it. Author a window limit and the same line reads `authored` and the score's own value is not in force at all — which is the division the design draws, and the reason the fourth take's authored parameters have to be left out of this one.

**The movement as records.** A supplied value is a field of a score version, and every decision names the version it was decided under — so the movement is read by following a decision to its version and that version to the one it superseded:

```sh
docker compose exec -T postgres psql -U factory -d factory \
  -c "select id, formula_version, supersedes, jsonb_pretty(supplied::jsonb) from score_version order by at"

# What each window closed on, which is what an exit is recomputable from — and what
# says whether the size it watched at was reachable by this service's traffic.
docker compose exec -T postgres psql -U factory -d factory \
  -c "select service_id, size, exit, closed_on_units, closed_on_failures, closed_on_baseline_units from analysis_window order by at"
```

The superseded version still says what it said. A decision taken before the movement is readable against the value it was decided under and not against today's, which is what an append-only record is for.

**The held-out sample**, which is the one thing here that changes what the factory decides rather than what it supplies. It is random — one firing in ten of those the score would have gated — so it cannot be summoned, and it is worth watching for rather than demonstrating. When it selects, the firing reads:

```
  held out: the score's sample selected this item at this firing
  no human decides: the score held this item out of a gate it would have gated, which is the one thing in the factory that removes a human from a row
```

and the deploy that follows says `its window runs to the cap — the longest watch there is`. Every row below that one on the same item reads `selected this item at an earlier gate`: the sample selects an item, not a firing, so an item selected once reaches production with a human removed at each gate the score would have gated. `learn` lists the items it has selected — and says so where it has selected none, because a factory that has never sampled has a threshold that can fall and cannot rise. A row a safeguard reached keeps its human however the draw falls — `safeguard -parameter risk_threshold -subject gate_row:merge_to_master` and the sample never passes that row again, which is the one guarantee a safeguard has to keep.

## Authoring gate policy

Six subcommands are duty 8, duty 9, the priority a queue is reordered with, and the People declaration a page routes on, none of which has a screen of its own until M7:

```sh
go run ./cmd/factory area payments -inside greeting
go run ./cmd/factory author -parameter risk_threshold -value 0.2 -gate merge_to_master
go run ./cmd/factory author -parameter attempt_limit -value 5 -stage implementation
go run ./cmd/factory author -parameter window_limit -value 2 -service greeter
go run ./cmd/factory policy -service greeter -area greeting
go run ./cmd/factory priority <item-id> -priority 5
go run ./cmd/factory people you -duty 12
```

`author` asks for the subject the parameter needs and no other, the record a parameter is a field of being a fact of the parameter: a threshold is authored on an environment for one gate row, an [attempt limit](../end-goal/how-humans-do-it/03-gates/05-the-attempt-limit.md) on the factory-wide settings record for one stage, an [item-size target](../end-goal/how-humans-do-it/02-intent-into-items/03-decomposition/README.md) on an area, and the [analysis window](../end-goal/how-humans-do-it/08-operations.md#the-analysis-window)'s four on a service. Authoring the threshold down to `0.2` before the second take is the other way to show a gate deciding — the item that auto-passed at `0.3` reads over `0.2` and a human is asked again.

What `policy` says about one of the eight parameters is that nothing reads it yet: the item-size target waits for a decomposition that sizes anything. Authoring it changes nothing today, and the print says so rather than leaving somebody to find out. The [list of allowed predicate kinds](../end-goal/how-humans-do-it/07-contracts.md#what-a-consumer-declares) was the other until M5, and it is also the one parameter whose unauthored value is neither the score's nor nothing: it is the five kinds of predicate the factory can decide, which is what an owner extends rather than replaces, and the print names that source. The other seven name their reader — and `window_limit = 2` above is worth authoring before a take with two intents, because at the one the score supplies the second release merges and its deploy waits behind the first one's window.

## Statements that work

The statement is the whole of what a human gives the factory, and what it says decides whether the run reaches a deploy — so these are written for the demo rather than for a real backlog. Five things earn their place in one:

| What the statement names | Why it is in there |
|---|---|
| The module path, and `package main` in `main.go` at the repository root | The build is `go build` in the repository root, so a program written anywhere else does not build. |
| Standard library only | Nothing in the run fetches a dependency, so a module requirement fails the build with the demo watching. |
| A `go.mod` | The build needs one and the implementation role writes what the role prompt asks for. |
| One behaviour, stated as a rule | The spec is one [criterion](../end-goal/how-humans-do-it/03-gates/07-what-particular-gates-decide/02-spec.md), so a statement naming three behaviours still yields one, and the other two ship with nothing deciding them. |
| A test that does not bind the port | The encodings run as `go test` on this machine, where a release from an earlier take may still be holding it. |

Any of these four is a whole take. Each one is one behaviour a sentence can state and a test can decide, which is what makes a criterion out of it:

> A Go HTTP service, module borg.demo/greeter, package main in main.go at the repository root, standard library only, with a go.mod. It answers GET /health with status 200 and the body ok, on port 8081. Test the handler through net/http/httptest rather than by binding the port.

> A Go HTTP service, module borg.demo/clock, package main in main.go at the repository root, standard library only, with a go.mod. It answers GET /time with status 200 and the current time as RFC 3339 in UTC, on port 8082. Test the handler through net/http/httptest rather than by binding the port.

> A Go HTTP service, module borg.demo/adder, package main in main.go at the repository root, standard library only, with a go.mod. It answers GET /sum?a=1&b=2 with status 200 and the sum as a decimal number, on port 8083. Test the handler through net/http/httptest rather than by binding the port.

> A Go HTTP service, module borg.demo/echo, package main in main.go at the repository root, standard library only, with a go.mod. It answers POST /echo with status 200 and the request body unchanged, on port 8084. Test the handler through net/http/httptest rather than by binding the port.

One more kind of statement is the second change on a service already shipped, which is [_The second take_](#the-second-take-which-is-m2s-demonstration) above. Watch the spec stage on that one. The implementation role is told every criterion in force for its build — the ones the merged items introduced and the one this spec adds — and the build is refused unless an encoding names each, which is the check that makes the criterion id the thing the whole demonstration is followed along.

A statement for [_The third take_](#the-third-take-which-is-m3s-demonstration) has one more part: which files the change may touch. Two candidates of one service are cut from the same master, so what decides whether the queue can merge both is whether they wrote to the same file — and saying so in the statement is the only place a run of this interface can say it.

What not to ask for, on a day people are watching: anything needing a dependency, a database, a container, or a port something else holds; a change to two services, since decomposition here writes one item on one service; and a program that exits as soon as it starts, which deploys correctly and then shows nothing running.

## Showing it afterwards

```sh
curl -s localhost:8081/health; echo          # the software the factory deployed, answering
pgrep -af borg-demo/targets                  # the build running as a process
ls ~/borg-demo/targets                       # production's directory, and one per candidate environment

# Every environment, and what a candidate's was composed from and when it was torn down.
docker compose exec -T postgres psql -U factory -d factory \
  -c "select kind, name, item_id, composed_from, torn_down_at from environment order by at"

# What each run on a candidate environment decided, per build and criterion.
docker compose exec -T postgres psql -U factory -d factory \
  -c "select build_id, criterion_id, outcome from criterion_result order by at"

# The decision is two chained rows: an opening naming the versions, a closing carrying the verdict.
docker compose exec -T postgres psql -U factory -d factory \
  -c "select seq, shape, part, actor_kind, actor_name, policy_version, score_version, closes from decision_log order by seq"

# What the score published when those decisions were taken, and every write an owner made.
docker compose exec -T postgres psql -U factory -d factory \
  -c "select id, formula_version, supersedes, supplied from score_version order by at"
docker compose exec -T postgres psql -U factory -d factory \
  -c "select action, parameter, subject_kind, qualifier, actor_name from policy_version order by at"

go run ./cmd/factory walk <deploy-id>        # the link walk on its own
```

The walk is the direction the milestone is named for. Every line it prints is a stored field on a record, read through the package that owns it — nothing is reconstructed, and that is the claim a demonstration of this milestone is actually making.

## Resetting between takes

A take leaves three things behind: the records, a git repository whose master is at the change, and a process holding the port. Reset all three:

```sh
pkill -f borg-demo/targets
rm -rf ~/borg-demo/greeter ~/borg-demo/targets/*
docker compose exec -T postgres psql -U factory -d factory \
  -c 'drop schema public cascade; create schema public;'
```

Drop the schema before the first M4 take on a database an earlier milestone wrote, and drop the drift detector's own beside it — `drop schema if exists driftdetector cascade;` — or a mismatch from a previous take goes on holding every production deploy. Every milestone so far has added a column to a table an earlier one wrote and `create table if not exists` does not alter one that is already there, so the first write against the old shape fails on the column — [`README.md`](README.md#running-it) says which columns and whose question it is.

Dropping the schema drops the score version, the policy version, every safeguard, and every outcome the score reads — so a factory reset this way puts a human back at every gate of its next first release, which is the mechanism working rather than a reset that failed. Or keep the records and run again with `-service greeter2=~/borg-demo/greeter2` and a different port in the statement; that service's first release is decided by a human too, the prior on the model being the one thing it inherits.

## When it fails

| What you see | What it is |
|---|---|
| `The implementer's reply was refused; N attempt(s) left` | Not a failure. The model wrote prose around its file blocks, the protocol refused it rather than repairing it, and the stage is retrying inside its [attempt limit](../end-goal/how-humans-do-it/03-gates/05-the-attempt-limit.md). The take carries on if a later attempt parses. |
| `used all 3 attempts … stuck on this item` | The limit is spent and the factory is saying it cannot do this one. The item keeps the count and the spend of every attempt, refused ones included, which is what an escalation is read from once [_Work_](../end-goal/how-humans-do-it/11-screens.md#work-ops-factory-people) exists to read it on at M7. Run the take again, or run it on a stronger model — `claude-haiku-4-5` was refused three times out of three on 2026-08-18, which is the model a subscription take is held to and the reason the default provider is the other one. |
| `go build … no required module provides` | The model reached outside the standard library. The statement above says not to; say it again more plainly. |
| `./main.go:N: undefined: X` or `imported and not used` | The model wrote Go that does not compile. The [Implementation gate](../end-goal/how-humans-do-it/03-gates/07-what-particular-gates-decide/05-implementation.md) is where the design rejects a build for exactly this, with Reject with feedback as an action, and that gate is not built — so the run stops at the compile instead, with the item readable at the stage it reached and nothing retried. Measured on 2026-08-20: `deepseek/deepseek-v4-flash` produced a non-compiling `main.go` on three takes of four — a `httptest` call with no import, an unused `fmt` — while every other part of the path held. Run it again; what fixes it properly is that gate. |
| `go: cannot find main module, but found .git/config` | The model wrote no `go.mod`, so there is nothing to build. The cause was found on 2026-08-20 and is not the implementer: the spec author was compressing the statement to its behaviour and dropping every constraint around it — the module, the layout, the go.mod the statement asks for in as many words — and the implementation stage is given the spec and never the statement, so it wrote what it was told. Measured across three models, the spec came back at 52 bytes on `deepseek/deepseek-v4-flash` and 71 on `deepseek/deepseek-v4-pro` from a four-sentence statement. [`agent/specauthor.go`](agent/specauthor.go)'s prompt now requires the spec to restate every constraint the statement makes, and the same model then authored a 451-byte spec and a `go.mod` with it. `claude-haiku-4-5` left it out on both takes of 2026-08-18, before that was understood. If it recurs, read the spec the run printed before blaming the implementer. |
| `<criterion id> is in force and no encoding in the build names it` | The build has a test for the criterion and the build cannot see it. An encoding is picked out by the id appearing exactly, and a Go test's name cannot begin with a lowercase id — a model asked for the id in a test's name wrote `func TestCr_<id>` on 2026-08-20, which is the id with its c capitalised and so a different string. [`agent/implementer.go`](agent/implementer.go)'s prompt now names the two forms that work, `func Test_cr_<id>` and the id in a comment, and names that one as failing. [`criterion/encoding.go`](criterion/encoding.go) is the matcher, and its own comment records the first time this collision was found — so a change to either belongs with a change to the other. |
| `go: downloading go1.x` | The `go.mod` it wrote names a newer toolchain than the one installed. Edit that line and run the take again. |
| `go.mod:3: invalid go version '1.x'` | The statement asked for "a Go version" and the model wrote the placeholder rather than choosing one. Measured on 2026-08-20 with `deepseek/deepseek-v4-flash`. An underdetermined request is what the [interview](../end-goal/how-humans-do-it/02-intent-into-items/02-the-interview.md) exists for and the model did not ask, so what is fixed is the request: name the version, which is an owner supplying a constraint (2). |
| `The encodings ran twice on the candidate environment and failed both times` | Not an error. The merge row fires anyway and shows each criterion's outcome, and you can approve over it — which is a fair thing to show, since a human deciding against the evidence is what the row is for. |
| `The encodings disagreed between two runs, so every criterion is undecided` | The suite is not deterministic. Undecided is read at the merge row the way a failure is, and the way out is to author the encoding again rather than to run it again — so send the item back rather than approving over it. |
| `the queue rejected item … merging master into the candidate branch failed` | Two candidates wrote to one file. The item is back at implementation with an attempt counted there, which is the queue working; see [_The queue rejecting a candidate_](#the-queue-rejecting-a-candidate). |
| `waits at deploy_to_candidate_environment: the substrate has no room` | `-candidate-environments` is set lower than the number of intents. The wait is in the log with the deploy agent as its actor, and it lifts when an item merges and frees one. |
| `bind: address already in use` | A release from an earlier take still holds the port. `pkill -f borg-demo/targets`. |
| `waits at deploy_to_production: the service holds as many analysis windows open as the window limit allows` | The window limit is doing its work. One window open per service is where the score starts, and it rises only after three of that service's windows have closed without failing a release, so the next release waits for that window to close — a wait on the factory, which writes nothing and pages nobody. `go run ./cmd/factory watch greeter …` closes what is open, or author the window limit higher. |
| `waits at deploy_to_production: a rollback's revert has not shipped` | Master still holds the change that was rolled back, so deploying anything built on it would redeliver the defect. The revert is not held and deploys ahead of this; `approve <item-id>` pushes it through and accepts the defect. |
| `neither exit is reachable: the release has no baseline` | A service's first release, or a release whose baseline's window has not closed yet. Nothing about it is discovered by watching and its window ends at the cap, which is the design's own account rather than a fault. |
| `N analysis window(s) are still open` at the end of a run | The run gave up before they closed, which a window's duration being measured and never set makes normal. `go run ./cmd/factory watch <service> …` continues from there, and nothing else closes one. |
| `MISMATCH` from the drift detector | What the factory recorded is not what the target runs. It holds that service's production deploys and pages, and only `driftdetector clear` ends it — the factory cannot, by design. |
| `the model API answered 401` or `403` | The secrets file has no key in it for the provider named, or the credential has expired — mint another at [openrouter.ai/keys](https://openrouter.ai/keys), or `claude setup-token` again under `-provider anthropic`. A 403 on a credential that is current is the account not entitling this call, which is an account question and not a code one. |
| `the model refused the request` | The model declined on its provider's policy grounds, and the sentence after the colon is the model's own. It is not retried — the request's shape is not what is wrong, and a stage that retried it would spend its attempt limit on a verdict already given. Measured on 2026-08-20: `anthropic/claude-opus-5` is served the spec stage and refuses the implementer's role prompt four times out of four under the cyber category, for a role prompt asking for a health-check HTTP handler, its own reasoning showing it part-way through writing that handler when the classifier stopped it. `deepseek/deepseek-v4-flash`, `anthropic/claude-opus-4.8` and `anthropic/claude-sonnet-5` author the same role prompt. Name a different model. |
| `the model API answered 200 carrying an error` | Only `-provider openrouter` answers this way: the request reached OpenRouter and the provider it routed to refused. The code and the message the body carried are in the error — an upstream rate limit, a model not serving, a request the upstream would not take. Nothing about the factory's own request is wrong, and the model id is the first thing to check. |
| `the model API answered 429` on every model but Haiku, under `-provider anthropic` | Not the account's allowance, and not a wait: the answer carries no `retry-after` and no rate-limit header, and the account's own buckets read as allowed on the same credential. What was measured on 2026-08-18, one variable at a time: a subscription token is served `claude-haiku-4-5` on a plain request and refused Fable 5, Opus 5, 4.8, 4.7, 4.6, Sonnet 5 and 4.6, and the only thing that changes a refusal into a 200 is the request carrying Claude Code's own system prompt — not a beta header and not a user agent. The factory sends its roles' prompts, so it is served Haiku and nothing above it, and claiming to be Claude Code to get the rest is not something this repository does. Re-measured on 2026-08-19 and unchanged: Opus 5 refused on the first call, Haiku served through the spec stage. This is what `-provider openrouter` is the default for: treat a subscription take as a Haiku take, and send anything that has to reach a gate through the other provider. |

A failure stops the run and damages nothing: each step writes its record before the next one runs, so what stopped halfway leaves an item readable at the stage it reached. A run that stopped after a Merge to master gate approved leaves that item in the queue, and the next run on the same service finishes it — the queue's membership is the service's, so there is nothing to clear by hand. The one window that is still open is between master's fast-forward and the release being minted: the queue holds one lock per service across the whole merge, so two runs cannot interleave and two candidates cannot read one number, but a crash between those two leaves master at a commit no release record names, and what repairs a record disagreeing with what is there is the drift detector, which is now installed: `go run ./cmd/driftdetector pass` finds it, and clearing it is a human's. [`mergequeue/doc.go`](mergequeue/doc.go) says so where it happens.

## What it does not show

Say this out loud to anyone watching, because the run looks more complete than the factory is. Five of the eight gate rows are not built, and the third action the production deploy row has, a safeguard on the [strategy](../end-goal/how-humans-do-it/03-gates/02-the-rollout-strategy.md), is refused with its reason: a target that runs a release as a local process moves a process rather than traffic, so the strategy that keeps a [control](../end-goal/how-humans-do-it/08-operations.md#the-health-monitor) is unavailable here and every deploy goes without a control.

Four things about the watching are worth saying plainly, and all four follow from that. **No control is ever started**, so the [comparison](../end-goal/how-humans-do-it/08-operations.md#the-health-monitor) is the weak fallback the design names: the release is read against the recent history of the release a rollback from it would return to, and the difference age makes between a process just started and one that has been running for a week is in that reading and unanswered. **Every [rollback](../end-goal/how-humans-do-it/06-releases.md#rollback) is the slow one**, the target's build redeployed and waited for, because there is no control to shift traffic onto. **The traffic is the release exercising itself** — these targets receive none, so the implementation role is told to append a line per unit of work it does, and a window passed says the boundary works rather than that the service is well. And **an explicit health threshold is not built**: the design lets an owner state one absolutely beside the comparison, which is the only thing that could fail a service's first release, and this factory has no parameter to state it on.

The window limit above one is honest and weak here for the same reason. Two windows may be open at once and both releases are recorded, but a deploy without a control replaces the process — so the lower release stops emitting the moment the upper one deploys, and only the newest release is really being measured. On a substrate that keeps a control both would go on serving and both would go on being read.

Two things about the candidate environment are worth saying plainly. It is composed from the [current releases](../end-goal/how-humans-do-it/06-releases.md#the-deploy-record) of the candidate's dependencies, and decomposition here yields one item per intent — so no run of this interface declares a dependency and every composition names nothing. And the queue re-verifies serially: the design has a candidate re-verify against master plus every candidate ahead of it, which is what makes a long queue fast, and the speculation is the queue's own state that nothing outside it reads, so it can arrive later without changing a record. A queue of ten waits ten re-verifications here.

Three things about the score are worth saying plainly. Its formula is authored and stays authored — the weights and the breakpoints were written by hand and calibrated so a first release is decided by a human and the item after it is not, and what learning moves is the seven values the score supplies rather than how the number is computed, which is the division [gate policy](../end-goal/how-humans-do-it/09-gate-policy.md#what-is-not-in-it) draws. **Five of the seven values move both ways and two move one way.** Both ends of each parameter are evidence: one end is something going wrong, the other is the parameter costing more than it returns, which gate policy's own table states for every row. So the cap follows how long a window of that service actually takes, the attempt limit falls where nothing has ever needed a second try, and the window's size is never finer than the traffic can rule anything out at — a size finer than that ends every window at the cap and protects nothing. The two that move one way say so where the rule is published: nothing here shows that a confidence was too high, and nothing measures the other end of an item-size target. And **the sample gets half of what the design gives it**: a held-out release should take a strategy that keeps a [control](../end-goal/how-humans-do-it/08-operations.md#the-health-monitor), and every deploy here goes without a control — so it is watched by the same confounded comparison as every other release and the longest watch available is all it gets. What its evidence supports is that a comparison was available, not that an unsampled release on the same author would have read the same.

And the terminal is the whole interface: the four [screens](../end-goal/how-humans-do-it/11-screens.md#work-ops-factory-people) come at M7, and a crude interface until then is what deferring them costs.
