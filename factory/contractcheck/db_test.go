// contractcheck.New refuses composition with no checkout to derive a
// candidate's publication from, or no run to observe a consumer contract
// against.
package contractcheck_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/contractcheck"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/score"
)

// TestACheckWithNoSeamIsRefused: a check that cannot read what a candidate publishes
// has nothing to diff, and one with no run to observe would report a consumer's
// assumption as met when it had not been read.
func TestACheckWithNoSeamIsRefused(t *testing.T) {
	ctx, g := newGraph(t)
	_ = ctx

	if _, err := contractcheck.New(g.pool, policy.NewReader(g.pool, score.Version{}), nil, nil, g.exchanges); err == nil {
		t.Error("a check with no checkout was composed")
	}
	if _, err := contractcheck.New(g.pool, policy.NewReader(g.pool, score.Version{}), nil, g.checkout, nil); err == nil {
		t.Error("a check with no run to observe was composed")
	}
}
