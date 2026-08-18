# Demoing one change end to end

How to run milestone M1's demonstration, [_One change ships_](../roadmap.md#m1--one-change-ships), by hand: a real model authoring against a real git repository, a human deciding at the one [gate](../end-goal/how-humans-do-it/03-gates.md#where-a-gate-is-and-what-decides-it) the milestone builds, and a [release](../end-goal/how-humans-do-it/06-releases.md#the-release-record) left running as a local process. The same path runs under `go test` as the end-to-end test in `cmd/factory`, with a fake model and scripted answers; this is the version with nothing faked, which is what there is to show somebody. [`README.md`](README.md) is the map of the code underneath it.

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
  -targets ~/borg-demo/targets
```

`-model` is the provider's model id and has no default, because M1 requires the model named in configuration. `-repo` is created if it is not there. `-human` names the deciding human and defaults to `owner`.

It prompts for the [intent](../end-goal/how-humans-do-it/02-intent-into-items.md#intake)'s statement. This one is written to survive a live run: it names the module, keeps the change inside the standard library, and asks for an encoding that does not bind the port, so a release still running from an earlier take cannot fail the next one's tests.

> A Go HTTP service, module borg.demo/greeter, package main in main.go at the repository root, standard library only, with a go.mod. It answers GET /health with status 200 and the body ok, on port 8081. Test the handler through net/http/httptest rather than by binding the port.

[_Statements that work_](#statements-that-work) below has three more and says what each part of one is for.

Then two prompts, and nothing else waits on a human:

| Prompt | What to type |
|---|---|
| `The spec author asks: …` | One line, any answer. A blank line is asked again — [the interview](../end-goal/how-humans-do-it/02-intent-into-items.md#the-interview) is one round or none, and this is what the round is spent on. Some runs are not asked anything. |
| `Verdict (approve, or reject <feedback>): ` | `approve`. Or `reject <feedback>`, which stops the path with no release minted, no deploy recorded, and the [item](../end-goal/how-humans-do-it/01-one-pipeline.md) left at the implementation stage — worth showing once, because a gate that cannot stop anything is not a gate. |

What prints between them is the demonstration, in order: the intent taken in and refined, the service and the item [cut](../end-goal/how-humans-do-it/02-intent-into-items.md#the-cut) with its branch, the spec version and the [criterion](../end-goal/how-humans-do-it/03-gates.md#spec) it introduces with its id and pattern, the implementation's commit, the build, the encodings checked in both directions and run, the gate firing with the [score](../end-goal/how-humans-do-it/04-risk-score.md)'s number and every factor in the vector, the verdict, master fast-forwarded, release number 1, the [straight](../end-goal/how-humans-do-it/03-gates.md#the-rollout-strategy) deploy, and the walk from the deploy record back to the intent.

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

One more kind of statement is the second change on a service already shipped, which is the take [_A second take_](#a-second-take) calls worth doing on purpose. Run it with the same `-service` and `-repo` as the first, and the statement says only what is new:

> Add a second route to this service: it answers GET /version with status 200 and the body 1.0.0. Keep the existing route and its test as they are. Test the new handler through net/http/httptest rather than by binding the port.

Watch the spec stage on that one. The implementation role is told both criteria — the one the first item introduced and the one this spec adds — and the build is refused unless an encoding names each, which is the check that makes the criterion id the thing the whole demonstration is followed along.

What not to ask for, on a day people are watching: anything needing a dependency, a database, a container, or a port something else holds; a change to two services, since the cut here writes one item on one service; and a program that exits as soon as it starts, which deploys correctly and then shows nothing running.

## Showing it afterwards

```sh
curl -s localhost:8081/health; echo          # the software the factory deployed, answering
pgrep -af borg-demo/targets                  # the release running as a process

# The decision is two chained rows: an opening naming the versions, a closing carrying the verdict.
docker compose exec -T postgres psql -U factory -d factory \
  -c "select seq, shape, part, actor_kind, actor_name, policy_version, closes from decision_log order by seq"

