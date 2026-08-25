# CLAUDE.md

Governs `end-goal/` and nothing else. Every path below is relative to this directory,
except the verification commands, which say so.

## What this is

`end-goal/` is Markdown files plus one that is not: [`terms.txt`](terms.txt), the
inventory of every name the document introduces, read only by the consistency pass.
Together they are one design document for a fully autonomous software factory — a
product each customer self-hosts, which refines intent, builds, deploys, monitors, and
fixes software on its own.

The repository around it is the monorepo that will build that thing; this directory is
the state it is built toward, so code is added beside it and never in it. There is no
build or test suite here: the consistency pass below is all that checks this directory.
Every task here is an edit to a design document, and the document says everything in it
is open to revision.

`README.md` indexes the document; `how-humans-do-it/README.md` is the dependency-order
table and only that.

There is no work list. A decision belongs in the file that owns its subject, and a cut
candidate is taken or refused with the reason written into that same file. Keeping answers
outside the sections that own them is the shape `open.md`'s own rule refuses.

One file per section. Each file's own heading is `#`, its subsections `##` and `###` — a
section that is its own file owns the top level. A new part of the document is a new file
in this directory; a new section of _How humans do it_ is a new numbered file plus a row in
that directory's table.

Keep each file readable on its own, and keep cross-section references by name — as a link
where the name points at another file, with the name as the link text.

## The document is a graph, and edits break it in predictable ways

The value of the document is that its claims interlock, and most damage comes from editing
one section and leaving another asserting the opposite. The links most easily broken:

**The numbered duty list.** `what-humans-do.md` numbers twelve owner duties, and the rest
of the document cites them as bare numbers — `(7)`, `(10)`, `(11, 12)`. Inserting,
removing, or reordering a duty silently repoints every reference in every other file, and
the range check below cannot see it: a reorder changes what a number means and not which
numbers exist, so it passes by construction, and the cold-read check is told to leave bare
duty numbers alone. An edit that inserts, removes, or reorders duties is finished only
when every citation of every moved number has been read and repointed — the duty-refs grep
below lists them, over a hundred across nine files — run only on an edit able to break the
references, not on every edit, because the duty list rarely moves.

**The gate table against the prose.** Every gate named in prose needs a row, and every
action in a row must be possible at that point in the lifecycle. `Deploy to production`
deliberately has no Reject: by then the merge has happened and the number is already
assigned, so hold is the only stop, and once it deploys all that is left is a human's undo
of a shipped change (10) — a rollback while the build it would return to is still running, a revert after.

**The lifecycle vocabulary.** It must run unbroken end to end. A **candidate** is
identified by item plus build and runs on an environment of its own; at merge to master it
becomes a **release** with an ordinal number, per service. Contract versions are a separate
axis — semver, one per published interface, because compatibility is the contract's job and
not the release's. Do not let a fifth name for any of these appear. Upstream of all of it
is the **intent** — what intake writes, what decomposition turns into items, and what
everything links back to; an uncut intent is not an item. **Current release** is not a fifth
name either — it is which release a service is running on every production target, a
fact of the production deploy record's completion per target, and every cross-service check reads
it rather than the newest number. `beta` was a
fifth name and was dropped: it named the build occupying the shared UAT slot, and there is
no such slot.

**Section order encodes dependency.** One pipeline (the unit of work) → Intent into items
(how a request becomes items, and decomposition) → Gates (where a decision happens) → Risk
score (what decides whether a human decides at one) → Environments (the branch, the
per-candidate environment, the merge queue) → Releases (what ships) → Contracts (what binds
services to each other) → Operations (the control, the analysis window, the window limit, the
page) → Gate policy (everything an owner authors, gathered from the sections that define
each parameter) → The fleet (what an agent runs on, and what a borrowed account costs) →
Screens (where a human sees it). A concept should be defined before the section that leans
on it. The numeric filename prefixes under `how-humans-do-it/` are that order and nothing
else — reordering means renaming files and fixing the links that point at them.

Seven forward references are known and left in place, each defined below a section that
depends on it because moving the definition up would put something more depended-on out of
order: the **analysis window** and the **window limit**; the **gate** and the **score** that
Intent into items leans on, with **current release** the same shape at smaller scale; the
**page**; the **drift detector**; **the fleet**, and with it the **role prompt** and
the **skill** an agent works from, which One pipeline, Gates, Risk score and Gate policy
all lean on and The fleet defines; the **last known-good release**, which Contracts leans
on and Operations defines; and the four screens — **Work**, **Ops**, **Factory**,
**People** — which _What humans do_ leans on and _Screens_ defines last. One treatment
covers the first six — a link forward at each early use, so a reader meeting the term there
can reach the definition — and a new early use must keep that true. The screens take that
treatment at the first use of each name in a file rather than at every use: the four recur
as ordinary nouns in nearly every file, and a link on each would put one in most paragraphs.

