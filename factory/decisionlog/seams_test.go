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
	ctx, pool, log := newLog(t)

	t.Run("the writer refuses", func(t *testing.T) {
		cases := map[string]struct {
			actor record.Actor
			want  error
		}{
			"no actor at all": {record.Actor{}, record.ErrKindUnknown},
			"no kind":         {record.Actor{Name: "owner"}, record.ErrKindUnknown},
			"unknown kind":    {record.Actor{Kind: "robot", Name: "owner"}, record.ErrKindUnknown},
			"no name":         {record.Actor{Kind: record.KindHuman}, record.ErrNameEmpty},
		}
		for name, c := range cases {
			entry := decisionlog.Entry{Actor: c.actor, Payload: "x", PolicyVersion: "p", ScoreVersion: "s"}
			if _, err := log.AppendDecisionOpen(ctx, entry); !errors.Is(err, c.want) {
				t.Errorf("an opening with %s: %v, want %v", name, err, c.want)
			}
			entry.PolicyVersion, entry.ScoreVersion = "", ""
			if _, err := log.AppendWait(ctx, entry); !errors.Is(err, c.want) {
				t.Errorf("a wait with %s: %v, want %v", name, err, c.want)
			}
		}
	})

	t.Run("the store refuses", func(t *testing.T) {
		unknown := aRow()
		unknown.Actor = record.Actor{Kind: "robot", Name: "owner"}
		if got, want := refusedBy(t, insertAround(ctx, pool, unknown)), "actor_kind_known"; got != want {
			t.Errorf("an unknown actor kind was refused by %q, want %q", got, want)
		}

		empty := aRow()
		empty.Actor = record.Actor{Kind: record.KindHuman}
		if got, want := refusedBy(t, insertAround(ctx, pool, empty)), "actor_name_present"; got != want {
			t.Errorf("an empty actor name was refused by %q, want %q", got, want)
		}

		none := aRow()
		none.Actor = record.Actor{}
		if got, want := refusedBy(t, insertAround(ctx, pool, none)), "actor_kind_known"; got != want {
			t.Errorf("no actor at all was refused by %q, want %q", got, want)
		}
	})

	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}
