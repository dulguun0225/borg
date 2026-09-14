Do MILESTONE step STEPNUM of `HANDOFF.md` at the repository root.

Read `CLAUDE.md` first, then `HANDOFF.md` whole, and follow its "Rules for a step session" exactly. Then read only what the step names: its design files, whole, and the packages it changes. `factory/README.md` says how the checks and tests run; the dev database is up on port 5433.

Build the step as `CLAUDE.md`'s Code section requires: one package per concept, the shape rule, no reflection, no `init`, no string-keyed dispatch, files under 500 lines, every import a line in `factory/deps.txt`, every changed `doc.go` citing the claims whose mechanism the package holds and no other, and no name the design does not use. Each claim's mechanism goes in the package the plan assigns it; `cmd/` composes and holds none. Write tests to the pattern of the neighbouring tests. Do not restate the design's reasoning in code comments or `doc.go`.

Run `go vet ./...`, `go run ./cmd/depscheck`, `go run ./cmd/tracecheck` from `factory/`, and the step's focused tests; do not run the whole suite. Fix what fails. Do not weaken a test or a check to pass it; if a check cannot pass, say why under **Unresolved**.

Do not commit. Do not push. Do not edit `end-goal/` except the status flip the rules name, and do not edit `roadmap.md`. Update `HANDOFF.md` as its rules say.

End with: the step done, or the split made and the part done; the directories changed, marking those whose `doc.go` or screen `README.md` changed; each check with its result, verbatim for a failure; and unresolved points.
