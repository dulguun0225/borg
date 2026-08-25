// Package notifier is the one component that delivers everything waiting on a
// human out of the product, on three channels: mail, chat, and the page.
//
// # One component, three channels, one routing
//
// All three route the same way — on the [people] declaration, by the duty the
// wait belongs to, or by the named human where it belongs to none — so routing is
// implemented once rather than beside each thing that waits. Its callers are the
// components that create a wait: the gate component when it opens a decision a
// human must close, intake when it writes a round of interview questions or an
// intent escalated, dispatch when it writes an item escalated, the health monitor when
// it reports a rollback, and an owner firing one from Ops.
//
// What a channel does is [Deliverer], an interface its caller implements, because
// what mail is on a self-hosted install is the owner's arrangement and not the
// factory's. What one component costs is that a fault in it stops all three at
// once and the factory falls back to whoever remembers to open Work.
//
// # A page is the narrow channel, and the condition is the test
//
// What qualifies for a page is a wait where the deployed software is worse until a
// human ends it, and nothing else fires one. That condition is the test and the
// list of things meeting it is derived from it, not the other way round — so
// [Kind] carries what the condition answers for each kind of wait rather than a
// flag its caller sets freely:
//
//   - [PagesNever]: the condition cannot be met by this kind. A UAT assignment
//     delays its own item, an escalation on a feature item has nothing live that is
//     worse, a deploy queued behind open windows at the window limit is waiting on
//     the factory, and a
//     rollback the factory performed is reported rather than requested — the factory
//     does not page to inform.
//   - [PagesAlways]: the condition is met by definition. A mismatch the
//     drift detector found holds that service's production deploys and does
//     not lift itself; a page
//     an owner fires from Ops is their judgment that production is worse, and nothing
//     scores it.
//   - [PagesIfWorse]: the condition depends on what the wait is about, and
//     [Wait.Worse] is the caller's answer. An escalation is the case: the factory
//     giving up on a defect that is live meets it, and giving up on a feature nobody
//     is running does not, which is read off the intent's source and not off a list
//     of the three sources an intent may have.
//
// A caller that sets Worse on a kind the condition cannot be met by is refused,
// and so is one that clears it on a kind the condition is met by definition. That
// makes "nothing else fires one" mechanical wherever the design settles it, and
// leaves the condition itself as the test wherever it does not.
//
// # A page is a sequence and not a record
//
// A page is the name for the sequence of page events on one wait. A page event is
// one append to the log naming the wait, the human reached, and which of three it
// is: reached, widened, answered. [Notify] writes one reached row per holder — the
// first delivery reaches every human holding the duty at once, there being no
// rotation and no narrower first recipient. [Widen] writes one widened row, to the
// owner, and refuses a second: unanswered a page widens exactly once. [Answered] is
// written when the wait stops waiting.
//
// Mail and chat write nothing. A delivery that changes no state is no evidence,
// and a record per delivery would size the log by how often the factory notifies.
//
// A duty with no holder is a routing answer and not a missing one: the page
// reaches the owner directly, who is the person that would have written the row.
// The owner is composed into this component rather than read from the declaration,
// because the design gives the owner no record — so an install with two owners
// cannot say so, and every widening reaches one of them.
//
// # Answered is the caller's, with one exception the store forced
//
// The component that ends a wait calls [Answered] at the same write it ends it
// with, so a caller which fails to make that call leaves a page widening
// against a wait already answered. One wait ends where nothing calls: a
// mismatch the drift detector found is cleared by a human inside the
// drift detector's own store, which no factory component may write and
// which calls nothing. So [Answered] for that one is called by whoever reads
// that store and finds the mismatch cleared, and the event's time is the time
// of that read rather than of the clearing.
//
// Who may write what: this package owns no table. It appends page events into the
// decision log through [decisionlog.Writer] and reads the [people] declaration, and
// it writes nowhere else.
//
// What defines it: ../../end-goal/how-humans-do-it/08-operations.md#pages — the one
// notifier, the three channels, the condition that qualifies for a page, the three
// page events, and the single widening — and
// ../../end-goal/what-humans-do.md for the twelve duties the routing reads and the
// obligations outside them.
package notifier
