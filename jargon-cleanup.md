# Jargon cleanup

Private vocabulary in this repository that a reader who has not memorised the repository cannot resolve, and what to do about each piece of it. The work is roughly 2.5–4M tokens and does not fit one session, which is why it is written down rather than done.

## What this file is, and when it goes

It records unfinished work and not a decision. Nothing in it binds a later session; where it disagrees with `end-goal/`, `end-goal/` is right, and a group here that turns out to be wrong is struck rather than argued with.

It is deleted when the last group lands. A group finished is struck from this file in the same commit that finishes it, so the file shrinks to nothing and is removed — that termination condition is the whole difference between this file and `next.md`, which `end-goal/` emptied on 2026-08-14 for being a list of answers kept outside the sections that own them.

It is an exception to the rule in [`CLAUDE.md`](CLAUDE.md#what-this-repository-is) that beyond `end-goal/` and [`roadmap.md`](roadmap.md) nothing in the repository records what work is under way. The owner made the exception on 2026-08-20 because the alternative was losing the inventory. What it costs is a fourth place a later session can read as authority, and the two paragraphs above are what hold that down.

## The test

Can a reader who has not read the rest of the repository resolve this term from the file they are in, or reach its definition from there? If neither, it is a defect. That is the `Understandable` rule in [`CLAUDE.md`](CLAUDE.md#writing-style), and its sharper claim in the same section: a private synonym that collides with the ordinary word is worse than a private synonym, because only a reader who already knows can tell which is meant.

What the test does **not** ask is whether the vocabulary could be smaller. A term with a line in [`end-goal/glossary.md`](end-goal/glossary.md), linked where it is used, costs a fresh reader one hop and stays. So **fleet** (115 uses), **the cut** (60 uses) and **the interview** (56 uses) are out of scope: each names a stage or a set that prose would otherwise re-describe a slightly different way per section, which is exactly what the writing-style rule says earns a term its place. **hold**, **standing**, **control**, **straight**, **clean**, **swept**, **floor** and **ceiling** are out of scope too — the settled-term exemption protects them by name.

## Verified evidence

Three findings were checked against the files rather than taken from a summary, and they set the scale.

**`end-goal/glossary.md`'s preamble is false.** It promises "Every term the document uses as though already defined, with one line saying what it is." It has 109 lines, and at least a dozen heavily used terms have none. Making that sentence true is most of Group 2.

**A registered term is defined by an unregistered one.** `end-goal/glossary.md` defines **swept** as an exit that leaves a release "neither condemned nor running". **condemned** is used 19 times, is never bolded, and is defined nowhere. Fix it before anything else in Group 2: a reader who follows the glossary to `swept` is handed a definition they cannot read.

**The repository commits the defect its own rule calls the worst.** **reader** means both a review-pass subagent and an ordinary person reading, about fifteen lines apart in `CLAUDE.md`, across roughly 44 uses. **form** carries two load-bearing senses and the glossary points a reader at the wrong one.

## Group 1 — collisions: one word, two meanings, no way to tell which

The sharpest class, because a reader cannot tell they have missed anything.

| Word | The two senses | Fix |
|---|---|---|
| **reader** (~44 uses, `CLAUDE.md`, `README.md`) | a review-pass subagent (~25) against the ordinary prose reader (~13), in one file | Rename the subagent sense to **auditor** — the text already says these audit the whole document from their field. So a discipline auditor, the absence auditor, the rule auditor, thirty auditors. Leaves **reader** meaning a person reading. |
| **row** (~210 uses repo-wide) | a line in a table against a live gate awaiting a human's verdict | The damaging pair. Use **firing** for the live sense: the word is already used 24 times for exactly that and is itself undefined, so defining it once fixes both. Table-line and log-row uses stay ordinary. |
| **form** (~35 uses) | the glossary's sense — a constraint's field deciding which gate can reject on it — against the derived shape a published interface has, in `end-goal/how-humans-do-it/07-contracts.md` | The contracts sense is unregistered, and the glossary actively sends a reader to the other one. Introduce it as **interface form** at first use per file and give it its own glossary line; the constraint sense keeps **form**. Read the sentences before settling the wording — this is the one item where the counts do not decide it. |
| **seam** (~60 uses) | the glossary's sense — one of the four deferred interfaces, numbered and settled — against the field or event that partitions two writers of one record | Replace the writer-boundary sense with plain words: "the boundary between the two writers is the field". Seams 1–4 stay. |
| **the number** (30 uses) | the per-service release ordinal against the score's number, the trust number and the cost number | Make it **the release number**. One word longer, and it lets the consistency pass drop the hard-coded `SKIP` set that exists only to compensate for the name. |
| **Factory** (10 uses, in `factory/**/doc.go`) | one of the four surfaces, against the factory the product, against the Go identifier `[Factory]` — all three in one paragraph in `factory/policy/doc.go` | Say "at Factory, the surface an owner authors through" at first use per file. `factory/pin/doc.go` is the worst site: the collision is in the package's first sentence. |
| **roster** (3 uses, `CLAUDE.md`) | the delegation agents in `~/.claude/agents/` against the thirty review auditors, ninety lines apart and different sets | Name each: "the agent set in `~/.claude/agents/`" and "the thirty auditors". |
| **store** (~41 uses), **reach** (3 senses), **page** (3–4) | the registered sense against a place bytes are kept; three unregistered senses; the escalation channel against a web page | Narrow the unregistered senses to plain words. |

## Group 2 — terms used as settled with no definition anywhere

Nothing to link to, so no link fixes these. Each gets introduced where it is first used **and** a glossary line — except where the plain phrase is shorter, and then it is simply replaced.

Do **condemned** (19 uses) first: `swept`'s definition depends on it.

**writer** (45 uses) is the document's most-invoked argument and exists nowhere as a rule. `end-goal/how-humans-do-it/09-gate-policy.md` says "a second writer with no seam" and hands the reader neither half. State the one-writer-per-record rule once, in the file that owns record structure, and link to it instead of re-deriving it per section.

**row** needs its two damaging senses separated before it needs a definition — see Group 1.

Register and introduce: **superseded** (16 uses, whose enum sibling `dropped` has a line), **in force** (~110 across `roadmap.md`, `factory/README.md` and the `doc.go` files, three distinct meanings), **the log** / **decision log** (32 uses, while its counterpart `the graph` has a line), **encoding** (21 uses), **firing** (24 uses), **unrefined** / **refined** / **re-cutting** (~25 uses, all bolded, none registered), **install** (11 uses, the scoping unit for retention and measurement), **organic traffic** (7 uses, the sole stated reason for three separate design refusals), **the steady state** (4 uses, and `03-gates.md`'s argument about the attempt bound rests on it), **band** (3 uses), **lattice** (4 uses), **factor vector** (6 uses), **grouper** (6 uses), **disposition** (5 uses), **span** (3 uses), **marked** (4 uses), **observables** (3 uses), **undecided** (2 uses), **checkpoint** (2 uses), **escape** (2 uses).

Four authored parameters with no glossary line, while five of their siblings have one: **retention** (10 uses), **item-size target** (9 uses), **risk threshold** (3 uses), **explicit health threshold** (2 uses).

Two are broken rather than missing. **the weak fallback** (4 uses) — two forward references point at a name that does not exist, because the section describing the mechanism never labels it. **effective parameter** (1 use) — the link resolves to a section that never uses the phrase; it says "the value in force".

## Group 3 — metaphors standing in for definitions

The `Literal` rule in [`CLAUDE.md`](CLAUDE.md#writing-style) bans these outright: metaphor reads as precision and is not. All cheap.

- **a leak** (3 uses) — say the thing: a term the subagent can resolve only because it was handed this file.
- **the blindness** (2 uses) — an agent that has read the document resolves every term and returns nothing.
- **register** (5 uses) — "written in one vocabulary — records, writers, seams". It also carries a fourth, unrelated sense where `CLAUDE.md` bans corporate register, which is the ordinary word and stays.
- **voice** (1 use) — "in wording that sounds right and is wrong".
- **an evidence** (3 uses) — a Go type name used as an English count noun, which reads as a grammatical error. Use "an evidence set", or name the type.
- **crude path** (2 uses) against **crude interface** (17 uses) — one name held constant, introduced once in `roadmap.md` as one binary on a terminal, `cmd/factory`.

## Group 4 — definitions that exist but are never linked

[`CLAUDE.md`](CLAUDE.md#writing-style) already requires a file outside `end-goal/` to link a term that directory defines. Roughly 35 links, mechanical.

- `CLAUDE.md`'s five discipline tables break the rule about twenty times: watch window, surfaces, clean, boundary, criterion patterns, merge queue, rollout strategies, K, rollback's target, reconciler, policy, and four terms in one cell — item, gate, hold, rollback.
- `factory/README.md`'s Packages table, which is where a reader of the code arrives first: hold, held-out sample, restore floor, in force, pin, catalog, duty 8 and duty 9, chained log. The file's opening sentence claims to be the map the code's rules require, and the map currently assumes the legend.
- The repository's own three processes, used bare in files that do not define them: **the review pass**, **the consistency pass**, **the cold-read check**.
- Bare **the design** and **the document** (~110 uses) are not renamed. One sentence per file saying `end-goal/` is the design document covers all of them, and the plain phrase is longer, which the rule ordering is willing to spend.

## Incidental — leftovers from the removal of "the tree"

Twelve sites survive the 2026-08-20 removal, which missed six files outright — root `README.md`, `factory/DEMO.md`, all of `tools/graph/PLAN-B.md`, and both `factory/cmd/tracecheck/refs.go` and `doc.go` — and left four in `end-goal/CLAUDE.md`. One of those four is stale as well as private: a grep comment reads "tree index" for a table whose own row now reads "this document".

Keep the roughly 65 git-working-tree and ordinary-sense uses. `FilesInTree`, `emptyTree`, `ls-tree`, two trees conflicting, a chain that is a list and not a tree, and depscheck's package tree are all the ordinary word.

One wording inconsistency, not a private sense: `factory/criterion/doc.go` and `writer.go` say "merged into the tree it was made from" where `end-goal/how-humans-do-it/03-gates.md` now says "merged into the repository". Align on the repository, for one spelling of one idea.

## Two decisions for the owner

**`substrate`.** `end-goal/CLAUDE.md` exempts it as an ordinary word that is not the settled term, and so exempts it from the link-at-first-use rule. The evidence contradicts that: across ~43 uses it names one half of a division the document turns on — everything the owner provides that falls outside the twelve duties — and a reader cannot recover that from any of them. Acting on it means editing a rule, so it waits.

**`form`.** Both senses are load-bearing and both are in the glossary's reach. Which one keeps the word should be decided from the sentences, not from the site counts.

## Order, and what each step costs

1. Group 3 and the `tree` leftovers. Cheapest, and adds no vocabulary.
2. Group 4 links. Mechanical, and enforces a rule already written.
3. Group 1 collisions, one commit per word, each carrying its reason and its cost.
4. Group 2 definitions, ending with `end-goal/glossary.md`'s preamble made true.

The expense is not the edits. It is the consistency pass in [`end-goal/CLAUDE.md`](end-goal/CLAUDE.md#verification), which runs a cold-read subagent per changed file and an eleven-file read-through after every edit; Groups 1 and 2 touch most of `end-goal/`, so it runs many times. That is unavoidable rather than incidental.

Group 1 is where a pass can go wrong. The earlier removal of "the tree" mis-substituted a git-sense use and turned "merged into the tree it was made from" into "merged into this document" before it was caught. So every rename is read in its own sentence, never applied across the repository at once.

## Verification

Run the consistency pass in `end-goal/CLAUDE.md` after every edit under that directory: the grep suite, the link and anchor checks, the coverage check, a cold-read subagent per changed file, then the eleven-file read-through.

The coverage check gets stricter for free as Group 2 lands, because its term list is the glossary's own multi-word lines. Expect it to surface sites this file missed; that is the check working.

The `SKIP` set in `end-goal/CLAUDE.md` should shrink to empty once **the number** becomes the release number. If it cannot, the rename is incomplete.

After any change under `factory/`, run `go build ./...`, `go test ./...`, `go run ./cmd/depscheck` and `go run ./cmd/tracecheck` from that directory.

When the `tree` leftovers are done, `grep -rn 'the tree' --include='*.md' --include='*.go' .` returns only the paragraph in `CLAUDE.md` that records the removal.
