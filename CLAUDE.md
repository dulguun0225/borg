# CLAUDE.md

## What this repository is

A monorepo for building the software factory [README.md](README.md) describes. It holds
the design document under `end-goal/`, the record of building toward it under
`bootstrap/`, and the artifacts each stage writes for one item under `items/`. Code is
added beside those three as it arrives, never inside any of them — `end-goal/` is the
state the repository is built toward, not a record of what it currently does.

**Read `end-goal/CLAUDE.md` before touching anything under `end-goal/`.** It has its own
editing rules and a consistency pass to run after every edit, which govern that directory
alone and say nothing about code. **Writing style** below is what governs prose
everywhere, that directory included; it sat in `end-goal/CLAUDE.md` until 2026-08-14.

**Read [`bootstrap/README.md`](bootstrap/README.md) before starting work.** It has the
plan and where the work has got to, and it is the only place that says what work is under
way. A decision reached while working folds into the `end-goal/` file that owns the
subject and never into `bootstrap/`, which holds process and not design. A spec, an
implementation plan, or a set of tasks goes under [`items/`](items/README.md) instead,
one directory per item. (Owner decision, 2026-08-14.)

## Writing style

Governs every file in this repository — the design document under `end-goal/`,
`bootstrap/`, this file, commit bodies — and anything written about them, a reply in the
terminal included.

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
