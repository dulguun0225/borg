# Design and code drift

How `factory/` is held to `end-goal/` after both have been written, so that the wave M7 recorded does not recur unnoticed.

## The problem

Three kinds of drift, none detected today:

1. **Design moved, code stayed.** An edit to `end-goal/` changes what a built package rests on, and nothing marks the package stale.
2. **Design claim with no code.** A mechanism the design states that no package implements, with nothing saying whether it is unbuilt or forgotten. Thirty-two of 129 `end-goal/` files are cited by no code.
3. **Code says what the design does not.** A package states a mechanism, name, or rule the design never does. The rule that code coins no second name is prose only; the `checker` to `driftdetector` rename was caught by a human.

What exists: `cmd/tracecheck` requires a `doc.go` or screen `README.md` to cite some file and fails on a citation that resolves to nothing. It has no notion of `end-goal/`, skips a bare `.md` name with no slash, and checks no name against `terms.txt`. The consistency pass reads `end-goal/` alone and CI does not run it.

## Decisions

- Enforcement is layered: a build check for what text and structure can tell, a cold Opus judgment for what they cannot.
- The trace grain is the claim, one sentence of the design.
- A claim's identifier lives in a sidecar inventory that quotes the sentence verbatim, not in the prose.
- The inventory is bootstrapped over the whole document at once, not as code cites it.
- The judgment runs in the session that made the edit, in the consistency pass and before a code commit, never headless in CI.

## 1. The claims inventory

`end-goal/claims.txt`, beside `terms.txt` and governed by `end-goal/CLAUDE.md` like it. A comment header, then one line per claim, four tab-separated fields:

```
C0217	how-the-factory-works/03-gates/01-where-a-gate-is-and-what-decides-it.md	built	A hold a human sets is that firing's verdict and counts no attempt.
C0218	how-the-factory-works/02-intent-into-items/01-intake/02-reports.md	unbuilt M9	A report enters through the one channel in from outside the factory.
C0219	what-the-factory-does/05-what-the-customer-runs-it-under.md	stated	One customer per install, and no tenancy.
```

| Field | Rule |
|---|---|
| id | `C` and four digits. Assigned in order of writing, never reused, never renumbered. |
| file | Path relative to `end-goal/`. |
| status | `built`: some `doc.go` or screen `README.md` cites it. `unbuilt Mn`: nothing cites it, and `Mn` is a `## Mn` heading in `roadmap.md`. `stated`: a design statement with no mechanism to build, so no code is expected. |
| sentence | One sentence of the file's Markdown source, verbatim. Matching normalizes whitespace so line wrapping does not matter. |

A claim is one sentence. A mechanism spanning two sentences is two claims. Lines are ordered by file path, then id. The file states no reasons; a contested claim is argued in the commit.

The verbatim sentence is the staleness detector. Editing the sentence breaks the match, the same commit must re-pin it, and re-pinning is what brings the drift judgment into the edit.

## 2. The machine check

`cmd/tracecheck` is extended. It gains a reader for `claims.txt` and fails on:

- A claim whose sentence is not found exactly once in its file.
- A claim whose file does not exist, whose status is not one of the three, or whose `unbuilt` milestone is not a `## Mn` heading in `roadmap.md`.
- A claim id cited in code that is not in the inventory. A citation is the token `C` plus four digits on a comment line of a `doc.go` or anywhere in a screen `README.md`.
- A `built` claim no code cites. An `unbuilt` or `stated` claim some code cites.
- A `doc.go` or screen `README.md` citing no claim, outside `cmd/`. This replaces the rule that it cite some file. File references stay allowed beside ids. A `cmd/` package implements the root `CLAUDE.md`'s rules, not the design, so it keeps the rule that it cite some file.
- A bare `.md` reference without a slash. Today such a reference is skipped; four exist in `factory/gate/doc.go` and `factory/agent/doc.go` and point at files that do not exist where they resolve.
- A package directory name that, with spaces inserted at any points, is not a phrase some `end-goal/` file uses, unless listed in the allowance in tracecheck's `names.go`, a Go map from name to the design phrase it stands for, one entry per line with the reason in its comment. `terms.txt` is not the source, because it lists only names the document introduces in bold and half the packages are named for design words it does not list. Today 43 of 50 packages pass; the allowance holds the other seven with the design phrase each stands for, and `cmd/` is not checked.

The consistency pass fence in `end-goal/CLAUDE.md` gains two commands: every `claims.txt` line has four fields, and every file under `how-the-factory-works/` and `what-the-factory-does/` that is not a `README.md` has at least one claim. The second makes drift kind 2 answerable across the whole document.

