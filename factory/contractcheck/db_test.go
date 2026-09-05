// contractcheck.New refuses composition with no checkout to derive a
// candidate's publication from, no run to observe a consumer contract against,
// no candidate store to decide a store migration's middle items against, or no
// way to read a backfill's completion.
package contractcheck_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/contractcheck"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/score"
)

// TestACheckWithNoSeamIsRefused: a check that cannot read what a candidate publishes
// has nothing to diff, one with no run to observe would report a consumer's
// assumption as met when it had not been read, one with no candidate store would
// pass a store migration's middle items unconditionally, and one that cannot read a
// backfill's completion would ship a drop over rows the copy never reached.
func TestACheckWithNoSeamIsRefused(t *testing.T) {
	ctx, g := newGraph(t)
	_ = ctx

	reader := policy.NewReader(g.pool, g.token, score.Version{})
	if _, err := contractcheck.New(g.pool, reader, nil, nil, g.exchanges, g.storeState, g.backfills); err == nil {
		t.Error("a check with no checkout was composed")
	}
	if _, err := contractcheck.New(g.pool, reader, nil, g.checkout, nil, g.storeState, g.backfills); err == nil {
		t.Error("a check with no run to observe was composed")
	}
	if _, err := contractcheck.New(g.pool, reader, nil, g.checkout, g.exchanges, nil, g.backfills); err == nil {
		t.Error("a check with no candidate store to read was composed")
	}
	if _, err := contractcheck.New(g.pool, reader, nil, g.checkout, g.exchanges, g.storeState, nil); err == nil {
		t.Error("a check with no way to read a backfill's completion was composed")
	}
}
