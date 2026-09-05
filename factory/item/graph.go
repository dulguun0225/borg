package item

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Edge is one wait: From cannot be verified until To has shipped. The declared
// edges are the waits_on lists of the items that have not ended; the caller
// supplies the rest.
type Edge struct {
	From string
	To   string
}

// ErrWouldCloseACycle is returned by [Decomposition.Create] and
// [Decomposition.Repoint] for a write that would leave a cycle in the graph of
// what waits on what. Two items each holding a deploy gate on the other is a
// wait nothing lifts and no instrument shows, so the write is refused where
// the items are kept. The error names the edge that closes it.
var ErrWouldCloseACycle = errors.New("item: the write would close a cycle in what waits on what")

// standingEdges is every declared edge of the graph: the waits_on list of each
// item that has not ended. A merged, dropped, or superseded item is out of the
// graph — its work is over, so nothing waits on it in a way anything can
// lift — and the relation is over the unmerged items alone.
//
// It reads inside the caller's transaction, so the rows it sees are the rows
// the write is checked against.
func standingEdges(ctx context.Context, tx pgx.Tx, skip string) ([]Edge, error) {
	rows, err := tx.Query(ctx, `select id, waits_on from `+Table+`
		where stage not in ('merged', 'dropped', 'superseded') and id <> $1`, skip)
	if err != nil {
		return nil, fmt.Errorf("item: reading what waits on what: %w", err)
	}
	defer rows.Close()

	var edges []Edge
	for rows.Next() {
		var id, waitsOn string
		if err := rows.Scan(&id, &waitsOn); err != nil {
			return nil, fmt.Errorf("item: reading an item's waits: %w", err)
		}
		for _, on := range splitIDs(waitsOn) {
			edges = append(edges, Edge{From: id, To: on})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("item: reading what waits on what: %w", err)
	}
	return edges, nil
}

// checkAcyclic refuses proposed where any one of its edges closes a cycle over
// the union of standing, hold, and the proposed edges before it. The union is
// what the design checks: the edges decomposition declared and the edges a
// rollback hold imposes, which no record holds and the caller reads off the
// hold.
//
// An edge closes a cycle exactly when its head already reaches its tail, so
// the refusal names the edge and needs no walk to explain itself.
func checkAcyclic(standing, hold, proposed []Edge) error {
	graph := map[string][]string{}
	add := func(edges []Edge) {
		for _, e := range edges {
			graph[e.From] = append(graph[e.From], e.To)
		}
	}
	add(standing)
	add(hold)

	for _, e := range proposed {
		if e.From == e.To || reaches(graph, e.To, e.From) {
			return fmt.Errorf("%w: %s waits on %s", ErrWouldCloseACycle, e.From, e.To)
		}
		graph[e.From] = append(graph[e.From], e.To)
	}
	return nil
}

// reaches reports whether to is reachable from from, following the edges as
// "waits on". It is a depth-first walk with a seen set, so a graph that
// already holds a cycle terminates rather than being walked forever.
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
