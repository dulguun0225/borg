# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A tree of Markdown files is the entire repository. It is one design document for a fully
autonomous software factory — a product each customer self-hosts, which refines intent,
builds, deploys, monitors, and fixes software on its own.

There is no code, build, test suite, or linter. Do not go looking for one. Every task
here is an edit to a design document, and the document says of itself that everything in
it is open to revision.

```
README.md                       # title, the draft caveat, index of the tree
what-the-factory-does.md
what-humans-do.md               # the twelve numbered duties
how-humans-do-it/
  README.md                     # the dependency-order table, and only that
  01-one-pipeline.md … 08-surfaces.md
deferred.md
open.md
```

One file per section, split on 2026-08-13. Each file's own heading is `#`, its
subsections `##` and `###` — a section standing on its own owns the top level. A new
`##`-level part of the document is a new file at root; a new `###` under _How humans do
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
`beta` was one and was dropped on 2026-08-13 — it named the build holding the shared UAT
slot, and there is no slot to hold.

**Section order encodes dependency.** One pipeline (the unit of work) → Gates (where a
decision happens) → Risk score (what decides whether a human stands at one) → Environments
(the branch, the per-candidate environment, the merge queue) → Releases (what travels) →
Contracts (what binds services to each other) → Operations (the control, the watch window,
K, the veto window) → Surfaces (where a human sees it). A concept should be defined before
the section that leans on it. The numeric filename prefixes under `how-humans-do-it/` are
that order and nothing else — reordering means renaming files and fixing the links that
point at them.

One exception, known and left standing: the **watch window** and **K** are defined in
Operations and leaned on by Gates, Risk score, Environments, and Releases. Operations has
to follow Releases and Contracts — what happens after a deploy needs what travels and what
binds it — so the forward reference is cheaper than the reordering that would remove it.
Every file that leans on either term before Operations carries a link into Operations near
its first use, so a reader meeting the term early can reach the definition. A new early use
is expected to keep that true.

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
uniform paragraphs gets `####` subheadings; a set of parallel facts gets a table. When a
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

## Verification

There are no tests. After editing, run the consistency pass:

```bash
grep -rn "^| " --include='*.md' . | grep -v CLAUDE.md          # seven tables: tree index, sections, gate actions, criterion patterns, build names, modes, window exits
grep -rno "([0-9, ]*)" --include='*.md' . | grep -v CLAUDE.md  # duty refs — every one must be 1–12
grep -rn "open question\|see Open" --include='*.md' . | grep -v CLAUDE.md   # positional cross-refs — expect none
grep -rn "^#" --include='*.md' . | grep -v CLAUDE.md           # one "# " per file, and nothing deeper than "### "
# every link resolves — expect no output
grep -rho "](\([^)#]*\)[^)]*)" --include='*.md' . --exclude=CLAUDE.md | sed 's/](//; s/[)#].*//' | sort -u \
  | while read -r p; do [ -z "$p" ] || [ -e "$p" ] || [ -e "how-humans-do-it/$p" ] || echo "dangling: $p"; done
# every anchor matches a heading — expect no output
comm -23 <(grep -rho "](\([^)]*\)#[a-z0-9-]*)" --include='*.md' . --exclude=CLAUDE.md | sed 's/.*#//; s/)$//' | sort -u) \
         <(grep -rh "^#\{1,3\} " --include='*.md' . --exclude=CLAUDE.md | sed 's/^#* //; s/[^A-Za-z0-9 -]//g; s/ /-/g' | tr 'A-Z' 'a-z' | sort -u)
```

The link check cannot see anchors — it strips fragments and skips same-file links — which
is why the anchor check is separate. That one matches an anchor against every heading in
the tree rather than against the target file's own, so it catches a renamed heading and not
a link pointed at the wrong file.

Then read One pipeline → Gates → Risk score → Environments → Releases → Contracts →
Operations → Surfaces straight through and confirm one identity survives end to end: item
plus build as a candidate, the same build in production, an ordinal attached at merge,
contracts versioned alongside it.

## Commits

Commit straight to `main`. The project is early; branches start when it is ready for
them, and not before. Do not create one unasked. (Owner rule, 2026-08-13.)

`docs: <imperative summary>`. The body says what the change resolved and names what stays
open — see `98b5430` for the shape. Include the `Co-Authored-By` trailer.