go run ./cmd/factory walk <deploy-id>        # the link walk on its own
```

The walk is the direction the milestone is named for. Every line it prints is a stored field on a record, read through the package that owns it — nothing is reconstructed, and that is the claim a demonstration of this milestone is actually making.

## A second take

A take leaves three things behind: the records, a git repository whose master is at the change, and a process holding the port. Reset all three:

```sh
pkill -f borg-demo/targets
rm -rf ~/borg-demo/greeter ~/borg-demo/targets/*
docker compose exec -T postgres psql -U factory -d factory \
  -c 'drop schema public cascade; create schema public;'
```

Or keep them and run again with `-service greeter2 -repo ~/borg-demo/greeter2` and a different port in the statement. Running a second change on the *same* service instead is the take worth doing on purpose: its branch is based on master, so the first item's encoding is in the tree, the implementation role is told every criterion in force, and the build is refused unless each one has an encoding naming it.

## When it fails

| What you see | What it is |
|---|---|
| `The implementer's reply was refused; N attempt(s) left` | Not a failure. The model wrote prose around its file blocks, the protocol refused it rather than repairing it, and the stage is retrying inside its [attempt bound](../end-goal/how-humans-do-it/03-gates.md#the-attempt-bound). The take carries on if a later attempt parses. |
| `used all 3 attempts … stuck on this item` | The bound is spent and the factory is saying it cannot do this one. The item keeps the count and the spend of every attempt, refused ones included, which is what an escalation is read from once [_Work_](../end-goal/how-humans-do-it/11-surfaces.md#work-ops-factory-people) exists to read it on at M7. Run the take again, or run it on a stronger model — `claude-haiku-4-5` was refused three times out of three on 2026-08-18. |
| `go build … no required module provides` | The model reached outside the standard library. The statement above says not to; say it again more plainly. |
| `go: downloading go1.x` | The `go.mod` it wrote names a newer toolchain than the one installed. Edit that line and run the take again. |
| `The encodings ran and failed` | Not an error. The gate fires anyway and shows the failed criterion, and you can approve over it — which is a fair thing to show, since a human deciding against the evidence is what the row is for. |
| `bind: address already in use` | A release from an earlier take still holds the port. `pkill -f borg-demo/targets`. |
| `the model API answered 401` or `403` | The secrets file has no token in it, or the token has expired — `claude setup-token` again. A 403 on a token that is current is the subscription not entitling this call, which is an account question and not a code one. |
| `the model API answered 429` on every model but Haiku | Not the account's allowance, and not a wait: the answer carries no `retry-after` and no rate-limit header, and the account's own buckets read as allowed on the same credential. What was measured on 2026-08-18, one variable at a time: a subscription token is served `claude-haiku-4-5` on a plain request and refused Fable 5, Opus 5, 4.8, 4.7, 4.6, Sonnet 5 and 4.6, and the only thing that changes a refusal into a 200 is the request carrying Claude Code's own system prompt — not a beta header and not a user agent. The factory sends its roles' prompts, so it is served Haiku and nothing above it, and claiming to be Claude Code to get the rest is not something this repository does. Use an API key for a take that needs a stronger model, and treat a subscription take as a Haiku take. |

A failure stops the run and damages nothing: each step writes its record before the next one runs, so what stopped halfway leaves an item readable at the stage it reached. The one exception is between master's fast-forward and the release being minted, which are one event in the design and two statements in the code — [`cmd/factory/path.go`](cmd/factory/path.go) says so at the place it happens, and closing it needs the merge queue, which is M3.

## What it does not show

Say this out loud to anyone watching, because the run looks more complete than the factory is. There is no candidate [environment](../end-goal/how-humans-do-it/05-environments.md#records-and-one-long-lived-branch) and no merge queue, so the criterion is decided wherever the build was made — M3. The score is a stub whose answer is always that a human decides, and there is no [gate policy](../end-goal/how-humans-do-it/09-gate-policy.md) to author — M2. Nothing watches the release after it starts: no [health signal](../end-goal/how-humans-do-it/08-operations.md#the-health-signal), no [watch window](../end-goal/how-humans-do-it/08-operations.md#the-watch-window), no [rollback](../end-goal/how-humans-do-it/06-releases.md#rollback) — M4, and the design's own account is that a service's first release has none of those anyway. Seven of the eight gate rows are not built. And the terminal is the whole interface: the four [surfaces](../end-goal/how-humans-do-it/11-surfaces.md#work-ops-factory-people) come at M7, and a crude interface until then is what deferring them costs.
