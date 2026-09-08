# Demoing the factory end to end

How to run the demonstration of [M8](../roadmap.md#m8--the-screens-and-the-fleet) by hand — the
setting up, the process, and the five episodes — with the takes below it in
[`DEMO-M1-M7.md`](DEMO-M1-M7.md): a real model authoring against a real
git repository, a candidate running on an
[environment](../end-goal/how-the-factory-works/05-environments/02-an-environment-per-candidate/README.md)
of its own, a human deciding at every [gate](../end-goal/how-the-factory-works/03-gates/01-where-a-gate-is-and-what-decides-it.md)
row at the four [screens](../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md),
and a [release](../end-goal/how-the-factory-works/06-releases/02-the-release-record.md) left running
as a local process. The same paths run under `go test` as end-to-end tests in `cmd/factory`, with a
fake model and the verdicts typed through the same calls a screen makes; this is the version with
nothing faked, which is what there is to show somebody. [`README.md`](README.md) is the map of the
code underneath it.

Every write a human makes is at a screen. `serve` is the process that serves them, and it is what
this runbook starts: the eight subcommands are each a pass or a read, and none of them takes a
verdict or an owner's write. So the shape of a take is `serve` running in one terminal, the client
open in a browser, and the records read from a second terminal.

Everything below is run from this directory.

## What it needs

| What | Why, and what goes wrong without it |
|---|---|
| The dev database | [`docker-compose.yml`](docker-compose.yml) on port 5433. `docker compose up -d`. Every record the run writes goes there, so an unreachable database stops it at the first write. |
| An OpenRouter API key | [openrouter.ai/keys](https://openrouter.ai/keys) mints one, and the [agent](../end-goal/how-the-factory-works/10-fleet/README.md) sends it as a bearer token. This is the default provider because it reaches every model; the alternative, `-provider anthropic` with the token `claude setup-token` mints, is served `claude-haiku-4-5` and refused above it — see [_When it fails_](#when-it-fails) for what was measured. An API key against Anthropic's own endpoint goes in a header this code does not write and would answer 401. |
| A directory outside this repository | The secrets file, the service's repository, and the directory releases run from — a candidate environment gets a directory of its own under that one. Nothing the demo creates belongs in `end-goal/`. |
| Go and git | The build is `go build` in the service's repository and the encodings run as `go test` there, so the demo's service is a Go program. |
| Node | The client is an Angular workspace and the binary embeds its build output, so the client is built before the binary that serves it. `mise.toml` at the repository root pins the version. |
| A browser | The four screens are the interface. Anything with `EventSource` will do; the subscription is one server-sent-events stream per address. |

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

Then the client, once per checkout and again after any change to it:

```sh
cd client && npm ci && npm run build && cd ..
```

`npm run build` writes into `clientdist/browser`, which the binary embeds with the standard
library's `embed` — so a binary carries exactly the client built beside it and neither is upgraded
alone. The output is the build's product and never committed: on a fresh clone `clientdist/browser`
holds nothing but `.gitkeep`, and `serve` answers a request for a screen with "the client is not
built" until this has run.

## Starting the factory

One terminal holds the process for the whole take:

```sh
go run ./cmd/factory serve \
  -secrets ~/borg-demo/secrets \
  -model deepseek/deepseek-v4-flash \
  -service greeter=~/borg-demo/greeter \
  -area greeting \
  -targets ~/borg-demo/targets
```

It acquires the lease and holds it for the life of the process, applies the schema, creates the
[factory-wide settings](../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md)
record, the project and production's [environment](../end-goal/how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md)
for it where they do not exist, and then runs each component's pass on a ticker of its own while
serving the screens on `:8080`. `GET /healthz` answers with the factory version and carries neither
the version header nor a principal, because it is what a reader outside the process reads before it
has either. `-every-<name>` sets any pass's interval — `-every-advance 5s` is the default and is
worth lowering to `2s` for a demonstration, since it is how long a decision waits before the pass
picks it up. `-port` moves the port. It exits on Ctrl-C, shutting the server down and releasing the
lease, so the next start does not wait the lease out.

Open `http://localhost:8080` and say who you are. Every call carries two headers: the factory
version, which is enforced — a call whose version is not the store's is refused with a required
reload rather than a failed action — and the per-person key of the human the client says it is,
which is enforced by nothing. [Seam 5](../end-goal/deferred.md#security-comes-last) is where
authentication attaches and this milestone attaches none, so an install where anybody can reach the
port is anybody they name. Say the owner's own key: `-human` defaults to `owner`, and the key the
mapping holds for that name is what the process's own writes are made as and the one key an acting
call exempts from the read-only reading. Read it out of the store:

```sh
docker compose exec -T postgres psql -U factory -d factory -tAc \
  "select human_key from people_mapping where name = 'owner'"
```

A second key is what shows routing, and it is written at People: a row with a name and nothing else
reads all four screens and acts nowhere, and declaring one duty on it is what makes it act.

## Episode one: the install with nothing in it

With `serve` running against a fresh store, the home view is the readiness reading and nothing else:
one row per [role](../end-goal/how-the-factory-works/01-one-pipeline.md), every one of them
uncovered, and the badge counting them. An install holding no [fleet entry](../end-goal/how-the-factory-works/10-fleet/01-what-an-agent-runs-on.md)
for a role dispatches nothing, so this is what a fresh install is met with — not an error and not an
empty screen. `serve` writes one entry per role from `-model`, `-provider` and `-effort` where the
install holds none, which is the stand-in for the owner's first act on a composition no screen is
open on; to see the reading empty as each entry is written, stop the process, drop the schema, and
write the entries at Factory instead.

The entry's form takes all nine fields the design gives it, and an entry written without one is an
entry every later version has to migrate. Leave the project, the service and the area empty to scope
it to the whole factory. One of the nine is stored and read by nothing — how many dispatches pass
between [evaluation-set runs](../end-goal/how-the-factory-works/10-fleet/02-a-model-under-a-name.md),
the evaluation set being content the product ships and this milestone shipping none.

Duty 1 is at Work: an [intent](../end-goal/how-the-factory-works/02-intent-into-items/01-intake/README.md)'s
statement, and the services its decomposition yields items on. This one is written to survive a live
run — it names the module, keeps the change inside the standard library, and asks for an encoding
that does not bind the port, so a release still running from an earlier take cannot fail the next
one's tests:

> A Go HTTP service, module borg.demo/greeter, package main in main.go at the repository root, standard library only, with a go.mod. It answers GET /health with status 200 and the body ok, on port 8081. Test the handler through net/http/httptest rather than by binding the port.

[_Statements that work_](#statements-that-work) below has three more and says what each part of one
is for.

Duty 2 is at Factory beside it: a [constraint](../end-goal/how-the-factory-works/02-intent-into-items/01-intake/01-constraints-and-the-design-system.md)
with the reach it binds — the factory, a project, or an area — which every item inside that reach is
authored under until an owner withdraws it. A constraint whose reach is one intent is supplied at
Work instead, on that intent.

## Episode two: one item through the screens

The advance pass takes the intent in hand on its next tick. What it does and where it stops is the
same on the terminal `serve` prints to and on the screens, and the screens are where the verdicts
go:

| What waits | Where, and what to do |
|---|---|
| The [interview](../end-goal/how-the-factory-works/02-intent-into-items/02-the-interview.md)'s question | Work, on the intent. One line, any answer — the interview is one round or none, and this is what the round is spent on. Some runs are not asked anything. |
| The confirming round | Work, on the intent: whether what the factory understood is what was wanted. A correction reopens the interview instead. |
| `spec` | Work, on the item. Approve, which confirms the acceptance criteria are the right ones — duty 6. Edit in place authors the version yourself. |
| `implementation_plan` | Work. Approve, or edit in place: the plan is a document, so a human who wants a different approach edits one into it rather than rejecting the item to get one. |
| `tasks` | Work. Approve, or edit in place to resequence or split a task without changing the plan above it. |
| `implementation` | Work. Approve what was built, or reject with feedback to have it built differently. This is the one document row with no edit in place: a human does not author a build at the row that decides it. |
| `deploy_to_candidate_environment` | Work. Approve, which creates the candidate's own environment and puts the build on it. Hold leaves the item with no environment and nothing running; reject sends the [item](../end-goal/how-the-factory-works/01-one-pipeline.md) back to implementation with an attempt counted there. |
| `merge_to_master` | Work, and this is where [UAT](../end-goal/what-humans-do.md) is performed (7): the candidate is running on its own environment and its address is on the row. Approve admits it to the merge queue; reject stops the path with no release minted and the item back at implementation. |
| `deploy_to_production` | Work. Approve, or hold, which leaves the release minted and nothing deployed — no attempt counted and nothing taught to the score, which is what separates a hold from a reject. |
| The [acceptance](../end-goal/how-the-factory-works/02-intent-into-items/02-the-interview.md) round | Work, on the intent, once every item of it is live: whether the intended effect was had. |

Every row shows what it offers, which differs per row: refer is on all of them, because it is about
the human and not the event, and acknowledge is beside them — it says a holder has the row, decides
nothing, and the row stays in front of every other holder. Three of the ten duties are here: the
question answered (3), the criteria confirmed (6), and UAT (7). Two more are one row each — an
implementation plan written together with the factory through edit in place (11), and an item the
factory gave up on taken over at its escalation (12), which is the row Work shows as escalated with
a stage to return it to.

Every verdict is asked for on a first take, and for two different reasons. At the four rows over a
build the score puts a human there and says why at each: a service's first release has no earlier
release to return to, its author has never been approved, its area has no history, and the diff
touches every file in the tree. At the three rows above a build a human is there whatever the number,
because the factor set those rows read holds the change's reach and nothing is built when they fire
— a factor that cannot be computed is resolved, and a resolved vector is decided by a human. Nothing
about either is a shortcut.

Each close event carries one field no terminal could fill: when the actor opened the row in Work.
The interval Factory reports as how long a row was open in front of a human is read from it, beside
the wait and never in place of it — it is the screen's report of itself, and a client that lies about
it is caught by nothing.

Two things are worth doing on purpose here. Acknowledge a row from one key and watch it stay in front
of the others, which is what acknowledging decides — nothing. And open one row in two browsers and
decide it in the first: the second is told the record changed by its own subscription, before its
human acts on it, which is what push not poll holds inside the product.

What prints on `serve`'s terminal between the verdicts is the demonstration, in order: the two
versions in force, the area, the intent taken in and refined, the service and the item
[decomposed](../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md) with
its branch, the spec version and the [criterion](../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/03-the-six-patterns.md)
it introduces, the implementation's commit, the build, each row firing with its number against the
threshold and every factor with the quantity it was read from, the candidate environment composed
and deployed to, the encodings checked in both directions and run twice there, the queue's order and
its re-verification, master fast-forwarded, release number 1, the environment torn down, the deploy
[without a control](../end-goal/how-the-factory-works/03-gates/02-the-rollout-strategy.md) to
production, and the analysis window opened over it — which on a service's first release says passed
was never available to it.

## Episode three: silence

With nothing waiting, the home view is the digest and the badge at zero: what shipped, what was
decided, and what the factory auto-approved over the one factory-owned span. The digest is the part
that appears only at zero, which is what makes an empty screen mean the factory is working.

Then stop a component and watch a named row appear beside the digest rather than the same emptiness.
The health monitor's own [last check](../end-goal/how-the-factory-works/08-operations/08-drift-detection.md)
names the interval it was reading on and owes a further pass while any window is open, so raising the
watch pass's interval past that interval is what makes it late:

```sh
# Restart serve with the watch pass held off, and the last check row goes past its interval.
go run ./cmd/factory serve … -every-watch 30m
```

The row sits above whatever is genuinely waiting and outside the badge: a component whose pass merely
ran late is not a wait on a human, and it is shown rather than aggregated away. Beside it is the drift
detector's own last check, read from the detector's own store — which is absent until the detector has
run, and reads as a factory with no drift detector installed.

## The takes below M8

[`DEMO-M1-M7.md`](DEMO-M1-M7.md) holds the demonstrations of the milestones before this one: five
takes driven by `run`, `watch` and `learn` rather than by `serve` — the analysis window's parameters
authored and a change auto-passing under them, two candidates through the merge queue, the
deliberately bad release the factory takes back off production on its own, contracts binding two
services, and what the score learned from those outcomes. Episodes four and five below start from
the fourth of them, so a take of the whole of it runs that file first.

## Episode four: Ops

The fourth take, in [`DEMO-M1-M7.md`](DEMO-M1-M7.md#the-fourth-take-which-is-m4s-demonstration), is
the first half of this episode: the deliberately bad release deployed, its
window failing, and the [rollback](../end-goal/how-the-factory-works/06-releases/06-rollback.md)
performed by the watch pass with no human in it. The rest is duty 10 in both its forms and the three
acts beside it, each at Ops on the service's own address — which reads the release running per
target, the contracts it publishes, its open [incidents](../end-goal/how-the-factory-works/08-operations/06-incidents.md),
the windows it is watched under, and the mitigation standing on a target while one does.

**A rollback while the build it returns to is still running.** Two good releases first, so there is
a release below the one running: take a second intent in and let it ship. Then roll back at Ops with
a reason, which the record carries — the deployer returns production to the release below, and the
source on the rollback's own record names the human who asked rather than the health monitor's
reading. The address reads the release below on the target afterwards, which is the demonstration in
one screen.

**A revert raised after.** Once the build a rollback would return to is gone from master the undo is
a revert instead: raise it at Ops naming the release that failed, and intake writes that link as the
intent's evidence — the same link the detector's own revert carries in
[_The hold, and shipping the revert_](DEMO-M1-M7.md#the-hold-and-shipping-the-revert), written the same way
whichever of the two raised it.

**A mitigation instructed and ended.** The factory performs neither of the class's two operations on
its own — a mitigation is a human's instruction and the record says which human. On this platform the
instance count is the one that can be performed and only at the count it already runs, one: it moves
a process rather than traffic, so it serves no share. Ops shows the mitigation while it stands, and
it stands until a human ends it — what it did to the target stays until a deploy replaces it, and the
drift detector reads the target against the deploy record meanwhile.

**A rollback marked as not caused by the release.** Where the comparison was confounded, say so at
Ops with what caused it instead. The score and its learning pass exclude that release from then on,
the revert item is dropped with Ops as the caller, and the hold the rollback set lifts — there is no
defect on master for it to keep off production, so the next release from master carries the change,
opens a window of its own and is measured again. The rollback and the incident stand: production was
worse, whatever made it so.

**A page fired on a human's own judgment.** The one action of the twelve duties no component ever
performed. It routes the way every page about deployed software routes — to the holders of duty 12,
widened once to the owner where nobody holds it — and nothing scores it and no bound applies to it.
What limits it is that a page nobody needed makes its recipient slower to answer the next one.
Factory counts them beside the page channel's other numbers.

## Episode five: the money

Lend the credential the fleet runs on at People, naming the human who lent it and whether the account
is theirs or an organisation's; author a rate per kind of unit on it, per model version and effort;
and author a [spend ceiling](../end-goal/how-the-factory-works/10-fleet/08-a-spend-ceiling.md) on it
— an amount in a currency over a period a length and a start date define, in the zone the date was
authored in. Duty 8 and duty 9 sit beside it at Factory: a parameter authored and a safeguard placed.

Then take an intent in and watch Factory while the pass works it. The burn rate is the units spent so
far against the period in force, and beside it is when spending at that rate projects to exhaust the
ceiling and how many runs of the period returned a kind with no rate — the units of those runs are in
no sum, so a burn rate with any of them is a lower bound. What exists so that the hold is not the
first anyone hears of it is the notice beside the reading: past a fixed fraction of the amount
authored, the factory delivers one, through the notifier and never as a page, once per credential and
period. Author the ceiling again at an amount already spent, which is how one is lowered, and the next
dispatch is what stops: nothing is reserved before a stage starts, so the run happens and the sum is
what declines the one after it.

The hold is written mid-stage by whoever could not proceed. It names the credential and never the
entry — an owner may re-credential or delete an entry without changing what that row says — and it
routes to the owner, raising or clearing one being the owner's, so no row of the three credential
holds reaches a human who cannot act on it. Work shows the item stopped at its credential's ceiling,
and Factory counts what is stopped at dispatch by cause.

Clearing it is at Work, on the credential: it authorises an overage for the period in force alone and
does not reset the sum. The period is derived at the read from the start date in force, so
re-anchoring the ceiling is what makes the next period next — author it again with yesterday's date
over two days and the same units stop the credential again, in a period nothing has cleared. That is
the whole of what clearing for that period alone means.

A run whose converted amount is absent because a kind it returned has no rate fails closed the same
way, naming the kind, the model version and the effort; authoring the rate is what clears that one,
not authorising an overage.

## Episode six: reports come in from outside

Duties 4 and 5 need two things the episodes above do not: `serve`'s third database, and a service
whose running release the factory itself built, so the build carries the way in — `greeter` from
episode two already does, the way in being Go source the factory injects at the two build sites in
`repo.go`, so nothing about the service or its statement changes for this take.

The report store has no default of its own: `serve` reads `REPORTSTORE_DATABASE_URL` before it takes
the lease and refuses to start without it, the way it reads `DRIFTDETECTOR_DATABASE_URL`. Export it
— a schema of its own on the same server, applied at the open the way the drift detector's is — and
start `serve` again with the grouper's own interval lowered the way `-every-advance` was in episode
two:

```sh
export REPORTSTORE_DATABASE_URL='postgres://factory:factory@localhost:5433/factory?search_path=reports'
go run ./cmd/factory serve \
  -secrets ~/borg-demo/secrets \
  -model deepseek/deepseek-v4-flash \
  -service greeter=~/borg-demo/greeter \
  -area greeting \
  -targets ~/borg-demo/targets \
  -every-grouper 5s
```

Place both admission safeguards at Factory before anything arrives, so what a report and an intent
each wait on is shown rather than raced against how fast a human can reach the screen: `report_admission`,
which holds an arrived report ungrouped until a human admits it, and `report_derived_intent_admission`,
which holds an intent grouped from reports from being refined further until a human admits it. Neither
takes a bound — placing either is what adds the human, the way a safeguard on the risk threshold does.

The way in inside `greeter`'s running process listens on a Unix socket in production's own directory,
named for the service — `~/borg-demo/targets/greeter.way-in` — and nothing on this platform reaches it
but a client that dials that socket directly. curl does, over any path: the way in reads the method and
nothing else.

```sh
curl -s --unix-socket ~/borg-demo/targets/greeter.way-in http://localhost/
```

What comes back is the notice in force and a session minted for this one: `{"notice_id":"","text":"",
"session":"<hex>"}`. Both are empty here — none of the four screens writes a notice yet, `constraint.
KindNotice`'s only writer being the record's own package — which is a fresh install's honest answer
and not a fault. Note the session, and submit a bug report and a harm-marked complaint through it, the
same session carrying both:

```sh
curl -s --unix-socket ~/borg-demo/targets/greeter.way-in http://localhost/ \
  -d '{"kind":"bug","text":"the save button does nothing","session":"<session>","notice_id":""}'

curl -s --unix-socket ~/borg-demo/targets/greeter.way-in http://localhost/ \
  -d '{"kind":"complaint","harm_marked":true,"text":"the export button empties the account instead of downloading it","session":"<session>","notice_id":""}'
```

Each answers `{"accepted":true}` in the same call, which is the whole of what a reporter is shown, and
each is written under the deploy the token resolves to — `greeter`'s own service and environment —
never under anything the submission itself named. With `report_admission` standing, the grouper's next
tick reads neither: Work's home view carries both as reports waiting ungrouped, oldest first, and
carries no intent yet, because nothing has grouped them. Admit each — two separate actions, one per
report — and on the grouper's following tick it reads both, decides whether they are one problem or
two (a model call, so which it decides is not fixed here), and raises an intent through intake with a
statement summarizing the reports. `report_derived_intent_admission` was already standing when it was
raised, so Work's home view now carries the intent itself waiting, with the count of reports grouped
into it and no item yet — dispatch puts no agent on it, not even the interview, while the safeguard
stands. Admit it, and the pipeline continues the way episode two's did: once it reaches an item, that
item's own page at Work carries the reports grouped into its intent, each with its own admission, the
notice it was shown under, and whether it marks harm.

At Factory, the report channel now reads two things a query cannot answer: refusals, per service and
over the whole channel, and submissions the store could not read are counters this store keeps rather
than derives, because the record a query would count is the write the rate exists to refuse. Author
the report channel's rate to zero and submit again through the socket — `accepted` turns to `refused`,
and the reason it renders is the rate, with the refusal now counted where a moment ago it was accepted.

One erasure reaches every record that quotes the same words, searched for rather than named. Read a
report's own text back against the report store's own schema:

```sh
docker compose exec -T postgres psql -U factory -d factory -c \
  "set search_path to reports; select id, text from report"
```

`the save button does nothing` puts `save button` at the half-open byte range 4-15. At Factory, erase
it: the report id, `4-15`, and a reason. The factory walks from the report to the intent it was
grouped into and to every version authored against that intent's items, searching each for `save
button` and destroying it wherever it is still standing verbatim — a record that paraphrases the words
rather than quoting them is not reached, because the walk searches for the words and not for a record
it was told about. Read the report back and `save button` is gone from it; read the intent's statement
back, and it is gone there too if the grouper's own statement happened to quote it; and, once the item
has reached spec, read the version authored against it the same way — every link between the three
records stands, and only the words moved.

The erasure list is a file beside the targets directory and outside the recovery unit,
`~/borg-demo/targets/erasure-list`, and it is what a restore is served through. Stop `serve`, put the
words back in the report by hand — the way a restore from a backup taken before the erasure would —
and start `serve` again:

```sh
docker compose exec -T postgres psql -U factory -d factory -c \
  "set search_path to reports; update report set text = 'the save button does nothing' where id = '<report-id>'"
```

The replay `serve` performs before it answers a screen reads the erasure list and destroys the same
words again, so the report reads erased once more with no second erasure performed.

What this take does not cover: the predicate-kind constraints over the way in are M12's, so nothing
here bounds what a submission may carry beyond the two rates it is refused against; and every target on
this platform is local, so there is no second host a report could arrive against — the way-in token and
the deploy record its digest resolves are what stand in for one.

## The five more things

**The upgrade.** A shipped [role prompt](../end-goal/how-the-factory-works/10-fleet/03-what-an-agent-is-told/README.md)
whose words an upgrade changed enters the chain at the first start on the new version, and the
version below it stands in force until the row every version fires is decided. Factory reads both:
the version in force per role, and the version awaiting its gate. Approving it there moves the
version in force. The row's third action is not a verdict: an edit in place authors a version with
the human as its author and fires the row again over what they wrote. Nothing here authors a version
any other way, so the only version this row ever sees is one the product shipped.

**The People declaration as a chain.** Declare a duty on two keys and route a safeguard's rows to
that duty — only a safeguard that adds a human at a gate carries a routing, which is the risk
threshold and no other parameter. One holder writes the safeguard's withdrawal; the row is routed
away from them, so while the second holder stands they cannot decide it. Withdraw the second holding
and they can: the row fires to them, closes, and the close carries the self-approval count, which is
what an install that cannot separate who wrote a record from who decides it records instead of
refusing. Restore the holding afterwards and People reads both holding again — that the row closed at
a moment when only one did is in the chain of policy versions and nowhere else. Then erase a mapping:
the key stands on every record it was written to, the chain and its counts are undisturbed, and
People resolves no name for it.

### The four rows outside every item

A safeguard's withdrawal, a halt's withdrawal, a legal hold's ending, and a shortening of
decision-log retention are each decided at Factory, and each is routed away from the actor on the
record it decides. Write each as one human and decide it as another: place a safeguard and withdraw
it, set a halt and withdraw it, set a legal hold and withdraw it, and author `decision_log_retention`
at a shorter value than the one in force — that last one is written pending rather than authored in
force, because shortening removes a protection. Factory lists all four awaiting a disposition.
Approving each is what takes the record it decides out of force, which writing the withdrawal alone
does not: each is an open event, a close event, and then the record's own approval, and package
`policy` refuses an approval naming no close event. The row that decides a shortening names one thing
more — every author whose per-author prior stands drifted and whose held-out decisions the cut would
remove, so the human at the row reads whose prior restarts before they approve it.

Then the pass the shortening was for:

```sh
go run ./cmd/factory truncate -boundary <row-id>
```

The truncation row is appended first and stays: it names the boundary, the value being enforced, who
authored that value, and the two versions in force at the cut. It is the one write in the factory
that destroys evidence, which is why the row that records it is written before anything goes. It is
refused where a legal hold stands, where nobody has authored a retention value, and where the
boundary is inside the retention.

**The client's own machines.** Each of the four screens declares empty, loading, failed and
disconnected, and the client's own test suite decides the four
[predicates a candidate environment decides](../end-goal/how-the-factory-works/05-environments/04-what-the-candidate-environment-decides/01-the-third-outcome.md)
over each — a contrast floor, a name on every control, a focus order, and a target size. `cd client
&& npm test` is that suite. The disconnected state is the one to show by hand: stop `serve` with a
screen open, and what it held stays readable and marked stale while every action is refused.
Re-establishing re-reads the address whole rather than resuming.

**The version refusal.** Leave a screen open, stop `serve`, and start it again from a binary built
under another version — or simply change `factoryVersion` and rebuild. The next call the open screen
makes is refused with a required reload rather than a failed action, on every call and not on the
load alone. What it costs is a human mid-edit at a gate losing the edit to a reload.

## What an owner writes, and where

Every write an owner makes is at Factory or at People, and
[`README.md`](README.md#running-it) lists them all. Duty 8 and duty 9 are at Factory: a project with
production's environment in the same write, an area, every authored parameter, a safeguard, a halt, a
legal hold, a fleet entry, a permanent constraint (2), a service retired, a project ended, and the
five rows that decide a record rather than an item. The declaration is at People: the duties, the
obligations, the credentials each human lent with the ceiling and the rates on one, and the mapping
from a key to a name. Each is one form and each appends a
[policy version](../end-goal/what-the-factory-does/02-traceability.md) — the mapping is the one
exception, kept outside the chain so an erasure deletes it alone.

Authoring the threshold down to `0.2` before the second take is the other way to show a gate
deciding — the item that auto-passed at `0.3` reads over `0.2` and a human is asked again. What
Factory says about one of the eight parameters is that nothing reads it yet: the item-size target
waits for a decomposition that sizes anything, so authoring it changes nothing today and the reading
says so rather than leaving somebody to find out. The
[list of allowed predicate kinds](../end-goal/how-the-factory-works/07-contracts/06-what-a-consumer-declares.md)
was the other until M5, and it is also the one parameter whose unauthored value is neither the
score's nor nothing: it is the five kinds of predicate the factory can decide, which is what an owner
extends rather than replaces, and the reading names that source.

## Statements that work

The statement is the whole of what a human gives the factory, and what it says decides whether the run reaches a deploy — so these are written for the demo rather than for a real backlog. Five things earn their place in one:

| What the statement names | Why it is in there |
|---|---|
| The module path, and `package main` in `main.go` at the repository root | The build is `go build` in the repository root, so a program written anywhere else does not build. |
| Standard library only | Nothing in the run fetches a dependency, so a module requirement fails the build with the demo watching. |
| A `go.mod` | The build needs one and the implementation role writes what the role prompt asks for. |
| One behaviour, stated as a rule | The spec is one [criterion](../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/README.md), so a statement naming three behaviours still yields one, and the other two ship with nothing deciding them. |
| A test that does not bind the port | The encodings run as `go test` on this machine, where a release from an earlier take may still be holding it. |

Any of these is a whole take. Each one is one behaviour a sentence can state and a test can decide, which is what makes a criterion out of it:

> A Go HTTP service, module borg.demo/greeter, package main in main.go at the repository root, standard library only, with a go.mod. It answers GET /health with status 200 and the body ok, on port 8081. Test the handler through net/http/httptest rather than by binding the port.

> A Go HTTP service, module borg.demo/clock, package main in main.go at the repository root, standard library only, with a go.mod. It answers GET /time with status 200 and the current time as RFC 3339 in UTC, on port 8082. Test the handler through net/http/httptest rather than by binding the port.

Two more of the same shape work: `borg.demo/adder` answering `GET /sum?a=1&b=2` with the sum on port
8083, and `borg.demo/echo` answering `POST /echo` with the request body unchanged on port 8084. What
changes between them is the route, the module and the port, and nothing else.

One more kind of statement is the second change on a service already shipped, which is [_The second take_](DEMO-M1-M7.md#the-second-take-which-is-m2s-demonstration). Watch the spec stage on that one. The implementation role is told every criterion in force for its build — the ones the merged items introduced and the one this spec adds — and the build is refused unless an encoding names each, which is the check that makes the criterion id the thing the whole demonstration is followed along.

A statement for [_The third take_](DEMO-M1-M7.md#the-third-take-which-is-m3s-demonstration) has one more part: which files the change may touch. Two candidates of one service are cut from the same master, so what decides whether the queue can merge both is whether they wrote to the same file — and saying so in the statement is the only place a run of this interface can say it.

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
| `The implementer's reply was refused; N attempt(s) left` | Not a failure. The model wrote prose around its file blocks, the protocol refused it rather than repairing it, and the stage is retrying inside its [attempt limit](../end-goal/how-the-factory-works/03-gates/05-the-attempt-limit.md). The take carries on if a later attempt parses. |
| `used all 3 attempts … stuck on this item` | The limit is spent and the factory is saying it cannot do this one. The item keeps the count and the spend of every attempt, refused ones included, and [Work](../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md) shows it escalated with a stage to return it to, which is duty 12. Run the take again, or run it on a stronger model — `claude-haiku-4-5` was refused three times out of three on 2026-08-18, which is the model a subscription take is held to and the reason the default provider is the other one. |
| `go build … no required module provides` | The model reached outside the standard library. The statement above says not to; say it again more plainly. |
| `./main.go:N: undefined: X` or `imported and not used` | The model wrote Go that does not compile. The [Implementation gate](../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/05-implementation/README.md) is where the design rejects a build for exactly this, with Reject with feedback as an action, and that gate now does: the build runner's refusal is caught mechanically, `gate.AutoRejectedByCompile`, with an attempt counted at implementation and the compiler's own words carried back as the feedback the implementer builds against next — the run stops only once the attempt limit is spent. Measured on 2026-08-20: `deepseek/deepseek-v4-flash` produced a non-compiling `main.go` on three takes of four — a `httptest` call with no import, an unused `fmt` — while every other part of the path held. |
| `go: cannot find main module, but found .git/config` | The model wrote no `go.mod`, so there is nothing to build — the same refusal `compiles` returns for a missing module as for one that will not compile, so this rejects at the Implementation row the same way: mechanically, with an attempt counted and the implementer building again. The cause was found on 2026-08-20 and is not the implementer: the spec author was compressing the statement to its behaviour and dropping every constraint around it — the module, the layout, the go.mod the statement asks for in as many words — and the implementation stage is given the spec and never the statement, so it wrote what it was told. Measured across three models, the spec came back at 52 bytes on `deepseek/deepseek-v4-flash` and 71 on `deepseek/deepseek-v4-pro` from a four-sentence statement. [`agent/specauthor.go`](agent/specauthor.go)'s prompt now requires the spec to restate every constraint the statement makes, and the same model then authored a 451-byte spec and a `go.mod` with it. `claude-haiku-4-5` left it out on both takes of 2026-08-18, before that was understood. If it recurs, read the spec the run printed before blaming the implementer. |
| `<criterion id> is in force and no encoding in the build names it` | The build has a test for the criterion and the build cannot see it. An encoding is picked out by the id appearing exactly, and a Go test's name cannot begin with a lowercase id — a model asked for the id in a test's name wrote `func TestCr_<id>` on 2026-08-20, which is the id with its c capitalised and so a different string. [`agent/implementer.go`](agent/implementer.go)'s prompt now names the two forms that work, `func Test_cr_<id>` and the id in a comment, and names that one as failing. [`criterion/encoding.go`](criterion/encoding.go) is the matcher, and its own comment records the first time this collision was found — so a change to either belongs with a change to the other. The merge row rejects the candidate on this defect's own terms before a verdict is asked for, naming it, and the item goes back to implementation with an attempt counted there; the two lists this check prints above the failure — the criteria in force and what the build names — are what to compare. An encoding naming a criterion the build withdraws is the same rejection, and what it tells the implementer is to remove that encoding. |
| `go: downloading go1.x` | The `go.mod` it wrote names a newer toolchain than the one installed. Edit that line and run the take again. |
| `go.mod:3: invalid go version '1.x'` | The statement asked for "a Go version" and the model wrote the placeholder rather than choosing one. Measured on 2026-08-20 with `deepseek/deepseek-v4-flash`. An underdetermined request is what the [interview](../end-goal/how-the-factory-works/02-intent-into-items/02-the-interview.md) exists for and the model did not ask, so what is fixed is the request: name the version, which is an owner supplying a constraint (2). |
| `The encodings ran twice on the candidate environment and failed both times` | Not an error. The merge row fires, reads the failed criterion, and rejects the candidate on the criterion's own terms before a verdict is asked for — naming which criterion and its outcome — so there is nothing to approve over. The item goes back to implementation with an attempt counted there. |
| `The encodings disagreed between two runs, so every criterion is undecided` | The suite is not deterministic. Undecided is read at the merge row the way a failure is, so the same mechanical rejection fires as above, naming every criterion undecided; the way out is to author the encoding again rather than to run it again. |
| `the queue rejected item … merging master into the candidate branch failed` | Two candidates wrote to one file. The item is back at implementation with an attempt counted there, which is the queue working; see [_The queue rejecting a candidate_](DEMO-M1-M7.md#the-queue-rejecting-a-candidate). |
| `waits at deploy_to_candidate_environment: the platform has no room for another candidate environment` | `-candidate-environments` is set lower than the number of intents. The wait is in the log with the deployer as its actor, and it lifts when an item merges and frees one. |
| `bind: address already in use` | A release from an earlier take still holds the port. `pkill -f borg-demo/targets`. |
| `waits at deploy_to_production: the service holds as many analysis windows open as the window limit allows` | The window limit is doing its work. One window open per service is where the score starts, and it rises only after three of that service's windows have closed without failing a release, so the next release waits for that window to close — a wait on the factory, which writes nothing and pages nobody. The watch pass closes what is open, or author the window limit higher at Factory. |
| `waits at deploy_to_production: a rollback's revert has not shipped` | Master still holds the change that was rolled back, so deploying anything built on it would redeliver the defect. The revert is not held and deploys ahead of this. Marking the rollback as not caused by the release at Ops is what lifts it without the revert. |
| `neither exit is reachable: the release has no baseline` | A service's first release, or a release whose baseline's window has not closed yet. Nothing about it is discovered by watching and its window ends at the cap, which is the design's own account rather than a fault. |
| `N analysis window(s) are still open` at the end of a run | The run gave up before they closed, which a window's duration being measured and never set makes normal. `go run ./cmd/factory watch <service> …` continues from there, and nothing else closes one. |
| `MISMATCH` from the drift detector | What the factory recorded is not what the target runs. It holds that service's production deploys and pages, and only `driftdetector clear` ends it — the factory cannot, by design. |
| A call answered `409` with `reload_required` | The client was built from a factory version the store no longer holds. Reload the page; a screen open across an upgrade is stopped by this on every call and not on the load alone. |
| A call answered with the read-only refusal | The People key the client says it is holds no duty, no obligation and no lent credential, so it reads all four screens and acts nowhere. Declare one duty on it at People. |
| `the model API answered 401` or `403` | The secrets file has no key in it for the provider named, or the credential has expired — mint another at [openrouter.ai/keys](https://openrouter.ai/keys), or `claude setup-token` again under `-provider anthropic`. A 403 on a credential that is current is the account not entitling this call, which is an account question and not a code one. |
| `the model refused the request` | The model declined on its provider's policy grounds, and the sentence after the colon is the model's own. It is not retried — the request's shape is not what is wrong, and a stage that retried it would spend its attempt limit on a verdict already given. Measured on 2026-08-20: `anthropic/claude-opus-5` is served the spec stage and refuses the implementer's role prompt four times out of four under the cyber category, for a role prompt asking for a health-check HTTP handler, its own reasoning showing it part-way through writing that handler when the classifier stopped it. `deepseek/deepseek-v4-flash`, `anthropic/claude-opus-4.8` and `anthropic/claude-sonnet-5` author the same role prompt. Name a different model. |
| `the model API answered 200 carrying an error` | Only `-provider openrouter` answers this way: the request reached OpenRouter and the provider it routed to refused. The code and the message the body carried are in the error — an upstream rate limit, a model not serving, a request the upstream would not take. Nothing about the factory's own request is wrong, and the model id is the first thing to check. |
| `the model API answered 429` on every model but Haiku, under `-provider anthropic` | Not the account's allowance, and not a wait: the answer carries no `retry-after` and no rate-limit header, and the account's own buckets read as allowed on the same credential. What was measured on 2026-08-18, one variable at a time: a subscription token is served `claude-haiku-4-5` on a plain request and refused Fable 5, Opus 5, 4.8, 4.7, 4.6, Sonnet 5 and 4.6, and the only thing that changes a refusal into a 200 is the request carrying Claude Code's own system prompt — not a beta header and not a user agent. The factory sends its roles' prompts, so it is served Haiku and nothing above it, and claiming to be Claude Code to get the rest is not something this repository does. Re-measured on 2026-08-19 and unchanged: Opus 5 refused on the first call, Haiku served through the spec stage. This is what `-provider openrouter` is the default for: treat a subscription take as a Haiku take, and send anything that has to reach a gate through the other provider. |

A failure stops the run and damages nothing: each step writes its record before the next one runs, so what stopped halfway leaves an item readable at the stage it reached. A run that stopped after a Merge to master gate approved leaves that item in the queue, and the next run on the same service finishes it — the queue's membership is the service's, so there is nothing to clear by hand. The one window that is still open is between master's fast-forward and the release being minted: the queue holds one lock per service across the whole merge, so two runs cannot interleave and two candidates cannot read one number, but a crash between those two leaves master at a commit no release record names, and what repairs a record disagreeing with what is there is the drift detector, which is now installed: `go run ./cmd/driftdetector pass` finds it, and clearing it is a human's. [`mergequeue/doc.go`](mergequeue/doc.go) says so where it happens.

## What it does not show

Say this out loud to anyone watching, because the run looks more complete than the factory is. Every row of the default path fires — Decomposition where decomposition yielded more than one item, and Spec, Implementation plan, Tasks, Implementation, Deploy to candidate environment, Merge to master and Deploy to production on every item — and so do all five that belong to no item — a role prompt or a skill, a safeguard's withdrawal, a halt's withdrawal, a legal hold's ending, and a shortening of decision-log retention — each fired at Factory and each closed by a human, the record's own approval written from that close event. A human is at Spec, Implementation plan and Tasks on every item, because the factor set those rows read holds the change's reach and nothing is built when they fire, so a factor that cannot be computed is resolved and a human decides whatever the formula returns. The third action the production deploy row has, a safeguard on the [strategy](../end-goal/how-the-factory-works/03-gates/02-the-rollout-strategy.md), is refused with its reason: a target that runs a release as a local process moves a process rather than traffic, so the strategy that keeps a [control](../end-goal/how-the-factory-works/08-operations/01-the-health-monitor.md) is unavailable here and every deploy goes without a control.

Four things about the watching are worth saying plainly, and all four follow from that. **No control is ever started**, so the [comparison](../end-goal/how-the-factory-works/08-operations/01-the-health-monitor.md) is the weak fallback the design names: the release is read against the recent history of the release a rollback from it would return to, and the difference age makes between a process just started and one that has been running for a week is in that reading and unanswered. **Every [rollback](../end-goal/how-the-factory-works/06-releases/06-rollback.md) is the slow one**, the target's build redeployed and waited for, because there is no control to shift traffic onto. **The traffic is the release exercising itself** — these targets receive none, so the implementation role is told to append a line per unit of work it does, and a window passed says the boundary works rather than that the service is well. And **an explicit health threshold is not built**: the design lets an owner state one absolutely beside the comparison, which is the only thing that could fail a service's first release, and this factory has no parameter to state it on.

The window limit above one is honest and weak here for the same reason. Two windows may be open at once and both releases are recorded, but a deploy without a control replaces the process — so the lower release stops emitting the moment the upper one deploys, and only the newest release is really being measured. On a platform that keeps a control both would go on serving and both would go on being read.

Two things about the candidate environment are worth saying plainly. It is composed from the [current releases](../end-goal/how-the-factory-works/06-releases/05-the-deploy-record/README.md) of the candidate's dependencies, and decomposition here yields one item per intent — so no run of this interface declares a dependency and every composition names nothing. And the queue re-verifies serially: the design has a candidate re-verify against master plus every candidate ahead of it, which is what makes a long queue fast, and the speculation is the queue's own state that nothing outside it reads, so it can arrive later without changing a record. A queue of ten waits ten re-verifications here.

Three things about the score are worth saying plainly. Its formula is authored and stays authored — the weights and the breakpoints were written by hand and calibrated so a first release is decided by a human and the item after it is not, and what learning moves is the seven values the score supplies rather than how the number is computed, which is the division [gate policy](../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/README.md) draws. **Five of the seven values move both ways and two move one way.** Both ends of each parameter are evidence: one end is something going wrong, the other is the parameter costing more than it returns, which gate policy's own table states for every row. So the cap follows how long a window of that service actually takes, the attempt limit falls where nothing has ever needed a second try, and the window's size is never finer than the traffic can rule anything out at — a size finer than that ends every window at the cap and protects nothing. The two that move one way say so where the rule is published: nothing here shows that a confidence was too high, and nothing measures the other end of an item-size target. And **the sample gets half of what the design gives it**: a held-out release should take a strategy that keeps a [control](../end-goal/how-the-factory-works/08-operations/01-the-health-monitor.md), and every deploy here goes without a control — so it is watched by the same confounded comparison as every other release and the longest watch available is all it gets. What its evidence supports is that a comparison was available, not that an unsampled release on the same author would have read the same.

Two of the twelve duties are not performed here at all. [Duties 4 and 5](../end-goal/what-humans-do.md) belong to end users, who never open this product: what they send is a [report](../end-goal/how-the-factory-works/02-intent-into-items/01-intake/02-reports.md) through the way in shipped inside each deployed service, and neither that way in, nor the report store, nor the grouper is built — so Work has no report to show under any intent, and those two are demonstrated as absent rather than performed. Factory holds a list of [fleet proposals](../end-goal/how-the-factory-works/10-fleet/07-a-fleet-proposal.md) awaiting a disposition and the list is always empty, both of its writers being M14's; the [evaluation set](../end-goal/how-the-factory-works/10-fleet/02-a-model-under-a-name.md) a fleet entry names a dispatch count for is content the product ships and this milestone ships none; and the principal every call carries is claimed and verified by nothing, so an install where anybody can reach the port is anybody they name.
