# What humans do

Non-exhaustive owner's list. The factory does everything else. It is a list of duties, not of people: an owner may hold all twelve or delegate any of them, and [_People_](how-humans-do-it/11-surfaces.md#work-ops-factory-people) is where each attaches to a named human — a designer holding (2), (6), and (11), a compliance officer holding (2) and a pin (9) over a regulated area. Whoever holds a duty gets its gate rows and its UAT assignments (7) in their own view of [_Work_](how-humans-do-it/11-surfaces.md#work-ops-factory-people).

Writing that record is the owner's and is not a duty of its own: it is what distributes the twelve, and a duty over it would be one of the twelve deciding who holds the twelve. Nothing enforces it, and nothing has to — a [_page_](how-humans-do-it/08-operations.md#pages) or a gate row with no holder recorded widens to the owner, who is the person that would have written the row.

What falls outside is substrate rather than work, and the tree names three: hosting the factory, installing [_The reconciler_](how-humans-do-it/08-operations.md#the-reconciler) beside it, and supplying the credentials [_The fleet_](how-humans-do-it/10-fleet.md) runs on. None of the three is something a human does with an item, a rule, or a running system, which is what the twelve are. Each is still an obligation, and nothing here scores one. Upgrading the factory is part of hosting it and not a fourth: a self-hosted product is brought up to date by whoever runs it, which is the same person and the same kind of obligation.

**Originate intent** — the factory cannot know what is wanted until told:

1. Request features.
2. Supply constraints, of two kinds. A **standing** one binds every item from then on and is permanent in the way the rules (8) are: the factory works inside it and never gets out of it. Laws and regulations are standing; so is a **design system** — the reusable decisions every screen is built from, its tokens, its components and the states each may be in, and the rules for using them, which is neither artwork nor the tool the artwork is drawn in — and an owner supplies one or picks from the ones the factory ships with. A **per-item** document refines one request and applies only to it, a screen design for a single request among them. Nothing removes a standing constraint on its own: it binds until it is withdrawn, so an owner who never withdraws one has the factory building against it years later.
3. Answer the factory's interview, for as many rounds as it asks, until the intent is refined.

**Feed back as end users** — routine, in end-user terms, not engineering terms:

4. Report bugs.
5. Complain ("this button is too slow").

**Verify against intent** — at a gate, so as often as the score or a pin (9) says:

6. Confirm the acceptance criteria are the right ones. Unit tests are today's encoding of them; what a human is checking is the criteria, not the test code.
7. Perform UAT.

**Set the rules** — permanent, not shrinking:

8. Author gate policy and risk thresholds. One line, seven parameters — [_Gate policy_](how-humans-do-it/09-gate-policy.md) is the set, and none of it has to be authored for the factory to run: what an owner leaves alone, the score supplies.
9. Pin a gate always-on for a stage, project, or area. An **area** is an owner-declared grouping of the software — coarser than a file, free to cut across services — and it is what names "the payments path" wherever a pin, a score factor, or a gate an owner has added back has to be narrower than a project.
10. Veto after the fact — undo a change the factory auto-approved: a rollback while its control is still running, a revert after.

**Backstop the factory** — only where it falls short, shrinking as it improves:

11. Help author a spec or an implementation plan when the factory cannot do it properly — up to writing it together with the AI.
12. Take over issues the factory cannot fix on its own.
