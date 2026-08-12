# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`end-goal-draft.md` is the entire repository. It is a design document for a fully
autonomous software factory — a product each customer self-hosts, which refines intent,
builds, deploys, monitors, and fixes software on its own.

There is no code, build, test suite, or linter. Do not go looking for one. Every task
here is an edit to a design document, and the document says of itself that everything in
it is open to revision.

## The document is a graph, and edits break it in predictable ways

The value of the doc is that its claims interlock. Most damage comes from editing one
section and leaving another asserting the opposite. The load-bearing links:

**The numbered duty list.** `## What humans do` numbers twelve owner duties, and the rest
of the document cites them as bare numbers — `(7)`, `(10)`, `(11, 12)`. Inserting,
removing, or reordering a duty silently repoints every reference in the file.

**The gate table against the prose.** Every gate named in prose needs a row, and the
actions in a row must be honorable at that point in the lifecycle. `Deploy to production`
deliberately has no Reject: by then the merge has happened and the number is spent, so
hold is the only stop and undoing is a revert.

**The lifecycle vocabulary.** It must run unbroken across four sections. A **candidate**
is identified by item plus build and wears the label `beta` on the UAT branch; at merge
to master it becomes a **release** with an ordinal number, per service. Contract versions
are a separate axis — semver, one per published interface, because compatibility is the
contract's job and not the release's. Do not let a fifth name for any of these appear.

**Section order encodes dependency.** Environments (branches, the UAT slot) → Releases
(what travels) → Contracts (what binds services to each other) → Surfaces (where a human
sees it). A concept should be defined before the section that leans on it.

**Never cross-reference by position.** "The second open question" broke the moment a
bullet was resolved and removed. Refer to things by name.

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

Prose wraps at about 88 columns and never exceeds 92.

## Resolved questions get folded, not deleted

When a bullet under `## Open` is answered, move the decision into the body **with its
reason and its cost**, then delete the bullet. Deleting it alone strands the reasoning in
git history where nobody will look. This follows the owner's standing rule on abandoning
a unit of work and the precedent in commit `98b5430`.

The inverse matters as much: material that is genuinely unsettled belongs in `## Open`,
phrased as the question and what turns on it. Do not resolve an open question by
asserting an answer in the body — the split between what is decided and what is not is
information the document is deliberately carrying.

## Verification

There are no tests. After editing, run the consistency pass:

```bash
grep -n "^| " end-goal-draft.md                 # gate table rows
grep -no "([0-9, ]*)" end-goal-draft.md         # duty refs — every one must be 1–12
grep -n "open question\|see Open" end-goal-draft.md   # positional cross-refs — expect none
grep -n "^#" end-goal-draft.md                  # section order
```

Then read Environments → Releases → Contracts → Surfaces straight through and confirm one
identity survives end to end: item plus build as a candidate, the same build in
production, an ordinal attached at merge, contracts versioned alongside it.

## Commits

`docs: <imperative summary>`. The body says what the change resolved and names what stays
open — see `98b5430` for the shape. Include the `Co-Authored-By` trailer.
