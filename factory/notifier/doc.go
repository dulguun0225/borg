// Package notifier is the one component that delivers everything waiting on a
// human out of the product, on three channels: mail, chat, and the page.
//
// wait.go is the vocabulary. [Channel] and [Channels] are the three; [Event] and
// [Events] are the three page events; [Kind] is a kind of wait and [Kinds] maps
// each to what the page's condition answers for it — [PagesNever],
// [PagesAlways], or [PagesIfWorse], where [Wait.Worse] is the caller's answer.
// [Wait] is what waits and whom it routes to, validated by the errors beside it:
// a caller that sets Worse on a [PagesNever] kind is refused with
// [ErrWorseRefused], and so is one that clears it on a [PagesAlways] kind, which
// is what makes "nothing else fires a page" mechanical. [Deliverer] is what a
// channel does and [Delivery] is what it is handed; it is an interface its
// caller implements, because what mail is on a self-hosted install is the
// owner's arrangement and not the factory's.
//
// notifier.go is [Notifier] and [New], composed with the log, a [Deliverer], and
// the owner's name — the owner is composed in rather than read from the [people]
// declaration, because the design gives the owner no record. All three channels
// route the same way, on that declaration by the duty the wait belongs to or by
// the named human where it belongs to none, so routing is implemented once
// rather than beside each thing that waits. [Notifier.Notify] writes one reached
// row per holder of the duty, [Notifier.Widen] writes one widened row to the
// owner and refuses a second with [ErrAlreadyWidened], and [Notifier.Answered]
// is called by whatever ends the wait, at the same write it ends it with.
// [Payload] of [PageEventKind] is a page event's shape and [EventsFor] reads
// them back. Mail and chat write nothing.
//
// Who may write what: this package owns no table. It appends page events into the
// decision log through [decisionlog.Writer] and reads the [people] declaration, and
// it writes nowhere else.
//
// What defines it: ../../end-goal/how-the-factory-works/08-operations/07-pages.md — the one
// notifier, the three channels, the condition that qualifies for a page, the three
// page events, and the single widening — and
// ../../end-goal/what-humans-do.md for the twelve duties the routing reads and the
// obligations outside them.
package notifier
