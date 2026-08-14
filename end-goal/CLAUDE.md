# CLAUDE.md

This file governs `end-goal/` and nothing else in the repository. Every path below is
relative to this directory; the verification commands are the exception and say so.

## What this is

A tree of Markdown files. It is one design document for a fully autonomous software
factory — a product each customer self-hosts, which refines intent, builds, deploys,
monitors, and fixes software on its own.

The repository around it is the monorepo that will build that thing. This directory is
the state it is built toward, so code lands beside it and never in it. There is no
build, test suite, or linter for this directory. Do not go looking for one. Every task
here is an edit to a design document, and the document says of itself that everything in
it is open to revision.

`README.md` indexes the tree; `how-humans-do-it/README.md` is the dependency-order table
and only that.

There is no work list. `next.md` held one until 2026-08-14, when its two lists were spent: the
eight decided-but-unwritten entries were folded into the files that own their subjects, which
is where a decision belongs, and the fifteen cut candidates were each taken or refused with
the reason written into the same files. A list of answers kept outside the sections that own
them is the shape `open.md`'s own rule refuses, and it was that shape.

One file per section, split on 2026-08-13. Each file's own heading is `#`, its
subsections `##` and `###` — a section standing on its own owns the top level. A new
part of the document is a new file in this directory; a new section of _How humans do
it_ is a new numbered file plus a row in that directory's table.

Keep each file able to stand on its own, and keep cross-section references by name — as a
link where the name points at another file, with the name as the link text.

## The document is a graph, and edits break it in predictable ways

The value of the doc is that its claims interlock. Most damage comes from editing one
section and leaving another asserting the opposite. The load-bearing links:

**The numbered duty list.** `what-humans-do.md` numbers twelve owner duties, and the rest
of the tree cites them as bare numbers — `(7)`, `(10)`, `(11, 12)`. Inserting, removing,
or reordering a duty silently repoints every reference in every other file.

**The gate table against the prose.** Every gate named in prose needs a row, and the
actions in a row must be honorable at that point in the lifecycle. `Deploy to production`
deliberately has no Reject: by then the merge has happened and the number is spent, so
hold is the only stop, and undoing it once it deploys is veto after the fact — a rollback
while its control stands, a revert after.

**The lifecycle vocabulary.** It must run unbroken end to end. A **candidate**
is identified by item plus build and stands on an environment of its own; at merge to
master it becomes a **release** with an ordinal number, per service. Contract versions
are a separate axis — semver, one per published interface, because compatibility is the
contract's job and not the release's. Do not let a fifth name for any of these appear.
Upstream of all of it is the **intent** — what intake writes, what the cut turns into
items, and what everything walks back to. An uncut intent is not an item: it was called an
unrefined item in two places until 2026-08-13, and both now say intent. **Current
release** is not a fifth name either — it is which release a service is running, a fact of
the production deploy record, and every cross-service check reads it rather than the
newest number.
`beta` was one and was dropped on 2026-08-13 — it named the build holding the shared UAT
slot, and there is no slot to hold.

**Section order encodes dependency.** One pipeline (the unit of work) → Intent into items
(how a request becomes items, and the cut) → Gates (where a
decision happens) → Risk score (what decides whether a human stands at one) → Environments
(the branch, the per-candidate environment, the merge queue) → Releases (what travels) →
Contracts (what binds services to each other) → Operations (the control, the watch window,
K, the page) → Gate policy (everything an owner authors, gathered from the sections
that define each parameter) → The fleet (what stands behind an agent, and what a borrowed
account costs) → Surfaces (where a human sees it). A concept should be defined before
the section that leans on it. The numeric filename prefixes under `how-humans-do-it/` are
that order and nothing else — reordering means renaming files and fixing the links that
point at them.

Six forward references are known and left standing, each defined below a section that
leans on it because moving the definition up would put something more load-bearing out of
order: the **watch window** and **K**; the **gate** and the **score** that Intent into items
leans on, with **current release** the same shape at smaller scale; the **page**; the
**reconciler**; **the fleet**; and the four surfaces — **Work**, **Ops**, **Factory**,
**People** — which _What humans do_ leans on and _Surfaces_ defines last. One treatment
covers the first five — a link forward at each early use, so a reader meeting the term
there can reach the definition — and a new early use is expected to keep that true. The
surfaces take that treatment at the first use of each name in a file rather than at every
use: the four recur as ordinary nouns in nearly every file, and a link on each would put
one in most paragraphs.

**Never cross-reference by position.** "The second open question" broke the moment a
bullet was resolved and removed. Refer to things by name. A link's path may carry a
number; its text never does.

## Writing style

**Precise, then concise, then simple.** The order is the rule — it only does work when
the three conflict. This governs the document and anything written about it.

**Precise beats concise.** If cutting a qualification blurs the claim, keep it: "a
breaking diff without the migration already in front of it" is not "a bad diff." Name the
scope — *per service*, *at merge to master*. One name per concept, held constant across
sections.