**Introduce a term at its first use in each file.** A reader meeting a term for the first
time in a file has to be able to finish the sentence they are on. Two ways give them that:
a clause in the prose saying what the term names, or a link to the section that defines it.
Either satisfies the rule, and the clause is the better one wherever it fits in a few
words, because a link is a page the reader has to leave. A glossary line is neither, and
having one is not what admits a term: a term earns its place by naming something the plain
words cannot say once, which the root `CLAUDE.md` sets, and [`glossary.md`](glossary.md) is
a list of industry words this document uses in a narrower or different sense rather than an
index of its vocabulary. What stays bare, deliberately: the bare duty numbers, which
`README.md` already makes a convention; **the factory** and the **owner**, the document's
subject and its reader; a file's own defined terms; external proper nouns — EARS, REST,
gRPC, protobuf, Kafka, OpenAPI; and an ordinary word that merely matches a term the
document defines — a builder of the product, a queue's rotation. What it costs is more than
the rule it replaced: a link was one edit wherever it was owed, and a clause is written
afresh in each file, so a term used in six files is introduced six times in six different
sentences.

**Never cross-reference by position.** "The second open question" breaks the moment a
bullet is resolved and removed. Refer to things by name. A link's path may contain a
number; its text never does.

## Resolved questions get folded, not deleted

When a question in `open.md` is answered, move the decision into the file that owns the
subject **with its reason and its cost**, then delete the question. Deleting it alone
leaves the reasoning in git history where nobody will look. This follows the owner's
standing rule on abandoning a unit of work and the precedent in commit `98b5430`.

The inverse matters as much: material that is genuinely unsettled belongs in `open.md`,
phrased as the question and what turns on it. Do not resolve an open question by asserting
an answer in the body — the split between what is decided and what is not is information
the document deliberately records.

Two kinds of question do not earn a place there. A question the document can already answer
by applying a pattern it holds is not open — apply the pattern and fold it. Three such
patterns: a safeguard (9) adds a human at a gate; an owner authors a parameter with gate policy
(8) and the score supplies the default; the score learns from outcomes. Nor is a loose end
a session noticed while doing something else: the subject must raise it, and an owner must
decide it.

## Verification

There are no tests. After editing, run the consistency pass. It checks this document
against rules this file and the root `CLAUDE.md` set, and finds nothing they do not name —
the review pass in the root `CLAUDE.md` is what looks for the rest, dispatched cold and on
request. Every command below is scoped to `end-goal/` and run from the repository root, so
no sibling directory enters a check written for this one:

```bash
grep -rhE "^\| *:?-{3,}" --include='*.md' end-goal/ --exclude=CLAUDE.md | wc -l  # expect 10: end-goal index, what comes from outside, sections, rollout strategies, gate actions, criterion patterns, build names, window exits, gate policy, what a role prompt and a skill reach
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
comm -23 <(grep -rho "]([^)]*#[^)]*)" --include='*.md' end-goal/ --exclude=CLAUDE.md | sed 's/.*#//; s/)$//' | sort -u) \
         <(grep -rh "^#\{1,3\} " --include='*.md' end-goal/ --exclude=CLAUDE.md | sed 's/^#* //; s/[^A-Za-z0-9 -]//g; s/ /-/g' | tr 'A-Z' 'a-z' | sort -u)
# every bolded name is on the inventory — expect no output
python3 - <<'EOF'
import re, glob, os
# Bolding is how this document introduces a name. A bolded run that is not a paragraph
# lead-in is a name, and a name not in terms.txt is one this edit invented. Each line
# there is the name, a tab, and the field the word comes from; only the name is matched,
# and a line with no field fails so the attribution cannot be skipped.
BOLD = re.compile(r'\*\*(.+?)\*\*', re.S)
LINK = re.compile(r'\[([^\]]*)\]\([^)]*\)')
known = set()
for l in open('end-goal/terms.txt', encoding='utf-8'):
    if not l.strip() or l.startswith('#'): continue
    name, tab, field = l.rstrip('\n').partition('\t')
    if not field.strip(): print('no field in terms.txt:', name)
    known.add(name.strip().lower())
for p in sorted(glob.glob('end-goal/**/*.md', recursive=True)):
    if os.path.basename(p) == 'CLAUDE.md': continue
    for raw in BOLD.findall(open(p, encoding='utf-8').read()):
        t = LINK.sub(r'\1', raw).strip()
        if '\n' in t or t.endswith(('.', '?', ':', '!')): continue
        if t.lower() not in known: print('name not in terms.txt:', p, '->', t)
EOF
# every glossary line names a term the document uses — expect no output
python3 - <<'EOF'
import re, glob, os
# The glossary is its own roster: which industry words this document bends is decided
# in the commit that adds or removes a line there, never in a list this file holds.
# What stays mechanical is that a line cannot outlive its term — a glossary line for
# a term nothing uses is a dead entry, the pile growing back as leftovers.
body = ''
for p in glob.glob('end-goal/**/*.md', recursive=True):
    if os.path.basename(p) in ('CLAUDE.md', 'glossary.md'): continue
    body += open(p, encoding='utf-8').read().lower()
for line in open('end-goal/glossary.md', encoding='utf-8'):
    m = re.match(r'- \*\*(.+?)\*\*', line)
    if m and m.group(1).lower() not in body:
        print('glossary line for a term the document does not use:', m.group(1))
EOF
```

