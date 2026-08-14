# Risk score

A vector of named factors, reduced to one number by a published formula. Both halves matter — the number is what a gate compares against a threshold, the vector is what a human reads when they disagree with the number. A score nobody can argue with is a score nobody will trust.

## Factors, at least

- **The change** — size, blast radius, area churn, test coverage, reversibility.
- **Authorship** — a prior, per human and per AI model, carried from that author's own history of vetoes and rollbacks. It starts wide for an author the factory has not seen and narrows with evidence, which is also how a new model version earns its way in.
- **Context** — what this change touches in this customer's business, and which sibling services consume what it publishes. The same diff is a different animal in a payments path than on a marketing page.

Likelihood and impact stay separate until the last step. They answer different questions and drive different responses: likely-wrong but cheap to undo should ship and let rollback handle it; unlikely but catastrophic should be gated regardless. This is also why one score drives three decisions about a change — the gate reads mostly likelihood against impact, the rollout strategy reads mostly impact against reversibility and how fast a problem would surface, and the boundary of the [_watch window_](08-operations.md#the-watch-window) reads mostly impact. Reading different halves is what keeps them from thinning together: a change misjudged on likelihood, which is where an authorship prior and an unfamiliar area land, loses its gate and keeps its watch.

The defaults the score supplies where an owner authored nothing are not a fourth: [_Gate policy_](09-gate-policy.md#one-shape-across-all-of-them)'s seven parameters stand per stage, per service, or per area and move as outcomes arrive, where the three above are read off one change.

## How it learns

The score is learned, not fixed. Every bad call feeds back and refines it: an auto-passed change that a human vetoes, a low-risk change its watch window rolled back, a gate the factory would have passed but a human rejected. Outcome feedback is the sharpest signal but not the only one — any source that improves the score is admissible, and the input set stays open by design.

Scoring on authorship feeds itself if left alone: a distrusted author is gated more, gated work draws more scrutiny, more scrutiny finds more faults, and the distrust is confirmed. The factory holds out a random sample — occasionally auto-passing what it would have gated, under the longest watch window there is — to keep unbiased signal on the authors and areas it has stopped trusting.

The sample is the score's own to hold out of, and it reaches nothing an owner pinned. A pin (9) can only add, and a gate pinned always-on is a human added; a sample that could pass one would be the single mechanism in the tree that takes a human off a gate, which is what a pin exists to prevent. So what the sample may auto-pass is what the score itself would have gated, and the price of that is that the areas an owner distrusts most are the ones it learns least about.
