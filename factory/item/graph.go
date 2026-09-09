package item

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// edge is one wait: From cannot be verified until To has shipped. The declared
// edges are the waits_on lists of the items that have not ended, and the rest
// are what a rollback hold imposes.
type edge struct {
	From string
	To   string
}

// Hold is a rollback hold standing on a service, which no record holds: the
// service the hold stands on, and the intent of the revert that lifts it,
// which is what the rollback's own deploy record names. [Decomposition] reads
// every hold standing through [RollbackHolds] at each write, from what the
// production deploy gate reads the hold from, and the edges they impose are
// computed here at that write, the way the hold itself is recomputed at each
// firing.
type Hold struct {
	ServiceID      string
	RevertIntentID string
}

// RollbackHolds is the seam [Decomposition] asks for every rollback hold
// standing, inside Create, CreateTx, Repoint, and RepointTx, since no record
// holds them: cmd/factory implements it over the same reading the production
// deploy gate makes, so the write and the gate never check two readings of
// what stands. [Decomposition.Holds] is nil in every composition but
// cmd/factory's own — a test that never wires one is checked against nothing
// standing, which is the zero value and not a hold a caller has to pass.
type RollbackHolds interface {
	Standing(ctx context.Context) ([]Hold, error)
}

// ErrHoldIncomplete is returned for a hold naming no service or no revert
// intent. A hold missing either names no edges at all, and a write checked
// against nothing would pass a cycle the union holds.
var ErrHoldIncomplete = errors.New("item: the rollback hold names a service and the intent of its revert")

// heldEdge is one edge a hold imposes, carrying the hold that imposes it, so a
// refusal over the union names the hold and not only the edge that closed the
// cycle.
type heldEdge struct {
	edge
	Hold Hold
}

// ErrWouldCloseACycle is returned by [Decomposition.Create] and
// [Decomposition.Repoint] for a write that would leave a cycle in the graph of
// what waits on what. Two items each holding a deploy gate on the other is a
// wait nothing lifts and no instrument shows, so the write is refused where
// the items are kept. The error names the edge that closes it, and the hold
// where the cycle runs through one.
var ErrWouldCloseACycle = errors.New("item: the write would close a cycle in what waits on what")

// standingEdges is every declared edge of the graph: the waits_on list of each
// item that has not ended. A merged, dropped, or superseded item is out of the
// graph — its work is over, so nothing waits on it in a way anything can
// lift — and the relation is over the unmerged items alone.
//
// It reads inside the caller's transaction, so the rows it sees are the rows
// the write is checked against.
func standingEdges(ctx context.Context, tx pgx.Tx, skip string) ([]edge, error) {
	rows, err := tx.Query(ctx, `select id, waits_on from `+Table+`
		where stage not in ('merged', 'dropped', 'superseded') and id <> $1`, skip)
	if err != nil {
		return nil, fmt.Errorf("item: reading what waits on what: %w", err)
	}
	defer rows.Close()

	var edges []edge
	for rows.Next() {
		var id, waitsOn string
		if err := rows.Scan(&id, &waitsOn); err != nil {
			return nil, fmt.Errorf("item: reading an item's waits: %w", err)
		}
		for _, on := range splitIDs(waitsOn) {
			edges = append(edges, edge{From: id, To: on})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("item: reading what waits on what: %w", err)
	}
	return edges, nil
}

// heldEdges is what the holds impose: while a rollback hold stands on a
// service, every unmerged item of that service other than the revert waits on
// the revert item. No record holds those edges, so they are computed here from
// the items of the held service and the intent the revert was decomposed from,
// and computed again at every write rather than stored.
//
// writing is the item the write is for, which at a creation is not in the
// table yet: an item decomposed onto a held service is one the hold reaches,
// and a revert item decomposed while its own hold stands is what the hold's
// edges lead into. It is ignored where the row is already there.
//
// It reads inside the caller's transaction, for the reason [standingEdges]
// does.
func heldEdges(ctx context.Context, tx pgx.Tx, holds []Hold, writing Item) ([]heldEdge, error) {
	var edges []heldEdge
	for _, h := range holds {
		if h.ServiceID == "" || h.RevertIntentID == "" {
			return nil, fmt.Errorf("%w: service %q, revert intent %q", ErrHoldIncomplete, h.ServiceID, h.RevertIntentID)
		}
		unmerged, err := unmergedOfService(ctx, tx, h.ServiceID, writing)
		if err != nil {
			return nil, err
		}
		var reverts []string
		for _, it := range unmerged {
			if it.IntentID == h.RevertIntentID {
				reverts = append(reverts, it.ID)
			}
		}
		for _, it := range unmerged {
			if it.IntentID == h.RevertIntentID {
				continue
			}
			for _, revert := range reverts {
				edges = append(edges, heldEdge{edge: edge{From: it.ID, To: revert}, Hold: h})
			}
		}
	}
	return edges, nil
}

// unmergedOfService is the id and the intent of every item of one service that
// has not ended, with writing among them where the write is what creates it. A
// merged, dropped, or superseded item is out of the graph, which is the set
// [standingEdges] reads its edges from.
func unmergedOfService(ctx context.Context, tx pgx.Tx, serviceID string, writing Item) ([]Item, error) {
	rows, err := tx.Query(ctx, `select id, intent_id from `+Table+`
		where service_id = $1 and stage not in ('merged', 'dropped', 'superseded')`, serviceID)
	if err != nil {
		return nil, fmt.Errorf("item: reading the unmerged items of %s: %w", serviceID, err)
	}
	defer rows.Close()

	var read []Item
	stored := false
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.IntentID); err != nil {
			return nil, fmt.Errorf("item: reading an unmerged item of %s: %w", serviceID, err)
		}
		stored = stored || it.ID == writing.ID
		read = append(read, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("item: reading the unmerged items of %s: %w", serviceID, err)
	}
	if !stored && writing.ID != "" && writing.ServiceID == serviceID {
		read = append(read, Item{ID: writing.ID, IntentID: writing.IntentID})
	}
	return read, nil
}

