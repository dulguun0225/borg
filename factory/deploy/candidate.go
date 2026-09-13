package deploy

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// ErrCandidateCompositionUnavailable is returned when a dependency is running
// nothing or cannot be reached before a candidate run begins.
var ErrCandidateCompositionUnavailable = errors.New("deploy: candidate environment composition unavailable")

// Version is the part of a service version a deployer needs when it follows a
// candidate's composition back to an authored seed or value set.
type Version struct {
	ID      string
	Content string
}

// Dependency is one producer named by a candidate's consumer contract. The
// source assembles its service name and reaches; CompositionFor owns the
// reachability check and the environment composition it produces.
type Dependency struct {
	ServiceID   string
	ReleaseID   string
	ServiceName string
	Addresses   []environment.ComposedAddress
	Reaches     []Reach
}

// CandidateSource is what the deployer reads while composing and preparing a
// candidate. The command-line composition supplies the database-backed source;
// this package owns the decisions made from these reads.
type CandidateSource interface {
	Dependencies(ctx context.Context, itemID, serviceID, productionID string) ([]Dependency, error)
	SeedVersions(ctx context.Context, serviceID string) ([]Version, error)
	ValueSetVersions(ctx context.Context, serviceID string) ([]Version, error)
}

// CandidateEnvironment is the environment writer the deployer uses to create
// or recompose a candidate record. The environment package owns the record and
// this interface keeps that store behind the deployer's operation.
type CandidateEnvironment interface {
	Compose(context.Context, record.Actor, string, string, []environment.Target, secretref.Ref, environment.Composition) (environment.Environment, error)
	Recompose(context.Context, record.Actor, string, environment.Composition) error
}

// Candidate is the identity and access used for one candidate environment.
type Candidate struct {
	ItemID       string
	ServiceID    string
	ServiceName  string
	ProductionID string
	Principal    principal.Principal
	Credential   secretref.Ref
}

// CompositionFor creates the composition a candidate is deployed against.
// Dependencies are checked before their release and interface addresses are
// written into the resulting environment composition.
func CompositionFor(ctx context.Context, source CandidateSource, c Candidate) (environment.Composition, error) {
	dependencies, err := source.Dependencies(ctx, c.ItemID, c.ServiceID, c.ProductionID)
	if err != nil {
		return environment.Composition{}, err
	}
	composed := make([]environment.Composed, 0, len(dependencies))
	for _, dependency := range dependencies {
		if dependency.ReleaseID == "" {
			return environment.Composition{}, fmt.Errorf("%w: dependency %s is running nothing", ErrCandidateCompositionUnavailable, dependency.ServiceID)
		}
		for _, reach := range dependency.Reaches {
			running, err := reach.Target.ReadRunning(ctx, c.Principal, dependency.ServiceName, c.Credential)
			if err != nil || running.Build == "" {
				return environment.Composition{}, fmt.Errorf("%w: dependency %s is unreachable at %s", ErrCandidateCompositionUnavailable, dependency.ServiceName, reach.Address)
			}
		}
		composed = append(composed, environment.Composed{
			ServiceID: dependency.ServiceID,
			ReleaseID: dependency.ReleaseID,
			Addresses: dependency.Addresses,
		})
	}
	composition := environment.Composition{From: composed}
	if version, found, err := newest(source.SeedVersions, ctx, c.ServiceID); err != nil {
		return environment.Composition{}, err
	} else if found {
		composition.SeedVersion = version.ID
		composition.SeedDeclaration = version.Content
	}
	if version, found, err := newest(source.ValueSetVersions, ctx, c.ServiceID); err != nil {
		return environment.Composition{}, err
	} else if found {
		composition.ValueSetVersion = version.ID
	}
	return composition, nil
}

// CompositionForCandidateRun performs composition again after the first narrow
// unavailable answer. The first answer is before a candidate environment or a
// candidate deploy record exists, so this retry writes no criterion or attempt;
// only the second answer is eligible to open a wait.
func CompositionForCandidateRun(ctx context.Context, source CandidateSource, c Candidate, retried bool) (environment.Composition, error) {
	composition, err := CompositionFor(ctx, source, c)
	if err == nil || !errors.Is(err, ErrCandidateCompositionUnavailable) || retried {
		return composition, err
	}
	return CompositionFor(ctx, source, c)
}

// ComposeCandidate creates the candidate environment record through the
// environment writer.
func ComposeCandidate(ctx context.Context, writer CandidateEnvironment, actor record.Actor,
	itemID, projectID string, targets []environment.Target, credential secretref.Ref,
	composition environment.Composition) (environment.Environment, error) {
	e, err := writer.Compose(ctx, actor, itemID, projectID, targets, credential, composition)
	if err != nil {
		return environment.Environment{}, fmt.Errorf("%w: candidate environment: %w", ErrCandidateCompositionUnavailable, err)
	}
	return e, nil
}

// ComposeCandidateForRun retries the first unavailable environment composition
// before the caller opens the second-attempt wait.
func ComposeCandidateForRun(ctx context.Context, writer CandidateEnvironment, actor record.Actor,
	itemID, projectID string, targets []environment.Target, credential secretref.Ref,
	composition environment.Composition, retried bool) (environment.Environment, error) {
	e, err := ComposeCandidate(ctx, writer, actor, itemID, projectID, targets, credential, composition)
	if err == nil || !errors.Is(err, ErrCandidateCompositionUnavailable) || retried {
		return e, err
	}
	return ComposeCandidate(ctx, writer, actor, itemID, projectID, targets, credential, composition)
}

