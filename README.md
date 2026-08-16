# borg

A monorepo for building a fully autonomous software factory — a product each customer
self-hosts, which refines intent, builds the software, deploys it, monitors it, and fixes
its own bugs, on its own.

Nothing is built yet. What exists is the design, in [end-goal/](end-goal/README.md): the
state this repository is built toward, not a record of what it does. Code is added beside
it as it arrives, never inside it.

The factory is built as ordinary software. It does not run its own pipeline over itself.

## What building it takes

More than one kind of engineering, and the design document's register — records, writers,
seams — hides that. [CLAUDE.md](CLAUDE.md) names what each of these owns.

- **What the product is** — product management, product design, design systems, technical
  writing.
- **What the factory decides** — applied statistics and sequential testing, risk scoring,
  requirements engineering, formal methods, safety engineering, human factors.
- **What the factory is made of** — software architecture, backend engineering, frontend
  engineering, data architecture, agent engineering, program analysis.
- **What the factory runs** — release engineering, database migration engineering, site
  reliability engineering, observability engineering, test architecture, platform
  engineering.
- **What the factory answers to** — security engineering, supply chain security, trust and
  safety, audit and compliance, legal, cost engineering.

## The review pass

Every discipline named above is also a reader — a subagent run by name, one at a time,
that audits the whole tree from its field ([CLAUDE.md](CLAUDE.md#the-review-pass) sets
how one is dispatched). Two readers are no discipline's:

- **the absence reader** — subjects a design of this kind normally covers and this one
  never mentions
- **the rule reader** — the instruction files alone: whether a rule earns its cost

Name one to run it: "run the security engineering reader".
