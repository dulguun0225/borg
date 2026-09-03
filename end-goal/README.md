# End goal

A draft. Everything here is open to revision.

| File | What it holds |
|---|---|
| [What the factory does](what-the-factory-does/README.md) | The product, the two properties it is built around, and what it does not build |
| [What humans do](what-humans-do.md) | Twelve owner duties, numbered — the rest of this document cites them as bare numbers |
| [How the factory works](how-the-factory-works/README.md) | Eleven sections in dependency order, one directory each — the section _One pipeline_, having no subsections, stays a file |
| [Records and their writers](records.md) | Every record in [the graph](what-the-factory-does/01-tight-integration.md), the one component that writes it, and the seams (a boundary declared between two such writers) where two reach one record — the inventory of the one-writer rule that [_Tight integration_](what-the-factory-does/01-tight-integration.md) states |
| [Components and what they call](components.md) | Every component, what it is, and which components it may call — the other half of that inventory, and where the calls the sections state one at a time are collected |
| [One process](one-process.md) | The deployment model: one process, the lease that makes it one, what a component's restart reads, and the factory's own store's schema history |
| [Deferred, but not designed out](deferred.md) | Security last, and the five seams that are nearly free now |
| [Open](open.md) | What is not settled, phrased as the question and what turns on it |
| [Glossary](glossary.md) | The words the industry owns that this document uses in a narrower or different sense, one line each |

Read _What humans do_ before anything under `how-the-factory-works/`: a bare `(7)` or `(11, 12)` anywhere in this document is a duty number from that list. Check the [Glossary](glossary.md) when a familiar word seems not to mean what it usually does.
