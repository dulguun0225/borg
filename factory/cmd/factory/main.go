package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// The secrets the run reads from the -secrets file: one model credential per
// provider, resolved inside the model call and stored in no record, and the
// deploy credential the target seam requires on every operation. A run reads
// the one its -provider names and never the other, so an install using one
// provider has no reason to hold the other's credential.
const (
	anthropicCredentialName  = "model.anthropic"
	openRouterCredentialName = "model.openrouter"
	deployCredentialName     = "deploy.local"
	// repositoryCredentialName is what a service's provisioning names as the
	// credential that pushes its branch. A repository here is a directory on
	// this host, which tells no branch from master — credential shape one — and
	// the clone resolves a path rather than a credential, so the secrets file
	// need not hold this one and nothing reads it.
	repositoryCredentialName = "repository.local"
)

// providers is what -provider accepts, in the order the flag's usage lists
// them. Two providers and a switch rather than one client with a base URL
// swapped: the two endpoints differ in their wire shape and in their
// credential's scheme, so a name here selects an implementation and configures
// nothing.
const providers = "openrouter, anthropic"

// defaultProjectName is what -project defaults to on every subcommand that
// takes it: an install with one project needs never name it.
const defaultProjectName = "default"

// factoryVersion is which build of the factory this binary is, named beside the
// extractor on every derivation: an upgrade that ships a changed extractor
// derives again for every release in force on that toolchain, and the factory
// version is half of what that comparison reads.
//
// It is a constant and nothing stamps this binary with one, so what it costs is
// that two builds of the factory carrying two extractors would name one version
// and the comparison would find nothing changed. The identity the design gives a
// shipped bundle is not built.
const factoryVersion = "unstamped"

// modelCredentialNameFor is the credential name [newModel] resolved the
// model through, carried onto every agentrun record this run writes.
func modelCredentialNameFor(provider string) string {
	if provider == "anthropic" {
		return anthropicCredentialName
	}
	return openRouterCredentialName
}

// providerOf is which provider answers a credential name, the reverse of
// [modelCredentialNameFor]: a fleet entry names the credential and no record
// names the provider, so an entry on the anthropic credential is answered by
// the anthropic client and one on the OpenRouter credential by OpenRouter's. A
// credential neither of them is refused rather than sent to whichever came
// first.
func providerOf(credentialName string) (string, error) {
	switch credentialName {
	case anthropicCredentialName:
		return "anthropic", nil
	case openRouterCredentialName:
		return "openrouter", nil
	default:
		return "", fmt.Errorf("factory: no provider of this install answers the credential %q; it reads %s and %s",
			credentialName, anthropicCredentialName, openRouterCredentialName)
	}
}

// modelsPerEntry is how a fleet entry's model version and credential name
// become a client to call, one client per pair and kept for the life of the
// process. Kept because [agent.Paced] holds the time of the last call: a client
// built afresh per dispatch would pace nothing, its first call never waiting,
// and the interval -pace names would bound no rate at all.
func modelsPerEntry(resolver *secretref.Resolver, pace time.Duration) func(string, string) (agent.Model, error) {
	var mu sync.Mutex
	made := map[string]agent.Model{}
	return func(modelVersion, credentialName string) (agent.Model, error) {
		mu.Lock()
		defer mu.Unlock()
		key := credentialName + " " + modelVersion
		if already, found := made[key]; found {
			return already, nil
		}
		named, err := providerOf(credentialName)
		if err != nil {
			return nil, err
		}
		provided, err := newModel(named, modelVersion, resolver)
		if err != nil {
			return nil, err
		}
		paced := agent.NewPaced(provided, pace)
		made[key] = paced
		return paced, nil
	}
}

// newModel is the one place a provider name becomes a model. The switch is
// exhaustive and its default is an error, so a name this interface does not
// implement is refused at the flag rather than reaching a request.
func newModel(provider, modelName string, resolver *secretref.Resolver) (agent.Model, error) {
	switch provider {
	case "openrouter":
		return agent.OpenRouter{
			ModelName:  modelName,
			Credential: secretref.MustNew(openRouterCredentialName),
			Resolver:   resolver,
		}, nil
	case "anthropic":
		return agent.Anthropic{
			ModelName:  modelName,
			Credential: secretref.MustNew(anthropicCredentialName),
			Resolver:   resolver,
		}, nil
	default:
		return nil, fmt.Errorf("factory run: -provider %q is not one of %s", provider, providers)
	}
}

