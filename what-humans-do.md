# What humans do

Non-exhaustive owner's list. The factory does everything else. It is a list of duties, not of people: an owner may hold all twelve or delegate any of them, and People is where each attaches to a named human — a designer holding (2), (6), and (11), a compliance officer holding (2) and a pin (9) over a regulated area. Whoever holds a duty gets its gate rows and its UAT assignments (7) in their own Inbox.

**Originate intent** — the factory cannot know what is wanted until told:

1. Request features.
2. Supply constraints, of two kinds. A **standing** one binds every item from then on and is permanent in the way the rules (8) are: the factory works inside it and never earns its way out. Laws and regulations are standing; so is a **design system** — the reusable decisions every screen is built from, its tokens, its components and the states each may hold, and the rules for using them, which is neither artwork nor the tool the artwork is drawn in — and an owner supplies one or picks from the ones the factory carries. A **per-item** document refines one request and is spent with it, a screen design for a single request among them. Nothing prunes the standing kind: it binds until it is withdrawn, so an owner who never withdraws one has the factory building against it years later.
3. Sit for the factory's interview — grilled — until the intent is refined.

**Feed back as end users** — routine, in end-user terms, not engineering terms:

4. Report bugs.
5. Complain ("this button is too slow").

**Verify against intent** — at a gate, so as often as the score or a pin (9) says:

6. Confirm the acceptance criteria are the right ones. Unit tests are today's encoding of them; what a human is checking is the criteria, not the test code.
7. Perform UAT.

**Set the rules** — permanent, not shrinking:

8. Author gate policy and risk thresholds. One line, eight parameters — [_Gate policy_](how-humans-do-it/09-gate-policy.md) is the set, and none of it has to be authored for the factory to run: what an owner leaves alone, the score supplies.
9. Pin a gate always-on for a stage, project, or area. An **area** is an owner-declared grouping of the software — coarser than a file, free to cut across services — and it is what names "the payments path" wherever a pin, a score factor, or a bought-back gate has to be narrower than a project.
10. Veto after the fact — undo a change the factory auto-approved: a rollback while its control still stands, a revert after.

**Backstop the factory** — only where it falls short, shrinking as it improves:

11. Help author a spec or an implementation plan when the factory cannot do it properly — up to writing it together with the AI.
12. Take over issues the factory cannot fix on its own.
