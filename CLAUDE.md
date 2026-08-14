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

Removed from this project on 2026-08-14 — the index, the ignore entry, the parked hooks,
and the rules that pointed at them. It stays installed on the machine and is not the
thing being refused; what is refused is running it here. An AST index is worth what the
code is, and this repository is still all prose, where graphify takes its other
extraction path and pays LLM subagents for it: a `--update` over the twenty-file design
tree spent 64k tokens on one of two chunks before the run was cut, to answer worse than
the greps in `end-goal/CLAUDE.md`, which answer exactly and in milliseconds. Reconsider
when code lands beside `end-goal/`, scoped to the code paths and never to the prose.
(Owner decision, 2026-08-14, superseding the keep of earlier the same day.)
