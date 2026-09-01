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

`README.md` indexes the document; `how-the-factory-works/README.md` is the dependency-order
table and only that; each section directory's `README.md` is the same kind of thing one
level down — the section's own prose, which is the lead-in and anything belonging to no
subsection, and the table of its subsections.

There is no work list. A decision belongs in the file that owns its subject, and a cut
candidate is taken or refused with the reason written into that same file. Keeping answers
outside the sections that own them is the shape `open.md`'s own rule refuses.

One file per subsection. A section is a numbered directory: its `README.md` holds the
section's own prose — the lead-in, and anything belonging to no subsection — and the table
of its subsections; each subsection is a numbered file in it, and a subsection with
subsections of its own is a directory of the same shape.
A section with no subsections stays a numbered file. Every file's own heading is `#` and no
heading is deeper — a subsection's heading is its file's own. Two files keep `##` headings
inline instead: [`open.md`](open.md), whose questions are its headings and leave when
answered, and [`deferred.md`](deferred.md); they are the only files an anchor may point
into. A new part of the document is
a new file in this directory; a new section of _How the factory works_ is a new numbered
directory plus a row in that directory's table; a new subsection is a new numbered file
plus a row in its section's table, and it renames every file after it in its section and
every link into them.

Keep each section readable on its own, and keep cross-file references by name — as a link
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
below lists them, over a hundred across the document — run only on an edit able to break
the references, not on every edit, because the duty list rarely moves.

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
on it. The numeric prefixes on the section directories under `how-the-factory-works/` are that
order and nothing else; the prefixes on the files inside a section are that section's
reading order, and nothing else either. Reordering one means renaming what moved and
repointing every link into it.

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
treatment at the first use of each name in a section rather than at every use: the four
recur as ordinary nouns in nearly every section, and a link on each would put one in most
paragraphs.

**Introduce a term at its first use in each section.** A section is read as one thing, so
the introduction lives in the section's `README.md` or in the first file of the section
that uses the term. A reader meeting a term for the first time in a section has to be able
to finish the sentence they are on. Two ways give them that:
a clause in the prose saying what the term names, or a link to the section that defines it.
Either satisfies the rule, and the clause is the better one wherever it fits in a few
words, because a link is a page the reader has to leave. A glossary line is neither, and
having one is not what admits a term: a term earns its place by naming something the plain
words cannot say once, which the root `CLAUDE.md` sets, and [`glossary.md`](glossary.md) is
a list of industry words this document uses in a narrower or different sense rather than an
index of its vocabulary. What stays bare, deliberately: the bare duty numbers, which
`README.md` already makes a convention; **the factory** and the **owner**, the document's
subject and its reader; a section's own defined terms; external proper nouns — EARS, REST,
gRPC, protobuf, Kafka, OpenAPI; and an ordinary word that merely matches a term the
document defines — a builder of the product, a queue's rotation. What it costs is more than
the rule it replaced: a link was one edit wherever it was owed, and a clause is written
afresh in each section, so a term used in six sections is introduced six times in six
different sentences.

**The record inventory.** [`records.md`](records.md) lists every record in the graph, its
writer, and the seam where two writers reach one, and it is the only place that list exists.
`what-the-factory-does/01-tight-integration.md` states the one-writer rule and the sections
below invoke it one record at a time, so before the list a second writer given to an
existing record broke a sentence in a file nobody had opened and passed every check here. A
section that introduces a record adds its row in the same commit, and a section that
declares a seam names both writers in that row. The file holds no reasons: each row points
at the section that owns the subject, which is where the writer is argued and where an edit
goes. What it costs is a second place to edit whenever a writer moves, and the count check
below is what fails when the table itself is dropped.

**The component inventory.** [`components.md`](components.md) is the same list from the
other side: a row per component, what it is, and which components it may call. It exists
because the one-writer rule is stated over components and the record inventory names them
without listing them, so a component that writes nothing appeared nowhere at all, and the
call edges the document states one at a time as costs were never collected into something a
reader could check for a loop. Two rules make it checkable, and an edit adding a component
or a call keeps both: a component does not exist until it has a row, and a call edge does
not exist until the row of the caller names it. What it costs is a second table to keep true
beside the record one.

**Never cross-reference by position.** "The second open question" breaks the moment a
bullet is resolved and removed. Refer to things by name. A link's path may contain a
number; its text never does.

