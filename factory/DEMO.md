# Demoing one change end to end

How to run milestone M1's demonstration, [_One change ships_](../roadmap.md#m1--one-change-ships), and M2's, [_The factory decides_](../roadmap.md#m2--the-factory-decides), by hand: a real model authoring against a real git repository, a human deciding at the two [gate](../end-goal/how-humans-do-it/03-gates.md#where-a-gate-is-and-what-decides-it) rows built so far, a [release](../end-goal/how-humans-do-it/06-releases.md#the-release-record) left running as a local process — and then a second change on the same service that ships with nobody deciding anything, which is what M2 is for. The same paths run under `go test` as end-to-end tests in `cmd/factory`, with a fake model and scripted answers; this is the version with nothing faked, which is what there is to show somebody. [`README.md`](README.md) is the map of the code underneath it.

Everything below is run from this directory.

## What it needs

| What | Why, and what goes wrong without it |
|---|---|
| The dev database | [`docker-compose.yml`](docker-compose.yml) on port 5433. `docker compose up -d`. Every record the run writes goes there, so an unreachable database stops it at the first write. |
| A Claude subscription token | `claude setup-token` mints one. The [agent](../end-goal/how-humans-do-it/10-fleet.md) sends it as a bearer token under the beta header that scheme requires, which is the one scheme it sends — an API key goes in a header this code does not write and would answer 401. |
| A directory outside this repository | The secrets file, the service's repository, and the directory releases run from. Nothing the demo creates belongs in this tree. |
| Go and git | The build is `go build` in the service's repository and the encodings run as `go test` there, so the demo's service is a Go program. |

## Setting it up

Once, anywhere outside the repository — `~/borg-demo` below:

```sh
mkdir -p ~/borg-demo/targets
claude setup-token                    # prints the token; paste it below
cat > ~/borg-demo/secrets <<'EOF'
model.anthropic=PASTE_THE_TOKEN_HERE
deploy.local=demo-credential
EOF
chmod 600 ~/borg-demo/secrets
```

Two secrets, by the names the run resolves. `model.anthropic` is the subscription token, read inside the model call and stored in no record. `deploy.local` is the credential the [seam between an agent and a deploy target](../end-goal/deferred.md#security-comes-last) requires on every operation and `localtarget` never reads — any value will do, and its being required is the point.

## The run

```sh
go run ./cmd/factory run \
  -secrets ~/borg-demo/secrets \
  -model claude-opus-5 \
  -repo ~/borg-demo/greeter \
  -service greeter \
  -area greeting \
  -targets ~/borg-demo/targets
```

`-model` is the provider's model id and has no default, because M1 requires the model named in configuration; it is also the author every version this run writes names, an [authorship prior](../end-goal/how-humans-do-it/04-risk-score.md#factors-at-least) being kept per model version. `-repo` is created if it is not there. `-area` names the [area](../end-goal/how-humans-do-it/02-intent-into-items.md#what-an-item-names) the item is in and declares it where it does not exist — leave it out and the [score](../end-goal/how-humans-do-it/04-risk-score.md) can read neither of its context factors, which puts a human at every gate of that item and makes M2's second take impossible. `-human` names the deciding human, who is also the owner every authoring write is made as, and defaults to `owner`. `-pace` holds the model calls at least two seconds apart, so a take never sends requests in rapid succession — raise it if a provider is objecting, and leave it alone otherwise.

The first run also installs what an owner authors on: the [factory policy](../end-goal/how-humans-do-it/09-gate-policy.md#one-shape-across-all-of-them) record, which exists before any project does, and production's [environment](../end-goal/how-humans-do-it/05-environments.md#records-and-one-long-lived-branch) record, which an owner does not choose because production exists everywhere. Both creations append a [policy version](../end-goal/what-the-factory-does.md#traceability), so the first line the run prints is the two versions in force — the policy's and the score's.

It prompts for the [intent](../end-goal/how-humans-do-it/02-intent-into-items.md#intake)'s statement. This one is written to survive a live run: it names the module, keeps the change inside the standard library, and asks for an encoding that does not bind the port, so a release still running from an earlier take cannot fail the next one's tests.

> A Go HTTP service, module borg.demo/greeter, package main in main.go at the repository root, standard library only, with a go.mod. It answers GET /health with status 200 and the body ok, on port 8081. Test the handler through net/http/httptest rather than by binding the port.

[_Statements that work_](#statements-that-work) below has three more and says what each part of one is for.

Then three prompts on the first take, and nothing else waits on a human:

| Prompt | What to type |
|---|---|
| `The spec author asks: …` | One line, any answer. A blank line is asked again — [the interview](../end-goal/how-humans-do-it/02-intent-into-items.md#the-interview) is one round or none, and this is what the round is spent on. Some runs are not asked anything. |
| `Verdict (approve, reject <feedback>): ` at `merge_to_master` | `approve`. Or `reject <feedback>`, which stops the path with no release minted, no deploy recorded, and the [item](../end-goal/how-humans-do-it/01-one-pipeline.md) left at the implementation stage — worth showing once, because a gate that cannot stop anything is not a gate. |
| `Verdict (approve, hold): ` at `deploy_to_production` | `approve`. Or `hold <a note>`, which leaves the release minted and nothing deployed, with the event queued and the change still good — no attempt counted and nothing taught to the score, which is what separates a hold from a reject. |

Both verdicts are asked for on a first take because the score puts a human at both rows, and it says why at each: a service's first release has no earlier release to return to, its author has never been approved, its area has no history, and the diff touches every file in the tree. Nothing about that is a shortcut — it is the design's own account of a first release, arriving as a number.

What prints between the prompts is the demonstration, in order: the two versions in force, the area, the intent taken in and refined, the service and the item [cut](../end-goal/how-humans-do-it/02-intent-into-items.md#the-cut) with its branch, the spec version and the [criterion](../end-goal/how-humans-do-it/03-gates.md#spec) it introduces with its id and pattern, the implementation's commit, the build, the encodings checked in both directions and run, then each gate firing — the number against the threshold it was compared against and where that threshold came from, every factor with the quantity it was read from, and whether a human decides and why — the verdict, master fast-forwarded, release number 1, the [straight](../end-goal/how-humans-do-it/03-gates.md#the-rollout-strategy) deploy, and the walk from the deploy record back to the intent with every decision it crossed.

## The second take, which is M2's demonstration

Run the path again with the same `-service`, `-repo`, and `-area`, on a statement that adds a route:

> Add a second route to this service: it answers GET /version with status 200 and the body 1.0.0. Keep the existing route and its test as they are. Test the new handler through net/http/httptest rather than by binding the port.

This one is asked nothing after the interview. The first take's two approvals narrowed the prior on the model that wrote the change and the history of the area it was in, its release gave the service something to return to, and the diff touches part of the tree rather than all of it — so the number is under the [threshold](../end-goal/how-humans-do-it/09-gate-policy.md#what-is-in-it) at both rows and the factory gives both verdicts itself. Each closing row says it was auto-passed by the threshold and is written by the gate component rather than by a person.

That is the whole of what M2 claims, and it is worth saying out loud while it runs: the factory earned this by having been watched once, and the evidence is in the log rather than in a setting.

Then put a human back at a row and hold the deploy:

```sh
go run ./cmd/factory pin -parameter risk_threshold -subject gate_row:deploy_to_production
go run ./cmd/factory policy -service greeter -area greeting -gate deploy_to_production
```

The first places a [pin](../end-goal/how-humans-do-it/09-gate-policy.md#one-shape-across-all-of-them) (9) and prints the policy version it appended; the second prints every parameter as it is in force, where its value came from, and the pins that reach it. Run the path a third time and the deploy row asks for a verdict again, saying the pin is why rather than the number — type `hold waiting to watch it` and nothing is deployed. `go run ./cmd/factory pin -withdraw <pin-id>` puts the row back in the score's hands.

## Authoring gate policy

Four subcommands are duty 8 and duty 9, which have no surface of their own until M7:

```sh
go run ./cmd/factory area payments -inside greeting
go run ./cmd/factory author -parameter risk_threshold -value 0.2 -gate merge_to_master
go run ./cmd/factory author -parameter attempt_bound -value 5 -stage implementation
go run ./cmd/factory author -parameter k -value 2 -service greeter
go run ./cmd/factory policy -service greeter -area greeting
```

`author` asks for the subject the parameter needs and no other, the record a parameter is a field of being a fact of the parameter: a threshold is authored on an environment for one gate row, an [attempt bound](../end-goal/how-humans-do-it/03-gates.md#the-attempt-bound) on the factory policy record for one stage, an [item-size target](../end-goal/how-humans-do-it/02-intent-into-items.md#the-cut) on an area, and the [watch window](../end-goal/how-humans-do-it/08-operations.md#the-watch-window)'s four on a service. Authoring the threshold down to `0.2` before the second take is the other way to show a gate deciding — the item that auto-passed at `0.3` reads over `0.2` and a human is asked again.

What `policy` says about four of the eight parameters is that nothing reads them yet: the item-size target waits for a cut that sizes anything, the [predicate catalog](../end-goal/how-humans-do-it/07-contracts.md#what-a-consumer-declares) for contracts at M5, and K and the window's parameters for M4. Authoring one changes nothing today, and the print says so rather than leaving somebody to find out.

## Statements that work

The statement is the whole of what a human gives the factory, and what it says decides whether the run reaches a deploy — so these are written for the demo rather than for a real backlog. Five things earn their place in one:

| What the statement names | Why it is in there |
|---|---|
| The module path, and `package main` in `main.go` at the repository root | The build is `go build` in the repository root, so a program written anywhere else does not build. |
| Standard library only | Nothing in the run fetches a dependency, so a module requirement fails the build with the demo watching. |
| A `go.mod` | The build needs one and the implementation role writes what the brief asks for. |
| One behaviour, stated as a rule | The spec is one [criterion](../end-goal/how-humans-do-it/03-gates.md#spec), so a statement naming three behaviours still yields one, and the other two ship with nothing deciding them. |
| A test that does not bind the port | The encodings run as `go test` on this machine, where a release from an earlier take may still be holding it. |

Any of these four is a whole take. Each one is one behaviour a sentence can state and a test can decide, which is what makes a criterion out of it:

> A Go HTTP service, module borg.demo/greeter, package main in main.go at the repository root, standard library only, with a go.mod. It answers GET /health with status 200 and the body ok, on port 8081. Test the handler through net/http/httptest rather than by binding the port.

> A Go HTTP service, module borg.demo/clock, package main in main.go at the repository root, standard library only, with a go.mod. It answers GET /time with status 200 and the current time as RFC 3339 in UTC, on port 8082. Test the handler through net/http/httptest rather than by binding the port.

> A Go HTTP service, module borg.demo/adder, package main in main.go at the repository root, standard library only, with a go.mod. It answers GET /sum?a=1&b=2 with status 200 and the sum as a decimal number, on port 8083. Test the handler through net/http/httptest rather than by binding the port.

> A Go HTTP service, module borg.demo/echo, package main in main.go at the repository root, standard library only, with a go.mod. It answers POST /echo with status 200 and the request body unchanged, on port 8084. Test the handler through net/http/httptest rather than by binding the port.

One more kind of statement is the second change on a service already shipped, which is [_The second take_](#the-second-take-which-is-m2s-demonstration) above. Watch the spec stage on that one. The implementation role is told both criteria — the one the first item introduced and the one this spec adds — and the build is refused unless an encoding names each, which is the check that makes the criterion id the thing the whole demonstration is followed along.

What not to ask for, on a day people are watching: anything needing a dependency, a database, a container, or a port something else holds; a change to two services, since the cut here writes one item on one service; and a program that exits as soon as it starts, which deploys correctly and then shows nothing running.

## Showing it afterwards

```sh
curl -s localhost:8081/health; echo          # the software the factory deployed, answering
pgrep -af borg-demo/targets                  # the release running as a process

# The decision is two chained rows: an opening naming the versions, a closing carrying the verdict.
docker compose exec -T postgres psql -U factory -d factory \
  -c "select seq, shape, part, actor_kind, actor_name, policy_version, score_version, closes from decision_log order by seq"

# What the score published when those decisions were taken, and every write an owner made.
docker compose exec -T postgres psql -U factory -d factory \
  -c "select id, formula_version, supersedes from score_version order by at"
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

Drop the schema before the first M2 take on a database an earlier milestone wrote. Three tables gained a column and `create table if not exists` does not alter one that is already there, so the first write against the old shape fails on the column — [`README.md`](README.md#running-it) says which three and whose question it is.

Dropping the schema drops the score version, the policy version, every pin, and every outcome the score reads — so a factory reset this way puts a human back at every gate of its next first release, which is the mechanism working rather than a reset that failed. Or keep the records and run again with `-service greeter2 -repo ~/borg-demo/greeter2` and a different port in the statement; that service's first release is decided by a human too, the prior on the model being the one thing it inherits.

## When it fails

| What you see | What it is |
|---|---|
| `The implementer's reply was refused; N attempt(s) left` | Not a failure. The model wrote prose around its file blocks, the protocol refused it rather than repairing it, and the stage is retrying inside its [attempt bound](../end-goal/how-humans-do-it/03-gates.md#the-attempt-bound). The take carries on if a later attempt parses. |
| `used all 3 attempts … stuck on this item` | The bound is spent and the factory is saying it cannot do this one. The item keeps the count and the spend of every attempt, refused ones included, which is what an escalation is read from once [_Work_](../end-goal/how-humans-do-it/11-surfaces.md#work-ops-factory-people) exists to read it on at M7. Run the take again, or run it on a stronger model — `claude-haiku-4-5` was refused three times out of three on 2026-08-18. |
| `go build … no required module provides` | The model reached outside the standard library. The statement above says not to; say it again more plainly. |
| `go: cannot find main module, but found .git/config` | The model wrote no `go.mod`, so there is nothing to build. The statement asks for one in as many words and `claude-haiku-4-5` left it out on both takes of 2026-08-18, writing `main.go` and `main_test.go` alone — which is the same shape of failure as the row above and the reason a subscription take reaches the implementation stage and stops there. Use an API key and a stronger model for a take that has to reach a gate. |
| `go: downloading go1.x` | The `go.mod` it wrote names a newer toolchain than the one installed. Edit that line and run the take again. |
| `The encodings ran and failed` | Not an error. The gate fires anyway and shows the failed criterion, and you can approve over it — which is a fair thing to show, since a human deciding against the evidence is what the row is for. |
| `bind: address already in use` | A release from an earlier take still holds the port. `pkill -f borg-demo/targets`. |
| `the model API answered 401` or `403` | The secrets file has no token in it, or the token has expired — `claude setup-token` again. A 403 on a token that is current is the subscription not entitling this call, which is an account question and not a code one. |
| `the model API answered 429` on every model but Haiku | Not the account's allowance, and not a wait: the answer carries no `retry-after` and no rate-limit header, and the account's own buckets read as allowed on the same credential. What was measured on 2026-08-18, one variable at a time: a subscription token is served `claude-haiku-4-5` on a plain request and refused Fable 5, Opus 5, 4.8, 4.7, 4.6, Sonnet 5 and 4.6, and the only thing that changes a refusal into a 200 is the request carrying Claude Code's own system prompt — not a beta header and not a user agent. The factory sends its roles' prompts, so it is served Haiku and nothing above it, and claiming to be Claude Code to get the rest is not something this repository does. Use an API key for a take that needs a stronger model, and treat a subscription take as a Haiku take. |

A failure stops the run and damages nothing: each step writes its record before the next one runs, so what stopped halfway leaves an item readable at the stage it reached. The one exception is between master's fast-forward and the release being minted, which are one event in the design and two statements in the code — [`cmd/factory/path.go`](cmd/factory/path.go) says so at the place it happens, and closing it needs the merge queue, which is M3.

## What it does not show

Say this out loud to anyone watching, because the run looks more complete than the factory is. There is no candidate environment and no merge queue, so the criterion is decided wherever the build was made — M3. Nothing watches the release after it starts: no [health signal](../end-goal/how-humans-do-it/08-operations.md#the-health-signal), no watch window, no [rollback](../end-goal/how-humans-do-it/06-releases.md#rollback) — M4, and the design's own account is that a service's first release has none of those anyway. Six of the eight gate rows are not built, and the third action the production deploy row has, pinning a [strategy](../end-goal/how-humans-do-it/03-gates.md#the-rollout-strategy), is refused with its reason: a target that runs a release as a local process moves a process rather than traffic, so the strategy that keeps a [control](../end-goal/how-humans-do-it/08-operations.md#the-health-signal) is unavailable here and every deploy is straight.

Two things about the score are worth saying plainly. Its formula is authored rather than learned — the weights and the breakpoints were written by hand and calibrated so a first release is decided by a human and the item after it is not, and learning is M6. And what narrows an [authorship prior](../end-goal/how-humans-do-it/04-risk-score.md#factors-at-least) here is a human's verdict and nothing else, so a prior stops moving once the factory stops putting humans at gates — which is the self-reinforcement [_How it learns_](../end-goal/how-humans-do-it/04-risk-score.md#how-it-learns) holds out a random sample to break, and there is no sample yet.

And the terminal is the whole interface: the four [surfaces](../end-goal/how-humans-do-it/11-surfaces.md#work-ops-factory-people) come at M7, and a crude interface until then is what deferring them costs.
