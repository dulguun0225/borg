// Package window owns the analysis window record: how long the factory may act on
// the health monitor alone, as a record rather than a timer.
//
// # One per release watched, and the health monitor writes it
//
// A window is opened for each production deploy of a release its service has not
// watched before, whichever attempt that is. So a rollback opens none — the
// release it returns to was watched already — and neither does a redeploy of one
// already watched. Watched rather than current, because a release failed at
// the failed exit never completes its deploy and so never becomes current, and the
// window that failed it has to have been opened over something.
//
// [Writer] is the health monitor and there is no other. The health monitor opens the
// window when the deploy record is written and closes it once, at exactly one of
// the four exits [Exits] names, because the health monitor is what evaluates every
// exit.
//
// # What is on it, and why the parameters are copied
//
// The window names the deploy it was opened over, and through it the release and
// the service. It also stores the size, the confidence, the cap, the boundary's
// formula, and the policy and score versions in force at the open — copied onto
// the record rather than read back later. That is the same rule the gate's
// open event keeps for the threshold it applied: a reading at an exit is not
// interpretable against anything but the boundary it was actually read against,
// and an owner who re-authors a size while a window is open would otherwise
// change what a window already closed is read to have meant.
//
// [Window.PassedAvailable] is whether the passed exit was reachable at all, which
// is a fact of the open and not of the exit: a release with nothing below it to
// compare against can be failed by an absolute threshold and can never be
// passed early, so its window ends at the cap. The field exists so that a window
// ending at the cap is readable as weak protection rather than as a comparison
// that ran out of time.
//
// [Window.HeldOut] is the second way the passed exit becomes unreachable and the
// reason one
// field could not carry both. The score's own sample selects an item and runs its
// release to the cap rather than stopping where the boundary would allow —
// auto-passing a change the score wanted gated is where the factory is most openly
// guessing, so it takes the longest watch available. A window that ran to the cap
// for that reason and one that ran to the cap for want of a baseline are not the
// same window, and a reader holding only PassedAvailable could not tell them apart.
// Both are the caller's to hand over: what the score selected is read off the
// decisions on the item, which this package does not read.
//
// [Window.ClosedOn] is the read the window closed on: what the release served and
// failed, and what its baseline did. Without it the record stored the boundary a
// window was read against and no reading to read against it, so an exit could not
// be recomputed from the numbers it was decided on — which is the rule
// [boundary.Reading] keeps one level down and this record was not keeping. The
// skipped exit carries none, that close being the one that is not a reading: a
// rollback aimed below the release ended the window and nothing was evaluated.
//
// It is also what makes the traffic a service actually receives arithmetic. The
// score reads it to answer whether a size it is asking for is reachable inside the
// cap at all, which is a question about volume rather than about harm.
//
// What is not on it is the control. A control is named on the production deploy
// record, not here — and on a substrate that moves a process rather than traffic
// no control is ever started, so the field would be a column nothing writes.
//
// # The two things a rollback computes from these records
//
// Neither the last known-good release nor a rollback's target is written
// anywhere. The release record is written once at the fast-forward and never
// again, so an outcome settled by a window closing long afterwards cannot be a
// field of it, and the fact is already implied by the records that exist.
// [Closed] is beside them and answers a different question: every closed window of
// every service, which is what the score learns from — the subjects it supplies a
// value for are the services the windows name, so a reader asking per service would
// first have to be told which services to ask about. [ClosedWithoutFailing] is what
// both of the two below are computed from: every window of the service
// whose exit is passed or timed out, which are the two exits that count. Timing
// out counts because a release that was never failed is one the factory can
// return to, and requiring a passed close would leave a service too quiet to ever
// reach one with no target at all. CloseEvent skipped does not count: a skipped
// release was never failed either, but nothing is left running its build.
//
// Ordering those windows into a last known-good release and a target is the
// caller's, because the order is the release's number and this package does not
// read release records.
// Copying the number onto the window would be one fact in two places able to
// disagree, and importing the package that owns it to answer a query about
// windows would make every reader of a window a reader of releases.
//
// [CountOpen] is what the window limit is compared against — how many windows one
// service holds open at once — and the limit in force is package policy's read
// rather than a field
// here.
//
// Who may write what: [Writer] inserts a window and closes it, and nothing
// updates any other field and nothing deletes. deploy_id, release_id, and
// service_id are id fields and not foreign keys, the rule record's doc.go states
// once.
//
// What defines it: the analysis window, its four exits, and the parameters resolved
// at the open are ../../end-goal/how-humans-do-it/08-operations.md#the-analysis-window;
// the window limit, the last known-good release, and a rollback's target are
// ../../end-goal/how-humans-do-it/08-operations.md#overlapping-windows; and the
// boundary the size and confidence resolve to is package boundary.
package window
