package policy_test

import (
	"context"
	"testing"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/safeguard"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
)

func TestHarmPageSwitchAndPagingHoursRestoreThenAgree(t *testing.T) {
	ctx, in := newFactory(t)
	if _, err := in.factory.SetHarmMarkPages(ctx, owner, false); err != nil {
		t.Fatal(err)
	}
	if _, err := in.factory.AuthorPagingHours(ctx, owner, in.service.ID, service.PagingHours{Start: "09:00", End: "17:00", Zone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	if rewritten, err := in.factory.Rederive(ctx, owner); err != nil || len(rewritten) != 0 {
		t.Fatalf("settled fields: %+v, %v", rewritten, err)
	}
	if _, err := in.pool.Exec(ctx, `update `+factorysettings.Table+` set harm_mark_pages = true`); err != nil {
		t.Fatal(err)
	}
	if rewritten, err := in.factory.Rederive(ctx, owner); err != nil || len(rewritten) != 1 {
		t.Fatalf("restore switch: %+v, %v", rewritten, err)
	}
	settings, err := factorysettings.Get(ctx, in.pool)
	if err != nil || settings.HarmMarkPages {
		t.Fatalf("switch not restored: %+v, %v", settings, err)
	}
	if rewritten, err := in.factory.Rederive(ctx, owner); err != nil || len(rewritten) != 0 {
		t.Fatalf("second restore: %+v, %v", rewritten, err)
	}
}

func TestProvisioningChangesCredentialReferencesAndRestoresThem(t *testing.T) {
	ctx, in := newFactory(t)
	first, err := in.factory.MarkServiceProvisioned(ctx, owner, in.service.ID, service.ShapeOne, secretref.MustNew("branch.first"), secretref.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := in.factory.MarkServiceProvisioned(ctx, owner, in.service.ID, service.ShapeTwo, secretref.MustNew("branch.second"), secretref.MustNew("master.second"))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatal("a changed credential pair was treated as a retry")
	}
	if _, err := in.factory.AuthorWindowLimit(ctx, owner, in.service.ID, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := in.pool.Exec(ctx, `update `+service.Table+` set provisioned_at = '', repository_credential_shape = '', repository_credential_branch = '', repository_credential_master = '' where id = $1`, in.service.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := in.factory.Rederive(ctx, owner); err != nil {
		t.Fatal(err)
	}
	svc, err := service.Get(ctx, in.pool, in.service.ID)
	if err != nil {
		t.Fatal(err)
	}
	if svc.Provisioned.Shape != service.ShapeTwo || svc.Provisioned.BranchCredential.Name() != "branch.second" || svc.Provisioned.MasterCredential.Name() != "master.second" {
		t.Fatalf("wrong restored provisioning: %+v", svc.Provisioned)
	}
	if rewritten, err := in.factory.Rederive(ctx, owner); err != nil || len(rewritten) != 0 {
		t.Fatalf("settled provisioning: %+v, %v", rewritten, err)
	}
}

func TestExplicitThresholdTracksNewestAndApprovedWithdrawals(t *testing.T) {
	ctx, in := newFactory(t)
	subject := safeguard.Subject{Kind: safeguard.SubjectService, ID: in.service.ID, Key: string(gatepolicy.QuantityErrorRate)}
	add := func(parameter gatepolicy.Parameter, number float64) safeguard.Safeguard {
		t.Helper()
		placed, _, err := in.factory.AddSafeguard(ctx, owner, parameter, subject, safeguard.Bound{Number: number}, safeguard.Routing{})
		if err != nil {
			t.Fatal(err)
		}
		return placed
	}
	check := func(number, size float64, present bool) {
		t.Helper()
		svc, err := service.Get(ctx, in.pool, in.service.ID)
		if err != nil {
			t.Fatal(err)
		}
		got, found := svc.ExplicitThreshold[gatepolicy.QuantityErrorRate]
		if found != present || (found && (got.Number != number || got.Size != size)) {
			t.Fatalf("explicit threshold = %+v, present %v; want %v/%v, present %v", got, found, number, size, present)
		}
	}
	add(gatepolicy.ExplicitThreshold, .3)
	newest := add(gatepolicy.ExplicitThreshold, .2)
	add(gatepolicy.ExplicitThresholdSize, .1)
	check(.2, .1, true)
	pending, _, err := in.factory.WriteSafeguardWithdrawal(ctx, owner, newest.ID)
	if err != nil {
		t.Fatal(err)
	}
	check(.2, .1, true)
	if _, err := in.factory.ApproveSafeguardWithdrawal(ctx, approver, pending.ID, decidedAt); err != nil {
		t.Fatal(err)
	}
	check(.3, .1, true)
	if err := withdraw(t, ctx, in, gatepolicy.ExplicitThresholdSize, subject); err != nil {
		t.Fatal(err)
	}
	check(0, 0, false)
}

func TestNonDeployRowAgainstCustomerEnvironmentReadsProduction(t *testing.T) {
	ctx, in := newFactory(t)
	custom, _, err := in.factory.CreateEnvironment(ctx, owner, environment.Spec{Kind: environment.KindCustomer, ProjectID: in.project.ID, Name: "preview", Platform: environment.Platform{Name: "local", Credential: credential, CanComposeOnDemand: true}, Targets: []environment.Target{{Address: "/srv/preview"}}, Credential: credential})
	if err != nil {
		t.Fatal(err)
	}
	production, found, err := environment.Production(ctx, in.pool, in.project.ID)
	if err != nil || !found {
		t.Fatalf("production: %v", err)
	}
	for _, authored := range []struct {
		id, row string
		value   float64
	}{{production.ID, "merge_to_master", .3}, {custom.ID, "merge_to_master", .9}, {custom.ID, "deploy_to_environment:" + custom.ID, .7}} {
		if _, err := in.factory.AuthorGateThreshold(ctx, owner, authored.id, authored.row, authored.value); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct {
		name      string
		threshold float64
	}{{"merge_to_master", .3}, {"deploy_to_environment:" + custom.ID, .7}} {
		applied, err := in.reader.AtGate(ctx, ownerReading, policy.Subjects{GateRow: row.name, EnvironmentID: custom.ID})
		if err != nil || applied.Threshold != row.threshold {
			t.Fatalf("%s = %+v, %v", row.name, applied, err)
		}
	}
}

func TestMissingRateReaderRefusesAndEmptyLatestRateDoesNotUseOldBaseline(t *testing.T) {
	ctx, in := newFactory(t)
	in.factory.AutoPassRates = nil
	if _, err := in.factory.AuthorGateThreshold(ctx, owner, in.prod.ID, "merge_to_master", .3); err == nil {
		t.Fatal("nil rate reader admitted")
	}
	in.factory.AutoPassRates = func(context.Context, policy.Scope, string, float64) ([]policy.AutoPassRate, error) {
		return []policy.AutoPassRate{{FactorSet: "item", Rate: .8}}, nil
	}
	if _, err := in.factory.AuthorGateThreshold(ctx, owner, in.prod.ID, "merge_to_master", .3); err != nil {
		t.Fatal(err)
	}
	in.factory.AutoPassRates = func(context.Context, policy.Scope, string, float64) ([]policy.AutoPassRate, error) { return nil, nil }
	if _, err := in.factory.AuthorGateThreshold(ctx, owner, in.prod.ID, "merge_to_master", .6); err != nil {
		t.Fatal(err)
	}
	rates, found, err := in.reader.AuthoredAutoPassRate(ctx, ownerReading, policy.Scope{Kind: policy.ScopeEnvironment, ID: in.prod.ID}, "merge_to_master")
	if err != nil || found || len(rates) != 0 {
		t.Fatalf("empty latest threshold reused an old baseline: %+v, %v, %v", rates, found, err)
	}
}
