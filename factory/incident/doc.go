// Package incident owns the incident record: something wrong with what is
// running, written by the comparison at the crossing.
//
// # The comparison is the only writer, and no human is one
//
// The comparison is the one component that reads production behaviour and knows
// which release and which deploy that behaviour belongs to, which is exactly what
// an incident points at. A human-written one would put a judgment where the design
// keeps arithmetic, and a human's judgment about live software already reaches
// production through veto after the fact and the page they may fire — so [Writer]
// refuses an actor that is not a component, the mirror of package policy refusing
// one that is not a human.
//
// It is written whether or not the release's window is still open. Inside the
// window the crossing also condemns the release; outside it the crossing raises an
// item and nothing rolls back, and the incident is the same record either way.
//
// # What it points at, and what that costs
//
// The deploy, and through the deploy the release, the release its item and its
// intent — so what caused an incident is answered by following those links, the
// same links the release record answers from the other end. There is no field
// saying what caused it, and there could not be: the incident points at the deploy
// that was running when it appeared and not always at the one that caused it. A
// consumer broken by its producer gets an incident on its own innocent deploy, and
// the item raised from it is what finds the producer.
//
// # Deduplication is a query and not a field
//
// [Open] is what the comparison keys its deduplication on before it calls intake:
// an open incident on this service and this release makes a further crossing an
// observation on it and never a second intent. The partial unique index in [DDL]
// is the same rule in the store, so two components crossing at once produce one
// incident rather than two.
//
// An observation is [Writer.Observe], which raises the count on the row and
// writes nothing else. The count is there because an incident nobody added to
// reads the same as one crossing every second, and it is a count rather than a row
// per observation because a row per crossing would size the store by how often
// the comparison runs — the reason the log holds no record per delivery either.
//
// # Resolved is two conditions and the second is the caller's
//
// [Writer.Resolve] advances the status, and what it requires is that the crossing
// has stopped against what is running *and* that what was raised from the incident
// has finished. This package checks neither: both are facts about other records —
// the comparison's own current reading, and the item cut from the intent it
// raised — and a package that checked them would read half the graph. What it does
// enforce is that an incident resolves once and from open.
//
// A rollback does not resolve one. It stops the crossing and leaves production
// worse than it should be, which is what the hold and the page both say.
//
// Who may write what: [Writer] inserts an incident, raises its observation count,
// and advances its status; nothing updates any other field and nothing deletes.
// environment_id, service_id, release_id, deploy_id, and intent_id are id fields
// and not foreign keys, the rule record's doc.go states once.
//
// What defines it: ../../end-goal/how-humans-do-it/08-operations.md#incidents —
// the record on a production environment, its writer, its links, its
// deduplication, and what resolving it requires — and
// ../../end-goal/how-humans-do-it/08-operations.md#after-the-watch-window for the
// intent a crossing writes once the window has closed.
package incident
