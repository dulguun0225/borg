# Items

One directory per item, holding what the factory's stages write about that item. During stage 0 the stages are run by hand, so the artifacts are written by hand — that is the only difference between this directory now and what the factory will write into it later.

## What an item directory holds

`items/<name>/` — the item's name, lower case with hyphens — holds `spec.md`, `implementation-plan.md`, and `tasks.md`, three of the four artifacts of an item that sitting 3 of [the interview](../bootstrap/README.md#the-interview-in-ten-sittings) names.

The fourth is the implementation, which is code. Code is added beside `end-goal/`, `bootstrap/`, and this directory, and never inside an item directory. The item's tasks name the paths they change, and that is the only link from an item to the code its implementation produced.

## What it does not hold

Anything settled about what the factory is goes to the `end-goal/` file that owns the subject. The plan, the constraints on the cut, the item list the cut produced with its declared order, and where the work has got to go to [`bootstrap/README.md`](../bootstrap/README.md). What is left for here is what a stage wrote about one item, and nothing else.

What that costs is a third place to look while an item is being built. It is taken because the alternative accumulates per-item answers inside `bootstrap/`, which is the shape that retired `next.md` on 2026-08-14. (Owner decision, 2026-08-14.)
