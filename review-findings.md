# Review findings

Run bounded to `end-goal/`: the full roster of twenty-nine, each stance reading `end-goal/` alone, each returning only findings a hard migration would be needed to fix after the factory is in production, with no cap on the count. All twenty-eight disciplines in _What the work spans_ ran, and Absence. The Rules stance did not run, since a run bounded to `end-goal/` omits it. Where one stance reached a finding another had reached separately, the entry says so. Nothing here is a disposition.

Regrouped after the run: each `##` heading below is the `end-goal/` file or section directory a finding names first, so one loop session handles every finding on a section. `**Raised by:**` names the discipline that wrote each entry; entries that reached one another separately sit adjacent under one heading.

## how-the-factory-works/11-screens/

### The People declaration decides segregation of duties and is the one owner-written record that is neither versioned nor chained
**Raised by:** Security engineering
**Where:** `how-the-factory-works/11-screens/01-work-ops-factory-people.md` — _People_; `records.md` — the `People declaration` row
**What is wrong or missing:** Gate policy writes append a chained policy version, for the reason the document states — "a decision names them by identifier and nothing else". The duty roster gets no such treatment: it is written at People, it is edited in place, and the log's eight shapes do not include it. Yet the gate component's self-approval refusal fires only "wherever another holder of the row's duty exists", a safeguard's rows route to "the duty or the named human", and pages widen to the owner where no holder is recorded — all reads of that mutable record at decision time. `what-humans-do.md` names the exposure ("no rule reads the write that decided who holds a row's duty. The party who assigns every approver is not separated from approving") and answers it with a sentence rather than a mechanism.
**What turns on it:** The only segregation-of-duties control in the factory can be defeated without leaving a trace: remove the second holder of a duty at People, close your own row (the close event then carries a self-approval field that reads as the ordinary one-owner install), restore the holder. An auditor reading a decision back resolves its actor through today's mapping and has no way to ask what the roster said when the row closed, which is precisely the failure _Traceability_ exists to rule out.
**Migration:** A chained row per People write — duty assignment by opaque key, leaving the key-to-name mapping outside the chain so erasure still works — cannot be backfilled, and every decision closed before it exists stays unreadable against the roster in force at its close.
**Also reached separately by:** Audit and compliance — "Who held a duty at the moment of a decision is unrecoverable, so segregation of duties cannot be evidenced".
