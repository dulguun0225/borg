# Deferred, but not designed out

Security comes last. The factory should be free and easy to play with at the start and tighten as the human world demands it. That is a sequencing decision, not permission to build something that cannot be secured later.

Four seams are nearly free now and expensive to retrofit:

1. **An actor on every record** — every gate decision, edit, approval, veto. No authentication, no enforcement, just the field, always populated. Identity cannot be added to a history that was written without it.
2. **One append-only decision log.** It is the audit trail and the risk score's training data at once, and it must not become two systems.
3. **Secrets by reference.** Artifacts and specs carry names, never values — they get copied, diffed, and handed to agents. The resolver can be a file on disk today.
4. **A named seam between agents and deploy targets.** However it is implemented, an agent reaches an environment through a small set of named operations. That seam is where policy attaches later; without it, prod access is diffused through the codebase.

One pipeline is the strongest of these and was chosen for coherence rather than safety: a single path is a single place to put policy.
