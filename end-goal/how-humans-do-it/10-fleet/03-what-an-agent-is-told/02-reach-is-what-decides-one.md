# Reach is what decides one

Every version fires [a gate of its own](../../03-gates/07-what-particular-gates-decide/09-a-role-prompt-or-a-skill.md), and the [score](../../04-risk-score/README.md) decides it as it decides any other. What it reads is the factor set [_Factors, at least_](../../04-risk-score/01-factors-at-least.md) names for a version rather than the one it names for a diff — a role prompt has no code to have sized — and reach is the factor that does the work.

| | What a version reaches | What follows |
|---|---|---|
| **A role prompt** | every item at that stage, in every project the factory runs | the widest impact anything in the factory has, so the score gates it whatever the likelihood |
| **A skill** | its subject and nothing outside it | impact bounded by the subject, so a narrow one [auto-passes](../../03-gates/01-where-a-gate-is-and-what-decides-it.md) |

So the factory improves its skills on its own, and on any [threshold](../../09-gate-policy/README.md) an owner is likely to author it does not improve a role prompt without a human at the gate. That is where the line belongs. A role prompt is the most damaging record the factory could write — one bad version degrades every item at that stage until somebody notices — and it is also the one whose effect nothing measures.

Nothing measures it, and that is not an omission. There is no [analysis window](../../08-operations/02-the-analysis-window.md) over a version and no [control](../../08-operations/01-the-health-monitor.md) to compare one against: a control is instances taking organic traffic, and a role prompt serves none. What a version did shows in [_Factory_](../../11-screens/01-work-ops-factory-people.md)'s gate rejection rate and rework rate, sliced by the version in force, over as many items as a rate takes to move — which on a small install is longer than anybody will wait. [_How does the factory learn whether a role prompt version is better?_](../../../open.md#how-does-the-factory-learn-whether-a-role-prompt-version-is-better) is that question, and it is open rather than answered here.

Two things follow while it stays open. A bad version is found late, by a human reading a number rather than by the factory reading a signal. And the [_per-author prior_](../../04-risk-score/01-factors-at-least.md) cannot see one at all: it is per model, so a model whose role prompt changed reads as one author across both, and the score's opinion of that author is an average over work done under two sets of words.

The role prompt in force is also what the authoring agent works from, because there is nothing else to give it. The factory revises its role prompts using the role prompts it has, so a version that degrades an agent degrades the agent that would author its replacement — and the way out of that is a human at the gate, which is where reach already put one.
