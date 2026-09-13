package buildrunner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/exposure"
	"github.com/dulguun0225/borg/factory/wayin"
)

const EmptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

var ErrDoesNotCompile = errors.New("buildrunner: the build does not compile")

type SelectionMode uint8

const (
	CandidateBranch SelectionMode = iota + 1
	DetachedCommit
)

type SelectionRequest struct {
	Directory      string
	Repository     string
	Branch         string
	Commit         string
	Base           string
	Mode           SelectionMode
	CredentialName string
	Credential     string
}

type Checkout struct {
	Directory string
	Branch    string
	Base      string
	Commit    string
}

type Clone interface {
	Select(context.Context, SelectionRequest) (Checkout, error)
	Push(context.Context, PushRequest) error
}

// PushRequest is the only repository write the runner permits. Branch names
// are checked by [Runner.Push] and the resolved credential is passed only to
// the clone seam.
type PushRequest struct {
	Checkout   Checkout
	Branch     string
	Credential string
}

type CredentialResolver interface {
	Resolve(context.Context, string) (string, error)
}

type Resolution struct {
	Entries        []build.ResolvedEntry
	Coverage       []build.Coverage
	CouldNotDerive string
	ModuleCache    string
}

type Resolver interface {
	Resolve(context.Context, Checkout, string, []string) (Resolution, error)
}

type ProcessInput struct {
	Checkout   Checkout
	Resolution Resolution
	Overlay    string
	Output     string
}

type ProcessOutput struct {
	ArtifactDigest string
	Results        map[string]criterion.Outcome
}

type Process interface {
	Run(context.Context, ProcessInput) (ProcessOutput, error)
}

type Schema interface {
	Read(context.Context, Checkout) (SchemaReading, error)
}

// SchemaReading is the schema declaration and the migration marks read beside
// it in the checkout.
type SchemaReading struct {
	Declares bool
	Marks    []string
}

type BuildWriter interface {
	Create(context.Context, build.Draft) (build.Build, error)
	Complete(context.Context, string, build.Completion) (build.Build, error)
}

type Config struct {
	Clone             Clone
	RepositorySecrets CredentialResolver
	RegistrySecrets   CredentialResolver
	Resolver          Resolver
	Process           Process
	Schema            Schema
	Builds            BuildWriter
	ShippedBundle     string
}

type Runner struct {
	clone             Clone
	repositorySecrets CredentialResolver
	registrySecrets   CredentialResolver
	resolver          Resolver
	process           Process
	schema            Schema
	builds            BuildWriter
	shippedBundle     string
}

type Request struct {
	Selection              SelectionRequest
	ItemID                 string
	ServiceID              string
	DesignSystemConstraint string
	CurrentRelease         []build.ResolvedEntry
	RepositoryCredential   string
	RegistryCredentials    []string
	Output                 string
	SourceConstraint       SourceConstraint
	SearchOrigin           *SearchOrigin
}

// SourceConstraint is the source allow-list in force for this build.
type SourceConstraint struct {
	Allowed []string
}

// SearchOrigin ties a search build to the release record it was made from.
type SearchOrigin struct {
	BuildID                  string
	DesignSystemConstraintID string
}

// ResolverToolchains is the factory version fact: these are the toolchains
// for which this package has a resolver.
func ResolverToolchains() []string { return []string{"go"} }

type Result struct {
	Build        build.Build
	Checkout     Checkout
	ArtifactPath string
}

func New(c Config) *Runner {
	return &Runner{clone: c.Clone, repositorySecrets: c.RepositorySecrets,
		registrySecrets: c.RegistrySecrets, resolver: c.Resolver, process: c.Process,
		schema: c.Schema, builds: c.Builds, shippedBundle: c.ShippedBundle}
}

func (r *Runner) Select(ctx context.Context, req SelectionRequest) (Checkout, error) {
	if r.clone == nil {
		return Checkout{}, errors.New("buildrunner: no clone seam")
	}
	credential, err := resolveOne(ctx, r.repositorySecrets, req.CredentialName)
	if err != nil {
		return Checkout{}, err
	}
	req.Credential = credential
	return r.clone.Select(ctx, req)
}

