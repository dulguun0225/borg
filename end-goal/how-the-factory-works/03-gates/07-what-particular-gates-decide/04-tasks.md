# Tasks

A task is an internal step of one item and never a unit that ships: one item is one release, and a task has no build, no number, and no environment of its own. The factory authors them from the approved plan — the plan is how the item will be built, the tasks are that divided into work an agent picks up — and they are complete when the implementation is.

What the gate provides is a look at the breakdown before agents work on it, and Edit in place is where a human resequences or splits one without changing the plan above it. A task that cannot be finished escalates nothing by itself: the attempt limit is per stage, so what appears in Work (12) is the item.
