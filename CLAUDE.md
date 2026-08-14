# CLAUDE.md

## What this repository is

A monorepo for building the software factory [README.md](README.md) describes. It holds
the design document under `end-goal/` and the record of building toward it under
`bootstrap/`. Code is added beside those two as it arrives, never inside either —
`end-goal/` is the state the repository is built toward, not a record of what it
currently does.

**Read `end-goal/CLAUDE.md` before touching anything under `end-goal/`.** It has its own
editing rules and a consistency pass to run after every edit, which govern that directory
alone and say nothing about code. **Writing style** below is what governs prose
everywhere, that directory included; it sat in `end-goal/CLAUDE.md` until 2026-08-14.

**Read [`bootstrap/README.md`](bootstrap/README.md) before starting work.** It has the
plan and where the work has got to, and it is the only place that says what work is under
way. A decision reached while working folds into the `end-goal/` file that owns the
subject and never into `bootstrap/`, which holds process and not design.

## Writing style

Governs every file in this repository — the design tree, `bootstrap/`, this file, commit
bodies — and anything written about them, a reply in the terminal included.

**Precise, then concise, then simple.** The order is the rule — it only does work when
the three conflict.

**Precise beats concise.** If cutting a qualification blurs the claim, keep it: "a
breaking diff without the migration already shipped ahead of it" is not "a bad diff." Name
the scope — *per service*, *at merge to master*. One name per concept, held constant
across sections.

**Precise beats simple.** A true statement that needs a caveat gets the caveat: "the last
place a human decides — by default, and by score, not because the gates downstream of it
are missing."

**Concise beats simple.** The short true sentence over the longer gentle one. No preamble,
no restating the heading, no summarizing what is about to be said. State reasons rather
than announcing them — `This is the reason:` was cut for exactly that.

Simple is last, not absent: plain words and short sentences wherever they take nothing
away.

**No figurative speech and no business speech.** Name the thing and say what happens to
it, literally. A record does not carry, hold, walk, stand, ride, or land — a component
writes it, reads it, or points at it. Nothing is bought, spent, or paid for unless money
or a quota actually moves. Metaphor reads as precision and is not: *an intent carries a
project* leaves a reader choosing between a field on the record, a link to another record,
and something a later stage looks up. Where the literal sentence is longer, write the
longer one — precise still beats concise.

Corporate register is the same rule and was swept with it: nothing **is key**, there are
no **touchpoints**, **handoffs**, or **readouts**, work is **in progress** rather than
**in flight**, and a problem **appears** rather than **surfaces**. Say what the thing is
or what it does. (Owner rule, 2026-08-14, replacing the licence for idiom that stood in
`end-goal/CLAUDE.md`.)

**Settled technical terms exempt.** A term the tree defines keeps its word even where the
word is on the list above: a **hold** at a gate, a **standing** constraint, a **control**,
a **straight** deploy, a window closing **clean** or **swept**, a **floor** under a
parameter or a **ceiling** over it. What makes a term settled is that a section defines
it, not that two paragraphs use it — so the exemption is checkable, and coining one to get
around the ban is not it. Outside its definition the word is ordinary again: a record
still does not hold its fields. (Owner rule, 2026-08-14.)

Two habits follow. A rule is stated together with the downside it creates — no-batching
with the human-UAT ceiling it creates. A qualification goes in an em-dash aside rather
than a sentence of its own.

**Structure for a reader.** Prose here is read by humans, not only parsed. A long run of
uniform paragraphs gets `###` subheadings; a set of parallel facts gets a table. When a
table contains a definition, the prose around it must not restate the table — trim the
prose to what the table cannot express.

No hard wrap in the design tree or in `bootstrap/`: one paragraph is one line, and the
renderer does the rest. (Owner rule, 2026-08-13, replacing the 88-column wrap.) The
instruction files — this one, `end-goal/CLAUDE.md`, and the root `README.md` — stay
wrapped, which is how all three are written today.

## Commits

Commit straight to `main`. The project is early; branches start when it is ready for
them, and not before. Do not create one unasked. (Owner rule, 2026-08-13.)

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

## graphify

Removed from this project on 2026-08-14 — the index, the ignore entry, the parked hooks,
and the rules that pointed at them. It stays installed on the machine and is not the
thing being refused; what is refused is running it here. An AST index is only as useful
as the code it indexes, and this repository is still all prose, where graphify takes its
other extraction path and pays LLM subagents for it: a `--update` over the twenty-file
design tree spent 64k tokens on one of two chunks before the run was cut, to answer worse
than the greps in `end-goal/CLAUDE.md`, which answer exactly and in milliseconds.
Reconsider when code is added beside `end-goal/`, scoped to the code paths and never to
the prose.
(Owner decision, 2026-08-14, superseding the keep of earlier the same day.)
