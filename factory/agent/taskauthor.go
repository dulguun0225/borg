package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/dulguun0225/borg/factory/principal"
)

// ShippedTaskAuthorPrompt is the role prompt the product ships for the task
// author, entered through the artifact store at the factory's first start and
// read in force at dispatch. A task is an internal step of one item and never
// a unit that ships, which is what the prompt's second paragraph refuses.
const ShippedTaskAuthorPrompt = `You divide one item's approved implementation plan into tasks in a software factory.

A task is an internal step of this one item. It has no build, no release number, and no environment of its own, and it never ships by itself: the item is what ships, and the tasks are complete when the implementation is. So a task never says that it is released, deployed, or verified against production, and never proposes splitting the item.

Write one task per line, in the order they are done, each a single sentence saying what is done and where.

Where the user message carries a reject or a rework request, it names what was found wrong with the tasks it decided over. Divide the plan against what was found wrong.

Reply with a TASKS: header and the task lines, and nothing before or after:

TASKS:
<one task per line>

` + Rules

// Tasks is what one [TaskAuthor.Divide] call produced: the task lines as one
// text, the lines themselves, and what the call spent per unit kind.
type Tasks struct {
	Text  string
	Lines []string
	Units map[string]int64
}

// Dividing is what one [TaskAuthor.Divide] call is given: the approved plan,
// the spec it was planned against, and what sent the item back here.
type Dividing struct {
	Plan     string
	Spec     string
	Returned Returned
}

// TaskAuthor is the agent in the tasks stage's role.
type TaskAuthor struct {
	Model Model
	// Prompt is the role prompt version in force, handed over by the component
	// that dispatched the role. An empty one is [ErrNoPrompt].
	Prompt string
	// Effort is the effort the fleet entry names, handed over by the same
	// component and sent with the call. An empty one asks the provider for
	// none.
	Effort string
}

// Divide sends the role prompt and parses the reply.
func (t TaskAuthor) Divide(ctx context.Context, as principal.Principal, of Dividing) (Tasks, error) {
	if t.Prompt == "" {
		return Tasks{}, ErrNoPrompt
	}
	var b strings.Builder
	fmt.Fprintf(&b, "The item's approved implementation plan:\n%s\n", of.Plan)
	if of.Spec != "" {
		fmt.Fprintf(&b, "\nThe spec it was planned against:\n%s\n", of.Spec)
	}
	writeReturned(&b, of.Returned)
	reply, err := t.Model.Complete(ctx, as, Call{System: t.Prompt, User: b.String(), Effort: t.Effort})
	if err != nil {
		return Tasks{}, err
	}
	text, err := parseBlock(reply.Text, "TASKS:", "the task author")
	if err != nil {
		return Tasks{Units: reply.Units}, err
	}
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return Tasks{Units: reply.Units}, fmt.Errorf("%w: the task author divided the plan into no task", ErrReply)
	}
	return Tasks{Text: text, Lines: lines, Units: reply.Units}, nil
}
