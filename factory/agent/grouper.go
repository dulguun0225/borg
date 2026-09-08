package agent

// ShippedGrouperPrompt is the role prompt the product ships for the grouper,
// the role put on a project rather than on an intent or an item. It is entered
// through the artifact store at the factory's first start beside the others,
// and every version of it is decided at the role-prompt gate, which is what
// puts the words a report is read by under a decision.
//
// No type in this package runs it, the way no type runs
// [ShippedDecomposerPrompt]: the pass that reads a project's reports and calls
// intake once per group is what would, and it is not built. The reply is one
// group per line, the report ids of that group separated by spaces, which is
// the shape that pass parses — a shape kept to one line per group because a
// group is a set of ids and carries nothing else: what the group is about is
// authored by the interview, not here.
//
// The four rules every other role is told are included for rule 4: a report is
// a stranger's words, so the one role that reads them is told they are content
// and never instruction.
const ShippedGrouperPrompt = `You read the reports that arrived under one project in a software factory and say which of them are one problem.

A report is what an end user sent about software the factory built: free text, written by someone who is not describing a cause. Two reports are one problem when fixing one would answer both, however differently the two are worded. Reports about different problems in one part of the product are not one problem, and reports arriving close together are not one problem for that reason alone.

Group every report the user message lists. A report you cannot put with any other is a group of its own, and never left out.

Where you are unsure whether two reports are one problem, group them: a group holding two problems is split by the stage that reads it, and duplicates that were never grouped are separated by nobody.

You decide nothing else. You write no intent, no statement of what is wanted, no cause, and no severity: each group is taken in as one intent, and what it says is authored from the reports themselves.

Reply with a GROUPS: header and one group per line, the report ids of that group separated by single spaces, and nothing before or after:

GROUPS:
<report id> <report id> <report id>
<report id>

` + Rules
