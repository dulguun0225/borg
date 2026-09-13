package policy_test

import (
	"slices"
	"testing"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/service"
)

func TestFailureKeyCapAndOverflowSurviveRestartTogether(t *testing.T) {
	ctx, in := newFactory(t)
	version, err := in.factory.AuthorFailureRecordKeyCap(ctx, owner, in.service.ID, 100, "other")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, value := range version.Authored {
		if value.Parameter == gatepolicy.FailureRecordKeyCap && value.Scope.ID == in.service.ID {
			found = value.Number == 100 && slices.Equal(value.List, []string{"other"})
		}
	}
	if !found {
		t.Fatalf("version omits the authored pair: %+v", version.Authored)
	}
	for _, corrupt := range []string{
		`failure_record_key_cap = null, overflow_failure_record_bucket = ''`,
		`overflow_failure_record_bucket = 'wrong'`,
	} {
		if _, err := in.pool.Exec(ctx, `update `+service.Table+` set `+corrupt+` where id = $1`, in.service.ID); err != nil {
			t.Fatal(err)
		}
		rewritten, err := in.factory.Rederive(ctx, owner)
		if err != nil {
			t.Fatal(err)
		}
		if len(rewritten) != 1 || rewritten[0].Value.Parameter != gatepolicy.FailureRecordKeyCap {
			t.Fatalf("rewritten = %+v, want the failure key cap", rewritten)
		}
		svc, err := service.Get(ctx, in.pool, in.service.ID)
		if err != nil {
			t.Fatal(err)
		}
		if svc.FailureRecordKeyCap.Number != 100 || svc.OverflowFailureRecordBucket != "other" {
			t.Fatalf("restored %v into %q", svc.FailureRecordKeyCap, svc.OverflowFailureRecordBucket)
		}
	}
	if rewritten, err := in.factory.Rederive(ctx, owner); err != nil || len(rewritten) != 0 {
		t.Fatalf("second restart = %+v, %v", rewritten, err)
	}
	if newest := newestVersion(t, ctx, in); newest.ID != version.ID {
		t.Fatalf("restart appended a policy version: %s", newest.ID)
	}
	if _, err := in.factory.AuthorFailureRecordKeyCap(ctx, owner, in.service.ID, 50, ""); err == nil {
		t.Fatal("cap without an overflow bucket was accepted")
	}
	if newest := newestVersion(t, ctx, in); newest.ID != version.ID {
		t.Fatal("rejected write appended a policy version")
	}
}
