package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/dulguun0225/borg/factory/principal"
)

// ShippedPlannerPrompt is the role prompt the product ships for the
// implementation planner, entered through the artifact store at the factory's
// first start and read in force at dispatch. The plan is a document and the
// gate over it takes Edit in place, so what the prompt asks for is prose a
// human can edit rather than a form.
const ShippedPlannerPrompt = `You author the implementation plan of one item in a software factory. From the item's approved spec and the criteria in force for the service, say how the item will be built.

The plan answers the decisions no permanent constraint answers: which files the change adds and which it rewrites, what each is for, what the change must not touch, and the order the work is done in. Where the item has a screen and the design system is silent about part of it, the plan is where that part is decided and says so.

The plan is a document a human may edit in place at its gate, so write it as prose with one paragraph per decision and no headings of your own.

You decide nothing about whether the item is worth building and nothing about the criteria: the spec is approved and you plan against it.

Where the user message carries a reject or a rework request, it names what was found wrong with the plan it decided over. Plan against what was found wrong.

Reply with a PLAN: header and the plan's text, and nothing before or after:

PLAN:
<the plan, free text>

` + Rules

// Plan is what one [Planner.Plan] call produced: the plan's text, and what the
// call spent per unit kind.
type Plan struct {
	Text  string
	Units map[string]int64
}

// Planning is what one [Planner.Plan] call is given: the item's approved spec,
// the criteria in force for the service, and what sent the item back here.
type Planning struct {
	Spec     string
	Criteria []Criterion
	Returned Returned
}

// Planner is the agent in the implementation plan stage's role.
type Planner struct {
	Model Model
	// Prompt is the role prompt version in force, handed over by the component
	// that dispatched the role. An empty one is [ErrNoPrompt].
	Prompt string
}

// Plan sends the role prompt and parses the reply.
func (p Planner) Plan(ctx context.Context, as principal.Principal, of Planning) (Plan, error) {
	if p.Prompt == "" {
		return Plan{}, ErrNoPrompt
	}
	var b strings.Builder
	fmt.Fprintf(&b, "The item's approved spec:\n%s\n", of.Spec)
	if len(of.Criteria) > 0 {
		b.WriteString("\n")
		writeCriteria(&b, "The criteria in force for the service:", of.Criteria)
	}
	writeReturned(&b, of.Returned)
	reply, err := p.Model.Complete(ctx, as, p.Prompt, b.String())
	if err != nil {
		return Plan{}, err
	}
	text, err := parseBlock(reply.Text, "PLAN:", "the implementation planner")
	if err != nil {
		return Plan{Units: reply.Units}, err
	}
	return Plan{Text: text, Units: reply.Units}, nil
}

// parseBlock reads a reply that is one header line and the text under it,
// which is the protocol the plan and the tasks share. A reply whose first line
// is not the header, and one with no text under it, are each [ErrReply].
func parseBlock(text, header, role string) (string, error) {
	lines := protocolLines(text)
	if len(lines) == 0 {
		return "", fmt.Errorf("%w: %s replied nothing", ErrReply, role)
	}
	if lines[0] != header {
		return "", fmt.Errorf("%w: %s's reply does not start with %s", ErrReply, role, header)
	}
	body := strings.TrimSpace(strings.Join(lines[1:], "\n"))
	if body == "" {
		return "", fmt.Errorf("%w: %s's reply carries no text under %s", ErrReply, role, header)
	}
	return body, nil
}