The link check resolves each path against the directory of the file it appears in, which a
one-liner resolving against `end-goal/` and `how-humans-do-it/` alike did not: a link
written with the wrong prefix resolved under the other directory and passed. It cannot see
anchors — it strips fragments and skips same-file links — which is why the anchor check is
separate. That one matches an anchor against every heading in this document rather than
against the target file's own, so it catches a renamed heading and not a link pointed at
the wrong file.

The inventory check is the only command here that is a gate rather than a reading. Every
other one finds a link or an anchor pointing at nothing; this one finds a name that did not
exist before this edit, which is the defect the vocabulary cleanup of 2026-08-20 to
2026-08-23 spent millions of tokens undoing. It runs one way on purpose: a name added
fails, a name removed does not, because that cleanup removed names constantly and a check
that fought it would have blocked the work it exists to protect. The pile is gone and this
is what stops it growing back.

It costs two things. It reads bolding rather than meaning, so a bolded run that is emphasis
and not a name has to be seeded in `terms.txt` like one — `git`, `always true` and `the
change` are there for that reason — and a name introduced without bold is invisible to it,
which makes bolding at introduction a convention this check depends on. A legitimate
new name costs a line plus a commit body saying which of the root `CLAUDE.md`'s two grounds
it survives on, which is the point rather than the price: the cost is paid where it is one
line, instead of later where it was forty terms. The check cannot tell a good name from a
bad one, and is not meant to — the cold-read check's coined list is what does that.

The glossary is its own roster: which industry words this document bends is decided in the
commit that adds or removes a line in `glossary.md`, the way a name is decided in the
commit that adds its `terms.txt` line — not in a list this file holds, which was a list of
answers kept outside the file that owns them, the shape this file itself refuses, and whose
only remedy for a wrong line was editing a Python literal inside a bash fence nothing said
anyone may edit. The check keeps the one direction that stays mechanical: every glossary
line names a term the document uses, so a line cannot outlive its term. The
direction given up is mechanical detection of a bent term with no line, which was only ever
as good as the list a session remembered to extend; what surfaces a reader left with the
industry meaning is the cold-read check's borrowed list, term by term, on the file that
uses it. What it costs is that a line added for a term the document uses but does not bend
fails nothing — that decision is the commit's, and a bad line is repaired by review rather
than by a grep. Whether a term is introduced where it is used is the cold-read check's,
which finds a name pointing at nothing and is the better instrument for it.

### The cold-read check

The greps above find a link pointing at nothing. This finds two things no grep can. One is
a name pointing at nothing, which is the same defect one level down and the one that made
the document unreadable. The other is a name that should not exist at all — a coinage in
the commit that introduces it, which every other check in this file passes over, because a
coined term properly introduced satisfies all of them.

