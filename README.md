# borg

A monorepo for building a fully autonomous software factory — a product each customer
self-hosts, which refines intent, builds the software, deploys it, monitors it, and fixes
its own bugs, on its own.

Nothing is built yet. What exists is the design, in [end-goal/](end-goal/README.md): the
state this repository is built toward, not a record of what it does. Code lands beside
that directory as it arrives, never inside it.

How it gets built is in [bootstrap/](bootstrap/README.md) — the factory's own stages run
by hand until it can adopt a codebase it did not build, which is the point it can adopt
itself.