// step is one edge as the walk follows it: where it leads, and the hold that
// imposed it where a hold did.
type step struct {
	to   string
	hold Hold
	held bool
}

// checkAcyclic refuses proposed where any one of its edges closes a cycle over
// the union of standing, held, and the proposed edges before it. The union is
// what the design checks: the edges decomposition declared and the edges a
// rollback hold imposes, which no record holds.
//
// An edge closes a cycle exactly when its head already reaches its tail, so
// the refusal names the edge and needs no walk to explain itself. Where the
// path from the head runs through a hold's edge the refusal names the hold as
// well: what was decomposed wrong is then read off the hold rather than off
// the set, nothing having declared that edge.
func checkAcyclic(standing []edge, held []heldEdge, proposed []edge) error {
	graph := map[string][]step{}
	for _, e := range standing {
		graph[e.From] = append(graph[e.From], step{to: e.To})
	}
	for _, e := range held {
		graph[e.From] = append(graph[e.From], step{to: e.To, hold: e.Hold, held: true})
	}

	for _, e := range proposed {
		if e.From == e.To {
			return fmt.Errorf("%w: %s waits on %s", ErrWouldCloseACycle, e.From, e.To)
		}
		if holds, found := reaches(graph, e.To, e.From); found {
			return fmt.Errorf("%w: %s waits on %s%s", ErrWouldCloseACycle, e.From, e.To, throughTheHolds(holds))
		}
		graph[e.From] = append(graph[e.From], step{to: e.To})
	}
	return nil
}

// throughTheHolds is what the refusal says about the holds the cycle runs
// through, and nothing where it runs through none.
func throughTheHolds(holds []Hold) string {
	said := ""
	for _, h := range holds {
		said += fmt.Sprintf(", through the rollback hold on service %s awaiting the revert of intent %s",
			h.ServiceID, h.RevertIntentID)
	}
	return said
}

// reaches reports whether to is reachable from from, following the edges as
// "waits on", and the holds whose edges the path it found runs through. It is
// a depth-first walk with a seen set, so a graph that already holds a cycle
// terminates rather than being walked forever, and it keeps the step that
// first reached each node so the path can be walked back for its holds.
func reaches(graph map[string][]step, from, to string) ([]Hold, bool) {
	seen := map[string]bool{from: true}
	reached := map[string]step{}
	before := map[string]string{}
	stack := []string{from}
	for len(stack) > 0 {
		at := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if at == to {
			return holdsAlong(reached, before, from, to), true
		}
		for _, next := range graph[at] {
			if !seen[next.to] {
				seen[next.to] = true
				reached[next.to] = next
				before[next.to] = at
				stack = append(stack, next.to)
			}
		}
	}
	return nil, false
}

// holdsAlong is the holds on the path the walk took from from to to, walked
// back from the far end, each named once however many of its edges the path
// uses.
func holdsAlong(reached map[string]step, before map[string]string, from, to string) []Hold {
	var holds []Hold
	named := map[Hold]bool{}
	for at := to; at != from; at = before[at] {
		s, ok := reached[at]
		if !ok {
			return holds
		}
		if s.held && !named[s.hold] {
			named[s.hold] = true
			holds = append(holds, s.hold)
		}
	}
	return holds
}
