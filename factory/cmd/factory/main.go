package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/lease"
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

// leaseTTL is how long an acquired lease stands before it lapses, and
// leaseRenewEvery is how often the renewal goroutine renews it — a third of the
// ttl, so a delay of a couple of renewals still lands before the lease would
// lapse. Both are this interface's own choice: the design names the lease and
// the fencing token and leaves the numbers to whoever runs the process.
const (
	leaseTTL        = 30 * time.Second
	leaseRenewEvery = leaseTTL / 3
)

// defaultInstance is this process's own identity for the lease: the machine's
// hostname and this process's id, which tells one instance from another without
// any configuration.
func defaultInstance() string {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown-host"
	}
	return fmt.Sprintf("%s:%d", host, os.Getpid())
}

// acquireLease creates package lease's own table and takes the lease for this
// process, per ../../../end-goal/one-process.md: every subcommand reaches the
// store while it runs, whether it writes or only reads — a read appends a read
// event, which is itself a write of the log — so every subcommand acquires it
// before anything else touches the store. The lease's own table is the one
// thing created first, because a lease cannot be taken in a store whose lease
// table does not exist; every other table is created by [postgres.Start] after
// this returns. A held lease is a start failure.
//
// It returns the token every writer and every reader below this point carries,
// and a stop function, deferred by every caller, that ends the goroutine
// renewing the lease every leaseRenewEvery and then releases the lease, so the
// next subcommand starts rather than waiting out the ttl this one left behind.
func acquireLease(ctx context.Context, pool *pgxpool.Pool) (lease.Token, func(), error) {
	if err := postgres.ApplyLease(ctx, pool); err != nil {
		return 0, nil, err
	}
	token, err := lease.Acquire(ctx, pool, defaultInstance(), leaseTTL)
	if err != nil {
		if errors.Is(err, lease.ErrHeld) {
			return 0, nil, fmt.Errorf("another instance holds the lease: %w", err)
		}
		return 0, nil, err
	}
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(leaseRenewEvery)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				_ = lease.Renew(ctx, pool, token, leaseTTL)
			}
		}
	}()
	return token, func() {
		close(stop)
		_ = lease.Release(context.WithoutCancel(ctx), pool, token)
	}, nil
}

func main() {
	if err := chosen(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// subcommands is what the command-line interface offers, in the order the usage
// message lists them. run and walk are the path and the link walk; watch is the
// health monitor, which is the one thing that closes an analysis window; learn
// is the score's own pass over the outcomes; approve is the emergency action at
// the production deploy row; contracts is every query contracts make.
//
// The six after those are the duties a human performs on something already
// running: rollback and its -revert are duty 10, drop ends an item or an intent
// for good, accept-commit ends the queue's stop over a commit it did not make,
// mark-rollback says a rollback was not caused by the release, mitigate is the
// deployer acting on a human's instruction, and truncate is the log's retention
// pass. The last eight are duty 8, duty 9, the priority an owner reorders a
// queue with, and the People declaration a page routes on — none of which has a
// screen yet.
const subcommands = "run, walk <deploy-id>, watch <service>, learn, approve <item-id>, contracts, " +
	"rollback <service>, drop <item|intent>, accept-commit <service> <commit>, mark-rollback <deploy-id>, " +
	"mitigate <deploy-id>, truncate, " +
	"area <name>, author, safeguard, halt, legal-hold, policy, priority <item-id>, people [<human>]"

// chosen is the switch on the subcommand name. It is not called dispatch:
// dispatch is the component that puts an agent on a stage, and a function of
// this package naming the same thing would be a second name for it.
func chosen(args []string) error {
	if len(args) == 0 {
		return errors.New("factory: a subcommand is required — " + subcommands)
	}
	switch args[0] {
	case "run":
		return runCommand(args[1:])
	case "walk":
		return walkCommand(args[1:])
	case "watch":
		return watchCommand(args[1:])
	case "learn":
		return learnCommand(args[1:])
	case "approve":
		return approveCommand(args[1:])
	case "contracts":
		return contractsCommand(args[1:])
	case "rollback":
		return rollbackCommand(args[1:])
	case "drop":
		return dropCommand(args[1:])
	case "accept-commit":
		return acceptCommitCommand(args[1:])
	case "mark-rollback":
		return markRollbackCommand(args[1:])
	case "mitigate":
		return mitigateCommand(args[1:])
	case "truncate":
		return truncateCommand(args[1:])
	case "area":
		return areaCommand(args[1:])
	case "author":
		return authorCommand(args[1:])
	case "safeguard":
		return safeguardCommand(args[1:])
	case "halt":
		return haltCommand(args[1:])
	case "legal-hold":
		return legalHoldCommand(args[1:])
	case "policy":
		return policyCommand(args[1:])
	case "priority":
		return priorityCommand(args[1:])
	case "people":
		return peopleCommand(args[1:])
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
	var services serviceFlag
	flags.Var(&services, "service", "a service as name=path, the path being its git repository (created when absent); given once per service, and at least once")
	targets := flags.String("targets", "", "the directory the local target runs releases from (required)")
	human := flags.String("human", "owner", "the deciding human's name, and the owner every authoring write is made as")
	projectName := flags.String("project", defaultProjectName, "the project this run installs and works in, created where it does not exist")
	areaName := flags.String("area", "", "the area the item is in, declared where it does not exist; without one the score reads no context factor and a human decides every gate of the item")
	var raw stringList
	flags.Var(&raw, "intent", "an intent's statement, given once per decomposition; `svcA,svcB: statement` decomposes one item per service named, each waiting on the one before it")
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
	provided, err := newModel(*provider, *model, resolver)
	if err != nil {
		return err
	}

	ctx := context.Background()
	pool, err := postgres.Open(ctx, postgres.URL())
	if err != nil {
		return err
	}
	defer pool.Close()
	token, stopLease, err := acquireLease(ctx, pool)
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

	// One buffered reader over standard input, shared between the prompt
	// below and the path: a second reader would lose whatever this one has
	// already buffered.
	in := bufio.NewReader(os.Stdin)
	if len(intents) == 0 {
		fmt.Print("The intent's statement: ")
		line, err := in.ReadString('\n')
		if err != nil {
			return fmt.Errorf("factory run: reading the statement: %w", err)
		}
		if err := intents.setFor(line, services); err != nil {
			return fmt.Errorf("factory run: %w", err)
		}
	}

	_, err = run(ctx, deps{
		pool:  pool,
		token: token,
		// The model's id is the author every version this run writes names, the
		// per-author prior being kept per model version.
		modelName:           *model,
		modelCredentialName: modelCredentialNameFor(*provider),
		// Paced around the provider client, so every call a stage makes —
		// including a retry after a refused reply, which would otherwise follow
		// the refusal with nothing in between — waits out the interval.
		model: agent.NewPaced(provided, *pace),
		// One target per environment: production's is the directory named here, and
		// each candidate environment's is a directory of its own under it.
		targets: newTargetSet(localTargetAt),
		dir:     *targets,
		project: *projectName,

		credential:       deployCredential(),
		in:               in,
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
	token, stopLease, err := acquireLease(ctx, pool)
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
