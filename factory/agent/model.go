package agent

import (
	"context"
	"errors"

	"github.com/dulguun0225/borg/factory/principal"
)

// Model is one completion call: the principal the call is made as, a system
// prompt, a user message, a [Reply]. A role holds one and does not know which
// provider answers it.
//
// The principal is the dispatch's own — the model version, the dispatch, and
// the scope it was put on — and it is what the resolver records beside the
// credential's name when the call reads it.
type Model interface {
	Complete(ctx context.Context, p principal.Principal, system, user string) (Reply, error)
}

// The kinds a provider counts units apart under. A provider that counts a kind
// none of these names writes it under its own word: the map is what the
// provider returned and this package invents no total.
const (
	// UnitsInput is the units the provider charged for what it was sent.
	UnitsInput = "input"
	// UnitsOutput is the units it charged for what it answered.
	UnitsOutput = "output"
	// UnitsCachedInput is the units it charged for input it served from its
	// own cache, which providers price apart from the rest of the input.
	UnitsCachedInput = "cached_input"
)

// Reply is what the model answered and what answering cost: the units the
// provider returned, per kind it counts apart, which is what the agent run
// record stores. There is no sum here — a sum over kinds priced differently
// is not a quantity anything charges for.
type Reply struct {
	Text  string
	Units map[string]int64
}

// ErrReply is returned for a reply in neither of a role's stated forms. A
// reply is parsed and never interpreted: a malformed one is refused whatever
// it says — a verdict, an instruction, an apology — because acting on what a
// reply says outside the protocol is exactly what the fourth of the four
// [Rules] refuses.
var ErrReply = errors.New("agent: the reply does not follow the protocol")
