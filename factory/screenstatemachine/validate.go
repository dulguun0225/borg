package screenstatemachine

import (
	"errors"
	"fmt"
)

var (
	// ErrInitialEmpty is returned by [Validate] for a draft naming no initial
	// state.
	ErrInitialEmpty = errors.New("screenstatemachine: the initial state is empty")
	// ErrStatesEmpty is returned by [Validate] for a draft declaring no
	// states.
	ErrStatesEmpty = errors.New("screenstatemachine: the machine declares no states")
	// ErrInitialNotDeclared is returned by [Validate] for a draft whose
	// initial state is not one of its declared states.
	ErrInitialNotDeclared = errors.New("screenstatemachine: the initial state is not one of the declared states")
)

// Draft is one machine as the artifact store's caller hands it in. Supersedes
// is empty for a machine that introduces a screen, and names the machine this
// one revises otherwise.
type Draft struct {
	Supersedes  string
	Initial     string
	States      []string
	Events      []string
	Transitions []Transition
	Terminal    []string
}

// DuplicateTransitionError is two transitions declared on one event from one
// state — the machine cannot be closed under an event it answers two ways.
type DuplicateTransitionError struct {
	State, Event string
}

func (e *DuplicateTransitionError) Error() string {
	return fmt.Sprintf("screenstatemachine: two transitions leave %s on %s", e.State, e.Event)
}

// UnreachableStateError is a declared state no path of declared transitions
// reaches from the initial one, following only transitions that stay inside
// the machine — one that leaves the screen names another screen's state and
// not one of this machine's.
type UnreachableStateError struct {
	State string
}

func (e *UnreachableStateError) Error() string {
	return fmt.Sprintf("screenstatemachine: %s is declared and unreachable from the initial state", e.State)
}

// NoEventFromStateError is a state that is not terminal and declares no
// event — a screen nothing leaves.
type NoEventFromStateError struct {
	State string
}

func (e *NoEventFromStateError) Error() string {
	return fmt.Sprintf("screenstatemachine: %s is not terminal and declares no event", e.State)
}

// Validate refuses a draft that is not well formed: [ErrInitialEmpty] and
// [ErrStatesEmpty] for a draft too incomplete to check, [ErrInitialNotDeclared]
// for an initial state outside the declared set, and otherwise every instance
// of the three ill-formed shapes joined into one error, so a caller sees every
// defect rather than resubmitting once per one.
func Validate(draft Draft) error {
	if draft.Initial == "" {
		return ErrInitialEmpty
	}
	if len(draft.States) == 0 {
		return ErrStatesEmpty
	}
	states := make(map[string]bool, len(draft.States))
	for _, s := range draft.States {
		states[s] = true
	}
	if !states[draft.Initial] {
		return ErrInitialNotDeclared
	}
	terminal := make(map[string]bool, len(draft.Terminal))
	for _, s := range draft.Terminal {
		terminal[s] = true
	}

	seen := map[[2]string]bool{}
	hasEvent := map[string]bool{}
	graph := map[string][]string{}
	var defects []error
	for _, t := range draft.Transitions {
		key := [2]string{t.From, t.Event}
		if seen[key] {
			defects = append(defects, &DuplicateTransitionError{State: t.From, Event: t.Event})
		}
		seen[key] = true
		hasEvent[t.From] = true
		// A transition that leaves the screen names another screen's state,
		// not one of this machine's, so it is not an edge of this machine's
		// reachability graph.
		if t.Screen == "" {
			graph[t.From] = append(graph[t.From], t.To)
		}
	}

	reachable := map[string]bool{draft.Initial: true}
	queue := []string{draft.Initial}
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		for _, next := range graph[s] {
			if !reachable[next] {
				reachable[next] = true
				queue = append(queue, next)
			}
		}
	}

	for _, s := range draft.States {
		if !reachable[s] {
			defects = append(defects, &UnreachableStateError{State: s})
		}
		if !terminal[s] && !hasEvent[s] {
			defects = append(defects, &NoEventFromStateError{State: s})
		}
	}
	return errors.Join(defects...)
}

// ScreenNotFoundError is a transition leaving the screen and naming one no
// machine in force declares.
type ScreenNotFoundError struct {
	MachineID, From, Event, Screen string
}

func (e *ScreenNotFoundError) Error() string {
	return fmt.Sprintf("screenstatemachine: %s's transition from %s on %s names screen %s, which no machine in force declares",
		e.MachineID, e.From, e.Event, e.Screen)
}

// CheckTransitionTargets rejects, one error per bad transition joined, a
// transition that leaves the screen and names one absent from screensInForce
// — the screen ids [InForce] returns for the item's own service and for the
// current release of every service it declares a dependency on. Assembling
// that set is not built here; doc.go names the caller.
func CheckTransitionTargets(machines []Machine, screensInForce map[string]bool) error {
	var defects []error
	for _, m := range machines {
		for _, t := range m.Transitions {
			if t.Screen == "" {
				continue
			}
			if !screensInForce[t.Screen] {
				defects = append(defects, &ScreenNotFoundError{MachineID: m.ID, From: t.From, Event: t.Event, Screen: t.Screen})
			}
		}
	}
	return errors.Join(defects...)
}