## Sentence, paragraph, and file length

The rules above protect what the document says. This one protects whether a reader can
tell what it says. The value of the document is that its claims interlock, and a claim
arriving as the eleventh clause of a hundred-word sentence cannot be checked against
another claim — not by the owner, not by a review agent, not by whoever builds `factory/`
from it. Four bounds, checked over the prose alone:

- **A sentence holds at most 60 words.** The median sentence here is 34, so the bound
  sits well above the register the document already writes in. What it catches is the one
  sentence in eight that carries a whole subject as a single grammatical unit.
- **A sentence holds at most one em dash.** One sets off one aside and its scope is
  plain; a second makes the reader guess whether it closes the first or opens another.
  The em dash is what turns three sentences into one here, so the word bound alone does
  not reach it.
- **A paragraph holds at most 300 words**, a top-level list item counting as a paragraph
  of its own, since that is how most of the document's enumerations are carried.
- **A file holds at most 1,500 words.** Every file's own heading is `#` and no heading is
  deeper, so a long file has no internal structure a sentence can point at — which is
  what produces a file citing its own subsection by name as though it were elsewhere.
  The remedy is the one _What this is_ already gives: the subsection becomes a directory
  and its parts become numbered files in it. Permitting `##` inside a long file instead
  is refused — it makes every heading in the document an anchor target, and the anchor
  check's whole shape is that only `open.md` and `deferred.md` are.

The check below gates a paragraph this edit changed and counts the rest. A bound broken
inside a paragraph the working tree touches is a failure; the same bound broken in prose
the edit did not touch is a number printed at the end. That is the shape the inventory
check already runs in, and for the same reason: the document was written before the
bounds, and a check that failed on all of it would block every edit until the whole of it
had been rewritten. The document comes within the bounds a paragraph at a time, as
paragraphs are edited for their own reasons, and the counts printed each run are what
says whether it is getting there.

The file bound is enforced in that one direction too. A file this edit pushes past 1,500
words is a failure and is split; a file already past it is counted and may still grow, so
a finding belonging in the longest file is not answered by a restructuring first.

This file is checked with the rest. The other commands here exclude it, because they read
the document's vocabulary and its links and this file is neither; a rule about whether a
sentence can be read has no such excuse, and one stated in prose nobody can follow would
be its own counterexample.

It costs three things. The check reads the working tree against `HEAD`, which no other
command here does, so a pass run after the commit rather than before it gates nothing:
the counts still print, but the gate holds only if the pass is run before the commit, as
this file says to. A paragraph edited for one word is brought within the bounds whole, which
makes a one-word fix inside a long paragraph a rewrite of that paragraph. And the bounds
count words and dashes rather than clauses, so a sentence can sit under every one of them
and still be unreadable. The cold-read check's unparsed list is the instrument for that,
and it is the only one here that reads a sentence as a sentence.

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
no sibling directory enters a check written for this one — except the link resolver, which
reads the repository's Markdown because `roadmap.md` and the factory's docs link into this
document and a file that moves here would break them silently otherwise:

