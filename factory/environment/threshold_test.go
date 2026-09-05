package environment_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/record"
)

// TestAThresholdExistsOnlyWhereAnOwnerAuthoredOne: an absent row is the score
// supplying the value, and re-authoring is one row rather than two.
func TestAThresholdExistsOnlyWhereAnOwnerAuthoredOne(t *testing.T) {
	ctx, pool, w, token := newTable(t)

	production, err := w.Create(ctx, owner, productionSpec())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	authored, err := environment.GateThreshold(ctx, pool, production.ID, "merge_to_master")
	if err != nil {
		t.Fatalf("GateThreshold: %v", err)
	}
	if authored.Present {
		t.Errorf("an unauthored threshold reads back as %+v", authored)
	}

	for _, threshold := range []float64{0.4, 0.2} {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		if err := environment.SetGateThreshold(ctx, tx, token, owner, production.ID, "merge_to_master", threshold); err != nil {
			t.Fatalf("SetGateThreshold(%v): %v", threshold, err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("Commit: %v", err)
		}
	}

	authored, err = environment.GateThreshold(ctx, pool, production.ID, "merge_to_master")
	if err != nil {
		t.Fatalf("GateThreshold: %v", err)
	}
	if !authored.Present || authored.Number != 0.2 {
		t.Errorf("the re-authored threshold reads back as %+v, want 0.2 present", authored)
	}

	var rows int
	if err := pool.QueryRow(ctx, `select count(*) from `+environment.ThresholdTable).Scan(&rows); err != nil {
		t.Fatalf("counting the threshold rows: %v", err)
	}
	if rows != 1 {
		t.Errorf("two authorings of one row left %d rows, want 1", rows)
	}

	// Another row on the same environment is another threshold, not the same one.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := environment.SetGateThreshold(ctx, tx, token, owner, production.ID, "deploy_to_production", 0.5); err != nil {
		t.Fatalf("SetGateThreshold on a second row: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	deployRow, err := environment.GateThreshold(ctx, pool, production.ID, "deploy_to_production")
	if err != nil {
		t.Fatalf("GateThreshold: %v", err)
	}
	if !deployRow.Present || deployRow.Number != 0.5 {
		t.Errorf("the second row's threshold reads back as %+v, want 0.5 present", deployRow)
	}
}

// TestAThresholdOffTheScaleIsRefusedTwice: the score's number is between nothing
// and one, so a threshold outside that compares against nothing the score can
// produce. The writer refuses it and so does the store.
func TestAThresholdOffTheScaleIsRefusedTwice(t *testing.T) {
	ctx, pool, w, token := newTable(t)

	production, err := w.Create(ctx, owner, productionSpec())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := environment.SetGateThreshold(ctx, tx, token, owner, production.ID, "merge_to_master", 1.5); !errors.Is(err, environment.ErrThresholdOutOfRange) {
		t.Errorf("SetGateThreshold(1.5) = %v, want ErrThresholdOutOfRange", err)
	}
	if err := environment.SetGateThreshold(ctx, tx, token, owner, production.ID, "", 0.5); !errors.Is(err, environment.ErrGateRowEmpty) {
		t.Errorf("SetGateThreshold naming no row = %v, want ErrGateRowEmpty", err)
	}

	if _, err := pool.Exec(ctx, `insert into `+environment.ThresholdTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, environment_id, gate_row, threshold)
		values ('egt_x', $1, 'human', 'owner', 'claimed', $2, $3, 'merge_to_master', 1.5)`,
		environment.FormatVersionThreshold, record.Now(), production.ID); err == nil {
		t.Error("the store accepted a threshold off the scale written around the writer")
	}
}