For each file this edit changed, dispatch a subagent with no other context and this
instruction, verbatim, with `<fields>` filled in from the rows
[_What the work spans_](../CLAUDE.md#what-the-work-spans) gives for that file's subjects:

> Read only this file: `<path>`. Do not open any other file and do not follow any link. Judge
> the file on its own, ignoring anything you were told about this repository elsewhere.
>
> The fields this file speaks from are: `<fields>`. That is the only thing you are told
> about it, and it is told to you so that you can tell a field's term of art from a name
> this document invented. It does not tell you the file is right about anything.
>
> Return four lists and nothing else.
>
> **Unlinked** — every term the file uses as though already defined, that it does not
> define, where the sentence using it offers no link to follow. Quote the sentence where
> each first appears.
>
> **Linked** — the same, but where the sentence does offer a link. Quote the sentence where
> each first appears.
>
> **Coined** — every term that reads as this document's private vocabulary: a name for a
> concept where an ordinary word or phrase would have said the same thing, and that you
> cannot place in any established field. Include a term even where the file introduces it
> properly. For each, give the plain phrase you expected instead.
>
> **Borrowed** — every term you recognise as a field's term of art, used here in that
> field's sense. For each, name the field and say where in it the term is established — a
> standard, a practice, a tool, a body of literature — so that the claim can be checked
> rather than taken. Put a term here rather than under Coined whenever a field uses it
> this way, even if a commoner sense exists elsewhere, and say so where it does. A field
> you cannot name that way is not one, and the term belongs under Coined.

Only the unlinked list is a failure, and a non-empty one means the edit is not finished.
Fix each by introducing the term in the prose where it is first used, or by linking it to
the section that defines it. Adding it to [`glossary.md`](glossary.md) is not a third fix:
a line there is for a word the industry owns and this document bends, and it never was what
made a term readable. The linked list is the rule already satisfied — a term linked to its
definition is introduced, which is the treatment six of the seven known forward references
get — and it is worth reading once for a link that points somewhere too far from the
sentence to help.

The borrowed list is neither a failure nor a candidate list. It is the attribution
[`terms.txt`](terms.txt) records, arriving from the one reader with no stake in the name,
and a term on it is answered by writing the field beside that name rather than by renaming
anything. Where it and `terms.txt` disagree, the disagreement is the finding: a name the
file calls `coined` that a reader places in a field is a rename this cleanup should not
make, and the reverse is a term the document inherited without knowing it. The list is
split out of the coined list because a cold reader with no field context cannot tell a term
of art from a coinage inside one file, and neither could the session triaging its list;
without that split the check converted field vocabulary into plain English. Requiring the
reader to say where the field establishes the term is what keeps the list from acquitting
everything: without it, a naming of any field at all moved a term off the coined list, and
a coinage escapes removal on a claim nobody can check. What it costs is that the reader now
claims a field for a word, and a claim is sometimes wrong: `terms.txt` takes the
attribution only where the session agrees with it, and a citation the session cannot place
leaves the term coined.

The coined list is neither, and it is not meant to be empty: **analysis window** will be on
it every run. It is a candidate list, read against the root `CLAUDE.md`'s two grounds —
the industry owns the word, or the plain word was never a figure — and against the one
test that admits a coinage at all, that the term names something the plain words cannot
say once. A name meeting none of the three is removed in the commit that introduced it.
That is the only cheap moment there is: the vocabulary cleanup of 2026-08-20 to 2026-08-23
is what the same removal cost once the names had been in the document a while, and it ran
to millions of tokens over about forty terms, a document-wide pass each, and every one of
them named again in the code. A cold reader is the right one to ask because it has no
investment in the name — the session that just wrote a term can always say what it means,
which is the same reason the check withholds the rest of the document. What it costs is
that the settled vocabulary comes back every run: a reader with
no memory cannot know which names were already argued for, so triaging the list is a
session's job and never the subagent's.

The first two lists are long on a file that summarises the whole document, and that is not
a fault in the file. Each term on them is either introduced in a clause or linked to the
section that defines it.

The subagent has to be given nothing but the path and the fields. An agent that has read
the rest of the document resolves every term and returns nothing unlinked, and one that has
read the rules argues every coinage back — which is exactly the failure the check exists to
defeat, twice over. The fields are not an exception to that and are the one thing the check
cannot work without: they say what the file is about and never that it is right, so nothing
in them resolves a term or defends a name. Withholding them was the defect, not the
discipline — a reader that does not know the file is speaking as sequential analysis
reports `boundary` and `crossing` as private vocabulary, correctly by what it was given and
wrongly about the document. The instruction files are what the check cannot withhold: a
subagent receives the repository's `CLAUDE.md` whether or not it is asked to, so a term
defined there rather than here is resolvable to the check and not to a reader. Tell the
subagent to judge the file on its own, and treat a term whose only definition is in an
instruction file as unresolved however the check reports it.

The check costs a subagent per changed file on every edit, and its result is a judgment
rather than a grep exit code.

Then read One pipeline → Intent into items → Gates → Risk score → Environments → Releases →
Contracts → Operations → Gate policy → The fleet → Screens straight through and confirm one
identity survives end to end: item plus build as a candidate, the same build in production,
an ordinal attached at merge, contracts versioned alongside it.

## Commits

`docs: <imperative summary>` for an edit in here. The body says what the change resolved
and names what stays open — see `98b5430` for the shape. Include the `Co-Authored-By`
trailer. The repository's own commit rules are at the root, and this adds to them rather
than restating them.
