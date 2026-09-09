// Package incident owns the incident record: something wrong with what is
// running, written by the health monitor at the crossing.
//
// incident.go is [Incident] and the [Raising] it is written from, [Status] with
// [Statuses] and [Incident.Open], [Reading] with [Readings] and
// [Reading.StatesARunLength], [Incident.OverBound], [Writer] and [NewWriter],
// and the reads [Get], [All], [ForService], and [Open]. An incident names the
// deploy and through it the release, the item, and the intent, so what caused
// one is answered by following those links; there is no field saying what
// caused it, because the record points at the deploy that was running when it
// appeared and not always at the one that caused it.
//
// overdue.go is [Overdue] and [OverdueItems], the reader nothing called until
// this pass existed: every incident-raised item still being worked past its
// service's incident-item bound, at the time given — an open incident naming
// an intent, [Incident.OverBound] read against the bound
// [service.IncidentItemBoundSecondsInForce] gives that incident's service, and
// an item of [item.ForIntent] whose stage is still ordinary work rather than
// merged, dropped, escalated or superseded. It is what a caller pages against,
// one uncleared page per item.
//
// [Reading] is which of the three readings crossed — the comparison, the
// reading against the service's own recent history, or an explicit threshold —
// because more than one runs on one service and an incident naming none of them
// would not say what happened. Beside it the incident names the quantity, the
// size, and a confidence or a run length according to the reading, the boundary
// version the reading was taken through, the policy and score versions in
// force, and the failure records the health monitor copied at the crossing — a
// field of the incident rather than a link to the store, so it is removable the
// way any kept record's text is, by a redaction.
//
// [Open] — an open incident on one service and one release — is what the health
// monitor keys its deduplication on, and the partial unique index in [DDL] is
// the same rule in the store, so two components crossing at once produce one
// incident rather than two. [Writer.Observe] raises the count on that row and
// writes nothing else: a count rather than a row per crossing, which would size
// the store by how often the health monitor runs. [Writer.Resolve] advances the
// status, and enforces only that an incident resolves once and from open — that
// the crossing has stopped and that what was raised from the incident has
// shipped are facts about other records and the caller's to check. schema.go is
// [Table], [IDPrefix], and [DDL].
//
// Who may write what: [Writer] is the health monitor — [Writer.Raise] refuses an actor
// that is not a component with [ErrNotAComponent], the mirror of package policy
// refusing one that is not a human. It inserts an incident, raises its
// observation count, and advances its status; nothing updates any other field
// and nothing deletes. environment_id, service_id, release_id, deploy_id, and
// intent_id are id fields and not foreign keys, the rule record's doc.go states
// once. [OverdueItems] writes nothing: it reads package item's records and
// package service's incident-item bound, and a caller pages against what it
// returns.
//
// What defines it:
// ../../end-goal/how-the-factory-works/08-operations/06-incidents.md (C2095,
// C2096, C2097, C2098, C2101) — the record on a production environment, its
// writer, its links, its deduplication, and what resolving it requires — and
// ../../end-goal/how-the-factory-works/08-operations/04-after-the-analysis-window.md
// for the intent a crossing writes once the window has closed.
//
// Passing the incident-raised item bound pages, on the same ground as the
// other clocks the design puts beside it, is
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md
// (C0739).
package incident
