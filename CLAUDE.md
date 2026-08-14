# CLAUDE.md

## What this repository is

A monorepo for building the software factory [README.md](README.md) describes. Today it
holds one thing: the design document under `end-goal/`. Code lands beside that directory
as it arrives, never inside it — `end-goal/` is the state the repository is built toward,
not a record of what it currently does.

**Read `end-goal/CLAUDE.md` before touching anything under `end-goal/`.** It has its own
editing rules, its own writing style, and a consistency pass to run after every edit,
which govern that directory alone and say nothing about code.

## Commits

Commit straight to `main`. The project is early; branches start when it is ready for
them, and not before. Do not create one unasked. (Owner rule, 2026-08-13.)

## How a change to the end goal is recorded

Change `end-goal/` directly. The commit is the record: the edit lands in the file that
owns the subject, and the body says what moved and why, which is the shape
`end-goal/CLAUDE.md` already sets. That is a record of what happened and nothing that
binds what comes next.

No ADRs until the project has proved itself. A record claiming authority over future work
is what made an earlier attempt unchangeable — the pile grew, an agent could always find
one to cite and answer a change with a wall of text, and pruning the ones that had stopped
being true was archaeology nobody did. The design document is a target, revised whenever
something is learned; an ADR is a claim on the future, and nothing here has earned one
yet. They start when the factory is proved and holding settled ground still is worth more
than staying cheap to change. (Owner decision, 2026-08-14.)

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
