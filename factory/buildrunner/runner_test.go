package buildrunner_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/buildrunner"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

type fakeClone struct {
	checkouts []buildrunner.Checkout
	requests  []buildrunner.SelectionRequest
	pushes    []buildrunner.PushRequest
}

func (f *fakeClone) Select(_ context.Context, request buildrunner.SelectionRequest) (buildrunner.Checkout, error) {
	f.requests = append(f.requests, request)
	checkout := f.checkouts[0]
	f.checkouts = f.checkouts[1:]
	return checkout, nil
}

func (f *fakeClone) Push(_ context.Context, request buildrunner.PushRequest) error {
	f.pushes = append(f.pushes, request)
	return nil
}

type fakeCredentials struct{ value string }

func (f fakeCredentials) Resolve(context.Context, string) (string, error) { return f.value, nil }

type fakeResolver struct {
	resolution  buildrunner.Resolution
	resolutions []buildrunner.Resolution
	calls       int
}

func (f *fakeResolver) Resolve(_ context.Context, _ buildrunner.Checkout, _ string, _ []string) (buildrunner.Resolution, error) {
	f.calls++
	if len(f.resolutions) > 0 {
		resolution := f.resolutions[0]
		f.resolutions = f.resolutions[1:]
		return resolution, nil
	}
	return f.resolution, nil
}

type fakeProcess struct {
	calls int
	input buildrunner.ProcessInput
}

func (f *fakeProcess) Run(_ context.Context, input buildrunner.ProcessInput) (buildrunner.ProcessOutput, error) {
	f.calls++
	f.input = input
	return buildrunner.ProcessOutput{ArtifactDigest: "sha256:artifact"}, nil
}

type fakeSchema struct{ declared bool }

func (f fakeSchema) Read(context.Context, buildrunner.Checkout) (buildrunner.SchemaReading, error) {
	return buildrunner.SchemaReading{Declares: f.declared}, nil
}

type fakeWriter struct {
	drafts    []build.Draft
	completed []build.Completion
}

func (f *fakeWriter) Create(_ context.Context, draft build.Draft) (build.Build, error) {
	f.drafts = append(f.drafts, draft)
	return build.Build{ID: "bl_test", CommitHash: draft.CommitHash, ServiceID: draft.ServiceID}, nil
}

func (f *fakeWriter) Complete(_ context.Context, id string, completion build.Completion) (build.Build, error) {
	completion.BuildID = id
	f.completed = append(f.completed, completion)
	state := completion.RunState
	return build.Build{ID: id, CommitHash: f.drafts[0].CommitHash, ServiceID: f.drafts[0].ServiceID,
		RunState: state, ArtifactDigest: completion.ArtifactDigest}, nil
}