**Precise beats simple.** A true statement that needs a caveat gets the caveat: "the last
human touchpoint — by default, and by score, not because the gates downstream of it are
missing."

**Concise beats simple.** The short true sentence over the longer gentle one. No preamble,
no restating the heading, no summarizing what is about to be said. State reasons rather
than announcing them — `This is the reason:` was cut for exactly that.

Simple is last, not absent: plain words and short sentences wherever they cost nothing.
Because it yields, an idiom that lands a precise point in few words stays.

Two habits follow. Rules arrive with their cost attached — no-batching is stated together
with the human-UAT ceiling it creates. Em-dash asides carry qualifications instead of
spawning sentences.

**Structure for a reader.** The document is read by humans, not only parsed. A long run of
uniform paragraphs gets `###` subheadings; a set of parallel facts gets a table. When a
table carries a definition, the prose around it must not restate the table — trim the
prose to what the table cannot hold.

No hard wrap. One paragraph is one line; the renderer does the rest. (Owner rule,
2026-08-13, replacing the 88-column wrap.)

## Resolved questions get folded, not deleted

When a question in `open.md` is answered, move the decision into the file that owns the
subject **with its reason and its cost**, then delete the question. Deleting it alone
strands the reasoning in git history where nobody will look. This follows the owner's
standing rule on abandoning a unit of work and the precedent in commit `98b5430`.

The inverse matters as much: material that is genuinely unsettled belongs in `open.md`,
phrased as the question and what turns on it. Do not resolve an open question by
asserting an answer in the body — the split between what is decided and what is not is
information the document is deliberately carrying.

Two kinds of question do not earn a place there. One the document can already answer by
applying a pattern it holds is not open — apply the pattern and fold it. A pin (9) buys a
human at a gate, an owner authors a parameter with gate policy (8) and the score supplies
the default, and the score learns from outcomes: those three were reached for late three
times rather than at the time. Nor is a loose end a session noticed on its way past —
the subject has to raise it, and an owner has to be who decides it. Six questions
accumulated by that second route before 2026-08-13, each spun off in a trailing `Open:`
line by a commit doing something else, and one asserted a premise the body of that same
commit contradicted. (Owner rule, 2026-08-13.)

## Verification

There are no tests. After editing, run the consistency pass. Every command is scoped to
`end-goal/` and run from the repository root, so nothing a sibling directory holds is
swept into a check written for this one:

```bash
grep -rh "^|---" --include='*.md' end-goal/ --exclude=CLAUDE.md | wc -l          # expect 8: tree index, sections, rollout strategies, gate actions, criterion patterns, build names, window exits, gate policy
grep -rho "([0-9, ]*)" --include='*.md' end-goal/ --exclude=CLAUDE.md | sort -u  # duty refs — every one must be 1–12
grep -rn "open question\|see Open" --include='*.md' end-goal/ --exclude=CLAUDE.md   # positional cross-refs — expect none
grep -rn "^####" --include='*.md' end-goal/ --exclude=CLAUDE.md                  # nothing deeper than "### " — expect none
grep -rc "^# " --include='*.md' end-goal/ --exclude=CLAUDE.md | grep -v ':1$'    # one "# " per file — expect none
# every link resolves — expect no output
grep -rho "]([^)]*)" --include='*.md' end-goal/ --exclude=CLAUDE.md | sed 's/](//; s/[)#].*//' | sort -u \
  | while read -r p; do [ -z "$p" ] || [ -e "end-goal/$p" ] || [ -e "end-goal/how-humans-do-it/$p" ] || echo "dangling: $p"; done
# every anchor matches a heading — expect no output
comm -23 <(grep -rho "]([^)]*#[a-z0-9-]*)" --include='*.md' end-goal/ --exclude=CLAUDE.md | sed 's/.*#//; s/)$//' | sort -u) \
         <(grep -rh "^#\{1,3\} " --include='*.md' end-goal/ --exclude=CLAUDE.md | sed 's/^#* //; s/[^A-Za-z0-9 -]//g; s/ /-/g' | tr 'A-Z' 'a-z' | sort -u)
```

The link check cannot see anchors — it strips fragments and skips same-file links — which
is why the anchor check is separate. That one matches an anchor against every heading in
the tree rather than against the target file's own, so it catches a renamed heading and not
a link pointed at the wrong file.

Then read One pipeline → Intent into items → Gates → Risk score → Environments → Releases →
Contracts → Operations → Gate policy → The fleet → Surfaces straight through and confirm one identity survives end to end: item
plus build as a candidate, the same build in production, an ordinal attached at merge,
contracts versioned alongside it.

## Commits

`docs: <imperative summary>` for an edit in here. The body says what the change resolved
and names what stays open — see `98b5430` for the shape. Include the `Co-Authored-By`
trailer. The repository's own commit rules are at the root, and this adds to them rather
than restating them.
