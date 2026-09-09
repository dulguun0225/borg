package policy_test

import (
	"slices"
	"testing"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/service"
)

// TestRederiveWritesBackWhatTheNewestVersionNames: the factory's start
// re-derives every authored field from the newest version naming its scope,
// which is what finishes a write a stop interrupted.
func TestRederiveWritesBackWhatTheNewestVersionNames(t *testing.T) {
	ctx, in := newFactory(t)

	if _, err := in.factory.AuthorWindowLimit(ctx, owner, in.service.ID, 3); err != nil {
		t.Fatalf("AuthorWindowLimit: %v", err)
	}

	// A field moved out from under the version is what a stop between the two
	// writes leaves, and the store is put in that state directly here because
	// nothing in the factory can write one without the other.
	if _, err := in.pool.Exec(ctx, `update `+service.Table+` set window_limit = null where id = $1`,
		in.service.ID); err != nil {
		t.Fatalf("clearing the field: %v", err)
	}

	rewritten, err := in.factory.Rederive(ctx, owner)
	if err != nil {
		t.Fatalf("Rederive: %v", err)
	}
	if len(rewritten) != 1 {
		t.Fatalf("the re-derivation rewrote %d fields, want the one that lost its value", len(rewritten))
	}
	if rewritten[0].Value.Parameter != gatepolicy.WindowLimit || rewritten[0].Held.Present {
		t.Errorf("the re-derivation reports %+v", rewritten[0])
	}
	svc, err := service.Get(ctx, in.pool, in.service.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !svc.Parameters.WindowLimit.Present || svc.Parameters.WindowLimit.Number != 3 {
		t.Errorf("the field reads %+v after the re-derivation, want the authored 3", svc.Parameters.WindowLimit)
	}

	// A re-derivation that finds the two already agreeing writes nothing, and
	// it appends no version either way.
	before := newestVersion(t, ctx, in)
	again, err := in.factory.Rederive(ctx, owner)
	if err != nil {
		t.Fatalf("Rederive again: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("a re-derivation over fields that agree rewrote %v", again)
	}
	if after := newestVersion(t, ctx, in); after.ID != before.ID {
		t.Errorf("the re-derivation appended a version: %s over %s", after.ID, before.ID)
	}
}

// TestRederiveWritesBackEveryFieldTheVersionNames is
// ../../end-goal/one-process.md's "Factory's restart reads the newest policy
// version per scope and rewrites any owner-authored field not already holding
// what that version names", and
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md's
// "the factory's start re-derives every authored field".
//
// Every one of these was skipped: each names two values or none the version
// carried a parameter for, so the re-derivation passed over the objective and
// its period, the paging hours, the service's targets, the product licence, the
// search budget's two numbers, the operation cap and its overflow, a
// change-freeze period, seam 5 and the ceiling on concurrent candidate
// environments — the writes an owner makes and a stop can interrupt.
func TestRederiveWritesBackEveryFieldTheVersionNames(t *testing.T) {
	ctx, in := newFactory(t)

	authored := []struct {
		parameter gatepolicy.Parameter
		write     func() (policy.Version, error)
	}{
		{gatepolicy.Objective, func() (policy.Version, error) {
			return in.factory.AuthorObjective(ctx, owner, in.service.ID, 0.99, 30*24*3600)
		}},
		{gatepolicy.PagingHours, func() (policy.Version, error) {
			return in.factory.AuthorPagingHours(ctx, owner, in.service.ID,
				service.PagingHours{Start: "09:00", End: "17:00", Zone: "UTC"})
		}},
		{gatepolicy.ServiceTargets, func() (policy.Version, error) {
			return in.factory.SetServiceTargets(ctx, owner, in.service.ID,
				[]string{"/srv/targets"}, []string{"/srv/targets"})
		}},
		{gatepolicy.ProductLicence, func() (policy.Version, error) {
			return in.factory.AuthorProductLicence(ctx, owner, in.service.ID, "Apache-2.0")
		}},
		{gatepolicy.SearchBudget, func() (policy.Version, error) {
			return in.factory.AuthorSearchBudget(ctx, owner, in.service.ID, 20, 3600)
		}},
		{gatepolicy.OperationCap, func() (policy.Version, error) {
			return in.factory.AuthorOperationCap(ctx, owner, in.service.ID, 40, "overflow")
		}},
		{gatepolicy.ChangeFreeze, func() (policy.Version, error) {
			return in.factory.AuthorChangeFreezePeriod(ctx, owner, in.service.ID,
				"2026-12-24T00:00:00.000000000Z", "2027-01-02T00:00:00.000000000Z")
		}},
		{gatepolicy.Seam5Enforced, func() (policy.Version, error) {
			return in.factory.SetSeam5Enforced(ctx, owner)
		}},
		{gatepolicy.MaxConcurrentCandidateEnvironments, func() (policy.Version, error) {
			return in.factory.SetMaxConcurrentCandidateEnvironments(ctx, owner, in.prod.ID, 4)
		}},
		{gatepolicy.RemediationPeriod, func() (policy.Version, error) {
			return in.factory.AuthorRemediationPeriod(ctx, owner, 7, 14*24*3600)
		}},
		{gatepolicy.ReportChannelRate, func() (policy.Version, error) {
			return in.factory.AuthorReportChannelRate(ctx, owner, 200)
		}},
		{gatepolicy.ServiceReportChannelRate, func() (policy.Version, error) {
			return in.factory.AuthorServiceReportChannelRate(ctx, owner, in.service.ID, 50)
		}},
		{gatepolicy.HarmMarkPageCap, func() (policy.Version, error) {
			return in.factory.AuthorHarmMarkPageCap(ctx, owner, in.service.ID, 3, 3600)
		}},
	}
	for _, a := range authored {
		version, err := a.write()
		if err != nil {
			t.Fatalf("authoring %s: %v", a.parameter, err)
		}
		if version.Parameter != a.parameter {
			t.Errorf("authoring %s appended a version naming %q", a.parameter, version.Parameter)
		}
	}

	// The fields moved out from under the version, which is what a stop between
	// the two writes leaves. Nothing in the factory can write one without the
	// other, so the store is put in that state directly.
	for _, statement := range []string{
		`update ` + service.Table + ` set objective = null, objective_period_seconds = null,
			paging_hours_start = '', paging_hours_end = '', paging_hours_zone = '', targets = '',
			product_licence = '', search_budget_builds = null, search_budget_seconds = null,
			operation_cap = null, overflow_operation = '' where id = $1`,
	} {
		if _, err := in.pool.Exec(ctx, statement, in.service.ID); err != nil {
			t.Fatalf("clearing the fields: %v", err)
		}
	}
	if _, err := in.pool.Exec(ctx, `delete from `+service.ChangeFreezeTable+` where service_id = $1`,
		in.service.ID); err != nil {
		t.Fatalf("clearing the freeze periods: %v", err)
	}
	if _, err := in.pool.Exec(ctx, `update `+factorysettings.Table+`
		set seam_5_enforced = false, report_channel_rate = null`); err != nil {
		t.Fatalf("clearing the settings fields: %v", err)
	}
	if _, err := in.pool.Exec(ctx, `delete from `+factorysettings.ReportChannelRateTable); err != nil {
		t.Fatalf("clearing the per-service rate: %v", err)
	}
	if _, err := in.pool.Exec(ctx, `delete from `+factorysettings.RemediationPeriodTable); err != nil {
		t.Fatalf("clearing the remediation period: %v", err)
	}
	if _, err := in.pool.Exec(ctx, `delete from `+factorysettings.PageCapTable); err != nil {
		t.Fatalf("clearing the page cap: %v", err)
	}
	if _, err := in.pool.Exec(ctx, `update `+environment.Table+`
		set max_concurrent_candidate_environments = 0 where id = $1`, in.prod.ID); err != nil {
		t.Fatalf("clearing the candidate ceiling: %v", err)
	}

	rewritten, err := in.factory.Rederive(ctx, owner)
	if err != nil {
		t.Fatalf("Rederive: %v", err)
	}
	restored := map[gatepolicy.Parameter]bool{}
	for _, r := range rewritten {
		restored[r.Value.Parameter] = true
	}
	for _, a := range authored {
		if !restored[a.parameter] {
			t.Errorf("the re-derivation passed over %s", a.parameter)
		}
	}

	svc, err := service.Get(ctx, in.pool, in.service.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// The pair is written together: a record holding the objective and not its
	// period is a state its own CHECK refuses.
	if svc.Objective.Target.Number != 0.99 || svc.Objective.PeriodSeconds.Number != 30*24*3600 {
		t.Errorf("the objective reads %+v, want 0.99 over 30 days", svc.Objective)
	}
	if svc.SearchBudgetBuilds.Number != 20 || svc.SearchBudgetSeconds.Number != 3600 {
		t.Errorf("the search budget reads %v builds over %v seconds, want 20 over 3600",
			svc.SearchBudgetBuilds.Number, svc.SearchBudgetSeconds.Number)
	}
	if svc.OperationCap.Number != 40 || svc.OverflowOperation != "overflow" {
		t.Errorf("the operation cap reads %v into %q, want 40 into overflow",
			svc.OperationCap.Number, svc.OverflowOperation)
	}
	if svc.PagingHours != (service.PagingHours{Start: "09:00", End: "17:00", Zone: "UTC"}) {
		t.Errorf("the paging hours read %+v", svc.PagingHours)
	}
	if svc.ProductLicence != "Apache-2.0" || !slices.Equal(svc.Targets, []string{"/srv/targets"}) {
		t.Errorf("the licence reads %q and the targets %v", svc.ProductLicence, svc.Targets)
	}
	periods, err := service.FreezePeriods(ctx, in.pool, in.service.ID)
	if err != nil {
		t.Fatalf("FreezePeriods: %v", err)
	}
	if len(periods) != 1 {
		t.Errorf("%d change-freeze periods stand after the re-derivation, want the one authored", len(periods))
	}

	settings, err := factorysettings.Get(ctx, in.pool)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !settings.Seam5Enforced {
		t.Error("seam 5 reads as unenforced after the re-derivation, and the version names it on")
	}
	if settings.ReportChannelRate.Number != 200 {
		t.Errorf("the factory-wide report channel rate reads %+v, want 200", settings.ReportChannelRate)
	}
	env, err := environment.Get(ctx, in.pool, in.prod.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if env.MaxConcurrentCandidateEnvironments != 4 {
		t.Errorf("the candidate ceiling reads %d, want 4", env.MaxConcurrentCandidateEnvironments)
	}

	// A re-derivation that finds every field agreeing writes nothing.
	again, err := in.factory.Rederive(ctx, owner)
	if err != nil {
		t.Fatalf("Rederive again: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("a re-derivation over fields that agree rewrote %v", again)
	}
}