```bash
# 10 content tables — what comes from outside, rollout strategies, gate actions, criterion patterns, build names, window exits, gate policy, what a role prompt and a skill reach, records and their writers, components and what they call — plus the one index table every README holds
[ $(grep -rhE "^\| *:?-{3,}" --include='*.md' end-goal/ --exclude=CLAUDE.md | wc -l) -eq $((10 + $(find end-goal/ -name README.md | wc -l))) ] && echo "tables: ok"
grep -rho "([0-9, ]*)" --include='*.md' end-goal/ --exclude=CLAUDE.md | sort -u  # duty refs — every one must be 1–12
grep -rn "open question\|see Open" --include='*.md' end-goal/ --exclude=CLAUDE.md   # positional cross-refs — expect none
grep -rn "^##" --include='*.md' end-goal/ --exclude=CLAUDE.md --exclude=open.md --exclude=deferred.md  # nothing below "# " outside the two whole files — expect none
grep -rc "^# " --include='*.md' end-goal/ --exclude=CLAUDE.md | grep -v ':1$'    # one "# " per file — expect none
# every link resolves against the directory of the file it appears in, repository-wide — expect no output
python3 -c "
import os, re, glob
for p in glob.glob('**/*.md', recursive=True):
    if p.startswith('graphify-out/') or os.path.basename(p) == 'CLAUDE.md': continue
    for t in re.findall(r'\]\(([^)]+)\)', open(p).read()):
        f = t.split('#')[0]
        if not f or f.startswith('http'): continue
        if not os.path.exists(os.path.join(os.path.dirname(p), f)): print('dangling:', p, '->', t)
"
# anchors survive only into open.md and deferred.md, which stay whole — expect none
grep -rho "]([^)]*#[^)]*)" --include='*.md' end-goal/ --exclude=CLAUDE.md | grep -v "open\.md#\|deferred\.md#"
# every surviving anchor matches a heading — expect no output
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
# prose form — the bounds of "Sentence, paragraph, and file length" above
python3 - <<'EOF'
import glob, os, re, subprocess
# A bound broken inside a paragraph this edit changed is a failure and prints its line;
# the same bound broken in prose the edit did not touch is counted and is not, because
# the document was written before the bounds and a check failing on all of it would
# block every edit until the whole of it had been rewritten. Prose only: tables,
# headings and fences are dropped, and a top-level list item counts as a paragraph.
# This file is read with the rest, which is the one command here that does not skip it.
SENT_MAX, DASH_MAX, PARA_MAX, FILE_MAX = 60, 1, 300, 1500
SENT = re.compile(r'(?<=[.?!])\s+(?=[A-Z“"`*\[(])')
ITEM = re.compile(r'([-*+] |\d+\. )')

def blocks(text):
    out, cur, start, last, fence = [], [], 0, 0, False
    for i, s in enumerate(text.split('\n'), 1):
        if s.startswith('```'): fence = not fence; continue
        if fence or s.startswith('#') or s.lstrip().startswith('|'): continue
        s = re.sub(r'^\s*> ?', '', s)  # a quoted block is prose, and its bare > is a break
        if not s.strip() or ITEM.match(s.lstrip()):
            if cur: out.append((start, last, ' '.join(cur))); cur = []
        if s.strip():
            if not cur: start = i
            cur.append(s.strip()); last = i
    if cur: out.append((start, last, ' '.join(cur)))
    return out

def git(*a):
    r = subprocess.run(('git',) + a, capture_output=True, text=True)
    return r.stdout if r.returncode == 0 else None

touched, path = {}, None
for l in (git('diff', '-U0', 'HEAD', '--', 'end-goal/') or '').split('\n'):
    if l.startswith('+++ b/'): path = l[6:]; touched.setdefault(path, set())
    elif l.startswith('@@') and path:
        m = re.search(r'\+(\d+)(?:,(\d+))?', l)
        if m: touched[path].update(range(int(m.group(1)), int(m.group(1)) + int(m.group(2) or 1)))
for p in (git('ls-files', '-o', '--exclude-standard', 'end-goal/') or '').split():
    touched[p] = None  # a file git has never seen is touched whole

fails, tail, sizes = 0, {'sentence': 0, 'em dash': 0, 'paragraph': 0}, {}
for p in sorted(glob.glob('end-goal/**/*.md', recursive=True)):
    hit = touched.get(p, set()) if p in touched else set()
    sizes[p] = 0
    for start, end, b in blocks(open(p, encoding='utf-8').read()):
        sizes[p] += len(b.split())
        live = p in touched and (hit is None or any(n in hit for n in range(start, end + 1)))
        broken = []
        if len(b.split()) > PARA_MAX: broken.append(('paragraph', len(b.split()), PARA_MAX))
        for s in SENT.split(b):
            if len(s.split()) > SENT_MAX: broken.append(('sentence', len(s.split()), SENT_MAX))
            if s.count('—') > DASH_MAX: broken.append(('em dash', s.count('—'), DASH_MAX))
        for kind, got, cap in broken:
            if live: print('%s: %d over %d — %s:%d' % (kind, got, cap, p, start)); fails += 1
            else: tail[kind] += 1
over = []
for p, w in sorted(sizes.items(), key=lambda kv: -kv[1]):
    if w <= FILE_MAX: continue
    was = git('show', 'HEAD:' + p)
    if was is None or sum(len(b[2].split()) for b in blocks(was)) <= FILE_MAX:
        print('file: %d over %d — split %s into subsections' % (w, FILE_MAX, p)); fails += 1
    else: over.append((w, p))
