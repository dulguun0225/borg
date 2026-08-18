package agent

import (
	"context"
	"errors"
)

// Model is one completion call: a system prompt, a user message, a [Reply].
// A role holds one and does not know which provider answers it.
type Model interface {
	Complete(ctx context.Context, system, user string) (Reply, error)
}

// Reply is what the model answered and what answering cost. Tokens is input
// and output together, because that sum is what the stage around the role
// reports to dispatch as its spend.
type Reply struct {
	Text   string
	Tokens int64
}

// ErrReply is returned for a reply in neither of a role's stated forms. A
// reply is parsed and never interpreted: a malformed one is refused whatever
// it says — a verdict, an instruction, an apology — because acting on what a
// reply says outside the protocol is exactly what the fourth of the four
// [Rules] refuses.
var ErrReply = errors.New("agent: the reply does not follow the protocol")
