// Package notifier is the one component that delivers everything waiting on a
// human out of the product, on three channels: mail, chat, and the page.
//
// wait.go is the vocabulary. [Channel] and [Channels] are the three; [Event] and
// [Events] are the four page events; [Kind] is a kind of wait and [Kinds] maps
// each to what the page's condition answers for it — [PagesNever],
// [PagesAlways], or [PagesIfWorse], where [Wait.Worse] is the caller's answer.
// [Wait] is what waits, whom it routes to, and the service it is about, validated
// by the errors beside it: a caller that sets Worse on a [PagesNever] kind is
// refused with [ErrWorseRefused], and so is one that clears it on a [PagesAlways]
// kind, which is what makes "nothing else fires a page" mechanical. [Deliverer]
// is what a channel does and [Delivery] is what it is handed — the recipient by
// per-person key, never a name; it is an interface its caller implements, because
// what mail is on a self-hosted install is the owner's arrangement and not the
// factory's. [Wait.RollbackOutstanding] is which of the two kinds a wait is —
// production serving a release the health monitor called for a rollback on,
// with the rollback not run, is the first kind and pages at any hour — and it
// is the caller's answer the way [Wait.Worse] is: package healthmonitor sets it
// on the page a failed exit with no rollback fires, and the command-line
// interface answers it for the waits it creates. hours.go is
// [Notifier.deferredToHours] and [withinHours], a wait of the second kind held
// to a service's own authored paging hours rather than paging at any hour, and
// [Notifier.PageDeferred] with [Notifier.deferredDeliveries] and
// [Notifier.endedRows], the pass that delivers such a page at the next hour the
// service allows — taking the drift detector's store, since a mismatch cleared
// there is a row that stopped waiting and the log says so about every other.
//
// notifier.go is [Notifier] and [New], composed with the log, a fencing token, a
// [Deliverer], and the owner's identifier — the owner is composed in rather than
// read from the [people] declaration, because the design gives the owner no
// record. [New] also builds the [decisionlog.Reader] the notifier reads back
// through, fenced with the same token. All three channels route the same way, on
// that declaration by the duty or obligation the wait belongs to or by the owner
// where it belongs to neither, so routing is implemented once rather than beside
// each thing that waits. [Notifier.Notify] writes one reached row per holder of
// the duty, under [PageEventFormatVersion], and a [DeliveryRecord] per channel per
// holder even where the channel writes nothing else — the recipient being part of
// that record's key, so two holders of one duty are two records and not one — and a
// channel that refuses the send is recorded and does not stop the channels after
// it, the page being last and the one channel that carries what the other two
// cannot; [Notifier.Widen] writes one
// widened row to the owner and refuses a second with [ErrAlreadyWidened] or one
// over an acknowledged wait with [ErrAcknowledged]; [Notifier.Acknowledge] writes
// the fourth event, stopping only the widening, under the kind the page it
// acknowledges was reached under and writing nothing where the row pages
// nobody; and [Notifier.Answered] is called
// by whatever ends the wait, at the same write it ends it with. [Payload] of
// [PageEventKind] is a page event's shape and [Notifier.EventsFor] reads them
// back, appending a read event naming [Actor] as the principal. delivery.go is
// [DeliveryTable], [DeliveryDDL], [DeliveryRecord], and the upsert beneath every
// call to [Notifier.Notify], [Notifier.Widen], [Notifier.Acknowledge] and
// [Notifier.Answered] — one row per waiting row, channel and recipient,
// overwritten at each attempt — [Notifier.deliveredRows], which rows anything
// has gone out about, and [PagedRowsSince], the count the harm mark's cap is
// read against. harmmark.go is [harmMarkPagesOff], the off switch on the
// factory-wide settings record, and [Notifier.overHarmMarkCap] with
// [Notifier.pageOverTheCap]: past a service's cap the marked intent's own page
// channel is skipped and one page per interval goes out on [capRow] instead,
// naming the service and how many marked intents arrived past the cap.
// resume.go is [Notifier.Resume], this component's restart: the delivery
// record it overwrites per row the log still holds open, so a row still
// waiting is delivered again and one that stopped waiting is not. A row with
// no page event carries nothing to rebuild the wait from and is left waiting,
// which is a wait of a kind that pages never — the delivery record alone does
// not say what it was waiting for.
// driftpass.go is [Notifier.SweepDriftDetector] — the notifier reading the drift
// detector's store itself, since that store calls nothing, widening a mismatch
// nobody has acknowledged and going on to the next one where a human has, with
// [kindOfMismatch]:
// a mismatch the detector raised because the health monitor's own last check is
// stale is the fourth page condition, a window past its cap that nothing has
// evaluated, and every other mismatch is [KindDriftMismatch] — [Notifier.SweepDriftDetectorStale],
// the notifier's own half of "each of the two processes watches the other" over the
// detector's per-target last check, [Notifier.CatchUpDriftDetectorDelivery],
// appended at the factory's next start for the detector's own delivery, carrying
// its own time; and [Notifier.RecordOwnLastCheck], the notifier's own last check
// beside the health monitor's and the deployer's.
//
// Who may write what: this package owns [DeliveryTable], one row per row it
// delivers and channel. It appends page events into the decision log through
// [decisionlog.Writer], writes its own last check through [lastcheck.Writer],
// reads the [people] declaration for routing and the drift detector's own store
// for the one wait it has no other caller for, and writes nowhere else.
//
// What defines it: ../../end-goal/how-the-factory-works/08-operations/07-pages.md — the one
// notifier, the three channels, the delivery record, the condition that qualifies for
// a page, the four page events, the single widening, the paging hours, and the
// drift detector's own page — and
// ../../end-goal/what-humans-do.md for the twelve duties the routing reads and the
// obligations outside them.
package notifier