// RecomposeCandidate rewrites the candidate environment's composition through
// the environment writer.
func RecomposeCandidate(ctx context.Context, writer CandidateEnvironment, actor record.Actor,
	id string, composition environment.Composition) error {
	if err := writer.Recompose(ctx, actor, id, composition); err != nil {
		return fmt.Errorf("%w: candidate environment: %w", ErrCandidateCompositionUnavailable, err)
	}
	return nil
}

// RecomposeCandidateForRun retries the first unavailable environment
// recomposition before the caller opens the second-attempt wait.
func RecomposeCandidateForRun(ctx context.Context, writer CandidateEnvironment, actor record.Actor,
	id string, composition environment.Composition, retried bool) error {
	err := RecomposeCandidate(ctx, writer, actor, id, composition)
	if err == nil || !errors.Is(err, ErrCandidateCompositionUnavailable) || retried {
		return err
	}
	return RecomposeCandidate(ctx, writer, actor, id, composition)
}

func newest(read func(context.Context, string) ([]Version, error), ctx context.Context, serviceID string) (Version, bool, error) {
	versions, err := read(ctx, serviceID)
	if errors.Is(err, ErrNoVersion) {
		return Version{}, false, nil
	}
	if err != nil {
		return Version{}, false, err
	}
	if len(versions) == 0 {
		return Version{}, false, nil
	}
	return versions[0], true, nil
}

// ErrNoVersion lets a source represent an owner that authored no seed or
// value set without coupling deploy to the service record package.
var ErrNoVersion = errors.New("deploy: no authored version")

func namedVersion(ctx context.Context, read func(context.Context, string) ([]Version, error), serviceID, id string) (Version, error) {
	versions, err := read(ctx, serviceID)
	if err != nil {
		return Version{}, err
	}
	for _, version := range versions {
		if version.ID == id {
			return version, nil
		}
	}
	return Version{}, fmt.Errorf("deploy: composition names version %s, but service %s does not have it", id, serviceID)
}

// CandidateSeed resolves the seed version named by a candidate's composition.
func CandidateSeed(ctx context.Context, source CandidateSource, c Candidate, composition environment.Composition) (targetseam.Seed, error) {
	seed := targetseam.Seed{Service: c.ServiceName, Credential: c.Credential}
	if composition.SeedVersion == "" {
		return seed, nil
	}
	version, err := namedVersion(ctx, source.SeedVersions, c.ServiceID, composition.SeedVersion)
	if err != nil {
		return targetseam.Seed{}, err
	}
	content := composition.SeedDeclaration
	if content == "" {
		content = version.Content
	}
	seed.Version, seed.Content = version.ID, content
	return seed, nil
}

// CandidateConfiguration resolves only the non-production value set named by
// the candidate's composition. It never reads the production configuration.
func CandidateConfiguration(ctx context.Context, source CandidateSource, serviceID, versionID string,
	resolver *secretref.Resolver) (targetseam.ValueSet, string, error) {
	if versionID == "" {
		return targetseam.ValueSet{}, "", nil
	}
	version, err := namedVersion(ctx, source.ValueSetVersions, serviceID, versionID)
	if err != nil {
		return targetseam.ValueSet{}, "", err
	}
	return ResolveValueSet(version.Content, resolver)
}

// ResolveValueSet turns the environment's typed authored content into the
// target seam's names and resolved values. An absent address is an empty value,
// so the run can perform and the criterion can record undecided.
func ResolveValueSet(content string, resolver *secretref.Resolver) (targetseam.ValueSet, string, error) {
	set, err := environment.ParseValueSet(content)
	if err != nil {
		return targetseam.ValueSet{}, "", err
	}
	values := targetseam.ValueSet{Names: make([]string, 0, len(set.Entries)), Values: make([]string, 0, len(set.Entries))}
	for _, entry := range set.Entries {
		resolved := entry.Address
		if !entry.Secret.IsZero() {
			resolved, err = ResolveNamedSecret(entry.Secret.Name(), resolver)
			if errors.Is(err, secretref.ErrUnknown) {
				resolved = ""
			} else if err != nil {
				return targetseam.ValueSet{}, "", err
			}
		}
		values.Names = append(values.Names, entry.Name)
		values.Values = append(values.Values, resolved)
	}
	return values, "", nil
}

// ResolveNamedSecret resolves a secret reference at deploy time and never
// stores its value in an environment or service record.
func ResolveNamedSecret(name string, resolver *secretref.Resolver) (string, error) {
	ref, err := secretref.New(name)
	if err != nil {
		return "", err
	}
	if resolver == nil {
		return "", secretref.ErrUnknown
	}
	return resolver.Resolve(principal.OfComponent("deployer"), ref)
}

// SchemaChanges turns the build's authored marks into the changes the deploy
// record and store step carry.
func SchemaChanges(marks []string, serviceName string, credential secretref.Ref) []targetseam.SchemaChange {
	changes := make([]targetseam.SchemaChange, 0, len(marks))
	for _, mark := range marks {
		name := strings.TrimSpace(strings.SplitN(mark, ":", 2)[0])
		name = strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
		if name == "" {
			continue
		}
		lower := strings.ToLower(mark)
		changes = append(changes, targetseam.SchemaChange{Service: serviceName, Change: name,
			Destroys: strings.Contains(lower, "drop") || strings.Contains(lower, "remove"), Credential: credential})
	}
	return changes
}
