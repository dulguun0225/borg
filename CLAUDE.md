# CLAUDE.md

## What this repository is

A monorepo for building the software factory [README.md](README.md) describes. It holds
the design document under `end-goal/`. Code is added beside it as it arrives, never
inside it — `end-goal/` is the state the repository is built toward, not a record of what
it currently does.

The factory is built as ordinary software and does not run its own pipeline over itself.

**Read `end-goal/CLAUDE.md` before touching anything under `end-goal/`.** It has its own
editing rules and a consistency pass to run after every edit, which govern that directory
alone and say nothing about code. **Writing style** below is what governs prose
everywhere, that directory included; it sat in `end-goal/CLAUDE.md` until 2026-08-14.

A decision reached while working folds into the `end-goal/` file that owns the subject.
Nothing else in the repository records one — there is no plan file, no status file, and
nothing that says what work is under way. (Owner rule, 2026-08-14.)

## What the work spans

Building this needs more than one kind of expertise, and the design document is written in
one register — records, writers, seams — which hides that. A question about the watch
window reads as an architecture question and is answered as one, in a voice that sounds
right and is wrong: what decides whether the window works is the arithmetic of a
sequential test at the traffic one install has. So before answering, name which row below
the subject belongs to, and say which it is where the answer would differ by row.

### What the product is

| Discipline | What in the tree it owns |
|---|---|
| Product management | Whether a self-hosted factory with one customer per install is worth having, and what the no-tenancy decision costs |
| Product design | The four surfaces, and a home view that is empty whenever the factory is working |
| Design systems | The design system a project holds as a standing constraint: what a machine can check, and what a designer has to produce as code before anything checks it |
| Technical writing | The document itself — one name per concept held constant, the glossary, and the cross-references that make its claims interlock |

### What the factory decides

| Discipline | What in the tree it owns |
|---|---|
| Applied statistics and sequential testing | The watch window: a boundary valid at every point it is read, the size and confidence an owner authors, and whether `clean` is reachable at all on a quiet service |
| Risk scoring | The score's factors, its published formula, its calibration, and a loop trained on outcomes its own decisions selected |
| Requirements engineering | The six criterion patterns, and the unwanted conditions a pattern-perfect set can still omit |
| Formal methods | The item, gate, hold, and rollback lifecycle as a state machine, and whether it deadlocks |
| Safety engineering | One path with no second path, and what an analysis over a control structure of that shape finds |
| Human factors | What a human at a gate does when nearly every gate auto-passes, and what a page that reaches everybody does to the next one |

### What the factory is made of

| Discipline | What in the tree it owns |
|---|---|
| Software architecture | Component boundaries, one writer per record, and the seam declared between two |
| Backend engineering | The record graph and the components that write it |
| Frontend engineering | The four surfaces as software |
| Data architecture | Backup, restore, retention, and records written by one version of a self-hosted product that the next version has to read |
| Agent engineering | The fleet — a model in a role with a scope — what a stage's agent is given, and how one is evaluated |
| Program analysis | Deriving a consumer's declaration from its build, which differs per toolchain |

### What the factory runs

| Discipline | What in the tree it owns |
|---|---|
| Release engineering | The merge queue, the two rollout strategies, K, and a rollback's target |
| Database migration engineering | The store's forward promise, which is what a rollback across a schema change rests on |
| Site reliability engineering | Pages, escalation, incidents, and the reconciler |
| Observability engineering | The quantity the comparison reads, and instrumenting software the factory wrote so that it emits one |
| Test architecture | What runs on a candidate environment, every pre-merge check being decided against that run |
| Platform engineering | An environment per candidate, and a place for a service the cut creates |

### What the factory answers to

| Discipline | What in the tree it owns |
|---|---|
| Security engineering | The four seams, and the policy that attaches at the one between an agent and a deploy target |
| Supply chain security | Dependencies the factory adds on its own — versions, vulnerabilities, and licenses |
| Trust and safety | The report channel, which is the one way in from outside the factory |
| Audit and compliance | Traceability as a claim made to an auditor, and segregation of duties in a system that authors, approves, and deploys |
| Legal | Laws and regulations as standing constraints, and licensing a product a customer self-hosts |
| Cost engineering | Cost per feature, the provider's quota, and the spend ceiling the tree refuses |

What it costs: a list this long names something for every question and so settles none of
them, and most sessions touch one row or two. It also describes a factory that does not
exist yet, so a row will turn out to be an afternoon's work rather than a discipline. Its
use is to stop an answer being given in the wrong register, and it staffs nothing.
[`README.md`](README.md) names the same disciplines and links here for what each owns.
(Owner rule, 2026-08-16.)

## Writing style

Governs every file in this repository — the design document under `end-goal/`, this file,
commit bodies — and anything written about them, a reply in the terminal included.

**Precise, then literal, then understandable, then concise, then simple.** The order is
the rule and only does work where they conflict. Simple is last, not absent: plain words
and short sentences wherever they take nothing away. (Owner rule, 2026-08-15.)

