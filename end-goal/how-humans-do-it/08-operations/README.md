# Operations

What happens after a deploy: how the factory measures the change, how long it may act on that measurement alone, and what it does when something is wrong.

| Subsection | What it settles |
|---|---|
| [The health monitor](01-the-health-monitor.md) | What compares a release against a control, and what it reads without one |
| [The analysis window](02-the-analysis-window.md) | How long the factory may act on the comparison alone, and what closes it |
| [Overlapping windows](03-overlapping-windows.md) | How many windows may stay open at once, and what a rollback undoes |
| [After the analysis window](04-after-the-analysis-window.md) | What the health monitor still catches once the window has closed |
| [Service level objectives](05-service-level-objectives.md) | What an owner authors, and what happens once the budget it sets is spent |
| [Incidents](06-incidents.md) | What record the health monitor writes at a crossing, and when it resolves |
| [Pages](07-pages.md) | How a wait reaches a human, and which waits qualify as pages |
| [Drift detection](08-drift-detection.md) | What compares what runs against what the factory recorded, and holds on a mismatch |
