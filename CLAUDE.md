# CLAUDE.md

## What this repository is

A monorepo for building a fully autonomous software factory. Today it holds one thing:
the design document describing what is to be built, under `end-goal/`. Code lands beside
that directory as it arrives, never inside it — `end-goal/` is the state the repository
is built toward, not a record of what it currently does.

**Read `end-goal/CLAUDE.md` before touching anything under `end-goal/`.** That document
is a graph of interlocking claims, and it has its own editing rules, its own writing
style, and a consistency pass to run after every edit. Editing it without them breaks
duty references, links, and vocabulary that other files lean on. Those rules govern that
directory alone and say nothing about code.

## Commits

Commit straight to `main`. The project is early; branches start when it is ready for
them, and not before. Do not create one unasked. (Owner rule, 2026-08-13.)

## graphify

`graphify-out/` holds a knowledge graph of the tree, gitignored. It is an AST-based
index, so what it is worth is the code: over `end-goal/` the greps in that directory's
own `CLAUDE.md` answer the same questions exactly and in milliseconds, and the index goes
stale on the next edit.

Rules:
- For codebase questions, first run `graphify query "<question>"` when graphify-out/graph.json exists. Use `graphify path "<A>" "<B>"` for relationships and `graphify explain "<concept>"` for focused concepts. These return a scoped subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when query/path/explain do not surface enough context.
- After modifying code, run `graphify update .` to keep the graph current (AST-only, no API cost).
