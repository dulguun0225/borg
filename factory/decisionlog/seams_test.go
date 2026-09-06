package decisionlog_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/record"
)

// TestEveryRecordCarriesAnActor is seam 1, checked in both places: the writer
// validates, and the store refuses what a writer that did not validate would
// have written.
func TestEveryRecordCarriesAnActor(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	t.Run("the writer refuses", func(t *testing.T) {
		cases := map[string]struct {
			actor record.Actor
			want  error
		}{
			"no actor at all":     {record.Actor{}, record.ErrKindUnknown},
			"no kind":             {record.Actor{Key: "gate.merge_to_master"}, record.ErrKindUnknown},
			"unknown kind":        {record.Actor{Kind: "robot", Key: "owner"}, record.ErrKindUnknown},
			"no key":              {record.Actor{Kind: record.KindHuman, Basis: record.BasisClaimed}, record.ErrKeyEmpty},
			"human no basis":      {record.Actor{Kind: record.KindHuman, Key: "person:abc"}, record.ErrBasisEmpty},
			"component no basis":  {record.Actor{Kind: record.KindComponent, Key: "gate.merge_to_master"}, record.ErrBasisEmpty},
			"component odd basis": {record.Actor{Kind: record.KindComponent, Key: "gate.merge_to_master", Basis: "guessed"}, record.ErrBasisUnknown},
		}
		for name, c := range cases {
			entry := decisionlog.Entry{
				Actor: c.actor, Payload: "x", FormatVersion: "decision/1", PolicyVersion: "p", ScoreVersion: "s",
			}
			if _, err := log.AppendDecisionOpen(ctx, entry); !errors.Is(err, c.want) {
				t.Errorf("an opening with %s: %v, want %v", name, err, c.want)
			}
			entry.PolicyVersion, entry.ScoreVersion, entry.FormatVersion = "", "", "wait/1"
			if _, err := log.AppendWaitOpen(ctx, entry); !errors.Is(err, c.want) {
				t.Errorf("a wait with %s: %v, want %v", name, err, c.want)
			}
		}
	})

	t.Run("the store refuses", func(t *testing.T) {
		unknown := aRow()
		unknown.Actor = record.Actor{Kind: "robot", Key: "owner", Basis: record.BasisClaimed}
		if got, want := refusedBy(t, insertAround(ctx, pool, unknown)), "actor_kind_known"; got != want {
			t.Errorf("an unknown actor kind was refused by %q, want %q", got, want)
		}

		empty := aRow()
		empty.Actor = record.Actor{Kind: record.KindHuman, Basis: record.BasisClaimed}
		if got, want := refusedBy(t, insertAround(ctx, pool, empty)), "actor_key_present"; got != want {
			t.Errorf("an empty actor key was refused by %q, want %q", got, want)
		}

		// An actor with no kind, no key and no basis violates three constraints
		// at once; the store reports actor_key_basis_known for this row.
		none := aRow()
		none.Actor = record.Actor{}
		if got, want := refusedBy(t, insertAround(ctx, pool, none)), "actor_key_basis_known"; got != want {
			t.Errorf("no actor at all was refused by %q, want %q", got, want)
		}

		noBasis := aRow()
		noBasis.Actor = record.Actor{Kind: record.KindHuman, Key: "person:abc"}
		if got, want := refusedBy(t, insertAround(ctx, pool, noBasis)), "actor_key_basis_known"; got != want {
			t.Errorf("a human with no basis was refused by %q, want %q", got, want)
		}

		// The basis is on every actor, so a component's row without one is
		// refused by the same constraint a human's is.
		componentNoBasis := aRow()
		componentNoBasis.Actor = record.Actor{Kind: record.KindComponent, Key: "gate.merge_to_master"}
		if got, want := refusedBy(t, insertAround(ctx, pool, componentNoBasis)), "actor_key_basis_known"; got != want {
			t.Errorf("a component with no basis was refused by %q, want %q", got, want)
		}
	})

	if err := reader.Verify(ctx, ownerReading); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}
