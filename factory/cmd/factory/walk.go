package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/release"
)

// walk follows the links from one deploy record back to the intent, printing
// one line per hop — the record, the field followed, and what it reached.
// Every step is a stored field and none is reconstructed, which is what
// roadmap M1's demonstration requires. After the hops it prints the decision
// readable in the log — the verdict, its actor, and the artifact version the
// opening row names — and whether the chain verifies clean.
func walk(ctx context.Context, pool *pgxpool.Pool, out io.Writer, deployID string) error {
	dep, err := deploy.Get(ctx, pool, deployID)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "deploy %s: field release_id names release %s\n", dep.ID, dep.ReleaseID)

	rel, err := release.Get(ctx, pool, dep.ReleaseID)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "release %s: field build_id names build %s\n", rel.ID, rel.BuildID)
	fmt.Fprintf(out, "release %s: field item_id names item %s\n", rel.ID, rel.ItemID)

	bl, err := build.Get(ctx, pool, rel.BuildID)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "build %s: field commit_hash names commit %s\n", bl.ID, bl.CommitHash)

	it, err := item.Get(ctx, pool, rel.ItemID)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "item %s: field intent_id names intent %s\n", it.ID, it.IntentID)

	in, err := intent.Get(ctx, pool, it.IntentID)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "intent %s: field statement reads: %s\n", in.ID, in.Statement)

	// The decision: the opening row whose payload names the item, the
	// closing row naming that opening, and the verdict the closing carries.
	rows, err := decisionlog.Read(ctx, pool)
	if err != nil {
		return err
	}
	var opening decisionlog.Row
	var payload gate.OpeningPayload
	found := false
	for _, row := range rows {
		if row.Shape != decisionlog.ShapeDecision || row.Part != decisionlog.PartOpening {
			continue
		}
		// A payload is unconstrained bytes by decisionlog's contract — that
		// package neither parses one nor constrains its format — so a row this
		// walk cannot read as a gate opening is not a fault in the log, and it
		// is skipped rather than ending the search. Only a log holding no
		// opening row for the item is the error.
		var p gate.OpeningPayload
		if err := json.Unmarshal([]byte(row.Payload), &p); err != nil {
			continue
		}
		if p.ItemID == it.ID {
			opening, payload, found = row, p, true
			break
		}
	}
	if !found {
		return fmt.Errorf("factory: no opening row in the log names item %s", it.ID)
	}

	art, err := artifact.Get(ctx, pool, payload.ArtifactID)
	if err != nil {
		return err
	}

	var closing decisionlog.Row
	closed := false
	for _, row := range rows {
		if row.Part == decisionlog.PartClosing && row.Closes == opening.ID {
			closing, closed = row, true
			break
		}
	}
	if !closed {
		return fmt.Errorf("factory: opening row %s has no closing row", opening.ID)
	}
	var verdict gate.ClosingPayload
	if err := json.Unmarshal([]byte(closing.Payload), &verdict); err != nil {
		return fmt.Errorf("factory: reading the payload of closing row %s: %w", closing.ID, err)
	}
	fmt.Fprintf(out, "decision: opening row %s names %s version %d (%s); closing row %s carries verdict %s, decided by %s %s\n",
		opening.ID, art.Kind, art.Version, art.ID,
		closing.ID, verdict.Verdict, closing.Actor.Kind, closing.Actor.Name)
	if verdict.Feedback != "" {
		fmt.Fprintf(out, "feedback: %s\n", verdict.Feedback)
	}

	if err := decisionlog.Verify(ctx, pool); err != nil {
		return err
	}
	fmt.Fprintln(out, "decisionlog.Verify: the chain is clean")
	return nil
}
