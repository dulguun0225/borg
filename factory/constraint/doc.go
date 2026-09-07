// Package constraint owns the constraint record of the document kind: what an
// owner supplies that binds every item inside its reach — the factory, one
// project, one area, or one intent — from arrival until an owner withdraws
// it.
//
// # The code
//
// schema.go holds [Table] — named constraint_record because constraint is a
// reserved word in SQL — [IDPrefix], [FormatVersion], [Kind] with [Kinds],
// [Reach] with [Reaches], and [DDL]. writer.go holds [Constraint] as it is
// stored, [Constraint.InForceAt], [CalendarDate] with [CalendarDate.StartIn]
// and [CalendarDate.EndIn], [New], [Writer] and [NewWriter], and
// [Writer.Arrive], [Writer.Withdraw] and [Writer.Replace]. read.go holds
// [Get], [Over], [InForce], [InForceForInterview], [Permanent] and
// [DueForReview].
//
// A constraint is never edited: [Writer.Withdraw] keeps the row and sets
// withdrawn_at, and a correction is [Writer.Replace] — one transaction that
// withdraws the record it replaces and inserts the new one naming it in
// ReplacesID — so what was in force when a stage was authored is
// reconstructible from arrival and withdrawal alone.
//
// Reach decides what drafting reads: [InForce] is the factory's own, the
// project's, any area on the chain, and the intent's own — the set a
// drafting stage reads over an item — and [InForceForInterview] is
// deliberately narrower, the factory's own and the intent's own alone,
// because before decomposition which project the work is in is a guess.
// [Permanent] is every constraint in force whose reach is not one intent,
// which is Factory's list of the permanent ones. [DueForReview] is every
// constraint not withdrawn whose review date has ended and which no
// replacement names — the row Work shows for whoever holds duty 2.
//
// Kind decides where a constraint is checked. The design names six kinds;
// this milestone builds [KindDocument] alone — checked by nobody, enforced
// by a safeguard putting a human at a gate. RequiresSeam5Enforced is the one
// thing a document-kind constraint makes dispatch read: an item under it
// waits at dispatch until the factory-wide settings record says seam 5 is
// enforced. Widening [Kinds] and the CHECK in [DDL] to the other five —
// build, predicate, factory-parameter, and notice — is M12's.
//
// BindsFrom and ReviewBy are a [CalendarDate] each: a date and the IANA zone
// it was authored in, both present or both absent. [CalendarDate.StartIn]
// and [CalendarDate.EndIn] take the [time.Location] the caller loaded from
// the zone, so a caller reading both from the same constraint loads it once.
// [Constraint.InForceAt] is what reads BindsFrom this way; nothing here
// reads ReviewBy for anything but [DueForReview].
//
// # Who may write what
//
// [Writer] is intake's: whichever screen an owner supplied the constraint on,
// intake calls [Writer.Arrive], [Writer.Withdraw] or [Writer.Replace] with
// the owner as actor, because the constraint's own rules are implemented
// once here rather than once per entrance. Every method refuses an actor
// that is not [record.KindHuman] with [ErrActorNotHuman]: intake calls this
// writer, but the actor it validates is the owner, never intake itself.
// [Writer.Arrive] is called from Factory for a constraint whose reach is one
// of the three widest, and from Work for one whose reach is an intent. The
// second writer ../../end-goal/records.md declares is not built, and What is
// not built says why.
//
// # What is not built
//
// The second writer ../../end-goal/records.md declares: the factory itself, at
// its first start after an upgrade that changed the design system it ships.
// [validateHuman] refuses it by construction — every write here requires
// [record.KindHuman], so the factory writing one would be a component storing
// itself as the owner who supplied it — and what such a write would carry, the
// design system as a constraint's content, is unbuilt below.
//
// The other five kinds; the pass over the constraints in force that decides
// a build against them; the design system as a constraint's content field,
// derived from its build; the period a law binds the factory's own conduct
// to, its trigger, and the deadline intake would write onto an intent from
// it; the detector that raises a conformance intent where a permanent
// constraint's reach departs from what is shipped; and the review-by row on
// Work, which reads [DueForReview] and is not built either. Each is M12's or
// later.
//
// What defines it: the reach, the kind, the three optional fields a law
// carries, and the design system as a constraint's content are
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/01-constraints-and-the-design-system.md.
// Supplying a constraint is duty 2 of
// ../../end-goal/what-humans-do.md.
package constraint
