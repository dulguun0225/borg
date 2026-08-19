// Package window owns the watch window record: how long the factory may act on
// the comparison alone, as a record rather than a timer.
//
// # One per release watched, and the comparison writes it
//
// A window is opened for each production deploy of a release its service has not
// watched before, whichever attempt that is. So a rollback opens none — the
// release it returns to was watched already — and neither does a redeploy of one
// already watched. Watched rather than current, because a release condemned at
// the harm exit never completes its deploy and so never becomes current, and the
// window that condemned it has to have been opened over something.
//
// [Writer] is the comparison and there is no other. The comparison opens the
// window when the deploy record is written and closes it once, at exactly one of
// the four exits [Exits] names, because the comparison is what evaluates every
// exit.
//
// # What is on it, and why the parameters are copied
//
// The window names the deploy it was opened over, and through it the release and
// the service. It also stores the size, the confidence, the cap, the boundary's
// formula, and the policy and score versions in force at the open — copied onto
// the record rather than read back later. That is the same rule the gate's
// opening row keeps for the threshold it applied: a reading at an exit is not
// interpretable against anything but the boundary it was actually read against,
// and an owner who re-authors a size while a window is open would otherwise
// change what a window already closed is read to have meant.
//
// [Window.CleanAvailable] is whether the clean exit was reachable at all, which
// is a fact of the open and not of the exit: a release with nothing below it to
// compare against can be condemned by an absolute threshold and can never be
// cleared early, so its window ends at the cap. The field exists so that a window
// ending at the cap is readable as weak protection rather than as a comparison
// that ran out of time.
//
// What is not on it is the control. A control is named on the production deploy
// record, not here — and on a substrate that moves a process rather than traffic
// no control is ever started, so the field would be a column nothing writes.
//
// # The two things a rollback computes from these records
//
// Neither the restore floor nor a rollback's target is written anywhere. The
// release record is written once at the fast-forward and never again, so an
// outcome settled by a window closing long afterwards cannot be a field of it,
// and the fact is already implied by the records that exist.
// [ClosedWithoutHarm] is what both are computed from: every window of the service
// whose exit is clean or at the cap, which are the two exits that count. Closing
// at the cap counts because a release that was never condemned is one the factory
// can return to, and requiring a clean close would leave a service too quiet to
// ever reach one with no target at all. Closing swept does not count: a swept
// release was never condemned either, but nothing is left running its build.
//
// Ordering those windows into a floor and a target is the caller's, because the
// order is the release's number and this package does not read release records.
// Copying the number onto the window would be one fact in two places able to
// disagree, and importing the package that owns it to answer a query about
// windows would make every reader of a window a reader of releases.
//
// [CountOpen] is what K is compared against — how many windows one service holds
// open at once — and the K in force is package policy's read rather than a field
// here.
//
// Who may write what: [Writer] inserts a window and closes it, and nothing
// updates any other field and nothing deletes. deploy_id, release_id, and
// service_id are id fields and not foreign keys, the rule record's doc.go states
// once.
//
// What defines it: the watch window, its four exits, and the parameters resolved
// at the open are ../../end-goal/how-humans-do-it/08-operations.md#the-watch-window;
// K, the restore floor, and a rollback's target are
// ../../end-goal/how-humans-do-it/08-operations.md#overlapping-windows; and the
// boundary the size and confidence resolve to is package boundary.
package window
