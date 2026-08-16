# CLAUDE.md

This file governs `end-goal/` and nothing else in the repository. Every path below is
relative to this directory; the verification commands are the exception and say so.

## What this is

A tree of Markdown files. It is one design document for a fully autonomous software
factory — a product each customer self-hosts, which refines intent, builds, deploys,
monitors, and fixes software on its own.

The repository around it is the monorepo that will build that thing. This directory is
the state it is built toward, so code is added beside it and never in it. There is no
build, test suite, or linter for this directory. Do not go looking for one. Every task
here is an edit to a design document, and the document says of itself that everything in
it is open to revision.

`README.md` indexes the tree; `how-humans-do-it/README.md` is the dependency-order table
and only that.

There is no work list. `next.md` held one until 2026-08-14, when its two lists were emptied: the
eight decided-but-unwritten entries were folded into the files that own their subjects, which
is where a decision belongs, and the fifteen cut candidates were each taken or refused with
the reason written into the same files. A list of answers kept outside the sections that own
them is the shape `open.md`'s own rule refuses, and it was that shape.

One file per section, split on 2026-08-13. Each file's own heading is `#`, its
subsections `##` and `###` — a section that is its own file owns the top level. A new
part of the document is a new file in this directory; a new section of _How humans do
it_ is a new numbered file plus a row in that directory's table.

Keep each file readable on its own, and keep cross-section references by name — as a
link where the name points at another file, with the name as the link text.

## The document is a graph, and edits break it in predictable ways

The value of the doc is that its claims interlock. Most damage comes from editing one
section and leaving another asserting the opposite. The links most easily broken:

**The numbered duty list.** `what-humans-do.md` numbers twelve owner duties, and the rest
of the tree cites them as bare numbers — `(7)`, `(10)`, `(11, 12)`. Inserting, removing,
or reordering a duty silently repoints every reference in every other file.

**The gate table against the prose.** Every gate named in prose needs a row, and every
action in a row must be possible at that point in the lifecycle. `Deploy to production`
deliberately has no Reject: by then the merge has happened and the number is already
assigned, so hold is the only stop, and undoing it once it deploys is veto after the fact
— a rollback while its control is still running, a revert after.

**The lifecycle vocabulary.** It must run unbroken end to end. A **candidate**
is identified by item plus build and runs on an environment of its own; at merge to
master it becomes a **release** with an ordinal number, per service. Contract versions
are a separate axis — semver, one per published interface, because compatibility is the
contract's job and not the release's. Do not let a fifth name for any of these appear.
Upstream of all of it is the **intent** — what intake writes, what the cut turns into
items, and what everything links back to. An uncut intent is not an item: it was called an
unrefined item in two places until 2026-08-13, and both now say intent. **Current
release** is not a fifth name either — it is which release a service is running, a fact of
the production deploy record, and every cross-service check reads it rather than the
newest number.
`beta` was one and was dropped on 2026-08-13 — it named the build occupying the shared UAT
slot, and there is no such slot.

**Section order encodes dependency.** One pipeline (the unit of work) → Intent into items
(how a request becomes items, and the cut) → Gates (where a
decision happens) → Risk score (what decides whether a human decides at one) → Environments
(the branch, the per-candidate environment, the merge queue) → Releases (what ships) →
Contracts (what binds services to each other) → Operations (the control, the watch window,
K, the page) → Gate policy (everything an owner authors, gathered from the sections
that define each parameter) → The fleet (what an agent runs on, and what a borrowed
account costs) → Surfaces (where a human sees it). A concept should be defined before
the section that leans on it. The numeric filename prefixes under `how-humans-do-it/` are
that order and nothing else — reordering means renaming files and fixing the links that
point at them.

Seven forward references are known and left in place, each defined below a section that
depends on it because moving the definition up would put something more depended-on out of
order: the **watch window** and **K**; the **gate** and the **score** that Intent into items
leans on, with **current release** the same shape at smaller scale; the **page**; the
**reconciler**; **the fleet**; the **restore floor**, which Contracts leans on and
Operations defines; and the four surfaces — **Work**, **Ops**, **Factory**,
**People** — which _What humans do_ leans on and _Surfaces_ defines last. One treatment
covers the first six — a link forward at each early use, so a reader meeting the term
there can reach the definition — and a new early use is expected to keep that true. The
surfaces take that treatment at the first use of each name in a file rather than at every
use: the four recur as ordinary nouns in nearly every file, and a link on each would put
one in most paragraphs.

**Never cross-reference by position.** "The second open question" broke the moment a
bullet was resolved and removed. Refer to things by name. A link's path may contain a
number; its text never does.

## Writing style

Moved to the repository root `CLAUDE.md` on 2026-08-14, unchanged. It always governed
every file here and not this directory alone, and a rule over the whole repository belongs
where the repository's rules are. The no-hard-wrap rule for this tree went with it.

## Resolved questions get folded, not deleted

When a question in `open.md` is answered, move the decision into the file that owns the
subject **with its reason and its cost**, then delete the question. Deleting it alone
leaves the reasoning in git history where nobody will look. This follows the owner's
standing rule on abandoning a unit of work and the precedent in commit `98b5430`.

The inverse matters as much: material that is genuinely unsettled belongs in `open.md`,
phrased as the question and what turns on it. Do not resolve an open question by
asserting an answer in the body — the split between what is decided and what is not is
information the document deliberately records.

