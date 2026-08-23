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
// roadmap M1's demonstration requires. After the hops it prints every decision
// the item's gates left in the log — what each was decided over and under, the
// number against the threshold applied, the verdict and its actor — and whether
// the chain verifies clean, which is what M2's demonstration is read from.
func walk(ctx context.Context, pool *pgxpool.Pool, out io.Writer, deployID string) error {
	dep, err := deploy.Get(ctx, pool, deployID)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "deploy %s: field environment_id names environment %s\n", dep.ID, dep.EnvironmentID)
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

	// The decisions: every opening row whose payload names the item, in the
	// order they were appended, each with the closing row that closed it. Both
	// rows M2 builds fire on one item, so this is a list and not one decision —
	// and reading them in order is reading what the factory decided about this
	// change and who decided it.
	rows, err := decisionlog.Read(ctx, pool)
	if err != nil {
		return err
	}
	closings := map[string]decisionlog.Row{}
	for _, row := range rows {
		if row.Part == decisionlog.PartClosing {
			closings[row.Closes] = row
		}
	}

	decisions := 0
	for _, opening := range rows {
		if opening.Shape != decisionlog.ShapeDecision || opening.Part != decisionlog.PartOpening {
			continue
		}
		// A payload is unconstrained bytes by decisionlog's contract — that
		// package neither parses one nor constrains its format — so a row this
		// walk cannot read as a gate opening is not a fault in the log, and it
		// is skipped rather than ending the search. Only a log holding no
		// opening row for the item is the error.
		var payload gate.OpeningPayload
		if err := json.Unmarshal([]byte(opening.Payload), &payload); err != nil {
			continue
		}
		if payload.ItemID != it.ID {
			continue
		}
		decisions++
		if err := printDecision(ctx, pool, out, opening, payload, closings); err != nil {
			return err
		}
	}
	if decisions == 0 {
		return fmt.Errorf("factory: no opening row in the log names item %s", it.ID)
	}

	if err := decisionlog.Verify(ctx, pool); err != nil {
		return err
	}
	fmt.Fprintln(out, "decisionlog.Verify: the chain is clean")
	return nil
}

// printDecision writes one decision as its two rows: what the opening was
// decided over and under, and what the closing decided. The artifact is read
// where the row names one — the merge row names the version under decision and
// the deploy row names none, there being no artifact under decision at a deploy.
func printDecision(ctx context.Context, pool *pgxpool.Pool, out io.Writer,
	opening decisionlog.Row, payload gate.OpeningPayload, closings map[string]decisionlog.Row) error {
	over := "no artifact version, this row deciding an event"
	if payload.ArtifactID != "" {
		art, err := artifact.Get(ctx, pool, payload.ArtifactID)
		if err != nil {
			return err
		}
		over = fmt.Sprintf("%s version %d (%s)", art.Kind, art.Version, art.ID)
	}

	closing, closed := closings[opening.ID]
	if !closed {
		return fmt.Errorf("factory: opening row %s has no closing row", opening.ID)
	}
	var verdict gate.ClosingPayload
	if err := json.Unmarshal([]byte(closing.Payload), &verdict); err != nil {
		return fmt.Errorf("factory: reading the payload of closing row %s: %w", closing.ID, err)
	}

	fmt.Fprintf(out, "decision at %s: opening row %s decided over %s\n", payload.Gate, opening.ID, over)
	fmt.Fprintf(out, "  under policy version %s and score version %s (formula %s)\n",
		opening.PolicyVersion, opening.ScoreVersion, payload.FormulaVersion)
	fmt.Fprintf(out, "  number %.3f against threshold %.3f (%s)\n",
		payload.Number, payload.Threshold, payload.ThresholdFrom)
	if payload.HumanDecides {
		fmt.Fprintf(out, "  a human decided: %s\n", payload.WhyHuman)
	} else {
		fmt.Fprintln(out, "  no human decided: the number was under the threshold and no safeguard added one")
	}
	for _, id := range payload.Safeguards {
		fmt.Fprintf(out, "  safeguard %s applied\n", id)
	}
	for _, name := range payload.Unavailable {
		fmt.Fprintf(out, "  factor %s was unavailable\n", name)
	}
	fmt.Fprintf(out, "  closing row %s carries verdict %s, decided by %s %s\n",
		closing.ID, verdict.Verdict, closing.Actor.Kind, closing.Actor.Name)
	if verdict.WhyItAutoPassed != "" {
		fmt.Fprintf(out, "  why it auto-passed: %s\n", verdict.WhyItAutoPassed)
	}
	if verdict.Feedback != "" {
		fmt.Fprintf(out, "  feedback: %s\n", verdict.Feedback)
	}
	if verdict.ReturnsTo != "" {
		fmt.Fprintf(out, "  the item returns to %s\n", verdict.ReturnsTo)
	}
	return nil
}