func TestRunnerResolvesBeforeProcessAndRecordsExposure(t *testing.T) {
	dir, commit := repository(t)
	clone := &fakeClone{checkouts: []buildrunner.Checkout{{Directory: dir, Base: buildrunner.EmptyTree, Commit: commit}}}
	resolver := &fakeResolver{resolution: buildrunner.Resolution{
		Entries:  []build.ResolvedEntry{{Package: "example.com/x", Version: "v1", Digest: "h1:new", Licence: "MIT"}},
		Coverage: []build.Coverage{{Ecosystem: "go", Source: "go.sum", Digests: true, FetchWithoutRunning: true}},
	}}
	process := &fakeProcess{}
	writer := &fakeWriter{}
	runner := buildrunner.New(buildrunner.Config{
		Clone: clone, Resolver: resolver, Process: process, Schema: fakeSchema{declared: true},
		Builds: writer, ShippedBundle: "bundle-test",
	})

	result, err := runner.Build(t.Context(), buildrunner.Request{
		Selection: buildrunner.SelectionRequest{Directory: dir, Branch: "item"},
		ServiceID: "svc", ItemID: "item",
		CurrentRelease: []build.ResolvedEntry{{Package: "example.com/x", Version: "v1", Digest: "h1:old"}},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if resolver.calls != 1 || process.calls != 1 {
		t.Fatalf("resolver calls %d, process calls %d, want one each", resolver.calls, process.calls)
	}
	if process.input.Overlay == "" {
		t.Fatalf("process input omitted overlay: %+v", process.input)
	}
	if len(writer.drafts) != 1 || writer.drafts[0].CommitHash != commit || len(writer.completed) != 1 {
		t.Fatalf("drafts = %+v, want the selected commit", writer.drafts)
	}
	if !writer.drafts[0].DeclaresSchemaChange || result.Build.ID == "" {
		t.Fatalf("result = %+v, draft = %+v", result.Build, writer.drafts[0])
	}
	if len(writer.drafts[0].Exposure.DependencyChanges) != 1 {
		t.Fatalf("digest-only dependency change = %+v", writer.drafts[0].Exposure)
	}
}

func TestRunnerPushUsesCredentialAndRefusesMaster(t *testing.T) {
	clone := &fakeClone{}
	runner := buildrunner.New(buildrunner.Config{Clone: clone, RepositorySecrets: fakeCredentials{value: "token"}})
	checkout := buildrunner.Checkout{Branch: "candidate"}
	if err := runner.Push(t.Context(), checkout, "candidate", "repository.local"); err != nil {
		t.Fatalf("Push candidate: %v", err)
	}
	if len(clone.pushes) != 1 || clone.pushes[0].Credential != "token" {
		t.Fatalf("candidate pushes = %+v, want the resolved credential", clone.pushes)
	}
	err := runner.Push(t.Context(), checkout, "master", "repository.local")
	if err == nil || len(clone.pushes) != 1 {
		t.Fatalf("Push to master = %v with pushes %+v, want refusal before clone", err, clone.pushes)
	}
}

func TestExcludedSourceWritesDidNotRunAndDoesNotCallProcess(t *testing.T) {
	dir, commit := repository(t)
	process := &fakeProcess{}
	writer := &fakeWriter{}
	runner := buildrunner.New(buildrunner.Config{
		Clone: &fakeClone{checkouts: []buildrunner.Checkout{{Directory: dir, Base: buildrunner.EmptyTree, Commit: commit}}},
		Resolver: &fakeResolver{resolution: buildrunner.Resolution{
			Entries:  []build.ResolvedEntry{{Package: "x", Source: "private", Digest: "sha256:x"}},
			Coverage: []build.Coverage{{Ecosystem: "go", Source: "private", Digests: true, FetchWithoutRunning: true}},
		}},
		Process: process, Schema: fakeSchema{}, Builds: writer, ShippedBundle: "bundle-test",
	})
	if _, err := runner.Build(t.Context(), buildrunner.Request{
		Selection: buildrunner.SelectionRequest{Directory: dir, Branch: "candidate"},
		ItemID:    "item", ServiceID: "service", SourceConstraint: buildrunner.SourceConstraint{Allowed: []string{"public"}},
	}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if process.calls != 0 || len(writer.drafts) != 1 || len(writer.completed) != 1 || writer.completed[0].RunState != build.RunDidNotRun {
		t.Fatalf("process calls %d, drafts %+v, completions %+v; want one did-not-run draft and no process", process.calls, writer.drafts, writer.completed)
	}
}

func TestCouldNotDeriveRemainsUnavailableAndSearchHasNoItem(t *testing.T) {
	dir, commit := repository(t)
	clone := &fakeClone{checkouts: []buildrunner.Checkout{
		{Directory: dir, Base: buildrunner.EmptyTree, Commit: commit},
		{Directory: dir, Base: buildrunner.EmptyTree, Commit: commit},
	}}
	resolver := &fakeResolver{resolution: buildrunner.Resolution{CouldNotDerive: "no resolver coverage"}}
	writer := &fakeWriter{}
	runner := buildrunner.New(buildrunner.Config{
		Clone: clone, Resolver: resolver, Process: &fakeProcess{}, Schema: fakeSchema{},
		Builds: writer, ShippedBundle: "bundle-test",
	})

	if _, err := runner.Build(t.Context(), buildrunner.Request{
		Selection: buildrunner.SelectionRequest{Directory: dir, Branch: "item"},
		ItemID:    "item", ServiceID: "svc",
	}); err != nil {
		t.Fatalf("could-not-derive Build: %v", err)
	}
	if _, err := runner.Build(t.Context(), buildrunner.Request{
		Selection: buildrunner.SelectionRequest{Directory: dir, Commit: commit, Mode: buildrunner.DetachedCommit},
		ServiceID: "svc", DesignSystemConstraint: "constraint",
		SearchOrigin: &buildrunner.SearchOrigin{BuildID: "bl_origin", DesignSystemConstraintID: "constraint"},
	}); err != nil {
		t.Fatalf("search Build: %v", err)
	}
	if writer.drafts[0].ResolvedSetCouldNotDerive == "" || writer.drafts[0].Exposure.Unavailable == "" {
		t.Fatalf("could-not-derive draft = %+v", writer.drafts[0])
	}
	if writer.drafts[1].ItemID != "" || writer.drafts[1].DesignSystemConstraintID != "constraint" {
		t.Fatalf("search draft = %+v", writer.drafts[1])
	}
}

func TestGitCloneAdoptsTheExistingTrunkForAVisibleBranch(t *testing.T) {
	dir, _ := repository(t)
	clone := buildrunner.GitClone{}
	checkout, err := clone.Select(t.Context(), buildrunner.SelectionRequest{
		Directory: dir, Branch: "candidate", Base: "master", Mode: buildrunner.CandidateBranch, Credential: "token",
	})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if checkout.Branch != "candidate" || checkout.Commit == "" {
		t.Fatalf("checkout = %+v, want the adopted repository branch", checkout)
	}
}

func TestGitCloneClonesARepositoryAndBranchesFromItsHead(t *testing.T) {
	source, commit := repository(t)
	destination := filepath.Join(t.TempDir(), "service")
	checkout, err := (buildrunner.GitClone{}).Select(t.Context(), buildrunner.SelectionRequest{
		Directory: destination, Repository: source, Branch: "candidate", Mode: buildrunner.CandidateBranch, Credential: "token",
	})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if checkout.Commit != commit || checkout.Branch != "candidate" {
		t.Fatalf("checkout = %+v, want source head on candidate", checkout)
	}
	branch := exec.Command("git", "-C", destination, "branch", "--show-current")
	if out, err := branch.Output(); err != nil || strings.TrimSpace(string(out)) != "candidate" {
		t.Fatalf("candidate branch = %q, %v", out, err)
	}
}

func TestGoResolverUsesOnlyItsFetchEnvironmentAndRecordsCoverage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeBin := t.TempDir()
	fakeGo := filepath.Join(fakeBin, "go")
	if err := os.WriteFile(fakeGo, []byte("#!/bin/sh\n/usr/bin/env > \"$PWD/resolver-env\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin)
	t.Setenv("FACTORY_ONLY_VARIABLE", "must-not-cross")
	resolution, err := (buildrunner.GoResolver{}).Resolve(t.Context(), buildrunner.Checkout{Directory: dir}, "repository-secret", []string{"registry-secret"})
	if err != nil || resolution.CouldNotDerive != "" {
		t.Fatalf("Resolve = %+v, %v", resolution, err)
	}
	env, err := os.ReadFile(filepath.Join(dir, "resolver-env"))
	if err != nil {
		t.Fatalf("resolver environment: %v", err)
	}
	if strings.Contains(string(env), "FACTORY_ONLY_VARIABLE") {
		t.Fatalf("factory environment crossed into resolver: %s", env)
	}
	if !strings.Contains(string(env), "BORG_REPOSITORY_CREDENTIAL=repository-secret") {
		t.Fatalf("resolved repository credential missing from fetch environment: %s", env)
	}
	if len(resolution.Coverage) != 1 || !resolution.Coverage[0].VendoredSource || !resolution.Coverage[0].StaticallyLinkedCode || resolution.Coverage[0].BaseImagePackages {
		t.Fatalf("coverage = %+v, want typed Go coverage", resolution.Coverage)
	}
}

func TestGoResolverRecordsModuleSourcesAndRuntimeMarks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.sum"), []byte("example.com/runtime v1.0.0 h1:runtime\nexample.com/testonly v1.0.0 h1:test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main_test.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeBin := t.TempDir()
	fakeGo := filepath.Join(fakeBin, "go")
	script := "#!/bin/sh\nif [ \"$1\" = mod ]; then exit 0; fi\nif [ \"$3\" = -test ]; then echo example.test; echo example.com/testonly; else echo example.test; echo example.com/runtime; fi\n"
	if err := os.WriteFile(fakeGo, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin)
	t.Setenv("GOPROXY", "direct")
	resolution, err := (buildrunner.GoResolver{}).Resolve(t.Context(), buildrunner.Checkout{Directory: dir}, "", nil)
	if err != nil || resolution.CouldNotDerive != "" {
		t.Fatalf("Resolve = %+v, %v", resolution, err)
	}
	if len(resolution.Entries) != 2 || resolution.Entries[0].Source != "direct:example.com" {
		t.Fatalf("entries = %+v, want direct module source", resolution.Entries)
	}
	if !resolution.Entries[0].RunTime || resolution.Entries[0].BuildTime || resolution.Entries[1].RunTime || !resolution.Entries[1].BuildTime {
		t.Fatalf("entry timing = %+v, want runtime and test-only marks", resolution.Entries)
	}
	if len(resolution.Coverage) != 1 || resolution.Coverage[0].BaseImagePackages || resolution.Coverage[0].Source != "direct" {
		t.Fatalf("coverage = %+v, want explicit base-image absence and source", resolution.Coverage)
	}
}

func TestRunnerRecordsFetchAndRunLimitationBesideCoverage(t *testing.T) {
	dir, commit := repository(t)
	writer := &fakeWriter{}
	runner := buildrunner.New(buildrunner.Config{
		Clone: &fakeClone{checkouts: []buildrunner.Checkout{{Directory: dir, Base: buildrunner.EmptyTree, Commit: commit}}},
		Resolver: &fakeResolver{resolution: buildrunner.Resolution{
			Entries:  []build.ResolvedEntry{{Package: "x", Source: "proxy", Version: "v1", Digest: "h1:x", Licence: "MIT"}},
			Coverage: []build.Coverage{{Ecosystem: "go", Source: "proxy", Digests: true, FetchWithoutRunning: false}},
		}},
		Process: &fakeProcess{}, Schema: fakeSchema{}, Builds: writer, ShippedBundle: "bundle-test",
	})
	if _, err := runner.Build(t.Context(), buildrunner.Request{
		Selection: buildrunner.SelectionRequest{Directory: dir, Branch: "candidate"}, ItemID: "item", ServiceID: "service",
	}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(writer.drafts) != 1 || writer.drafts[0].Coverage[0].FetchWithoutRunningReason == "" {
		t.Fatalf("draft coverage = %+v, want recorded limitation", writer.drafts)
	}
}

func TestRunnerWritesNormalCouldNotDeriveAndSearchBuilds(t *testing.T) {
	ctx, pool, writer := database(t)
	dir, commit := repository(t)
	clone := &fakeClone{checkouts: []buildrunner.Checkout{
		{Directory: dir, Base: buildrunner.EmptyTree, Commit: commit},
		{Directory: dir, Base: buildrunner.EmptyTree, Commit: commit},
		{Directory: dir, Base: buildrunner.EmptyTree, Commit: commit},
	}}
	resolver := &fakeResolver{resolutions: []buildrunner.Resolution{
		{Entries: []build.ResolvedEntry{{Ecosystem: "go", Source: "go.sum", Package: "x", Version: "v1", Digest: "h1:x", Licence: "MIT"}}, Coverage: []build.Coverage{{Ecosystem: "go", Source: "go.sum", Digests: true, FetchWithoutRunning: true}}},
		{CouldNotDerive: "resolver unavailable"},
		{Coverage: []build.Coverage{{Ecosystem: "go", Source: "go.sum", Digests: true, FetchWithoutRunning: true}}},
	}}
	normal := buildrunner.New(buildrunner.Config{
		Clone: clone, Resolver: resolver, Process: &fakeProcess{}, Schema: fakeSchema{},
		Builds: dbWriter{writer: writer}, ShippedBundle: "bundle-test",
	})
	origin, err := writer.Create(ctx, record.Actor{Kind: record.KindComponent, Key: "origin", Basis: record.BasisClaimed}, build.Draft{
		ItemID: "origin-item", ServiceID: "svc", CommitHash: "origin-commit", ArtifactDigest: "sha256:origin",
		DesignSystemConstraintID: "constraint", ShippedBundleIdentity: "bundle-test",
		Coverage: []build.Coverage{{Ecosystem: "test", Source: "fixture"}},
	})
	if err != nil {
		t.Fatalf("origin Create: %v", err)
	}
	for _, request := range []buildrunner.Request{
		{Selection: buildrunner.SelectionRequest{Directory: dir, Branch: "normal"}, ItemID: "it_normal", ServiceID: "svc"},
		{Selection: buildrunner.SelectionRequest{Directory: dir, Branch: "unknown"}, ItemID: "it_unknown", ServiceID: "svc"},
		{Selection: buildrunner.SelectionRequest{Directory: dir, Branch: "search", Commit: commit, Mode: buildrunner.DetachedCommit}, ServiceID: "svc",
			SearchOrigin: &buildrunner.SearchOrigin{BuildID: origin.ID, DesignSystemConstraintID: "constraint"}},
	} {
		if _, err := normal.Build(ctx, request); err != nil {
			t.Fatalf("Build %+v: %v", request, err)
		}
	}
	unknown, err := build.Get(ctx, pool, mustBuildID(ctx, pool, "it_unknown"))
	if err != nil {
		t.Fatalf("Get could-not-derive: %v", err)
	}
	if unknown.ItemID != "it_unknown" || unknown.ResolvedSetCouldNotDerive == "" {
		t.Fatalf("unknown build = %+v", unknown)
	}
}

type dbWriter struct{ writer *build.Writer }

func (w dbWriter) Create(ctx context.Context, draft build.Draft) (build.Build, error) {
	return w.writer.Create(ctx, record.Actor{Kind: record.KindComponent, Key: "buildrunner", Basis: record.BasisClaimed}, draft)
}

func (w dbWriter) Complete(ctx context.Context, id string, completion build.Completion) (build.Build, error) {
	completion.BuildID = id
	completion.Actor = record.Actor{Kind: record.KindComponent, Key: "buildrunner", Basis: record.BasisClaimed}
	return w.writer.Complete(ctx, completion)
}

func mustBuildID(ctx context.Context, pool *pgxpool.Pool, itemID string) string {
	var id string
	if err := pool.QueryRow(ctx, "select id from build where item_id = $1", itemID).Scan(&id); err != nil {
		panic(err)
	}
	return id
}

func repository(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "master")
	run("config", "user.name", "test")
	run("config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "initial")
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return dir, strings.TrimSpace(string(out))
}

func database(t *testing.T) (context.Context, *pgxpool.Pool, *build.Writer) {
	t.Helper()
	ctx := t.Context()
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	schema := "m10_" + hex.EncodeToString(suffix[:])
	base, err := url.Parse(postgres.URL())
	if err != nil {
		t.Fatal(err)
	}
	query := base.Query()
	query.Set("search_path", schema)
	base.RawQuery = query.Encode()
	pool, err := postgres.Open(ctx, base.String())
	if err != nil {
		t.Fatalf("database: %v", err)
	}
	t.Cleanup(func() {
		cleanup := context.Background()
		_, _ = pool.Exec(cleanup, "drop schema if exists "+pgx.Identifier{schema}.Sanitize()+" cascade")
		pool.Close()
	})
	if _, err := pool.Exec(ctx, "create schema "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	for _, statement := range lease.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range build.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	token, err := lease.Acquire(ctx, pool, "buildrunner-test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, pool, build.NewWriter(pool, token)
}