Two kinds of question do not earn a place there. One the document can already answer by
applying a pattern it holds is not open — apply the pattern and fold it. A pin (9) adds a
human at a gate, an owner authors a parameter with gate policy (8) and the score supplies
the default, and the score learns from outcomes: those three were reached for late three
times rather than at the time. Nor is a loose end a session noticed while doing something
else — the subject has to raise it, and an owner has to be who decides it. Six questions
accumulated by that second route before 2026-08-13, each raised in a trailing `Open:`
line by a commit doing something else, and one asserted a premise the body of that same
commit contradicted. (Owner rule, 2026-08-13.)

## Verification

There are no tests. After editing, run the consistency pass. It checks this document
against rules this file and the root `CLAUDE.md` set, and finds nothing they do not name —
the review pass in the root `CLAUDE.md` is what looks for the rest, dispatched cold and on
request. Every command below is scoped to `end-goal/` and run from the repository root, so
nothing in a sibling directory is included in a check written for this one:

```bash
grep -rh "^|---" --include='*.md' end-goal/ --exclude=CLAUDE.md | wc -l          # expect 8: tree index, sections, rollout strategies, gate actions, criterion patterns, build names, window exits, gate policy
grep -rho "([0-9, ]*)" --include='*.md' end-goal/ --exclude=CLAUDE.md | sort -u  # duty refs — every one must be 1–12
grep -rn "open question\|see Open" --include='*.md' end-goal/ --exclude=CLAUDE.md   # positional cross-refs — expect none
grep -rn "^####" --include='*.md' end-goal/ --exclude=CLAUDE.md                  # nothing deeper than "### " — expect none
grep -rc "^# " --include='*.md' end-goal/ --exclude=CLAUDE.md | grep -v ':1$'    # one "# " per file — expect none
# every link resolves against the directory of the file it appears in — expect no output
python3 -c "
import os, re, glob
for p in glob.glob('end-goal/**/*.md', recursive=True):
    if os.path.basename(p) == 'CLAUDE.md': continue
    for t in re.findall(r'\]\(([^)]+)\)', open(p).read()):
        f = t.split('#')[0]
        if not f or f.startswith('http'): continue
        if not os.path.exists(os.path.join(os.path.dirname(p), f)): print('dangling:', p, '->', t)
"
# every anchor matches a heading — expect no output
comm -23 <(grep -rho "]([^)]*#[a-z0-9-]*)" --include='*.md' end-goal/ --exclude=CLAUDE.md | sed 's/.*#//; s/)$//' | sort -u) \
         <(grep -rh "^#\{1,3\} " --include='*.md' end-goal/ --exclude=CLAUDE.md | sed 's/^#* //; s/[^A-Za-z0-9 -]//g; s/ /-/g' | tr 'A-Z' 'a-z' | sort -u)
```

The link check resolves each path against the directory of the file it appears in, which a
one-liner resolving against `end-goal/` and `how-humans-do-it/` alike did not: a link
written with the wrong prefix resolved under the other directory and passed. It cannot see
anchors — it strips fragments and skips same-file links — which is why the anchor check
is separate. That one matches an anchor against every heading in the tree rather than
against the target file's own, so it catches a renamed heading and not a link pointed at
the wrong file.

### The cold-read check

The greps above find a link pointing at nothing. This finds a name pointing at nothing,
which is the same defect one level down and the one that made the document unreadable.

For each file this edit changed, dispatch a subagent with no other context and this
instruction, verbatim:

> Read only this file: `<path>`. Do not open any other file and do not follow any link. Judge
> the file on its own, ignoring anything you were told about this repository elsewhere.
>
> List every term it uses as though already defined and does not define. Put each in one of
> two groups: **unlinked**, where the sentence using the term offers no link to follow, and
> **linked**, where it does. Quote the sentence where each first appears. Return the two
> lists and nothing else.

Only the unlinked list is a failure, and a non-empty one means the edit is not finished. Fix
each by introducing the term where it is first used, linking it to the section that defines
it, or adding it to [`glossary.md`](glossary.md) and linking there. The linked list is the
rule already satisfied — a term linked to its definition is introduced, which is the
treatment the six known forward references get — and it is worth reading once for a link
that points somewhere too far from the sentence to help.

Both lists are long on a file that summarises the whole document, and that is not a fault in
the file: one link to the glossary at its first unlinked term is often the whole fix.

The subagent has to be given nothing but the path. An agent that has read the rest of the
document resolves every term and returns an empty list, which is exactly the blindness the
check exists to defeat — the document has always been readable to whoever just wrote it.
The instruction files are the leak in this: a subagent receives the repository's `CLAUDE.md`
whether or not it is asked to, so a term defined there rather than here is resolvable to the
check and not to a reader. Tell the subagent to judge the file on its own, and treat a term
whose only definition is in an instruction file as unresolved however the check reports it.

(Owner rule, 2026-08-15. What it costs is a subagent per changed file on every edit, and a
check whose result is a judgment rather than a grep exit code.)

Then read One pipeline → Intent into items → Gates → Risk score → Environments → Releases →
Contracts → Operations → Gate policy → The fleet → Surfaces straight through and confirm one
identity survives end to end: item plus build as a candidate, the same build in production,
an ordinal attached at merge, contracts versioned alongside it.

## Commits

`docs: <imperative summary>` for an edit in here. The body says what the change resolved
and names what stays open — see `98b5430` for the shape. Include the `Co-Authored-By`
trailer. The repository's own commit rules are at the root, and this adds to them rather
than restating them.
