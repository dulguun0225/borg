package deploy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/targetseam"
)

type candidateEnvironment struct {
	err   error
	errs  []error
	calls int
}

func (e *candidateEnvironment) Compose(context.Context, record.Actor, string, string, []environment.Target,
	secretref.Ref, environment.Composition) (environment.Environment, error) {
	e.calls++
	if len(e.errs) >= e.calls {
		return environment.Environment{}, e.errs[e.calls-1]
	}
	return environment.Environment{}, e.err
}

func (e *candidateEnvironment) Recompose(context.Context, record.Actor, string, environment.Composition) error {
	return e.err
}

type candidateSource struct {
	dependencies []deploy.Dependency
	reads        int
}

func (s *candidateSource) Dependencies(context.Context, string, string, string) ([]deploy.Dependency, error) {
	s.reads++
	return s.dependencies, nil
}

func (*candidateSource) SeedVersions(context.Context, string) ([]deploy.Version, error) {
	return nil, deploy.ErrNoVersion
}

func (*candidateSource) ValueSetVersions(context.Context, string) ([]deploy.Version, error) {
	return nil, deploy.ErrNoVersion
}

func TestCompositionRetriesTheNarrowUnavailableAnswerBeforeOpeningAWait(t *testing.T) {
	source := &candidateSource{dependencies: []deploy.Dependency{{
		ServiceID: "svc_dependency", ReleaseID: "rel_1", ServiceName: "dependency",
		Reaches: []deploy.Reach{{Address: "/dependency", Target: targetseam.NewFake()}},
	}}}
	candidate := deploy.Candidate{
		ItemID: "it_1", ServiceID: "svc_candidate", ProductionID: "env_1",
		Principal: principal.OfComponent("deployer"), Credential: secretref.MustNew("deploy.local"),
	}
	if _, err := deploy.CompositionForCandidateRun(t.Context(), source, candidate, false); err == nil {
		t.Fatal("CompositionForCandidateRun after the first unavailable answer succeeded")
	}
	if source.reads != 2 {
		t.Fatalf("the first unavailable answer caused %d composition reads, want the retry", source.reads)
	}
	source.reads = 0
	if _, err := deploy.CompositionForCandidateRun(t.Context(), source, candidate, true); err == nil {
		t.Fatal("CompositionForCandidateRun after a wait succeeded")
	}
	if source.reads != 1 {
		t.Fatalf("the second unavailable answer caused %d composition reads, want no third retry", source.reads)
	}
}

func TestComposeCandidateClassifiesAnEnvironmentComposeFailure(t *testing.T) {
	underlying := errors.New("platform refused composition")
	_, err := deploy.ComposeCandidate(t.Context(), &candidateEnvironment{err: underlying},
		record.Actor{Kind: record.KindComponent, Key: "deployer", Basis: record.BasisClaimed},
		"item", "project", nil, secretref.MustNew("deploy.local"), environment.Composition{})
	if !errors.Is(err, deploy.ErrCandidateCompositionUnavailable) || !errors.Is(err, underlying) {
		t.Fatalf("ComposeCandidate error = %v, want the unavailable kind wrapping the environment error", err)
	}
}

func TestComposeCandidateForRunRetriesTheFirstEnvironmentComposeFailure(t *testing.T) {
	writer := &candidateEnvironment{errs: []error{errors.New("temporary platform failure"), nil}}
	_, err := deploy.ComposeCandidateForRun(t.Context(), writer,
		record.Actor{Kind: record.KindComponent, Key: "deployer", Basis: record.BasisClaimed},
		"item", "project", nil, secretref.MustNew("deploy.local"), environment.Composition{}, false)
	if err != nil {
		t.Fatalf("ComposeCandidateForRun: %v", err)
	}
	if writer.calls != 2 {
		t.Fatalf("ComposeCandidateForRun called the environment writer %d times, want the failed first attempt and its retry", writer.calls)
	}
}

func TestResolveValueSetLeavesAnUnavailableAddressForTheRun(t *testing.T) {
	values, unavailable, err := deploy.ResolveValueSet("API_URL=", nil)
	if err != nil {
		t.Fatalf("ResolveValueSet: %v", err)
	}
	if unavailable != "" {
		t.Fatalf("ResolveValueSet reported an unavailable address: %s", unavailable)
	}
	if len(values.Names) != 1 || values.Names[0] != "API_URL" || values.Values[0] != "" {
		t.Fatalf("resolved values = %+v, want the named value with no address", values)
	}
}

func TestResolveValueSetLeavesAnExternalAndSecretUnset(t *testing.T) {
	values, unavailable, err := deploy.ResolveValueSet(`{"API":{"external":"partner"},"TOKEN":{"secret":"missing"}}`, nil)
	if err != nil || unavailable != "" {
		t.Fatalf("ResolveValueSet = %+v, %q, %v", values, unavailable, err)
	}
	if len(values.Values) != 2 || values.Values[0] != "" || values.Values[1] != "" {
		t.Fatalf("resolved values = %+v, want both unresolved entries empty", values)
	}
}

func TestResolveValueSetKeepsNamedValuesInStableOrder(t *testing.T) {
	values, unavailable, err := deploy.ResolveValueSet(`{"Z":"z", "A":"a"}`, nil)
	if err != nil || unavailable != "" {
		t.Fatalf("ResolveValueSet = %+v, %q, %v", values, unavailable, err)
	}
	want := targetseam.ValueSet{Names: []string{"A", "Z"}, Values: []string{"a", "z"}}
	if len(values.Names) != len(want.Names) || values.Names[0] != want.Names[0] || values.Values[1] != want.Values[1] {
		t.Fatalf("resolved values = %+v, want %+v", values, want)
	}
}
