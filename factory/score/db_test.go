// The score version's own tests: appended only when what it publishes changes,
// concurrent Ensure calls, the advisory lock's key, and the version every
// decision names.
package score_test

import (
	"crypto/sha256"
	"encoding/binary"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
)

// TestTheVersionIsAppendedOnlyWhenWhatItPublishesChanges: starting the factory
// twice on unchanged source appends nothing, and the version in force is the one
// every decision names.
func TestTheVersionIsAppendedOnlyWhenWhatItPublishesChanges(t *testing.T) {
	ctx, pool, token, s := newScore(t)
	w := score.NewWriter(pool, token)

	again, err := w.Ensure(ctx, scoreActor)
	if err != nil {
		t.Fatalf("Ensure again: %v", err)
	}
	if again.ID != s.Version().ID {
		t.Errorf("a second Ensure on unchanged source appended %s beside %s", again.ID, s.Version().ID)
	}

	version := s.Version()
	if version.FormulaVersion != score.FormulaVersion || version.Formula != score.Formula {
		t.Error("the version does not name the published formula")
	}
	if version.FactorSet != score.FactorSet() || version.Rules != score.Rules {
		t.Error("the version does not name the factor set and the published rules")
	}
	if len(version.Supplied) == 0 {
		t.Error("the version names no supplied value")
	}
	if version.Supersedes != "" {
		t.Errorf("the first version supersedes %q", version.Supersedes)
	}
	if version.Actor != scoreActor {
		t.Errorf("the version's actor is %+v, want the score", version.Actor)
	}

	read, err := score.Get(ctx, pool, version.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.ID != version.ID || read.Formula != version.Formula || read.Rules != version.Rules ||
		len(read.Supplied) != len(version.Supplied) {
		t.Errorf("the version reads back as %+v", read)
	}

	// A version whose supplied values differ is a version of its own, and it
	// names the one it replaced.
	if _, err := pool.Exec(ctx, `insert into `+score.Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, formula_version, formula, factor_set, rules, supplied, supersedes)
		values ('scv_next', 'score_version/1', 'component', 'score', '', $1, $2, $3, $4, $5,
			'[{"parameter":"risk_threshold","value":0.9,"why":"a hand-written row this test appended"}]', $6)`,
		record.Now(), version.FormulaVersion, version.Formula, version.FactorSet, version.Rules, version.ID); err != nil {
		t.Fatalf("appending a second version: %v", err)
	}
	newest, found, err := score.Newest(ctx, pool)
	if err != nil || !found {
		t.Fatalf("Newest = %+v, %v, %v", newest, found, err)
	}
	if newest.ID != "scv_next" || newest.Supersedes != version.ID {
		t.Errorf("the newest version is %s superseding %s", newest.ID, newest.Supersedes)
	}
	// The newest version no longer says what the source publishes, so Ensure
	// appends one that does and names the newest as its predecessor — the same
	// path a change to the source takes, and the reason nothing refuses two
	// versions that say the same thing: this one says what the first one said,
	// and it is not the first.
	ensured, err := w.Ensure(ctx, scoreActor)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if ensured.Supersedes != "scv_next" {
		t.Errorf("the appended version supersedes %q, want scv_next", ensured.Supersedes)
	}
	if len(ensured.Supplied) != len(version.Supplied) {
		t.Error("the appended version does not say what the source publishes")
	}
	if ensured.ID == version.ID {
		t.Error("Ensure reused the id of the version that said the same thing")
	}
}

// TestAdvisoryLockKeyIsDerivedFromTheName recomputes the key rather than trusting
// the constant, which is what keeps it a value no other part of the factory
// derives from a name of its own.
func TestAdvisoryLockKeyIsDerivedFromTheName(t *testing.T) {
	sum := sha256.Sum256([]byte("borg/factory/score"))
	want := int64(binary.BigEndian.Uint64(sum[:8]) & 0x7fffffffffffffff)
	if got := score.AdvisoryLockKey(); got != want {
		t.Errorf("AdvisoryLockKey() = %#x, want %#x", got, want)
	}
	if score.AdvisoryLockKey() <= 0 {
		t.Errorf("AdvisoryLockKey() = %d, want a positive value", score.AdvisoryLockKey())
	}
}

// TestEnsuringAtOnceAppendsOneVersion: the lock is what makes the read of the
// newest and the append that supersedes it one step, which is what nothing in the
// schema can enforce — two versions saying the same thing are legitimate where
// they are not adjacent.
func TestEnsuringAtOnceAppendsOneVersion(t *testing.T) {
	ctx, pool, token, s := newScore(t)
	w := score.NewWriter(pool, token)

	const ensures = 8
	done := make(chan error, ensures)
	for range ensures {
		go func() {
			_, err := w.Ensure(ctx, scoreActor)
			done <- err
		}()
	}
	for range ensures {
		if err := <-done; err != nil {
			t.Errorf("Ensure: %v", err)
		}
	}

	var rows int
	if err := pool.QueryRow(ctx, `select count(*) from `+score.Table).Scan(&rows); err != nil {
		t.Fatalf("counting the versions: %v", err)
	}
	if rows != 1 {
		t.Errorf("%d ensures at once left %d versions, want the one the first append wrote", ensures, rows)
	}
	newest, _, err := score.Newest(ctx, pool)
	if err != nil {
		t.Fatalf("Newest: %v", err)
	}
	if newest.ID != s.Version().ID {
		t.Errorf("the version in force is %s, want %s", newest.ID, s.Version().ID)
	}
}

// TestEveryDecisionNamesTheVersionInForce: the version moves as outcomes arrive
// in the design, so a decision that did not name it could not be read back
// against what the score published when it was taken.
func TestEveryDecisionNamesTheVersionInForce(t *testing.T) {
	ctx, pool, token, s := newScore(t)
	g := gate.New(decisionlog.NewWriter(pool, token), s, fakePolicy{threshold: 0.9}, gate.NoDriftDetector{})

	it, implementation := decomposeItem(t, ctx, pool, token, "item/one")
	opened, err := g.Fire(ctx, firing(it, implementation,
		score.Measurement{LinesChanged: 5, FilesChanged: 1, FilesInTree: 10}))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if opened.Row.ScoreVersion != s.Version().ID {
		t.Errorf("the opening names score version %q, want %q", opened.Row.ScoreVersion, s.Version().ID)
	}
	if opened.Assessment.Version != s.Version().ID {
		t.Errorf("the assessment names version %q", opened.Assessment.Version)
	}
	if opened.Assessment.FormulaVersion != score.FormulaVersion {
		t.Errorf("the assessment names formula %q", opened.Assessment.FormulaVersion)
	}
}
