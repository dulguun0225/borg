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
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/reconciler"
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
	if err := dispatch(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// subcommands is what the crude interface offers, in the order the usage message
// lists them. run and walk are the path and the link walk; watch is the comparison,
// which is the one thing that closes a watch window; approve is the emergency action
// at the production deploy row; and the other six are duty 8, duty 9, the priority an
// owner reorders a queue with, and the People declaration a page routes on — none of
// which has a surface until the four of M7 are built.
const subcommands = "run, walk <deploy-id>, watch <service>, approve <item-id>, " +
	"area <name>, author, pin, policy, priority <item-id>, people [<human>]"

func dispatch(args []string) error {
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
	case "approve":
		return approveCommand(args[1:])
	case "area":
		return areaCommand(args[1:])
	case "author":
		return authorCommand(args[1:])
	case "pin":
		return pinCommand(args[1:])
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

// openReconciler opens the reconciler's own store where one is reachable, and
// returns nothing where it is not. Nothing here applies its schema — that store is
// the reconciler's and a factory that created it would own it — so a store the
// reconciler has never run against reads as absent, which is a factory with no
// reconciler installed and is a state the design has.
//
// The absence is not an error. Installing the reconciler is substrate outside the
// twelve duties, and a factory that refused to run without one would make it a
// requirement the design does not make.
func openReconciler(ctx context.Context) (*pgxpool.Pool, func(), error) {
	pool, err := reconciler.Open(ctx, reconciler.URL())
	if err != nil {
		fmt.Fprintf(os.Stderr, "no reconciler store at %s, so nothing checks this factory's records against what runs: %v\n",
			reconciler.URL(), err)
		return nil, func() {}, nil
	}
	if _, err := reconciler.LastComparisons(ctx, pool, ""); err != nil {
		// The store is reachable and holds no schema, which is a reconciler that has
		// never run. Applying it here is what this must not do.
		fmt.Fprintln(os.Stderr, "the reconciler's store holds no schema, so it has never run; `reconciler pass` is what creates it")
		pool.Close()
		return nil, func() {}, nil
	}
	return pool, pool.Close, nil
}

// statements is -intent given more than once, one intent per candidate. It is a
// repeated flag rather than a count, because what a run needs per candidate is the
// statement itself: two candidates at once is the whole of what an environment per
// candidate buys, and one flag per intent is how the crude interface says it.
type statements []string

func (s *statements) String() string { return strings.Join(*s, "; ") }

func (s *statements) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("an intent's statement is not empty")
	}
	*s = append(*s, value)
	return nil
}

// runCommand parses the flags, opens the database, applies the schema, and
// hands the path everything it composes. The model name is a flag because
// roadmap M1 requires the model named in configuration, so it has no default.
func runCommand(args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	secrets := flags.String("secrets", "", "path of the secrets file (required)")
	model := flags.String("model", "", "the provider's model id (required; the roadmap names the model in configuration)")
	provider := flags.String("provider", "openrouter", "which provider answers the model — "+providers+"; each reads its own credential from the secrets file")
	repo := flags.String("repo", "", "path of the service's git repository (required; created when absent)")
	serviceName := flags.String("service", "", "the service's name (required)")
	targets := flags.String("targets", "", "the directory the local target runs releases from (required)")
	human := flags.String("human", "owner", "the deciding human's name, and the owner every authoring write is made as")
	areaName := flags.String("area", "", "the area the item is in, declared where it does not exist; without one the score reads no context factor and a human decides every gate of the item")
	var intents statements
	flags.Var(&intents, "intent", "an intent's statement, given once per candidate; prompted for one when absent")
	pace := flags.Duration("pace", 2*time.Second, "the least time between two model calls; 0 sends them back to back")
	ceiling := flags.Int("candidate-environments", 8, "how many candidate environments this substrate has room for at once; a candidate that meets it waits, and the wait is written into the log")
	watchFor := flags.Duration("watch", time.Minute, "how long to watch this run's own windows before leaving what is open, open; `factory watch` continues from there")
	watchEvery := flags.Duration("watch-every", time.Second, "how often to read the quantity while watching")
	if err := flags.Parse(args); err != nil {
		return err
	}
	for _, required := range []struct{ name, value string }{
		{"secrets", *secrets}, {"model", *model}, {"repo", *repo},
		{"service", *serviceName}, {"targets", *targets},
	} {
		if required.value == "" {
			return fmt.Errorf("factory run: -%s is required", required.name)
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
	if err := postgres.Apply(ctx, pool); err != nil {
		return err
	}
	reconcilerStore, shut, err := openReconciler(ctx)
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
		if err := intents.Set(line); err != nil {
			return fmt.Errorf("factory run: %w", err)
		}
	}

	_, err = run(ctx, deps{
		pool: pool,
		// The model's id is the author every version this run writes names, the
		// authorship prior being kept per model version.
		modelName: *model,
		// Paced around the provider client, so every call a stage makes —
		// including a retry after a refused reply, which would otherwise follow
		// the refusal with nothing in between — waits out the interval.
		model: agent.NewPaced(provided, *pace),
		// One target per environment: production's is the directory named here, and
		// each candidate environment's is a directory of its own under it.
		targets: newTargetSet(localTargetAt),
		dir:     *targets,

		credential:       deployCredential(),
		in:               in,
		out:              os.Stdout,
		human:            *human,
		service:          *serviceName,
		area:             *areaName,
		repo:             *repo,
		candidateCeiling: *ceiling,
		reconciler:       reconcilerStore,
		watchFor:         *watchFor,
		watchEvery:       *watchEvery,
	}, intents)
	return err
}

// walkCommand runs the link walk alone, against an existing database.
func walkCommand(args []string) error {
	if len(args) != 1 {
		return errors.New("factory walk: one argument, the deploy id")
	}
	ctx := context.Background()
	pool, err := postgres.Open(ctx, postgres.URL())
	if err != nil {
		return err
	}
	defer pool.Close()
	return walk(ctx, pool, os.Stdout, args[0])
}
