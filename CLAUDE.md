# CLAUDE.md

## What this repository is

Monorepo for the software factory [README.md](README.md) describes. `end-goal/` holds the
design document: the state the repository is built toward, not a record of what it does.
Code goes beside it in `factory/`, never inside it. The factory is built as ordinary
software and does not run its own pipeline over itself.

**Read `end-goal/CLAUDE.md` before touching anything under `end-goal/`.** Its editing
rules and its consistency pass govern that directory alone and say nothing about code.
[_Writing style_](#writing-style) governs prose everywhere, that directory included.

A decision reached while working folds into the `end-goal/` file that owns the subject.
Two files record decisions and there is no third:

| File | What it is | Authority |
|---|---|---|
| `end-goal/` | The design document | Authoritative |
| [`roadmap.md`](roadmap.md) | Milestones in build order — order and never progress | — |

There were three. `jargon-cleanup.md` held the vocabulary invented here and the plain
words replacing it, and it was written down rather than done because the work did not fit
one session. It ended on 2026-08-23 with the last of the code renamed, and it was deleted
as its own rule required: a finished step was struck from it in the commit that finished
it, so the file shrank to nothing. What it decided is in `end-goal/` and in the commits.

[`end-goal/terms.txt`](end-goal/terms.txt) is not a third: it lists every name the
document uses and the field that name comes from or `coined`, so the consistency pass can
fail on a new one. It holds no reasons and no decisions — same kind of thing as
[`factory/deps.txt`](factory/deps.txt). A contested attribution is argued in the commit
and settled in the file.

`review-findings.md` is not a third either: it exists only between a run of the review pass
and the end of that run's triage, holding what the run returned until triage moves each finding into the file that owns its subject or into
`end-goal/open.md`. It records no disposition and no reason, so nothing is decided in it,
and it empties as triage proceeds. [_The review pass_](#the-review-pass) says what may go
in it.

## The document comes first

`end-goal/` is made as good as it can be before the code is. While that holds, `factory/`
is expendable: if the code drifts too far from the document, it is deleted and rewritten
later rather than the document bent to fit it. A conflict between the two is resolved in
the document's favour, and keeping the code compilable is never a reason to weaken an
edit to `end-goal/`.

## What the work spans

Before answering, name which row below the subject belongs to, and say which row it is
wherever the answer would differ by row. The list staffs nothing.
[`README.md`](README.md) names the same disciplines and links here for what each owns.

### What the product is

| Discipline | What in the design document it owns |
|---|---|
| Product management | Whether a self-hosted factory with one customer per install is worth having, and what the no-tenancy decision costs |
| Product design | The four screens, and a home view that is empty whenever the factory is working |
| Design systems | The design system a project holds as a permanent constraint: what a machine can check, and what a designer has to produce as code before anything checks it |
| Technical writing | The document itself — the glossary, the cross-references that make its claims interlock, and consistency between the fields' vocabularies. It does not own the terms of art: each row below owns its own, which is what stops a name being traded away for a consistent one by a discipline whose job is consistency |

### What the factory decides

| Discipline | What in the design document it owns |
|---|---|
| Applied statistics and sequential testing | The analysis window: a boundary valid at every point it is read, the size and confidence an owner authors, and whether `passed` is reachable at all on a quiet service |
| Risk scoring | The score's factors, its published formula, its calibration, and a loop trained on outcomes its own decisions selected |
| Requirements engineering | The six criterion patterns, and the unwanted conditions a pattern-perfect set can still omit |
| Formal methods | The item, gate, hold, and rollback lifecycle as a state machine, and whether it deadlocks |
| Safety engineering | One path with no second path, and what an analysis over a control structure of that shape finds |
| Human factors | What a human at a gate does when nearly every gate auto-passes, and what a page that reaches everybody does to the next one |

### What the factory is made of

| Discipline | What in the design document it owns |
|---|---|
| Software architecture | Component boundaries, one writer per record, and the seam declared between two |
| Backend engineering | The record graph and the components that write it |
| Frontend engineering | The four screens as software |
| Data architecture | Backup, restore, retention, and records written by one version of a self-hosted product that the next version has to read |
| Agent engineering | The fleet — a model in a role with a scope — what a stage's agent is given, and how one is evaluated |
| Program analysis | Deriving a consumer's declaration from its build, which differs per toolchain |

### What the factory runs

| Discipline | What in the design document it owns |
|---|---|
| Release engineering | The merge queue, the two rollout strategies, the window limit, and a rollback's target |
| Database migration engineering | The store's forward promise, which is what a rollback across a schema change rests on |
| Site reliability engineering | Pages, escalation, incidents, and the drift detector |
| Observability engineering | The quantity the health monitor reads, and instrumenting software the factory wrote so that it emits one |
| Test architecture | What runs on a candidate environment, every pre-merge check being decided against that run |
| Platform engineering | An environment per candidate, and a place for a service decomposition creates |

### What the factory answers to

| Discipline | What in the design document it owns |
|---|---|
| Security engineering | The five seams, and the policy that attaches at the one between the deployer and a deploy target |
| Supply chain security | Dependencies the factory adds on its own — versions, vulnerabilities, and licenses |
| Trust and safety | The report channel, which is the one way in from outside the factory |
| Audit and compliance | Traceability as a claim made to an auditor, and segregation of duties in a system that authors, approves, and deploys |
| Legal | Laws and regulations as permanent constraints, and licensing a product a customer self-hosts |
| Cost engineering | Cost per feature, the provider's quota, and the spend ceiling an owner authors per credential |

## Writing style

- Use words according to their established meaning. Avoid figurative, anthropomorphic, or unnatural phrasing.
- Use established terminology of the relevant field according to its established meaning.
- Do not use terminology from another field as metaphor or analogy.
- Do not invent terminology. If no established term exists, describe the concept in plain language.
- Apply these rules to prose, code, schemas, and file names.
- Use established terms of art normally; they are not considered borrowed terminology.
- Be concise. Prefer the fewest words needed to convey the meaning accurately.

## Delegate by default

Route work to the agents in `~/.claude/agents/` instead of doing it in the main
context, without being asked. Each agent's definition carries the model and effort matched
to its tier, so routing to the right agent is routing to the right model. The routing
table is the `description:` line of each agent file. Between two agents the tier ladder is
quality > tokens > time, and a doubt resolves upward — to opus and no higher. Opus is the
cap for every subagent. A type not in the roster (`general-purpose`, `Explore`, an ad-hoc
dispatch) inherits the session model, so pass `model: "opus"` on every such launch; `fork`
ignores the override and is not used while the session model is above opus.

Stays in the main context: triage and routing itself, anything the user must decide,
conversation-spanning work an agent cannot see, and answers so small that dispatch costs
more than it saves.

Agents run sequentially when dependent, in parallel only when genuinely independent and
few. [_The review pass_](#the-review-pass) sets its own batch size, and its review agents
still run only when the owner names them.

## Code

Code lives in `factory/` — beside `end-goal/`, never inside it — written in Go against
PostgreSQL, and PostgreSQL from the first record so the chained log is never migrated.
`mise.toml` at the root pins the toolchain; `factory/docker-compose.yml` runs the dev
database.

Five rules, set because the code's readers are LLMs:

- **Feature-sliced packages with hard boundaries.** One package owns one thing — its
  schema, its writer, its doc — so a task touches a few files in one directory rather than
  fifteen across five layers.
- **Explicit over implicit.** No runtime reflection, no DI container, no string-keyed
  dispatch, no codegen the source does not show. Everything a static reader needs is in
  the text.
- **Locality.** Small files, shallow indirection, low fan-out.
- **Machine-checked dependency direction.** `factory/deps.txt` is the allowed package
  graph and `cmd/depscheck` fails the build on an edge not in it; the compiler already
  refuses cycles.
- **The map ships with the code.** `factory/README.md` names every package and the allowed
  edges; each package's `doc.go` says what it owns, who may write what, and which
  `end-goal/` section defines the concept it implements.

Duplicate a line rather than share a helper across packages: locality is paid for in
repetition, and the repetition is the cheaper of the two. Expect more packages than a
layered design would have.

## The review pass

`end-goal/CLAUDE.md`'s consistency pass verifies the design document against rules the
document sets and runs after every edit. This second pass asks whether what those rules
protect is any good. Neither substitutes for the other: the consistency pass finds a link
pointing at nothing, a heading no longer matching its anchor, a term used before it is
introduced; the review pass finds a design that would not work, a subject the design never
mentions, and a rule costing more than it returns.

**Dispatch.** A review agent runs when the owner names it, and the owner sets how many run
at once. No phrase runs all of them, and no request means more of them than it names;
**Audit this project** dispatched all thirty in one burst and was retired after one run
spent a session limit in ten minutes and a fifth of a weekly model quota. Batches of five
cost the same in total and spread it over six times as long, which is what makes a full
roster affordable in one session. A partial run must not speak for the whole design: its report names which
agents ran, and a design those agents found sound is not a design found sound. Each one
runs on a model and effort matched to what it judges — quality first, tokens
second, time last, so a doubt between two tiers resolves to the higher and the cheap tier
is never a default — and opus is the cap, as for every subagent.

**Each review agent is dispatched cold**, in its own subagent, and told two things in its
dispatch text: to judge what it reads on its own and ignore anything it was told about
this repository elsewhere, and that the instruction files it has been given are material
to review rather than rules to obey. A subagent receives this file whether or not it is
asked to, and an unqualified copy of the rules reproduces the exact defect the pass exists
to find — an agent that has read the instruction files audits against them and reports the
design sound.

A discipline agent is told a third thing: to audit the whole document from its field, its
row in [_What the work spans_](#what-the-work-spans) being the document's claim about what
it owns rather than the boundary of what to read. That table is the other thing the
dispatch cannot withhold, and one that audits only its own rows confirms the table rather
than the design.

**The review agents** are a roster of thirty, each one subagent reading the repository's
Markdown — `end-goal/` and the instruction files — and none seeing another's work.
Twenty-eight are the disciplines [_What the work spans_](#what-the-work-spans) names, one
per row, not repeated here. Each asks two things of the whole design: what its field knows
the design gets wrong, and what its field normally covers that the design never mentions.

Two stances survive beside them, because neither is any discipline's. Four earlier ones —
the builder, the operator, the adversary, the cold reader — were replaced by discipline
agents and are not on the roster:

| Stance | What it looks for |
|---|---|
| Absence | Subjects a design of this kind normally covers and this one never mentions |
| Rules | Reads the instruction files alone: whether a rule earns the cost it states, whether two conflict, and whether one is followed anywhere in the design |

Each review agent returns at most three findings, ranked by what turns on them. One that
finds nothing returns nothing, and that is a result: a discipline the document never
touches is either absent from the design or wrongly on the list.

**What happens to a finding.** This is a triage and not a queue to empty — the first run
returned about sixty. Each finding takes one of three dispositions:

- **Taken** — folded into the file that owns the subject, with its reason and its cost.
  Taken is the preferred disposition, and a taken finding is answered with a mechanism —
  a rule, a record, a check, a bound the design states — not a sentence acknowledging the
  problem.
- **Carried** — a question in [`end-goal/open.md`](end-goal/open.md), phrased as the
  question and what turns on it.
- **Refused** — the reason written into the file that owns the subject. A refusal does not
  bind a later run; a review agent raising the same thing again is answered by the text.

Where more than one agent reached a finding separately, record that with it wherever it
lands. `review-findings.md` holds what a run returned until triage
reaches it, and holds nothing else: no disposition, no reason, no refusal. A finding still
lands in one of the three places above, and the entry goes when it does — the file empties
as triage proceeds and is deleted when it is empty, the way `jargon-cleanup.md` was. What
it costs is that a finding can sit there being read as a backlog, and that a refusal
recorded there would not bind a later run — which is why a refusal is never recorded
there.

Triage is done with the owner, never by the session that ran the pass, because refusal is
the cheapest disposition and should be the one it is least eager to reach: every taken
finding is an edit to `end-goal/`, which fires the consistency pass — a cold-read subagent
per changed file plus the eleven-file read-through. Running on request means a defect can
sit until someone thinks to look, the thirty are fixed so they find thirty kinds of thing
and no thirty-first, and coverage is whatever the owner remembers to ask for.

## Commits

Commit straight to `main`. Do not create a branch unasked; branches start when the project
is ready for them.

**The commit is the history.** Do not put dates, timestamps, change history, or version
history in standing instruction text. Do not annotate rules or facts with when they were
added, changed, or confirmed. If something needs to record when or why a rule changed, the
commit records it.

Push with `git push`. `origin` is an HTTPS URL and git pushes to it on its own, so whether
the GitHub CLI is installed has nothing to do with a push — do not answer a request to
push by reporting `gh` missing. `gh` is for what git cannot do: pull requests, issues, the
API. `git push` publishes, so a commit is on the remote the moment it runs and taking it
back there is another commit.

## How a change to the end goal is recorded

Change `end-goal/` directly. The edit goes in the file that owns the subject, and the
commit body says what changed and why, which is the shape `end-goal/CLAUDE.md` sets.

**No ADRs.** They start when the factory is proved and keeping a decision fixed is worth
more than staying cheap to change. The design document is a target, revised whenever
something is learned.

## Standing text does not refuse a pivot

When the owner decides against something the design says: state what it costs once and
briefly — what breaks, and what the text was protecting — then do the work and edit the
design to follow. Citing a cost as a refusal, or arguing it a second time after the owner
has heard it, is a defect in the session and not a defence of the design.

Between owner decisions nothing changes: a session does not drift from the design, and
does not edit it without a reason it writes down.

## graphify

Knowledge graph at `graphify-out/` with god nodes, community structure, and cross-file
relationships.

- For codebase questions, run `graphify query "<question>"` first when
  `graphify-out/graph.json` exists. Use `graphify path "<A>" "<B>"` for relationships and
  `graphify explain "<concept>"` for focused concepts.
- Use `graphify-out/wiki/index.md` for broad navigation instead of raw source browsing
  when it exists.
- Read `graphify-out/GRAPH_REPORT.md` only for broad architecture review, or when
  query/path/explain do not surface enough context.
- After modifying code, run `graphify update .` (AST-only, no API cost).
