// Package notifier is the one component that delivers everything waiting on a
// human out of the product, on three channels: mail, chat, and the page.
//
// wait.go is the vocabulary. [Channel] and [Channels] are the three; [Event] and
// [Events] are the four page events; [Kind] is a kind of wait and [Kinds] maps
// each to what the page's condition answers for it — [PagesNever],
// [PagesAlways], or [PagesIfWorse], where [Wait.Worse] is the caller's answer.
// [KindSpendCeilingFraction] is the one kind dispatch delivers on beside an
// escalation, and it pages never: the design gives a spend ceiling a notice at
// a fixed fraction so that the hold is not the first anyone hears of it, and a
// notice is not a page.
// [Wait] is what waits, whom it routes to, and the service it is about, validated
// by the errors beside it: a caller that sets Worse on a [PagesNever] kind is
// refused with [ErrWorseRefused], and so is one that clears it on a [PagesAlways]
// kind, which is what makes "nothing else fires a page" mechanical. [Deliverer]
// is what a channel does and [Delivery] is what it is handed — the recipient by
// per-person key, never a name; it is an interface its caller implements, because
// what mail is on a self-hosted install is the owner's arrangement and not the
// factory's. [Wait.Person] is the named human a wait routes to ahead of
// [Wait.Holding] — the requester of an intent a wait on it names, the human
// who lent a credential a ceiling wait is about, and a named human a row
// belongs to where it belongs to no duty — and is the raiser's own answer,
// left empty where the raiser has no such fact; [Notifier.routeTo] reaches it
// first. [Wait.RollbackOutstanding] is which of the two kinds a wait is —
// production serving a release the health monitor called for a rollback on,
// with the rollback not run, is the first kind and pages at any hour — and it
// is the caller's answer the way [Wait.Worse] is: package healthmonitor sets it
// on the page a failed exit with no rollback fires, and the command-line
// interface answers it for the waits it creates. hours.go is
// [Notifier.deferredToHours] and [withinHours], a wait of the second kind held
// to a service's own authored paging hours rather than paging at any hour,
// computing [Wait.PageAt] — the next instant the hours allow, [nextAllowedHour] —
// once at the deferral, and [Notifier.PageDeferred] with
// [Notifier.deferredDeliveries] and [Notifier.endedRows], the pass that
// delivers such a page once that instant has passed rather than reading the
// hours again at every pass — taking the drift detector's store, since a
// mismatch cleared there is a row that stopped waiting and the log says so
// about every other.
//
// notifier.go is [Notifier] and [New], composed with the log, a fencing token, a
// [Deliverer], and the owner's identifier — the owner is composed in rather than
// read from the [people] declaration, because the design gives the owner no
// record. [New] also builds the [decisionlog.Reader] the notifier reads back
// through, fenced with the same token. All three channels route the same way, on
// [Wait.Person] where the raiser named one, otherwise on the declaration by the
// duty or obligation the wait belongs to, or by the owner where it belongs to
// neither, so routing is implemented once rather than beside each thing that
// waits. [Notifier.Notify] writes one reached row per holder of
// the duty, under [PageEventFormatVersion], and a [DeliveryRecord] per channel per
// holder even where the channel writes nothing else — flattened, at the read, out
// of the one record [DeliveryRowTable] keeps per waiting row — and a
// channel that refuses the send is recorded and does not stop the channels after
// it, the page being last and the one channel that carries what the other two
// cannot; [Notifier.Widen] writes one
// widened row to the owner and refuses a second with [ErrAlreadyWidened] or one
// over an acknowledged wait with [ErrAcknowledged]; [Notifier.Acknowledge] writes
// the fourth event, stopping only the widening, under the kind the page it
// acknowledges was reached under and writing nothing where the row pages
// nobody; and [Notifier.Answered] is called
// by whatever ends the wait, at the same write it ends it with, appending
// through [decisionlog.Writer.AppendPageEvent] on a transaction of its own —
// [Notifier.AnswerTx] is the identical write made on a transaction the
// caller already holds open, through [decisionlog.Writer.AppendPageEventInTx],
// for a caller whose own ending write shares the one transaction. None of
// [Notifier.Acknowledge], [Notifier.Answered], or [Notifier.AnswerTx] reaches
// the [Deliverer]: each is an act the product already recorded rather than a
// delivery, and [pageEventEntry] is the one row [Notifier.appendPageEvent],
// [Notifier.Answered] and [Notifier.AnswerTx] each build and append, shared
// with [Notifier.deliver]'s own append, through [Notifier.appendPageEvent],
// once a send on the page channel is accepted. [Payload] of
// [PageEventKind] is a page event's shape and [Notifier.EventsFor] reads them
// back, appending a read event naming [Actor] as the principal. delivery.go is
// [DeliveryRowTable], [DeliveryDDL], [DeliveryAttempt], [DeliveryRecord], and the
// upsert beneath every
// call to [Notifier.Notify], [Notifier.Widen], [Notifier.Acknowledge] and
// [Notifier.Answered] — one record per waiting row, carrying every channel and
// recipient's own attempt inside it, overwritten at each attempt except for
// [DeliveryRecord.FirstAcceptedAt],
// which is set once, on the attempt the transport first accepts, and left as
// it is on every attempt after — [Notifier.deliveredRows], which rows
// anything has gone out about, [PagedRowsSince], the count the harm mark's
// cap is read against, and [DeliveriesOf], every delivery record of one row
// on every channel and to every recipient, in the order they were written,
// flattening the one stored record back into one per attempt. [DeliveryTable]
// is the table an earlier form of this package kept one row per waiting row,
// channel and recipient in; its DDL stays declared and applied, since a
// removal is not a change [postgres.Changes] can declare yet, and nothing
// here writes it any longer.
// harmmark.go is [harmMarkPagesOff], the off switch on the factory-wide
// settings record — it governs the page channel alone, so [Notifier.Notify]
// still delivers mail and chat where it is set — and [Notifier.overHarmMarkCap]
// with [Notifier.pageOverTheCap]: past a service's cap the marked intent's own
// page channel is skipped and one page per interval goes out on [capRow]
// instead, naming the service and how many marked intents arrived past the
// cap. Nothing ever answers a cap row — a page about a volume rather than
// about one thing has no closing act — so [capRow] names the interval too,
// through [intervalBucket]: the interval after the one a row already paged for
// mints a row of its own, which is what pages again rather than staying
// silent for good.
// resume.go is [Notifier.Resume], this component's restart: the delivery
// record it overwrites per row the log still holds open, so a row still
// waiting is delivered again and one that stopped waiting is not. The wait
// each row is delivered again as is rebuilt from the delivery record itself —
// [deliveryRowStored.wait] — and never from the page events, which is what
// lets a kind that pages never, and carries no page event at all, be
// delivered again too. A row of a kind that never opens in the log at all — a
// drift mismatch, the drift detector's own stale check, the harm mark's cap
// row — is read as still waiting or not by [Notifier.stillWaitingBySubject],
// off that kind's own subject through the same seam driftpass.go and
// harmmark.go already read it by, rather than off a log opening that row
// never had.
// driftpass.go is [Notifier.SweepDriftDetector] — the notifier reading the drift
// detector's store itself, since that store calls nothing, widening a mismatch
// nobody has acknowledged and going on to the next one where a human has, with
// [kindOfMismatch]:
// a mismatch the detector raised because the health monitor's own last check is
// stale is the fourth page condition, a window past its cap that nothing has
// evaluated; a mismatch on the notifier's own last check pages nobody — the
// channel that would carry that page is the thing the mismatch is about, and
// the detector's own delivery, already built, reaches the owner instead — and
// every other mismatch is [KindDriftMismatch] — [Notifier.SweepDriftDetectorStale],
// the notifier's own half of "each of the two processes watches the other" over the
// detector's per-target last check, [Notifier.CatchUpDriftDetectorDelivery],
// appended at the factory's next start for the detector's own delivery, carrying
// its own time; and [Notifier.RecordOwnLastCheck], the notifier's own last check
// beside the health monitor's and the deployer's.
//
// Who may write what: this package owns [DeliveryRowTable], one row per row
// it delivers, and [DeliveryTable], declared and applied but no longer
// written. It appends page events into the decision log through
// [decisionlog.Writer], writes its own last check through [lastcheck.Writer],
// reads the [people] declaration for routing and the drift detector's own store
// for the one wait it has no other caller for, and writes nowhere else.
//
// What defines it:
// ../../end-goal/how-the-factory-works/08-operations/07-pages.md (C2105, C2106,
// C2107, C2108, C2109, C2110, C2111, C2112, C2113, C2114, C2115, C2117, C2118,
// C2119, C2125, C2128, C2129, C2130, C2132, C2133, C2134, C2135, C2136, C2138,
// C2139, C2140, C2142) — the one notifier, the three channels, the delivery
// record, the condition that qualifies for a page, the four page events, the
// single widening, the paging hours, and the drift detector's own page — and
// ../../end-goal/what-humans-do.md (C2853) for the twelve duties the routing
// reads and the obligations outside them. That gates and escalations leave the
// product by mail or chat, and that the page is the third channel and the
// narrow one, is
// ../../end-goal/how-the-factory-works/11-screens/02-three-properties-every-screen-needs.md
// (C2692, C2694).
//
// Routing to the requester or the holder, and an unheld question widening to
// the owner, are
// ../../end-goal/how-the-factory-works/02-intent-into-items/02-the-interview.md
// (C0590, C0600); a holderless wait widening to the owner is
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md
// (C0729); routing a row on the People declaration is
// ../../end-goal/how-the-factory-works/10-fleet/09-what-the-fleet-is-not.md
// (C2554).
//
// One page per mismatch, routed to the installer's obligation, is
// ../../end-goal/how-the-factory-works/08-operations/08-drift-detection.md
// (C2178); pages routing on the duties the declaration holds are
// ../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md
// (C2655); and [Notifier.Resume] overwriting the delivery per waiting row is
// ../../end-goal/one-process.md (C2765).
package notifier