// Push permits a candidate branch push and refuses master through the runner.
func (r *Runner) Push(ctx context.Context, checkout Checkout, branch, credentialName string) error {
	if branch == "" || branch == "master" || branch != checkout.Branch {
		return errors.New("buildrunner: pushes are permitted only to the selected candidate branch")
	}
	credential, err := resolveOne(ctx, r.repositorySecrets, credentialName)
	if err != nil {
		return err
	}
	return r.clone.Push(ctx, PushRequest{Checkout: checkout, Branch: branch, Credential: credential})
}

func (r *Runner) Build(ctx context.Context, req Request) (Result, error) {
	if req.Selection.CredentialName == "" {
		req.Selection.CredentialName = req.RepositoryCredential
	}
	repositoryAccess, err := resolveOne(ctx, r.repositorySecrets, req.RepositoryCredential)
	if err != nil {
		return Result{}, err
	}
	if req.Selection.CredentialName != req.RepositoryCredential {
		repositoryAccess, err = resolveOne(ctx, r.repositorySecrets, req.Selection.CredentialName)
		if err != nil {
			return Result{}, err
		}
	}
	if r.clone == nil {
		return Result{}, errors.New("buildrunner: no clone seam")
	}
	req.Selection.Credential = repositoryAccess
	checkout, err := r.clone.Select(ctx, req.Selection)
	if err != nil {
		return Result{}, err
	}
	registryAccess, err := resolveMany(ctx, r.registrySecrets, req.RegistryCredentials)
	if err != nil {
		return Result{}, err
	}
	if r.resolver == nil || r.process == nil || r.schema == nil || r.builds == nil {
		return Result{}, errors.New("buildrunner: incomplete build seams")
	}
	resolution, err := r.resolver.Resolve(ctx, checkout, repositoryAccess, registryAccess)
	if err != nil {
		return Result{}, err
	}
	coverage := coverageWithReason(resolution)
	reading, err := r.schema.Read(ctx, checkout)
	if err != nil {
		return Result{}, err
	}
	evidence := r.exposure(ctx, checkout, req.CurrentRelease, resolution)
	draft := build.Draft{
		ItemID: req.ItemID, ServiceID: req.ServiceID, CommitHash: checkout.Commit,
		Resolved: resolution.Entries, Coverage: coverage,
		ResolvedSetCouldNotDerive: resolution.CouldNotDerive,
		NoticeFile:                notice(resolution), DesignSystemConstraintID: req.DesignSystemConstraint,
		ShippedBundleIdentity: r.shippedBundle, Exposure: &evidence,
		DeclaresSchemaChange: reading.Declares, SchemaMarks: reading.Marks,
		RunState: build.RunStarted,
	}
	if req.SearchOrigin != nil {
		draft.SearchBuild = true
		draft.SearchOriginBuildID = req.SearchOrigin.BuildID
		draft.DesignSystemConstraintID = req.SearchOrigin.DesignSystemConstraintID
	}
	made, err := r.builds.Create(ctx, draft)
	if err != nil {
		return Result{}, err
	}
	if reason := constraintViolation(req.SourceConstraint, resolution.Entries); reason != "" {
		completed, err := r.builds.Complete(ctx, made.ID, build.Completion{
			BuildID: made.ID, RunState: build.RunDidNotRun, RunReason: reason,
		})
		if err != nil {
			return Result{}, err
		}
		return Result{Build: completed, Checkout: checkout}, nil
	}

	output, remove, err := outputPath(req.Output)
	if err != nil {
		return Result{}, err
	}
	defer remove()
	overlay, err := wayin.Overlay(checkout.Directory, filepath.Dir(output), r.shippedBundle)
	if err != nil {
		return Result{}, err
	}
	processOutput, err := r.process.Run(ctx, ProcessInput{Checkout: checkout, Resolution: resolution, Overlay: overlay, Output: output})
	if err != nil {
		return Result{}, err
	}
	if processOutput.ArtifactDigest == "" {
		return Result{}, errors.New("buildrunner: process returned no artifact digest")
	}
	completed, err := r.builds.Complete(ctx, made.ID, build.Completion{
		BuildID: made.ID, ArtifactDigest: processOutput.ArtifactDigest,
		RunState: build.RunCompleted, Results: processOutput.Results,
	})
	if err != nil {
		return Result{}, err
	}
	return Result{Build: completed, Checkout: checkout, ArtifactPath: output}, nil
}