print('prose form: %d in this edit; elsewhere %d sentences, %d em dashes and %d paragraphs '
      'past their bound; %d files past %d, longest %s'
      % (fails, tail['sentence'], tail['em dash'], tail['paragraph'], len(over), FILE_MAX,
         ', '.join('%s at %d' % (os.path.basename(p), w) for w, p in over[:3])))
EOF
```

The link check resolves each path against the directory of the file it appears in, which a
one-liner resolving against `end-goal/` and `how-the-factory-works/` alike did not: a link
written with the wrong prefix resolved under the other directory and passed. It cannot see
anchors — it strips fragments and skips same-file links — which is why the anchor check is
separate. That one matches an anchor against every heading in this document rather than
against the target file's own, so it catches a renamed heading and not a link pointed at
the wrong file.

Two commands here are gates and the rest are readings. The prose-form check is one, and
the inventory check is the other. A reading finds a link or an anchor pointing at
nothing; the inventory check finds a name that did not
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

The glossary is its own roster. Which industry words this document bends is decided in the
commit that adds or removes a line in `glossary.md`, the way a name is decided in the commit
that adds its `terms.txt` line. It is not decided in a list this file holds: that was a list
of answers kept outside the file that owns them, the shape this file itself refuses, and its
only remedy for a wrong line was editing a Python literal inside a bash fence nothing said
anyone may edit. The check keeps the one direction that stays mechanical: every glossary
line names a term the document uses, so a line cannot outlive its term. The direction given
up is mechanical detection of a bent term with no line, which was only ever as good as the
list a session remembered to extend. What surfaces a reader left with the industry meaning
is the cold-read check's bent list, term by term, on the file that uses it, and a non-empty
one is a failure the commit resolves by rewording or by a line. What it costs is that a line
added for a term the document uses but does not bend fails nothing — that decision is the
commit's, and a bad line is repaired by review rather than by a grep. Whether a term is
introduced where it is used is the cold-read check's business too, and its unlinked list is
the better instrument for that, since it finds a name pointing at nothing.

### The cold-read check

The greps above find a link pointing at nothing. This finds four things no grep can. One
is a name pointing at nothing, which is the same defect one level down and the one that
made the document unreadable. Another is a name that should not exist at all — a coinage
in the commit that introduces it, which every other check in this file passes over,
because a coined term properly introduced satisfies all of them. The third is a sentence
that does not parse. The prose-form check bounds how long a sentence may be and how much
subordination it may carry, which is a count of words and dashes; whether the words in
that budget resolve into a claim is a judgment, and this is where it is made. The fourth
is a field's term of art this document uses in some sense other than that field's, which
every other check here also passes over: it is introduced, it is on the inventory with a
field beside it, and the reader who arrives with the field's meaning is left confidently
wrong.

For each section directory this edit changed, dispatch one subagent; a changed file that
is not in a section directory dispatches on the file alone. A change confined to link
paths, adding and removing no prose, does not fire the check for what it touched: a cold
reader has nothing to judge in a path, and the link and anchor checks above are what verify
one. Dispatch with no other context and this instruction, verbatim, with `<target>` the
directory or the file and `<fields>` filled in from the rows
[_What the work spans_](../CLAUDE.md#what-the-work-spans) gives for its subjects:

> Read only `<target>` — the one file, or every file in the one directory. Do not open
> anything else and do not follow a link that leaves it. Reading a directory, read each
> directory's `README.md` first and then its entries in name order, and treat what you read
> as one document: a term one file introduces is introduced for the files after it. Judge
> what you read on its own, ignoring anything you were told about this repository
> elsewhere.
>
> The fields this material speaks from are: `<fields>`. That is the only thing you are told
> about it, and it is told to you so that you can tell a field's term of art from a name
> this document invented. It does not tell you the material is right about anything.
>
> Return six lists and nothing else.
>
> **Unparsed** — every sentence whose grammatical structure you could not resolve on one
> reading: you could not find its subject, or you could not tell which clause a phrase
> attaches to, or you reached its end without being able to say what it asserts. Quote
> each in full and say which part you could not attach. Judge the sentence and not the
> subject: a sentence you parsed and disagreed with does not go here, and a sentence
> about something you know nothing about still parses.
>
> **Unlinked** — every term the material uses as though already defined, that it does not
> define, where the sentence using it offers no link to follow. Quote the sentence where
> each first appears.
>
> **Linked** — the same, but where the sentence does offer a link. Quote the sentence where
> each first appears.
>
> **Coined** — every term that reads as this document's private vocabulary: a name for a
> concept where an ordinary word or phrase would have said the same thing, and that you
> cannot place in any established field. Include a term even where the material introduces
> it properly. For each, give the plain phrase you expected instead.
>
> **Borrowed** — every term you recognise as a field's term of art, used here in that
> field's sense. For each, name the field and say where in it the term is established — a
> standard, a practice, a tool, a body of literature — so that the claim can be checked
> rather than taken. Put a term here rather than under Coined whenever a field uses it
> this way, even if a commoner sense exists elsewhere, and say so where it does. A field
> you cannot name that way is not one, and the term belongs under Coined.
>
> **Bent** — every term you recognise as a field's term of art that this material uses in a
> sense other than that field's. Name the field, say what the field means by the term, and
> say what this material appears to mean by it. A term goes here rather than under Borrowed
> wherever the two senses differ, and here rather than under Coined wherever you can place
> the word in a field at all.

Three of the six lists are failures. A non-empty unlinked list means the edit is not
finished; fix each by introducing the term in the prose where it is first used, or by
linking it to the section that defines it. Adding it to [`glossary.md`](glossary.md) is not
a third fix: a line there is for a word the industry owns and this document bends, which is
the bent list's business below and not this one, and it never was what made a term readable.
The linked list is the rule already satisfied: a term linked to its
definition is introduced, which is the treatment six of the seven known forward references
get. It is worth reading once, for a link pointing somewhere too far from the sentence to
help.

The unparsed list is the second failure, scoped the way the prose-form check is. A quoted
sentence this edit wrote or changed is rewritten before the edit is finished; one it did
not touch is left, so a section is not rewritten whole before an edit to one paragraph of
it can pass. Rewriting means splitting the sentence or lifting a clause out of it. It
never means deleting the claim the reader could not follow: a claim nobody could parse is
still a claim the document is making, and finding out which one it was is the work. A
reader is entitled to name a sentence sitting under every bound the check counts, and
that is the case the list exists for. What it costs is a judgment where unlinked, linked,
coined and borrowed are closer to inventories. A reader with no memory of the argument a
sentence carries will sometimes name one that a reader holding the argument parses easily — the
same trade the coined list already makes, answered the same way, by the session triaging
the list rather than the subagent.

The bent list is the third failure, and it is the only instrument here that finds a term of
art used in some sense other than its field's. Such a term passes every other check in this
file: it is introduced where it is used, it sits on [`terms.txt`](terms.txt) with a field
beside it, and the reader who arrives with the field's meaning finishes the sentence and is
wrong about what it said. A non-empty bent list is resolved one of two ways and no third.
It is scoped the way the unparsed list is: a term this edit's own prose bent is resolved
before the edit is finished, and one standing in prose the edit did not touch is left, so a
section is not reworded whole before an edit to one paragraph of it can pass. Either the
prose is reworded to the field's sense, which is the better one wherever the
field's word fits what this document means; or a line is added to
[`glossary.md`](glossary.md) saying what the term names here, which is what that file is
for and the only thing that populates it. Leaving the term as it stands is not a
resolution, and neither is a clause introducing it in one file: the reader who needs the
line meets the word wherever else it is used. What it costs is the same judgment the coined
list costs, in the other direction — a reader who half-recognises a field will name a term
bent that the field uses exactly this way, and the session triaging the list is what settles
it against `terms.txt`.

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

The unlinked and linked lists are long on a file that summarises the whole document, and
that is not a fault in the file. Each term on them is either introduced in a clause or
linked to the section that defines it.

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
subagent to judge what it reads on its own, and treat a term whose only definition is in an
instruction file as unresolved however the check reports it.

The check costs a subagent per changed section on every edit, and its result is a judgment
rather than a grep exit code.

Then read One pipeline → Intent into items → Gates → Risk score → Environments → Releases →
Contracts → Operations → Gate policy → The fleet → Screens straight through — each section
directory read README first and then its files in name order — and confirm one identity
survives end to end: item plus build as a candidate, the same build in production, an
ordinal attached at merge, contracts versioned alongside it.

## Commits

`docs: <imperative summary>` for an edit in here. The body says what the change resolved
and names what stays open — see `98b5430` for the shape. Include the `Co-Authored-By`
trailer. The repository's own commit rules are at the root, and this adds to them rather
than restating them.
