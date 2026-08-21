# Jargon cleanup

Vocabulary invented in this repository, and the plain words that replace it. The work is roughly 4–6M tokens and does not fit one session, which is why it is written down rather than done.

## What this file is, and when it goes

It records unfinished work and not a decision. Nothing in it binds a later session; where it disagrees with `end-goal/`, `end-goal/` is right, and an entry here that turns out to be wrong is struck rather than argued with.

It is deleted when the last step lands. A step finished is struck from this file in the same commit that finishes it, so the file shrinks to nothing and is removed — that termination condition is the whole difference between this file and `next.md`, which `end-goal/` emptied on 2026-08-14 for being a list of answers kept outside the sections that own them.

It is an exception to the rule in [`CLAUDE.md`](CLAUDE.md#what-this-repository-is) that beyond `end-goal/` and [`roadmap.md`](roadmap.md) nothing in the repository records what work is under way. The owner made the exception on 2026-08-20 because the alternative was losing the inventory. What it costs is a fourth place a later session can read as authority, and the two paragraphs above are what hold that down.

## The test

**Well-known technical jargon from the wider software industry stays. Jargon invented here goes, replaced with plain words. The glossary ends up small.**

That is a test of provenance. It replaced an earlier test — whether a fresh reader could resolve a term — on 2026-08-20, and the replacement is not a refinement. Under the old test a coined term stayed as long as it had a glossary line and a link, so `the cut` and `the interview` were ruled out of scope for exactly that reason. Under this one they go. Conversely a word the industry already owns needs no glossary line at all, because a reader arriving from CI/CD, SRE, git or requirements engineering already has it.

All 109 glossary terms were triaged against the sections that define them. Roughly 50 define industry or ordinary words, about 40 are coined here, and 19 are industry words bent to a different meaning.

Use counts throughout are order-of-magnitude. The triage counted markdown link targets as prose and inflated several figures — `pin` is about 42 uses rather than 84, `thread` about 8 rather than 23. Count again before quoting a number in a commit.

## Three owner decisions, 2026-08-20

**Bent industry words are renamed where they actively mislead** — where a reader's existing knowledge produces a wrong belief about behaviour — and kept where the bend is only a narrowing that the prose already states. A bent word is more dangerous than a coined one: a coined term leaves a reader knowingly lost, and a bent one leaves them confidently wrong.

**The glossary shrinks to the bent terms alone**, about fifteen lines, and its purpose inverts. It stops being an index of this document's vocabulary and becomes a list of words this document uses in a narrower or different sense than the industry does. Its preamble changes with it: it currently promises a line for every term used as though already defined, which is the opposite of a small glossary.

**`surfaces` becomes `screens`**, including renaming `end-goal/how-humans-do-it/11-surfaces.md` and every anchor and link pointing into it.

## Evidence, checked against the files

**The document argues against the meaning its own word implies.** `09-gate-policy.md` reads: "A pin is a bound and not a precedence … Read as a precedence, a pinned ceiling over K of five would override an authored two and raise the number, which is a pin adding throughput and removing safety." A term that needs a sentence defending it against its industry meaning is the clearest case on the list.

**`artifact` is the worst term in the document.** The industry means the built output — a jar, an image, the thing Artifactory holds. Here it means an authored document: a spec, a plan, a task list, an implementation. The built output has its own separate word in this same document, `build`, so both senses are live in one text.

**`reconciler` names a component forbidden to reconcile.** Every Kubernetes and GitOps reader arrives expecting a loop that converges on desired state. This one is read-only, and the one thing it can do is stop.

**Two coined exit names are already glossed in plain words by the document itself.** `08-operations.md` says "A swept release is one the factory skipped over", and elsewhere "a release condemned at harm". The replacements are the document's own words.

## Step 1 — change the rules first

The glossary is large because a rule requires it to be. `end-goal/CLAUDE.md` says a term worth linking is a term with a glossary line, so a new term enters the document by getting its line there. The link-at-first-use rule then demands a link for every distinctive term, and the coverage check in that file's verification section enforces the whole arrangement by reading the glossary's own multi-word lines as its term list.

So deleting fifty lines without changing the rule puts them straight back, and every edit in between fails the consistency pass. Nothing else in this file can start until this step is committed with that pass green.

- `end-goal/CLAUDE.md`: replace link-at-first-use with introduce-at-first-use. A term is explained in prose where it is used. A glossary line stops being the prerequisite for linking, and stops being what makes a term legitimate.
- `end-goal/CLAUDE.md`: rewrite the coverage check. It cannot go on reading the glossary as its term list once the glossary is a list of bent words. Its replacement checks the bent terms only.
- `end-goal/CLAUDE.md`: delete the `SKIP` set. It exists only to compensate for `the number` colliding with the score's number, the trust number and the cost number, and that name is going.
- `end-goal/CLAUDE.md`: drop the exemption that treats `substrate` as an ordinary word. Across about 43 uses it names one half of a division the document turns on — everything the owner provides that falls outside the twelve duties — and a reader cannot recover that from any of them.
- Root [`CLAUDE.md`](CLAUDE.md#writing-style): the settled-term exemption protects `hold`, `standing`, `control`, `straight`, `clean`, `swept`, `floor` and `ceiling` by name. Four of those are going, so the rule is rewritten rather than quoted back. Its replacement: a term is either industry vocabulary or a plain phrase, and there is no third protected category.
- [`end-goal/glossary.md`](end-goal/glossary.md): invert the preamble.

## Step 2 — replace the coined terms

| Coined | Becomes | Note |
|---|---|---|
| **the cut** (70 uses) | decomposition | The document already names this stage's own gate `Decomposition`. The noun swaps cleanly; the verb forms cost the most, since "re-cut" becomes "re-decompose". |
| **the number** (~14 uses in this sense) | the release number | Four characters per use, and it lets the `SKIP` set go. |
| **K** (26 uses) | window limit | A bare capital letter used as a noun in running prose is the least readable thing in the document. It needs a short word, not a spelled-out phrase. |
| **clean** (10 uses) | cleared | Carries the real content better. The exit does not mean nothing went wrong; it means a regression of the specified size was ruled out at the specified confidence. |
| **swept** (8 uses) | skipped | The document's own gloss. |
| **harm** (14 uses) | condemned | The document's own verb, and it simultaneously fixes `condemned` being used 19 times while defined nowhere. |
| **crossing** (9 uses) | breach | Industry-standard, as in an SLO breach, and it drops a collision with "cross-service". |
| **cap**, the exit name | timed out | `cap` stays for the time limit itself. Only the exit name is coined. |
| **restore floor** (8 uses) | last known-good release | "Last known good" is industry. Also drops a collision with the floor-and-ceiling metaphor used for pin directions. |
| **thread** (23 uses) | timeline | The document's own explanation is "on a single timeline". To every software reader `thread` means a unit of concurrent execution, which makes this the worst word choice in a document about building software. |
| **surfaces** (10 uses in prose) | screens | Already the more common word in the text. The industry senses — attack surface, API surface — mean exposure, so a security or API reader misreads it. |
| **straight** (13 uses) | in place | Nearly free: the table row already reads "all of it, in place, with none of the build it replaces left running". |
| **return**, the noun (~13 uses) | a send-back | The plain form already outnumbers the coinage, which also collides with its own verb — "the release it returns to" — and with the programming keyword. |
| **form**, the constraint's field (~5 uses in this sense) | kind | Matches the discriminator field that already exists on environments and contracts. `form` currently carries three other senses in the document as well. |
| **standing**, the constraint reach (~7 uses in this sense) | permanent, or name the three reaches | A label over three of four enum values. |
| **attempt bound** (18 uses) | attempt limit | Only "bound" is non-standard. "Retry limit" is the industry phrase but reads oddly against interview rounds, which the limit also counts. |
| **authorship prior** (10 uses) | the per-author prior | `prior` is genuine Bayesian statistics and earns its place. "Authorship" is the local half. |
| **veto after the fact** (15 uses) | undo a change after it shipped | Its own definition already names the two mechanisms: rollback, then revert. |
| **last comparison** (6 uses) | last check | |
| **merge gate** (13 uses) | the Merge to master gate | A redundant second name for a row the gate table already names. |
| **state flow** (2 uses) | state machine | The defining section already says state machine, and the coinage collides with Kotlin's `StateFlow`. |
| **trust number** (4 uses) | nothing — un-name it | Say the two rates. "nothing ships that the trust number cannot see" becomes "nothing ships unrecorded". |
| **instance set** (7 uses) | the instances running that build | Probably needs no name at all. |
| **predicate catalog** (4 uses) | the list of allowed check kinds | It needs some name, being a gate-policy row, and this is barely longer. |
| **factory policy** (6 uses) | the factory-wide settings record | |
| **partial intent** (4 uses) | the section's own better sentence: "a feature half delivered, not production half broken" | It is computed and never written, so no field or record depends on the name. |
| **per-intent** (4 uses) | binds only the request it arrived with | |
| **originating project** (2 uses) | the project the request came in under | |
| **auto-passed by** (1 use) | why it auto-passed | A field label masquerading as vocabulary. |
| **detector** (6 uses) | the factory's own monitors | |
| **health signal** (7 uses) | fold into the health monitor | It overlaps almost entirely with `comparison`. Keep one of the two, not both. |

## Step 3 — rename the bent terms that mislead

| Bent | Becomes | The wrong belief it creates now |
|---|---|---|
| **artifact** (58 uses) and **artifact store** (9 uses) | work product, and the one writer for work products | That it is a build output. `build` already means that in this document. |
| **pin** (~42 uses) | guardrail | That it freezes a value. It is a one-way bound, and the document spends a sentence saying so. |
| **the reconciler** (~15 uses) | the independent checker | That it converges and repairs drift. It is read-only and forbidden to. |
| **declaration** (~23 uses) | derived expectation | That a human wrote it. It is derived from the consumer's build, "not entered by hand", stated twice in its own section. |
| **comparison** (~55 uses) | the health monitor | That it is an act rather than a component. The text says "the comparison writes an incident". |
| **brief** (~28 uses) | role instructions | That it is a summary. It is a system prompt. |
| **deploy agent** (15 uses) | the deployer | That it is an LLM agent, because `agent` means exactly that everywhere else in this document. |

Kept, because the bend is a narrowing the prose already states: **item**, **service**, **intent**, **release**, **rollback**, **candidate**, **hold**, **decision**, **page**, **incident**, **the fast-forward**, **Edit in place**, **the pipeline**, **seam**, **the graph**. These are the roughly fifteen that keep a glossary line, and their lines are the most load-bearing in the file — `rollback` most of all, because an SRE reader assumes a capability the design deliberately withholds. Here a rollback shifts traffic onto a still-running control, is possible only inside an open watch window, and undoes every release above its target; once the control is gone there is no rollback at all, only a revert.

## Step 4 — shrink the glossary

Delete the line for every industry or ordinary word: gate, build, environment, targets, master, revert, escalation, deploy record, merge queue, task, score version, contract version, risk score, rollout strategy, acceptance criterion, stage, agent, report, area, intake, dispatch, question, role, constraint, scope, actor, the interview, UAT, policy version, design system, contract, baseline, control, boundary, gate policy, fleet, effort, project, cap, mismatch, rollback's target, notifier, page event, provider account, fleet proposal, duty, dropped, source, gate component, report store, kind, current release, held out.

Where a line carried the one fact a reader could not guess, that fact moves into its section rather than disappearing: `area`'s nesting chain, `report`'s "a report is not an intent" — every other tracker makes the bug report be the work item, so that sentence is the whole value of the line — `the interview`'s "a state, not a stage, and it has no gate", and `role`'s warning that permissions live in scope rather than in role.

`kind`'s line is worse than nothing and goes without replacement: it welds two unrelated fields together while the document also uses the word ordinarily.

The `duty` line goes, but the bare-number notation does need one line of explanation, in `what-humans-do.md` where the duties are.

## Step 5 — jargon outside the glossary

Found under the earlier test and still valid under this one. One group from that pass largely evaporates: adding links to definitions was most of it, and once terms are introduced in prose rather than linked to glossary lines, the fix is an introducing clause instead.

**Collisions.** `reader` in root `CLAUDE.md` means both a review-pass subagent and an ordinary person reading, about fifteen lines apart across some 44 uses — rename the subagent sense to **review agent**, not "auditor", which would collide with the reconciler's replacement. `row` conflates a line in a table with a live gate awaiting a human's verdict: use **firing** for the live sense, which the document already uses about 24 times for exactly that and which is itself undefined, so one definition fixes both. `seam`'s second, writer-boundary sense becomes plain words — "the boundary between the two writers is the field" — while the numbered seams 1 to 4 stay. Then `Factory` in the `doc.go` comments, where it is a screen, the product, and a Go identifier in one paragraph; `roster` in root `CLAUDE.md`, naming two different sets ninety lines apart; `store`; `reach`; and `blind case`, which [`07-contracts.md`](end-goal/how-humans-do-it/07-contracts.md#what-a-diff-cannot-see) uses for two scopes in one paragraph — the pair inside derivation, a read it misses and a read it invents, and then "the blind case with a harder cause", a consumer outside the factory with no build to read at all. The term itself stays: it is introduced where it is used and it names something the plain words cannot say once. The five code sites are accurate and cite the derivation pair correctly, so nothing under `factory/` changes — what needs the second name is the outer case.

**Used as settled, defined nowhere.** `writer` is the document's most-invoked argument and exists nowhere as a rule — `09-gate-policy.md` says "a second writer with no seam" and hands the reader neither half. State the one-writer-per-record rule once, where record structure is owned, and refer to it instead of re-deriving it per section. Then `in force`, `install`, `encoding`, `superseded`, `firing`, `organic traffic`, `the steady state`, `band`, `lattice`, `the log`, `unrefined` and `refined` and `re-cutting`, and about a dozen small ones: `span`, `factor vector`, `grouper`, `disposition`, `marked`, `observables`, `undecided`, `checkpoint`, `escape`. Two are broken rather than missing: `the weak fallback`, where two forward references point at a name no section supplies, and `effective parameter`, whose link resolves to a section that says "the value in force" instead.

**Two Go comments quote a design sentence that has since changed.** `factory/criterion/doc.go` and `writer.go` both say a build is "the ones merged into the tree it was made from", where [`03-gates.md`](end-goal/how-humans-do-it/03-gates.md) now says "merged into the repository it was made from". This is the git working tree and not the private synonym, so what is left is an alignment and not a removal — the private sense went on 2026-08-20, fifteen sites across six files the first pass had missed.

## What is deliberately kept

`watch window` is coined and stays. The concept must have a name — it is a record with fields, a four-row exit table, and two gate-policy rows — and "watch window" is a plain phrase rather than a private synonym for something the plain words already say. The same reasoning keeps `window limit` as the replacement for K.

The statistics borrowings stay because they are the most accurate vocabulary in the document. `boundary` is a sequential-analysis stopping boundary, and "valid at every point it is read" is literally the always-valid property. `control` is a control group, and automated canary analysis does start a fresh old-version fleet for exactly the reason given here. `baseline` and `held-out sample` are used as statistics and machine learning use them. Replacing these would trade precise borrowed terms for vaguer prose, which is the opposite of the point.

`fleet` stays, and the reason is worth recording because the first pass guessed wrong: it looked like a coinage and it is industry vocabulary — EC2 Fleet, fleet management, fleet-wide rollout — and the metaphor transfers from machines to agents without help.

## Order, and what each step costs

1. **Step 1, the rules.** Small edits, but everything depends on them, and they must be committed with the consistency pass green.
2. **Step 4**, the glossary. A large diff at low risk, and it shrinks the surface every later step has to check.
3. **Step 2**, the coined replacements, cheapest first: `auto-passed by`, `state flow`, `trust number`, `originating project`, `per-intent`, `partial intent`, then the watch-window exit names as one commit, then `the number`, `the cut`, `thread`, and `surfaces`.
4. **Step 3**, the seven bent renames, one commit each carrying its reason and its cost.
5. Step 5's collisions and undefined terms last, since the earlier steps will have removed some of them.

The expense is not the edits. It is the consistency pass in [`end-goal/CLAUDE.md`](end-goal/CLAUDE.md#verification), which runs a cold-read subagent per changed file and an eleven-file read-through after every edit, and Steps 2 through 4 touch most of `end-goal/`.

Every rename is read in its own sentence rather than applied across the repository at once. The removal of "the tree" mis-substituted a git-sense use and turned "merged into the tree it was made from" into "merged into this document" before it was caught. That is the failure mode to design against, and it is why Step 3's seven renames are seven commits.

## Verification

Run the consistency pass in `end-goal/CLAUDE.md` after every edit under that directory: the grep suite, the link and anchor checks, the coverage check, a cold-read subagent per changed file, then the eleven-file read-through.

After Step 1 the coverage check is a different check, and it has to pass before any term changes. Running the old one against a shrunk glossary reports the whole document as broken.

The `surfaces` to `screens` rename is verified by the dangling-link and anchor greps, which catch a half-done job. That rename is tedious rather than risky.

The `SKIP` set should be gone once `the number` becomes the release number. If it cannot go, the rename is incomplete.

`cmd/tracecheck` mechanically requires every `doc.go` to name an `end-goal/` section, so renaming a section heading or a file breaks the Go build until the trailers follow. That is a real coupling between Step 2 and the code, and it is a feature: the build refuses a half-finished rename. After any change under `factory/`, run `go build ./...`, `go test ./...`, `go run ./cmd/depscheck` and `go run ./cmd/tracecheck` from that directory.