CI gains a step running `tools/consistency-commands.sh`.

Tests follow tracecheck's temp-dir fixture style: one test per failure above and one green fixture.

## 3. The judgment step

A new agent, `.claude/agents/drift-reviewer.md`, on Opus. It is dispatched cold with three things and nothing else: one package directory or one screen directory, the current sentences of the claims that directory cites, and the instruction that the sentences are the design and the directory is what is judged. It reads only that directory and returns three lists and no prose:

| List | Meaning | Effect |
|---|---|---|
| Not implemented | A cited claim the code does not do | The edit is not finished |
| Implemented differently | A cited claim the code does in a way the sentence does not describe, with file and line | The session decides: the code moves, or the claim is re-pinned to a corrected sentence. The design wins a conflict. |
| Claimed nowhere | A mechanism, field, state, or rule the code or `doc.go` states that no cited claim covers | The session decides: a claim is added, the code is removed, or the mechanism is recorded in the commit as the design's own vocabulary. |

It runs from two sides:

- **A design edit.** The fence gains one reading: the claims whose sentence changed in this edit, from `git diff` on `claims.txt` and from the quotes tracecheck reports missing, and the packages citing each. The pass's judgment section gains a step beside the cold-read check: one `drift-reviewer` dispatch per package listed. This catches drift kind 1 when the design moves.
- **A code edit.** A change to any `doc.go` or screen `README.md` dispatches one `drift-reviewer` for that directory before the commit. The rule is stated in the root `CLAUDE.md` Code section. `coder` runs it before returning, as it runs tracecheck. This catches drift kind 3.

No headless run and no model call in CI.

Cost: a reworded sentence cited by five packages costs five Opus reads even when the meaning is unchanged. The bound is that a claim is one sentence, so a reword touches few claims.

Claimed nowhere was a blocking list in the first version of this rule; the first live run showed it names mechanisms the design states in sentences the extraction did not take, so it became a session decision.

## 4. Bootstrap

In order, each step a commit point:

1. **The check, green on fixtures.** Extend tracecheck per section 2 with tests. It fails on the real tree until step 4 and is not run in CI until then. One `coder` dispatch.
2. **Extraction wave.** One Opus dispatch per section directory, thirteen in all: the eleven under `how-the-factory-works/`, `what-the-factory-does/`, and the top-level files together, excluding `open.md`, `glossary.md`, and `CLAUDE.md`, which state no claims. Each reads its directory only and returns candidate lines: file, verbatim sentence, and `stated` where the sentence has no mechanism, else blank. Batches of three, each finishing before the next. The session assigns ids and appends.
3. **Citation wave.** One Opus dispatch per Go package and per screen, fifty-eight in all, batches of six. Each gets its directory and the claim lines for every file its `doc.go` already names, rewrites `What defines it` to cite ids, and returns two lists: claims it implements and claims among those files it does not. The session marks the first `built`. The second, and every claim no package names, it marks `unbuilt Mn` from the roadmap or `stated`. The owner reviews `claims.txt` here, before the check goes live.
4. **Green on the real tree.** Run tracecheck and fix until it passes. Add the CI step for the consistency pass.
5. **Judgment live.** The `drift-reviewer` agent file, the fence reading, the consistency-pass step, and the `coder` rule.

About seventy Opus dispatches once, no batch above six.

## 5. Where the rules are written

| File | Change |
|---|---|
| Root `CLAUDE.md` | "The map ships with the code" says a `doc.go` cites claim ids and lists what tracecheck fails on. "Code coins no second name" gains its check. The Code section gains the `drift-reviewer` rule on a `doc.go` or screen `README.md` edit. The two-files table adds `claims.txt` beside `terms.txt` as an inventory, not a third decision file. Delegate-by-default lists `drift-reviewer` among the Opus workers. |
| `end-goal/CLAUDE.md` | `claims.txt` described beside `terms.txt`: fields, statuses, the two fence commands, and the drift step in the judgment section with the three lists and which one means the edit is not finished. |
| `factory/README.md` | Tracecheck's entry lists what it checks. |
| `roadmap.md` | One sentence in the intro: the claims a milestone builds are the `unbuilt Mn` lines naming it. |
| `.claude/agents/` | New `drift-reviewer.md`. `coder.md` gains the drift run before returning. |
| `factory/cmd/tracecheck/names.go` | The package-name allowance, a Go map from name to the design phrase it stands for, one entry per line with the reason in its comment. |

No `end-goal/` body file changes.

## Out of scope

- Claim ids inside the prose, or one heading per claim.
- Any model call in CI.
- Tracing identifiers below the package name against `terms.txt`.
- Changes to the review pass or the unattended loop.
