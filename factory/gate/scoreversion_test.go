// Which score version a firing computes its vector under, read off the policy:
// the newest where nobody authored a threshold at the row's scope, and the last
// one confirmed there where somebody did.
package gate_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
)

// TestAFiringAssessesUnderTheVersionInForceAtItsScope: a version that redefined
// the number does not decide a gate an authored threshold binds until its owner
// has confirmed it, so a firing at such a row computes under the version the
// policy resolved and the decision names that one — not the newest the process
// started with.
func TestAFiringAssessesUnderTheVersionInForceAtItsScope(t *testing.T) {
	s := &fakeScore{assessment: assessed(0.2)}
	p := &fakePolicy{applied: applied(0.9)}
	ctx, pool, token, g := newGate(t, s, p)

	// One real version in the log, which is what the firing has to read back:
	// the fake score's own version stands for the newest, and this one for the
	// one in force at the scope.
	inForce, err := score.NewWriter(pool, token, score.NoMarks{}).Ensure(ctx,
		record.Actor{Kind: record.KindComponent, Key: "score"})
	if err != nil {
		t.Fatalf("appending the score version in force at the scope: %v", err)
	}
	p.applied.ScoreVersion = inForce.ID

	opened, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if s.askedVersion != inForce.ID {
		t.Errorf("the firing assessed under %q, want the version in force at the scope %q",
			s.askedVersion, inForce.ID)
	}
	if opened.Row.ScoreVersion != inForce.ID {
		t.Errorf("the decision names score version %q, want the one it was computed under %q",
			opened.Row.ScoreVersion, inForce.ID)
	}
}
