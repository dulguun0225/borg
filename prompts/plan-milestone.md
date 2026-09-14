You are planning milestone MILESTONE of this repository. You write one file, `HANDOFF.md` at the repository root, and change nothing else. Do not commit. Do not edit `end-goal/`, `factory/`, or `roadmap.md`.

Read, in this order:
1. `CLAUDE.md`, whole; its Code section and the subsection "Building a milestone" are what the plan is held to.
2. The `## MILESTONE` entry in `roadmap.md`, and the entry above it for the shape of a finished milestone.
3. `grep -n 'unbuilt MILESTONE' end-goal/claims.txt` — the claims this milestone builds. Each line is: id, file under `end-goal/`, status, sentence. Read every one.
4. Every design file those claims name, whole.
5. `factory/README.md`, `factory/deps.txt`, and the `doc.go` of each package the plan will touch. Read code only as far as needed to know what exists.
6. `git log --oneline -20` and `git show --stat` of two step commits of the milestone before, for how a step was cut and its commit titled.

Then write `HANDOFF.md` with this content and nothing else:

- **Task**: milestone MILESTONE as `roadmap.md` states it, built as ordered steps, one commit per step titled `factory: MILESTONE step N — <name>`, straight to `main`.
- **Base commit**: the current HEAD hash.
- **Rules for a step session**: copy the list under "Building a milestone" in `CLAUDE.md` verbatim.
- **Completed**: `(none)`.
- **Steps**: an ordered list. For each step: a number and a name; the claim ids it builds; the design files to read; for every claim, the one package that will hold its mechanism and cite it — a new package needs a `doc.go`, a `deps.txt` line, the shape rule's file names, and a name that is a phrase the design uses or a `names.go` entry, and `cmd/` composes and holds no mechanism; what test or end-to-end demonstration proves it, the roadmap entry's demonstrations each being an end-to-end test in `cmd/factory` in some step; and the focused checks to run.
- **Coverage**: a table of every claim id to the step that builds it and the package that will cite it. Every claim appears exactly once. A claim no code can implement is listed under `Claims that may be "stated"` with one sentence why.
- **Unresolved**: what the design leaves the implementer to decide, and what you inferred rather than read, marked as inference.

Constraints on the steps:
- Order by dependency: a step's tests run green on its own commit, with earlier steps' code and no later step's.
- Bound each step so one fresh session can do it: about 1,000 lines of change or fewer, reads confined to the packages it touches plus the design files it names. Split a subject that would exceed that.
- Follow the dependency direction `deps.txt` allows; a step that needs a new edge says so and why.
- Do not restate the design's reasoning; cite claim ids and paths.
- Writing style: established terms only, no metaphor, no invented terms, concise.

End with a summary of what you read, what you could not resolve, and the line count of `HANDOFF.md`.
