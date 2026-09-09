package gate

import (
	"fmt"

	"github.com/dulguun0225/borg/factory/item"
)

// The Decomposition row's third mechanical rejection: a set whose dependencies
// form a cycle. Two items each holding a deploy gate on the other is a wait
// nothing lifts and no instrument shows — a hold over a record that already
// exists writes nothing, counts no attempt, and pages nobody — so a wrong order
// comes back here as feedback rather than as a stage that could not write.
//
// The comparison the design draws is to the compile and not to the forbidden
// transition the Implementation row also rejects on: a cycle in a relation the
// factory holds whole is decidable, where a transition in a build is decided
// only as far as an extractor follows it.

// SetCycleRejection is that rejection, over the members the row decides and
// the rollback holds standing, given the way decomposition's own write reads
// them: the service the hold stands on and the intent of the revert that
// lifts it. It returns [AutoRejectedByACycle] and the edge that closes the
// cycle, and false where the proposed order is acyclic.
//
// What it reads is the set's own edges: the row decides how many items, which
// service each changes, and what waits on what, and those are what its open
// event names. The holds are read the same way [item.Decomposition.Create]'s
// own write-time check reads them, so a member of the held service carries a
// node for the hold's revert in its own reachability — present for a caller
// that later has enough to connect it back into the set.
//
// The whole relation this row cannot see is still checked where decomposition
// writes: [item.heldEdges] reads every unmerged item of the held service
// across every intent, where this function reads one intent's own proposed
// set, so a cycle whose other end is an item outside the set — the ordinary
// case a hold matters at all, since a hold's revert is rarely a member of the
// very set proposing against it — is refused at the write, as before, and not
// here. What this adds is a check that does not regress: naming a hold whose
// service no member of the set touches, or whose revert is not among the
// set's own items, leaves this function's verdict unchanged.
//
// An edge closes a cycle exactly when its head already reaches its tail, so the
// refusal names the edge and needs no walk to explain itself. The walk is this
// package's own rather than package item's: locality is paid for in repetition,
// and a set the gate decides is not the graph item's writer holds.
func SetCycleRejection(members []SetMember, holds []item.Hold) (check, found string, rejects bool) {
	graph := map[string][]string{}
	for _, h := range holds {
		revertNode := "hold:" + h.ServiceID + ":" + h.RevertIntentID
		for _, m := range members {
			if m.ServiceID == h.ServiceID {
				graph[m.ItemID] = append(graph[m.ItemID], revertNode)
			}
		}
	}
	for _, m := range members {
		for _, on := range m.WaitsOn {
			if m.ItemID == on || reaches(graph, on, m.ItemID) {
				return AutoRejectedByACycle,
					fmt.Sprintf("item %s waits on %s, which closes a cycle in what waits on what", m.ItemID, on),
					true
			}
			graph[m.ItemID] = append(graph[m.ItemID], on)
		}
	}
	return "", "", false
}

// reaches reports whether to is reachable from from, following the edges as
// "waits on". It is a depth-first walk with a seen set, so a graph that already
// holds a cycle terminates rather than being walked forever.
func reaches(graph map[string][]string, from, to string) bool {
	seen := map[string]bool{from: true}
	stack := []string{from}
	for len(stack) > 0 {
		at := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if at == to {
			return true
		}
		for _, next := range graph[at] {
			if !seen[next] {
				seen[next] = true
				stack = append(stack, next)
			}
		}
	}
	return false
}
