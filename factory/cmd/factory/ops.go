package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/window"
)

// The watch over one service, and what every subcommand other than run needs
// to compose the path.
//
// The health monitor is the one thing that closes an analysis window, so a run
// that left one open is finished here.

// watchCommand is the health monitor over one service, run against an existing
// database until every window closes or the time allowed runs out.
//
// Nothing but the health monitor closes a window, so this is what finishes what a run
// gave up on — and a window nothing closes reaches the window limit and holds that
// service's
// production deploys, which is a wait on the factory and does not page.
func watchCommand(args []string) error {
	flags := flag.NewFlagSet("watch", flag.ContinueOnError)
	secrets := flags.String("secrets", "", "path of the secrets file (required)")
	targets := flags.String("targets", "", "the directory the local target runs releases from (required)")
	human := flags.String("human", "owner", "the owner a page widens to")
	forHow := flags.Duration("for", time.Minute, "how long to keep reading before leaving what is open, open")
	every := flags.Duration("every", time.Second, "how often to read the quantity")

	// The service's name is taken off the front before the flags are parsed, the
	// way `priority <item-id>` is: it is what a person types first, and the flag
	// package stops at the first argument that is not a flag.
	name := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, args = args[0], args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if name == "" || flags.NArg() != 0 {
		return errors.New("factory watch: one argument, the service's name, and then any flags")
	}
	for _, required := range []struct{ name, value string }{
		{"secrets", *secrets}, {"targets", *targets},
	} {
		if required.value == "" {
			return fmt.Errorf("factory watch: -%s is required", required.name)
		}
	}

	return withPath(pathFlags{
		secrets: *secrets, targets: *targets, human: *human,
	}, func(ctx context.Context, p *path) error {
		svc, found, err := service.ByName(ctx, p.d.pool, name)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("factory watch: no service is named %q", name)
		}
		if err := p.watchTo(ctx, svc, time.Now().Add(*forHow), *every); err != nil {
			return err
		}
		return printWindows(ctx, p, svc)
	})
}

// printWindows is every window of the service and how it closed, which is what an
// owner reads to see that a window ending at the cap is weak protection rather than a
// comparison that ran out of time.
func printWindows(ctx context.Context, p *path, svc service.Service) error {
	all, err := window.All(ctx, p.d.pool, svc.ID)
	if err != nil {
		return err
	}
	for _, w := range all {
		state := "open"
		if !w.Open() {
			state = string(w.Exit) + " at " + w.ClosedAt
		}
		passed := ""
		if !w.PassedAvailable {
			passed = "; passed was never available to it"
		}
		fmt.Fprintf(p.d.out, "window %s over deploy %s: %s (size %v, confidence %v, cap %vs)%s\n",
			w.ID, w.DeployID, state, w.Size, w.Confidence, w.CapSeconds, passed)
	}
	return nil
}

// pathFlags is what a subcommand other than run needs to compose the path: enough to
// reach the store and the targets, and no model — none of these authors anything.
//
// There is no repository here and no service name. Both are the service record's own,
// and every one of these subcommands acts on a service that already has a record: a
// flag naming a repository could disagree with the record, and a flag naming one
// service would leave a two-service install's other one unknown to the run.
type pathFlags struct {
	secrets string
	targets string
	human   string
	// project is the project this composition works in, empty for the one run
	// installs under [defaultProjectName]. It is read and never created: a
	// project that does not exist is refused.
	project string
}

// withPath composes the path for a subcommand that drives one step of it rather than
// the whole thing. The model is nil, which is what says these commands author nothing:
// a stage that reached for one would fail here rather than spending a token.
//
// The services are read out of the store, which is what a subcommand acting on
// existing records needs and what makes these commands work on an install of any
// number of services. A factory with no service record yet has nothing for one of
// these to act on, and the error says so.
//
// It installs nothing. run creates the project and production's environment for
// it; every one of these reads the project [pathFlags.project] names, the
// default one where it names none, and refuses where it does not exist, so a
// subcommand can never leave a second project behind under a name run never
// used. The candidate ceiling below is what the composition needs to
// exist and is authored on no record here, none of these composing a candidate
// environment.
func withPath(f pathFlags, command func(context.Context, *path) error) error {
	if _, err := secretsResolver(f.secrets); err != nil {
		return err
	}
	projectName := f.project
	if projectName == "" {
		projectName = defaultProjectName
	}
	return withPool(func(ctx context.Context, pool *pgxpool.Pool, token lease.Token) error {
		driftStore, shut, err := openDriftDetector(ctx)
		if err != nil {
			return err
		}
		defer shut()

		services, err := service.All(ctx, pool)
		if err != nil {
			return err
		}
		known := make([]serviceRepo, 0, len(services))
		for _, svc := range services {
			known = append(known, serviceRepo{name: svc.Name, repo: svc.Repository})
		}
		if len(known) == 0 {
			return errors.New("factory: this factory has no service record yet, so there is nothing for this subcommand to act on")
		}

		p, err := compose(ctx, deps{
			pool:             pool,
			token:            token,
			targets:          newTargetSet(localTargetAt),
			dir:              f.targets,
			project:          projectName,
			credential:       deployCredential(),
			out:              os.Stdout,
			human:            f.human,
			services:         known,
			candidateCeiling: 1,
			driftdetector:    driftStore,
		})
		if err != nil {
			return err
		}
		return command(ctx, p)
	})
}
