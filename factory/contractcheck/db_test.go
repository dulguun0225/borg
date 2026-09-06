// contractcheck.New refuses composition with no checkout to derive a
// candidate's publication from, no run to observe a consumer contract against,
// or no candidate store to decide a store migration's middle items against.
package contractcheck_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/contractcheck"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/score"
)

// TestACheckWithNoSeamIsRefused: a check that cannot read what a candidate publishes
// has nothing to diff, one with no run to observe would report a consumer's
// assumption as met when it had not been read, and one with no candidate store
// would pass a store migration's middle items unconditionally.
func TestACheckWithNoSeamIsRefused(t *testing.T) {
	ctx, g := newGraph(t)
	_ = ctx

	reader := policy.NewReader(g.pool, g.token, score.Version{})
	if _, err := contractcheck.New(g.pool, reader, nil, nil, g.exchanges, g.storeState); err == nil {
		t.Error("a check with no checkout was composed")
	}
	if _, err := contractcheck.New(g.pool, reader, nil, g.checkout, nil, g.storeState); err == nil {
		t.Error("a check with no run to observe was composed")
	}
	if _, err := contractcheck.New(g.pool, reader, nil, g.checkout, g.exchanges, nil); err == nil {
		t.Error("a check with no candidate store to read was composed")
	}
}
