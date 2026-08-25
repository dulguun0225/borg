// Package people owns the People declaration: which of the owner's twelve
// duties each named human holds, and who holds an obligation that is not one of
// the twelve.
//
// # It arrives with its reader and not with its screen
//
// People is one of the four screens, and the screens are a later milestone. The
// record is here because the notifier routes on it and routing cannot be built
// without it — the arrangement the analysis window's four parameters already had one
// milestone earlier, authorable on the service record before anything read them.
// What writes it until the screen exists is the crude interface.
//
// # Two kinds of holding, and exactly one per row
//
// A row names a [Duty] or an [Obligation] and never both. The duties are the
// twelve of ../../end-goal/what-humans-do.md and are held as numbers here rather
// than as names: the names are in that file, and a second copy of a numbered list
// is two lists able to disagree — which is the defect that file warns about
// itself, every other section citing its duties as bare numbers.
//
// The obligations are the three that fall outside the twelve because they are
// substrate rather than work: hosting the factory, installing the independent
// driftdetector, and composing the fleet. They are here because routing needs a named
// human where a row belongs to no duty, which is what an drift detector's
// mismatch is — installing the drift detector is not one of the twelve, so
// the page it fires reaches whoever this record says installed it. Inventing a
// thirteenth duty instead is what the design refuses: a duty would score an
// obligation the twelve deliberately leave unscored, where a named human only
// has to be reachable.
//
// # The owner is not a row
//
// A page widens to the owner, and who the owner is stays what the install knows:
// the notifier is composed with the name. Nothing here records it, because the
// design gives the owner no record — the owner is the document's reader, the
// person who would have written every row in this table. What that costs is that
// an install with two owners cannot say so, and every widening reaches one of
// them.
//
// # Nothing is enforced, and nothing has to be
//
// A duty with no holder is not an error. A page or a gate row with no holder
// recorded widens to the owner, who is the person that would have written the row
// — so an empty table is a factory where every page reaches the owner, which is a
// working factory and not a misconfigured one. [Writer.Declare] is idempotent on
// the pair, so declaring the same holding twice is one row, and [Writer.Withdraw]
// keeps the row and marks it: a page delivered to a holder who has since stopped
// holding is still readable against the row that routed it.
//
// Who may write what: [Writer] inserts a declaration and withdraws it, and it
// refuses an actor that is not a human — distributing the twelve is the owner's
// and a component doing it would be the factory deciding who holds the factory's
// obligations. Nothing updates the holding on a row and nothing deletes.
//
// What defines it: the twelve duties and the three obligations outside them are
// ../../end-goal/what-humans-do.md; the record and the screen that writes it are
// ../../end-goal/how-humans-do-it/11-screens.md#work-ops-factory-people; and what
// reads it is ../../end-goal/how-humans-do-it/08-operations/07-pages.md, where the
// notifier routes all three channels on it.
package people
