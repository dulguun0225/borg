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

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/secretref"
)

// The two secrets the run reads from the -secrets file: the Claude
// subscription token `claude setup-token` mints, resolved inside the model call
// and stored in no record, and the deploy credential the target seam requires
// on every operation.
const (
	modelCredentialName  = "model.anthropic"
	deployCredentialName = "deploy.local"
)

func main() {
	if err := dispatch(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// subcommands is what the crude interface offers, in the order the usage message
// lists them. run and walk are the path and the link walk; the other four are
// duty 8 and duty 9 — everything an owner authors, and the pins — which have no
// surface until the four are built.
const subcommands = "run, walk <deploy-id>, area <name>, author, pin, policy"

func dispatch(args []string) error {
	if len(args) == 0 {
		return errors.New("factory: a subcommand is required — " + subcommands)
	}
	switch args[0] {
	case "run":
		return runCommand(args[1:])
	case "walk":
		return walkCommand(args[1:])
	case "area":
		return areaCommand(args[1:])
	case "author":
		return authorCommand(args[1:])
	case "pin":
		return pinCommand(args[1:])
	case "policy":
		return policyCommand(args[1:])
	default:
		return fmt.Errorf("factory: %q is none of %s", args[0], subcommands)
	}
}

// runCommand parses the flags, opens the database, applies the schema, and
// hands the path everything it composes. The model name is a flag because
// roadmap M1 requires the model named in configuration, so it has no default.
func runCommand(args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	secrets := flags.String("secrets", "", "path of the secrets file (required)")
	model := flags.String("model", "", "the provider's model id (required; the roadmap names the model in configuration)")
	repo := flags.String("repo", "", "path of the service's git repository (required; created when absent)")
	serviceName := flags.String("service", "", "the service's name (required)")
	targets := flags.String("targets", "", "the directory the local target runs releases from (required)")
	human := flags.String("human", "owner", "the deciding human's name, and the owner every authoring write is made as")
	areaName := flags.String("area", "", "the area the item is in, declared where it does not exist; without one the score reads no context factor and a human decides every gate of the item")
	statement := flags.String("intent", "", "the intent's statement; prompted for when absent")
	pace := flags.Duration("pace", 2*time.Second, "the least time between two model calls; 0 sends them back to back")
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

	resolver, err := secretref.Load(*secrets)
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

	// One buffered reader over standard input, shared between the prompt
	// below and the path: a second reader would lose whatever this one has
	// already buffered.
	in := bufio.NewReader(os.Stdin)
	if *statement == "" {
		fmt.Print("The intent's statement: ")
		line, err := in.ReadString('\n')
		if err != nil {
			return fmt.Errorf("factory run: reading the statement: %w", err)
		}
		*statement = strings.TrimSpace(line)
		if *statement == "" {
			return errors.New("factory run: the statement is empty")
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
		model: agent.NewPaced(
			agent.Anthropic{ModelName: *model, Credential: secretref.MustNew(modelCredentialName), Resolver: resolver},
			*pace,
		),
		target:     localtarget.New(*targets),
		targets:    *targets,
		credential: secretref.MustNew(deployCredentialName),
		in:         in,
		out:        os.Stdout,
		human:      *human,
		service:    *serviceName,
		area:       *areaName,
		repo:       *repo,
	}, *statement)
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
