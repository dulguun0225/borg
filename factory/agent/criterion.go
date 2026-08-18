package agent

import (
	"fmt"
	"strings"
)

// Criterion is one criterion as a role is told it: the stable id an encoding
// names, so the build says which criterion the encoding decides, and the
// sentence the encoding's expected behaviour is derived from. Nothing else on
// the stored record is any role's business — a role writes no record, and the
// pattern, the actor, and the spec version that introduced it decide nothing
// either role does.
type Criterion struct {
	ID       string
	Sentence string
}

// writeCriteria renders criteria into a role's user message under heading, one
// per line as the id, a colon, a space, and the sentence. Both roles render
// the set through this one function, so the two prompts cannot describe the
// same lines differently.
func writeCriteria(b *strings.Builder, heading string, criteria []Criterion) {
	b.WriteString(heading)
	b.WriteString("\n")
	for _, c := range criteria {
		fmt.Fprintf(b, "%s: %s\n", c.ID, c.Sentence)
	}
}
