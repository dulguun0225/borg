// Package grouper owns the pass that turns reports into intents: it reads the
// reports that arrived under one project, dispatches the grouper role over
// them, and applies the answer, so five hundred reports of one slow button are
// one intent and not five hundred.
//
// It owns no table. What it writes is written through the packages that own
// the records: the intent through intake, the link from a report to its intent
// through the report store, and the page a harm mark fires through the
// notifier.
//
// # The files
//
// grouper.go is [Fleet], [Services], [Decompositions] and [Admission] with
// [AdmitsEverything], the four seams the composition supplies; [Composition]
// and [Grouper] with [New]; [Grouped], what one pass did; and [Grouper.Pass].
// grouping.go is what one group causes: apply with joining and dropEmptied,
// [Grouper.finished], raise, alreadyPaged and page, and statementOf with the
// bound on how many reports a statement quotes.
//
// It is a package and not a pass inside the composition because a doc.go under
// cmd/ cites no claim, and the claims below need a home.
//
// # What one pass does
//
// A report raises an intent on arrival rather than waiting for a batch: the
// composition hands each accepted report to [Grouper.Pass] at once, so the
// first report of a group raises its intent and a later matching one attaches
// without a second pass over the whole project. Running this on an interval
// as well is the catch-up a restart needs — a report accepted while the
// factory was down reaches no arrival to hand it off, and the interval is
// what finds it — and every other tick of it costs nothing, arrival having
// already read what there was to read.
//
// The pass reads how many of the project's reports are linked to no intent
// before it reads any words, so a call with nothing to group makes no model
// call and appends no read event. Where there is something, every report of
// the project is read — the grouped ones beside the ungrouped ones, a report
// that arrived after a group was raised being one the role can only match
// against the reports of that group — and the role is dispatched over all of
// them through [Fleet].
//
// Each group the role answers with is applied against the reports it names, in
// the order they arrived. An intent belongs to the group holding the report
// that raised it — its statement was written over that report — so a group
// claims an intent only where that report is a member of it, and a group naming
// an intent whose first report it does not hold is a group the role split off.
// Where a group claims none, it raises one through intake, whose statement
// summarizes the reports; where it claims one that has not finished, every
// member not already in it joins it and raises its count, and nothing about the
// intent is rewritten.
//
// A member joins by a link where it was in no group, and by a move where it was
// in another — which is the split, and what a group the role got wrong being
// corrected writes. An intent a split leaves holding no report names nothing,
// its statement summarizing reports that are somewhere else, and it is ended
// through intake.
//
// # Decomposition is the boundary
//
// The split reaches only as far as decomposition. A member whose own intent has
// items does not move: after that boundary a report matching work already
// decomposed attaches and raises the count rather than being taken out of it,
// and what repairs a group the role got wrong from there is the repair the
// design gives it one stage down, where decomposition yields an item per
// problem or a spec comes back too large. [Decompositions] is what says which
// side of the boundary an intent is on, read once per intent a group names and
// not at all for a group of arriving reports.
//
// A group naming an intent whose timeline is finished — delivered or dropped —
// raises a new intent linked to that one as a recurrence. The fix shipped, so
// evidence that it did not work is a new intent and never a reopening. A
// group that claims no intent but held a member the boundary above left
// behind takes the same recurrence: the member does not move, so the new
// intent the rest of the group raises is linked to the decomposed intent it
// was judged against and left in, rather than naming nothing. What the
// boundary costs is a run of reports just after a narrow decomposition
// showing as linked timelines where a human would call it one problem.
//
// A report the reply left in no group stays ungrouped and is counted as such at
// Factory, which is the answer to a report nobody sees; the next pass reads it
// again.
//
// A harm-marked report grouped into an intent already carrying a page raises
// no second one: the notifier's own delivery record for that intent is read
// before [notifier.Notify] is called, so a later pass finding the mark on
// another of the intent's reports pages nothing further. It is one page per
// intent and never per report, however many passes it takes to see them all.
//
// Nothing waits for a batch or a count. Grouping is deduplication and not
// triage: one report from one end user is an intent, and the count of reports
// under one is evidence on it rather than a rank this package orders anything
// by.
//
// # What it does not do
//
// It fires no gate and writes no artifact version. Grouping takes no gate and
// moves no per-author prior, and both are absences held by a test in the
// composition rather than by code here: this package reaches neither the gate
// component nor the artifact store, and its line in ../../deps.txt is what
// says so.
//
// The two safeguards on the report store are the composition's to read.
// [Admission] is whether an arrived report waits for a human before this pass
// may read it: with one in force the pass reads the admitted reports alone, so
// a report nobody has admitted is never grouped and its words never leave the
// install. That is the whole of what this pass does about a safeguard — the
// second, which holds a report-derived intent until a human admits it at Work,
// stops the component that puts an agent on it and never this one, which has
// already finished by then.
//
// # Who may write what
//
// Nothing of its own, and every write goes through the record's own writer.
// The intent is intake's, written as the actor the composition names —
// ../../end-goal/components.md gives the grouper no row, it being an agent and
// not a component, so the actor on the row and the principal a read event names
// are the composition's to supply and not names this package coins. The link
// from a report to the intent it was grouped into is the report store's, which
// keeps the report rather than deleting it: the rate of reports before and
// after the release meant to fix what they describe is what keeping them makes
// possible.
//
// [Fleet] is an interface because the material an input manifest names and the
// reply an agent parses are the fleet's own vocabulary, and a pass that named
// either would import the whole of what runs an agent to make one call. What
// the composition wires it to is the dispatch of the role put on a project,
// which is what writes the run record naming the project and the processing
// location the credential resolved to, and the input manifest beside it.
//
// What defines it: the grouper, what a group raises and what a later report
// attaches to, the split before decomposition and the boundary it stops at, the
// recurrence, the statement, the page a harm mark fires, the admitted report
// being the only one it reads, and the two absences are
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/02-reports.md
// (C0377, C0382, C0384, C0385, C0393, C0395, C0396, C0397, C0398, C0399,
// C0400, C0432, C0439); the agent that calls intake before an intent exists
// is
// ../../end-goal/components.md (C0031).
//
// The grouper running before an intent exists, scoped by the project whose
// reports it reads, is
// ../../end-goal/how-the-factory-works/01-one-pipeline.md (C0168).
//
// One page per intent however many marked reports is
// ../../end-goal/how-the-factory-works/08-operations/07-pages.md (C2126).
package grouper
