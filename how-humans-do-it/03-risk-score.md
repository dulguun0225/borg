# Risk score

A vector of named factors, reduced to one number by a published formula. Both halves matter — the number is what a gate compares against a threshold, the vector is what a human reads when they disagree with the number. A score nobody can argue with is a score nobody will trust.

## Factors, at least

- **The change** — size, blast radius, area churn, test coverage, reversibility.
- **Authorship** — a prior, per human and per AI model, carried from that author's own history of vetoes and rollbacks. It starts wide for an author the factory has not seen and narrows with evidence, which is also how a new model version earns its way in.
- **Context** — what this change touches in this customer's business, and which sibling services consume what it publishes. The same diff is a different animal in a payments path than on a marketing page.

Likelihood and impact stay separate until the last step. They answer different questions and drive different responses: likely-wrong but cheap to undo should ship and let rollback handle it; unlikely but catastrophic should be gated regardless. This is also why one score drives three decisions — the gate reads mostly likelihood against impact, the rollout strategy reads mostly impact against reversibility and how fast a problem would surface, and the watch window's threshold reads mostly impact. Reading different halves is what keeps them from thinning together: a change misjudged on likelihood, which is where an authorship prior and an unfamiliar area land, loses its gate and keeps its watch.

## How it learns

The score is learned, not fixed. Every bad call feeds back and refines it: an auto-passed change that a human vetoes, a low-risk change its watch window rolled back, a gate the factory would have passed but a human rejected. Outcome feedback is the sharpest signal but not the only one — any source that improves the score is admissible, and the input set stays open by design.

Scoring on authorship feeds itself if left alone: a distrusted author is gated more, gated work draws more scrutiny, more scrutiny finds more faults, and the distrust is confirmed. The factory holds out a random sample — occasionally auto-passing what it would have gated, under the longest watch window there is — to keep unbiased signal on the authors and areas it has stopped trusting.