**Precise.** If cutting a qualification blurs the claim, keep it: "a breaking diff
without the migration already shipped ahead of it" is not "a bad diff." A true statement
that needs a caveat gets the caveat: "the last place a human decides — by default, and
by score, not because the gates downstream of it are missing." Name the scope — *per
service*, *at merge to master*. One name per concept, held constant across sections.

**Literal.** Name the thing and say what happens to it. A record does not carry, hold,
walk, stand, ride, or land — a component writes it, reads it, or points at it. Nothing
is bought, spent, or paid for unless money or a quota actually moves. Corporate register
is the same rule: nothing **is key**, and work is **in progress** rather than **in
flight**. Metaphor reads as precision and is not: *an intent carries a project* leaves a
reader choosing between a field on the record, a link to another record, and something a
later stage looks up. Where the literal sentence is longer or harder to take in, write
it anyway — the remedy for hard is the next rule, never the figure. (Owner rule,
2026-08-14.)

**Settled technical terms exempt.** A term `end-goal/` defines keeps its word even where
the word is on the list above: a **hold** at a gate, a **standing** constraint, a
**control**, a **straight** deploy, a window closing **clean** or **swept**, a **floor**
under a parameter or a **ceiling** over it. What makes a term settled is that a section
defines it, not that two paragraphs use it — so the exemption is checkable, and coining
one to get around the ban is not it. Outside its definition the word is ordinary again:
a record still does not hold its fields. (Owner rule, 2026-08-14.)

**Understandable.** A term is introduced where it is first used, or linked to where it
is introduced. A reader who has not read the rest of the document has to be able to
finish the sentence they are on. Where introducing a term needs another clause, write
the clause. A term `end-goal/` defines is linked to its section or to
[`end-goal/glossary.md`](end-goal/glossary.md), which is what a file outside that
directory does rather than assuming the name. What it costs is length: a paragraph
naming six records introduces six. (Owner rule, 2026-08-15.)

A section opens with one sentence saying what it is about, in ordinary words. That
sentence may restate the heading and may summarise what follows. It is the one place
redundancy is not waste.

**Concise.** The short true sentence over the longer gentle one. State reasons rather
than announcing them — `This is the reason:` was cut for exactly that.

Two habits follow. A rule is stated together with the downside it creates — no-batching
with the human-UAT ceiling it creates. A qualification goes in an em-dash aside where
the sentence still reads without it; where the aside is itself a claim the reader has to
keep in mind, it becomes its own sentence. (Owner rule, 2026-08-15.)

**Structure for a reader.** A long run of uniform paragraphs gets `###` subheadings; a
set of parallel facts gets a table. When a table contains a definition, the prose around
it must not restate the table — trim the prose to what the table cannot express.

No hard wrap anywhere but the instruction files: one paragraph is one line, and the
renderer does the rest. The instruction files — this one, `end-goal/CLAUDE.md`, and the
root `README.md` — stay wrapped, which is how all three are written today. (Owner rule,
2026-08-13.)

## The review pass

`end-goal/CLAUDE.md` has a consistency pass that verifies the design document against
rules the document sets, and it runs after every edit. This is the second pass, and it
asks whether what those rules protect is any good. A reader below runs when the owner
names it, and readers run one at a time — the next starts after the previous returns. No
phrase runs all of them, and no request means more readers than it names. There was one —
**Audit this project** dispatched all thirty at once — and it was retired: one run spent
a session limit in ten minutes and a fifth of a weekly model quota. What a partial run
must not do is speak for the tree: its report names which readers ran, and a tree those
readers found sound is not a tree found sound. A reader's subagent runs on a model and at
an effort matched to what it judges — quality of the work decides first, tokens second,
time last, so a doubt between two tiers resolves to the higher, and the cheap tier is
never a default. (Owner rule, 2026-08-17.)

Neither substitutes for the other. The consistency pass finds a link pointing at nothing,
a heading no longer matching the anchor aimed at it, a term used before it is introduced —
defects the rules name, which is why a grep finds most of them. The review pass finds a
design that would not work, a subject the tree never mentions, and a rule costing more
than it returns. Nothing in the repository looked for those until 2026-08-16.

### Why it is dispatched cold

An agent that has read the instruction files audits against them and reports the tree
sound, because every rule in it is satisfied. That is the failure this pass exists to
defeat, and it is the blindness the cold-read check already names one level down: on each
changed file, `end-goal/CLAUDE.md` sends a subagent nothing but the path, because one that
has read the whole document resolves every term and returns an empty list.

So each reader runs in its own subagent, and each is told two things in its dispatch text
— to judge what it reads on its own and ignore anything it was told about this repository
elsewhere, and that the instruction files it has been given are material to review rather
than rules to obey. A discipline reader is told a third, below. A subagent receives this
file whether or not it is asked to, which `end-goal/CLAUDE.md` records as a leak for the
cold-read check. Here an unqualified copy of the rules reproduces the exact defect the
pass is for.

