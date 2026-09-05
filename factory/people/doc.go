// Package people owns the People declaration: which of the owner's twelve
// duties each named human holds, and the named human for an obligation outside
// the twelve.
//
// people.go holds the vocabulary and the calls. A row names a [Duty] or an
// [Obligation] and never both, which is what [Holding] carries — [OfDuty] and
// [OfObligation] compose one. A duty is held as its number rather than its name,
// because the names are in ../../end-goal/what-humans-do.md and a second copy is
// two lists able to disagree. [Duties] is the twelve and [Obligations] the
// three: hosting the factory, installing the drift detector, and composing the
// fleet. [Declaration] is one row and [Declaration.Holds] whether it still
// stands. [Holders] is the notifier's read, [ByHolding] and [Get] read one row,
// and [All] is what the crude interface prints.
//
// Nothing is enforced and nothing has to be: a duty with no holder is not an
// error, and an empty table is a factory where every page reaches the owner. The
// owner is not a row here — the notifier is composed with the name.
//
// schema.go is [Table], [IDPrefix] and [DDL], whose CHECK holds the same twelve
// duties and the same three obligations the code names.
//
// Who may write what: [Writer] inserts a declaration and withdraws it, and it
// refuses an actor that is not a human with [ErrNotAnOwner]. [Writer.Declare] is
// idempotent on the pair, so declaring the same holding twice is one row, and
// [Writer.Withdraw] keeps the row and marks it, so a page delivered to a holder
// who has since stopped holding is still readable against the row that routed
// it. Nothing updates the holding on a row and nothing deletes.
//
// What defines it: the twelve duties and the three obligations outside them are
// ../../end-goal/what-humans-do.md; the record and the screen that writes it are
// ../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md; and what
// reads it is ../../end-goal/how-the-factory-works/08-operations/07-pages.md, where the
// notifier routes all three channels on it.
package people
