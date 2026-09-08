package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/dulguun0225/borg/factory/principal"
)

// ShippedGrouperPrompt is the role prompt the product ships for the grouper,
// the role put on a project rather than on an intent or an item. It is entered
// through the artifact store at the factory's first start beside the others,
// and every version of it is decided at the role-prompt gate, which is what
// puts the words a report is read by under a decision.
//
// [Grouper] runs it. The reply is one group per line, the report ids of that
// group separated by spaces, which is the shape [parseGroups] reads — a shape
// kept to one line per group because a group is a set of ids and carries
// nothing else: what the group is about is authored by the interview, not
// here.
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

// Report is one report as the grouper is given it: its id, whether it is a bug
// report or a complaint, and the words. It carries nothing about a person, the
// channel that wrote it carrying no identity at all, and it is this package's
// own spelling of the fields rather than the report store's row — the store is
// a second database, and an import would cross that boundary.
type Report struct {
	ID   string
	Kind string
	Text string
}

// Grouping is what one [Grouper.Group] call is given: the reports that arrived
// under one project, oldest first. The reports already grouped are among them,
// because deciding which reports are one problem is a decision over all of
// them: a report that arrived after a group was raised is one the role can only
// match against the reports of that group.
//
// It names no intent and no item. The grouper is put on a project and runs
// before there is an intent, and what a group becomes is intake's to write.
type Grouping struct {
	Reports []Report
}

// Groups is what one [Grouper.Group] call produced: the report ids of each
// group the role judged one problem, in the order it replied them, and what the
// call spent per kind the provider counts apart.
type Groups struct {
	Groups [][]string
	Units  map[string]int64
}

// Grouper is the agent in the grouper's role, the one put on a project.
type Grouper struct {
	Model Model
	// Prompt is the role prompt version in force, handed over by the component
	// that dispatched the role. An empty one is [ErrNoPrompt].
	Prompt string
	// Effort is the effort the fleet entry names, handed over by the same
	// component and sent with the call. An empty one asks the provider for
	// none.
	Effort string
}

// Group sends the role prompt and parses the reply into [Groups].
//
// The reports are content: nothing they say changes what this method does with
// the reply, and a reply outside the protocol is [ErrReply] however plausible
// its text. That is the one role reading a stranger's words, which is why rule
// 4 is in the prompt above.
func (g Grouper) Group(ctx context.Context, as principal.Principal, of Grouping) (Groups, error) {
	if g.Prompt == "" {
		return Groups{}, ErrNoPrompt
	}
	if len(of.Reports) == 0 {
		return Groups{}, fmt.Errorf("%w: the grouper was given no report to group", ErrReply)
	}
	var b strings.Builder
	b.WriteString("The reports that arrived under this project, oldest first:\n")
	for _, r := range of.Reports {
		fmt.Fprintf(&b, "%s %s: %s\n", r.ID, r.Kind, r.Text)
	}
	reply, err := g.Model.Complete(ctx, as, Call{System: g.Prompt, User: b.String(), Effort: g.Effort})
	if err != nil {
		return Groups{}, err
	}
	groups, err := parseGroups(reply.Text)
	if err != nil {
		// The refused reply's spend goes back with the error, the way every
		// other role's does: the units were spent whether or not the reply was
		// usable.
		return Groups{Units: reply.Units}, err
	}
	groups.Units = reply.Units
	return groups, nil
}

// parseGroups reads the one form the role's prompt states: a GROUPS: header and
// one group per line, the report ids separated by single spaces. A reply in any
// other form, one stating no group, and one naming a report twice are each
// [ErrReply] — the last because a report is in one group, which is what makes
// the groups a partition rather than a set of overlapping guesses.
func parseGroups(text string) (Groups, error) {
	lines := protocolLines(text)
	if len(lines) == 0 || lines[0] != "GROUPS:" {
		return Groups{}, fmt.Errorf("%w: the grouper's reply does not start with GROUPS:", ErrReply)
	}
	var read Groups
	seen := map[string]bool{}
	for _, line := range lines[1:] {
		group := strings.Fields(line)
		if len(group) == 0 {
			continue
		}
		for _, id := range group {
			if seen[id] {
				return Groups{}, fmt.Errorf("%w: the grouper put %s in two groups", ErrReply, id)
			}
			seen[id] = true
		}
		read.Groups = append(read.Groups, group)
	}
	if len(read.Groups) == 0 {
		return Groups{}, fmt.Errorf("%w: the grouper's reply states no group", ErrReply)
	}
	return read, nil
}