[_What the work spans_](#what-the-work-spans) is a second leak and a sharper one. It tells
each discipline reader what in the tree it owns, which is a brief where half the job is
finding what its field covers and the tree never mentions — a reader that audits its own
rows confirms the table rather than the design. So a discipline reader is told to audit
the whole tree from its field, and that its row is the tree's claim about what it owns
rather than the boundary of what to read.

### The readers

A roster of thirty, each run as one subagent reading the repository's Markdown —
`end-goal/` and the instruction files — and none seeing another's work. Twenty-eight are
the disciplines [_What the work spans_](#what-the-work-spans) names, one per row, each
asking two things of the whole tree: what its field knows the design gets wrong, and what
its field normally covers that the design never mentions. The rows are not repeated here — the table is one
place, and a copy would be two able to disagree.

Two stances survive beside them, because neither is any discipline's. A stance is a
position with a reason to find something, not a checklist:

| Stance | What it looks for |
|---|---|
| The absence reader | Subjects a design of this kind normally covers and this one never mentions |
| The rule reader | Reads the instruction files alone: whether a rule earns the cost it states, whether two conflict, and whether one is followed anywhere in the tree |

Four stances went, each replaced by readers that know the field rather than occupy a
position in it: the builder by software architecture and backend engineering, the operator
by site reliability and observability engineering, the adversary by safety and security
engineering, the cold reader by technical writing. The absence reader stays because the
twenty-eight were derived by reading this tree and inherit its blind spots — a subject no
discipline on the list owns is invisible to all twenty-eight and to nothing else. The rule
reader stays because the instruction files are what it reads, and no discipline is pointed
at them.

Each reader returns at most three findings, ranked by what turns on them. An uncapped
reader returns what nobody reads, and the cap costs the fourth finding of a reader that
had more to say. A reader that finds nothing returns nothing, and that is a result rather
than a gap to fill: a discipline the tree never touches is either absent from the design
or wrongly on the list.

### What happens to a finding

Findings accumulate faster than sessions act on them — the first run, six stances
dispatched together, returned about sixty — so this is a triage and not a queue to empty.
Each finding takes one of three dispositions:

- **Taken** — folded into the file that owns the subject, with its reason and its cost.
- **Carried** — a question in [`end-goal/open.md`](end-goal/open.md), phrased as the
  question and what turns on it. That file is the backlog, and it already sets what may
  sit there: genuinely unsettled, with an owner as who decides it. A review-pass finding
  meets the other test it sets too — the subject raised it, rather than a session
  noticing a loose end while doing something else.
- **Refused** — the reason written into the file that owns the subject.

Where more than one reader reached a finding separately, that is recorded with it wherever
it lands, because independent arrival is evidence about the finding rather than about the
reader. `end-goal/open.md` held that rule until the run it described was discarded.

A refusal is written because the document records why it is what it is, the way it
records that `beta` was dropped and that an uncut intent is not an item. It does not bind
a later run. A reader raising the same thing again is answered by the text, which costs a
paragraph to read and is the text doing its job. What must not appear is a fourth place:
a findings file, a report per run, a list of what each reader said. `next.md` was that
shape, and `end-goal/` emptied it on 2026-08-14.

What it costs: a subagent per reader named, and a result that is a judgment rather than a
command's exit status. Running on request rather than after every edit means a defect can
sit in the tree until someone thinks to look, and the readers are fixed, so they find
thirty kinds of thing and no thirty-first. One reader per request means coverage is
whatever the owner remembers to ask for — a defect in a field whose reader is never named
sits indefinitely — and covering the roster costs thirty requests paid in latency where
the retired full run paid in quota. The larger cost is downstream. Every taken
finding is an edit to `end-goal/`, which fires the consistency pass — a cold-read subagent
per changed file and the eleven-file read-through — so ten taken findings cost ten of
each. That makes refusal the cheapest disposition, which is the one it should be least
eager to reach, and is why the triage is done with the owner rather than by the session
that ran the pass. (Owner rule, 2026-08-16.)

## Commits

Commit straight to `main`. The project is early; branches start when it is ready for
them, and not before. Do not create one unasked. (Owner rule, 2026-08-13.)

Push with `git push`. `origin` is an HTTPS URL and git pushes to it on its own, so
whether the GitHub CLI is installed has nothing to do with a push — answering a request
to push with `gh` being missing refuses work that would have succeeded. `gh` is for what
git cannot do: pull requests, issues, the API. What it costs: `git push` publishes, so a
commit is on the remote the moment it runs, and taking it back there is another commit.
(Owner rule, 2026-08-17.)

## How a change to the end goal is recorded

Change `end-goal/` directly. The commit is the record: the edit goes in the file that
owns the subject, and the body says what changed and why, which is the shape
`end-goal/CLAUDE.md` already sets. That is a record of what happened and nothing that
binds what comes next.

No ADRs until the project has proved itself. A record claiming authority over future work
is what made an earlier attempt unchangeable — the pile grew, an agent could always find
one to cite and answer a change with pages of text, and removing the ones that had stopped
being true was work nobody did. The design document is a target, revised whenever
something is learned; an ADR binds what comes next, and nothing here has earned that yet.
They start when the factory is proved and keeping a decision fixed is worth more than
staying cheap to change. (Owner decision, 2026-08-14.)
