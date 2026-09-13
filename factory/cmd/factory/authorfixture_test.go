package main

import (
	"bytes"
	"context"
	"testing"
)

// authorOne is one intent decomposed into one item on the install's one service, for a
// test that drives the steps rather than calling run. Decomposition yields one item, so no
// Decomposition row fires — the row fires where there is a set to ratify.
func authorOne(t *testing.T, ctx context.Context, p *path, statement string, out *bytes.Buffer) *candidate {
	t.Helper()
	set, candidates, err := p.authorIntent(ctx,
		asked{statement: statement, services: []string{theService}}, statement)
	if err != nil {
		t.Fatalf("authoring %q: %v\noutput so far:\n%s", statement, err, out)
	}
	if len(candidates) != 1 {
		t.Fatalf("authoring %q yielded %d candidates, want one", statement, len(candidates))
	}
	if set.decided {
		t.Fatalf("a decomposition of one item fired Decomposition, and that row fires where there is a set to ratify")
	}
	c := candidates[0]
	p.byItem[c.itemID] = c
	p.authored[c.itemID] = true
	authorStages(t, ctx, p, c, out)
	return c
}