func coverageWithReason(resolution Resolution) []build.Coverage {
	coverage := append([]build.Coverage(nil), resolution.Coverage...)
	for i := range coverage {
		if !coverage[i].FetchWithoutRunning {
			coverage[i].FetchWithoutRunningReason = "the toolchain cannot separate fetching from running"
		}
		if !coverage[i].Digests {
			coverage[i].MissingDigests = "the resolved set has no content digests"
		}
	}
	digestless := false
	for _, entry := range resolution.Entries {
		if entry.Digest == "" {
			digestless = true
			break
		}
	}
	if digestless {
		found := false
		for _, item := range coverage {
			if item.MissingDigests != "" {
				found = true
				break
			}
		}
		if !found {
			coverage = append(coverage, build.Coverage{
				Ecosystem: "go", Source: "resolved", MissingDigests: "the resolved set has no content digests",
			})
		}
	}
	return coverage
}

func (r *Runner) exposure(ctx context.Context, checkout Checkout, current []build.ResolvedEntry,
	resolution Resolution) exposure.Evidence {
	if resolution.CouldNotDerive != "" {
		return exposure.Evidence{Unavailable: "the resolved set could not be derived: " + resolution.CouldNotDerive}
	}
	base := checkout.Base
	if base == "" {
		base = EmptyTree
	}
	evidence, _, err := exposure.Derive(ctx, exposure.Checkout{
		Dir: checkout.Directory, Resolved: exposurePackages(resolution.Entries, resolution.CouldNotDerive),
		CurrentRelease: exposurePackages(current, ""),
	}, base, checkout.Commit)
	if err != nil {
		return exposure.Evidence{Unavailable: fmt.Sprintf("the exposure list could not be derived: %v", err)}
	}
	return evidence
}

func exposurePackages(entries []build.ResolvedEntry, unavailable string) []exposure.Package {
	if unavailable != "" {
		return nil
	}
	packages := make([]exposure.Package, 0, len(entries))
	for _, entry := range entries {
		packages = append(packages, exposure.Package{Package: entry.Package, Version: entry.Version,
			Digest: entry.Digest, Licence: entry.Licence})
	}
	return packages
}

func notice(resolution Resolution) string {
	if resolution.CouldNotDerive != "" {
		return "could not derive"
	}
	var lines []string
	for _, entry := range resolution.Entries {
		if entry.Source == "" || entry.Version == "" || entry.Licence == "" {
			return build.CouldNotDeriveNotice
		}
		lines = append(lines, fmt.Sprintf("%s %s %s %s", entry.Source, entry.Package, entry.Version, entry.Licence))
	}
	return strings.Join(lines, "\n")
}

func resolveOne(ctx context.Context, resolver CredentialResolver, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	if resolver == nil {
		return "", fmt.Errorf("buildrunner: no resolver for repository credential %q", name)
	}
	return resolver.Resolve(ctx, name)
}

func resolveMany(ctx context.Context, resolver CredentialResolver, names []string) ([]string, error) {
	values := make([]string, 0, len(names))
	for _, name := range names {
		value, err := resolveOne(ctx, resolver, name)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func outputDirectory() (string, func(), error) {
	dir, err := os.MkdirTemp("", "borg-build-")
	if err != nil {
		return "", func() {}, fmt.Errorf("buildrunner: making the build output directory: %w", err)
	}
	return filepath.Join(dir, "artifact"), func() { _ = os.RemoveAll(dir) }, nil
}

func outputPath(requested string) (string, func(), error) {
	if requested != "" {
		if err := os.MkdirAll(filepath.Dir(requested), 0o755); err != nil {
			return "", func() {}, fmt.Errorf("buildrunner: making the output directory: %w", err)
		}
		return requested, func() {}, nil
	}
	return outputDirectory()
}

func constraintViolation(constraint SourceConstraint, entries []build.ResolvedEntry) string {
	if len(constraint.Allowed) == 0 {
		return ""
	}
	for _, entry := range entries {
		if !contains(constraint.Allowed, entry.Source) {
			return fmt.Sprintf("resolved package %s comes from excluded source %s", entry.Package, entry.Source)
		}
	}
	return ""
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