func main() {
	if err := chosen(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// subcommands is what the command-line interface offers, in the order the usage
// message lists them. serve is the process the lease was always for: it holds
// the lease for its own life, runs each component's pass on its own interval,
// and serves the four screens over HTTP between them, where every other
// subcommand acquires the lease, makes one pass, and exits.
//
// The seven beside it are a pass or a read: run is the path's own pass, walk is
// the link walk from a deploy back to its intent, watch is the health monitor,
// which is the one thing that closes an analysis window, learn is the score's
// pass over the outcomes, contracts is every query contracts make, policy
// prints every parameter as it is in force, and truncate is the decision log's
// retention pass.
//
// Every write a human makes is at a screen and reaches the same writer through
// package screens' own call: what a subcommand acted on is an item, an intent,
// a service, a project, a record or the People declaration, and each of those
// has an address a human can be on.
const subcommands = "serve, run, walk <deploy-id>, watch <service>, learn, contracts, policy, truncate"

// chosen is the switch on the subcommand name. It is not called dispatch:
// dispatch is the component that puts an agent on a stage, and a function of
// this package naming the same thing would be a second name for it.
func chosen(args []string) error {
	if len(args) == 0 {
		return errors.New("factory: a subcommand is required — " + subcommands)
	}
	switch args[0] {
	case "serve":
		return serveCommand(args[1:])
	case "run":
		return runCommand(args[1:])
	case "walk":
		return walkCommand(args[1:])
	case "watch":
		return watchCommand(args[1:])
	case "learn":
		return learnCommand(args[1:])
	case "contracts":
		return contractsCommand(args[1:])
	case "policy":
		return policyCommand(args[1:])
	case "truncate":
		return truncateCommand(args[1:])
	default:
		return fmt.Errorf("factory: %q is none of %s", args[0], subcommands)
	}
}

// secretsResolver loads the secrets file, which every command that reaches a target
// needs and which is where a mistyped path is caught before anything is opened.
func secretsResolver(path string) (*secretref.Resolver, error) {
	return secretref.Load(path)
}

// deployCredential is the reference the target seam requires on every operation. It
// is a name and never a value: what sits behind the seam resolves it, and nothing sits
// behind this one.
func deployCredential() secretref.Ref { return secretref.MustNew(deployCredentialName) }

// repositoryCredential is the reference a service's provisioning names. It is a
// name and never a value, the way the deploy credential is.
func repositoryCredential() secretref.Ref { return secretref.MustNew(repositoryCredentialName) }

// localTargetAt is how every command in this interface makes a target: one local
// process per service in one directory.
func localTargetAt(dir string) targetseam.Target { return localtarget.New(dir) }

// openDriftDetector opens the drift detector's own store where one is reachable,
// and returns nothing where it is not. Nothing here applies its schema — that
// store is the drift detector's and a factory that created it would own it
// — so a store the drift detector has never run against reads as absent,
// which is a factory with no drift detector installed and is a state the
// design has.
//
// The absence is not an error. Installing the drift detector is substrate
// outside the twelve duties, and a factory that refused to run without one
// would make it a requirement the design does not make.
func openDriftDetector(ctx context.Context) (*pgxpool.Pool, func(), error) {
	pool, err := driftdetector.Open(ctx, driftdetector.URL())
	if err != nil {
		fmt.Fprintf(os.Stderr, "no drift detector store at %s, so nothing checks this factory's records against what runs: %v\n",
			driftdetector.URL(), err)
		return nil, func() {}, nil
	}
	if _, err := driftdetector.LastChecks(ctx, pool, ""); err != nil {
		// The store is reachable and holds no schema, which is an drift detector
		// that has never run. Applying it here is what this must not do.
		fmt.Fprintln(os.Stderr, "the drift detector's store holds no schema, so it has never run; `driftdetector pass` is what creates it")
		pool.Close()
		return nil, func() {}, nil
	}
	return pool, pool.Close, nil
}

// runCommand parses the flags, opens the database, applies the schema, and
// hands the path everything it composes. The model name is a flag because
// roadmap M1 requires the model named in configuration, so it has no default.
func runCommand(args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	secrets := flags.String("secrets", "", "path of the secrets file (required)")
	model := flags.String("model", "", "the provider's model id (required; the roadmap names the model in configuration)")
	provider := flags.String("provider", "openrouter", "which provider answers the model — "+providers+"; each reads its own credential from the secrets file")
	effort := flags.String("effort", "", "how long the model works before it answers, the field a fleet entry has for it; empty asks for none, and a value the provider does not offer fails at its own answer")
	var services serviceFlag
	flags.Var(&services, "service", "a service as name=path, the path being its git repository (created when absent); given once per service, and at least once")
	targets := flags.String("targets", "", "the directory the local target runs releases from (required)")
	human := flags.String("human", "owner", "the deciding human's name, and the owner every authoring write is made as")
	projectName := flags.String("project", defaultProjectName, "the project this run installs and works in, created where it does not exist")
	areaName := flags.String("area", "", "the area the item is in, declared where it does not exist; without one the score reads no context factor and a human decides every gate of the item")
	var raw stringList
	flags.Var(&raw, "intent", "an intent's statement, given once per decomposition; `svcA,svcB: statement` decomposes one item per service named, each waiting on the one before it")
	answer := flags.String("answer", "", "what to answer a round of the interview with; empty leaves the round waiting in Work, where a screen answers it")
	pace := flags.Duration("pace", 2*time.Second, "the least time between two model calls; 0 sends them back to back")
	ceiling := flags.Int("candidate-environments", 8, "how many candidate environments this platform has room for at once; a candidate that meets it waits, and the wait is written into the log")
	watchFor := flags.Duration("watch", time.Minute, "how long to watch this run's own windows before leaving what is open, open; `factory watch` continues from there")
	watchEvery := flags.Duration("watch-every", time.Second, "how often to read the quantity while watching")
	if err := flags.Parse(args); err != nil {
		return err
	}
	for _, required := range []struct{ name, value string }{
		{"secrets", *secrets}, {"model", *model}, {"targets", *targets},
	} {
		if required.value == "" {
			return fmt.Errorf("factory run: -%s is required", required.name)
		}
	}
	if len(services) == 0 {
		return errors.New("factory run: -service is required, at least once")
	}
	var intents statements
	for _, value := range raw {
		if err := intents.setFor(value, services); err != nil {
			return fmt.Errorf("factory run: %w", err)
		}
	}

	resolver, err := secretsResolver(*secrets)
	if err != nil {
		return err
	}
	// The provider is resolved here for the flag's sake alone: a -provider this
	// interface does not implement is refused at the flag rather than at the
	// first dispatch. What a run calls is built per fleet entry, below.
	if _, err := newModel(*provider, *model, resolver); err != nil {
		return err
	}

	ctx := context.Background()
	pool, err := postgres.Open(ctx, postgres.URL())
	if err != nil {
		return err
	}
	defer pool.Close()
	token, _, stopLease, err := acquireLease(ctx, pool)
	if err != nil {
		return err
	}
	defer stopLease()
	if _, err := postgres.Start(ctx, pool); err != nil {
		return err
	}
	driftStore, shut, err := openDriftDetector(ctx)
	if err != nil {
		return err
	}
	defer shut()

	if len(intents) == 0 {
		return errors.New("factory run: -intent is required, at least once")
	}

	_, err = run(ctx, deps{
		pool:  pool,
		token: token,
		// The model's id is the author every version this run writes names, the
		// per-author prior being kept per model version.
		modelName:           *model,
		modelCredentialName: modelCredentialNameFor(*provider),
		// The effort the one composed fleet entry names, sent to the provider on
		// every call and recorded on every agent run.
		effort: *effort,
		// One client per fleet entry, built from the entry's own model version
		// and the provider its credential resolves to, and paced, so every call
		// a stage makes — including a retry after a refused reply, which would
		// otherwise follow the refusal with nothing in between — waits out the
		// interval.
		modelFor: modelsPerEntry(resolver, *pace),
		// One target per environment: production's is the directory named here, and
		// each candidate environment's is a directory of its own under it.
		targets: newTargetSet(localTargetAt),
		dir:     *targets,
		project: *projectName,
		// run is the one subcommand that installs: it creates the project and
		// production's environment for it in the same event where they do not
		// exist, and every other one reads them and refuses where they do not.
		install: true,

		credential:       deployCredential(),
		answer:           *answer,
		out:              os.Stdout,
		human:            *human,
		services:         services,
		area:             *areaName,
		candidateCeiling: *ceiling,
		driftdetector:    driftStore,
		watchFor:         *watchFor,
		watchEvery:       *watchEvery,
	}, intents)
	return err
}

// walkCommand runs the link walk alone, against an existing database.
func walkCommand(args []string) error {
	flags := flag.NewFlagSet("walk", flag.ContinueOnError)
	human := flags.String("human", "owner", "the human this command reads the log as")
	id := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id, args = args[0], args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if id == "" || flags.NArg() != 0 {
		return errors.New("factory walk: one argument, the deploy id, and then any flags")
	}
	ctx := context.Background()
	pool, err := postgres.Open(ctx, postgres.URL())
	if err != nil {
		return err
	}
	defer pool.Close()
	token, _, stopLease, err := acquireLease(ctx, pool)
	if err != nil {
		return err
	}
	defer stopLease()
	if _, err := postgres.Start(ctx, pool); err != nil {
		return err
	}
	actor, err := humanNamed(ctx, pool, token, *human)
	if err != nil {
		return err
	}
	return walk(ctx, pool, os.Stdout, token, asPrincipal(actor), id)
}

// stringList is a repeated flag whose values are read later, because reading one
// needs another flag's value. -intent is the only one: its service prefix is
// resolved against the services -service named, and flag parsing gives no order
// between two flags.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, "; ") }

func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}
