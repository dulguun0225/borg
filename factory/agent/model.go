package agent

import (
	"context"
	"errors"

	"github.com/dulguun0225/borg/factory/principal"
)

// Model is one completion call: the principal the call is made as, a [Call],
// and a [Reply]. A role holds one and does not know which provider answers it.
//
// The principal is the dispatch's own — the model version, the dispatch, and
// the scope it was put on — and it is what the resolver records beside the
// credential's name when the call reads it.
type Model interface {
	Complete(ctx context.Context, p principal.Principal, call Call) (Reply, error)
}

// Call is what one completion sends: the system prompt, the user message, and
// the effort. It is a struct and not three arguments because all three are
// strings and a caller that swapped two would compile.
type Call struct {
	System string
	User   string
	// Effort is how long the model works before it answers, as the fleet entry
	// names it, and empty where the entry names none. Each implementation sends
	// it in the field its own request shape has for it. The factory does not
	// check that the credential's provider offers what the entry asks for, the
	// way it does not check a quota: an entry asking for an effort nobody
	// offers fails at the provider's own answer, which is where an exhausted
	// account fails too.
	Effort string
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
