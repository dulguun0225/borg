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

	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/window"
)

// The three subcommands of everything downstream of a deploy: who a page reaches,
// the watch that closes a window, and a human approving through a factory hold.
//
// The first is the People declaration, which is the screen People will write and
// this reaches until it exists. The second is the health monitor, which nothing else
// closes a window with — so a run that left one open is finished here. The third is
// the emergency action the design keeps at the production deploy row: approve now, not
// skip.

// peopleCommand declares that a human holds a duty or an obligation, withdraws one,
// or prints the declaration. Nothing enforces it and nothing has to: a page or a gate
// row with no holder recorded widens to the owner, who is the person that would have
// written the row.
func peopleCommand(args []string) error {
	flags := flag.NewFlagSet("people", flag.ContinueOnError)
	duty := flags.Int("duty", 0, "one of the owner's twelve duties, by number")
	obligation := flags.String("obligation", "", "an obligation outside the twelve: hosting, driftdetector, or fleet")
	withdraw := flags.Bool("withdraw", false, "end this holding rather than declaring it")
	human := flags.String("human", "owner", "the owner writing the declaration")

	// The name the holding is about is taken off the front, the way `area <name>` is.
	// It is not -human: the owner writing the row and the human the row is about are two
	// people as often as one.
	holder := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		holder, args = args[0], args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("factory people: at most one argument, the human the holding is about, and then any flags")
	}

	return withPool(func(ctx context.Context, pool *pgxpool.Pool, token lease.Token) error {
		if holder == "" {
			return printPeople(ctx, pool)
		}
		holding := people.OfDuty(people.Duty(*duty))
		if *obligation != "" {
			holding = people.OfObligation(people.Obligation(*obligation))
		}

		writer := people.NewWriter(pool, token)
		if *withdraw {
			standing, err := people.ByHolding(ctx, pool, holder, holding)
			if err != nil {
				return err
			}
			ended, err := writer.Withdraw(ctx, standing.ID)
			if err != nil {
				return err
			}
			fmt.Printf("%s no longer holds %s, as of %s; the row is kept\n", ended.Human, holding, ended.WithdrawnAt)
			return nil
		}

		declared, err := writer.Declare(ctx, owner(*human), holder, holding)
		if err != nil {
			return err
		}
		fmt.Printf("%s holds %s, declared as %s by %s %s\n",
			declared.Human, holding, declared.ID, declared.Actor.Kind, *human)
		fmt.Println("A page about a row belonging to this holding reaches every human who holds it at once, and widens once to the owner")
		return nil
	})
}

func printPeople(ctx context.Context, pool *pgxpool.Pool) error {
	all, err := people.All(ctx, pool)
	if err != nil {
		return err
	}
	if len(all) == 0 {
		fmt.Println("The People declaration is empty, so every page and every gate row reaches the owner directly")
		return nil
	}
	for _, d := range all {
		state := "holds it"
		if !d.Holds() {
			state = "withdrew at " + d.WithdrawnAt
		}
		fmt.Printf("  %s: %s — %s\n", d.Human, holdingOf(d), state)
	}
	return nil
}

// holdingOf is one declaration's holding, read back off the row's two columns.
func holdingOf(d people.Declaration) people.Holding {
	if d.Obligation != "" {
		return people.OfObligation(d.Obligation)
	}
	return people.OfDuty(d.Duty)
}

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
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
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
		svc, found, err := service.ByName(ctx, p.d.pool, flags.Arg(0))
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("factory watch: no service is named %q", flags.Arg(0))
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
		clean := ""
		if !w.PassedAvailable {
			clean = "; clean was never available to it"
		}
		fmt.Fprintf(p.d.out, "window %s over deploy %s: %s (size %v, confidence %v, cap %vs)%s\n",
			w.ID, w.DeployID, state, w.Size, w.Confidence, w.CapSeconds, clean)
	}
	return nil
}

// approveCommand is a human approving through a factory hold at the production
// deploy row. The row fires with the hold on its open event and the human decides,
// which is the emergency action the design keeps there — approve now, not skip.
//
// What approving through the hold a rollback leaves redelivers is the defect that was
// just removed. That is the most damaging thing in the factory to approve through and
// the one most likely to be tried during an incident, which is why the reason is
// required and goes on the close event.
func approveCommand(args []string) error {
	flags := flag.NewFlagSet("approve", flag.ContinueOnError)
	secrets := flags.String("secrets", "", "path of the secrets file (required)")
	targets := flags.String("targets", "", "the directory the local target runs releases from (required)")
	human := flags.String("human", "owner", "the human deciding")
	verdict := flags.String("verdict", string(gate.VerdictApprove), "approve or hold")
	reason := flags.String("reason", "", "what the human says with the verdict, which goes on the close event")

	id := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id, args = args[0], args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if id == "" || flags.NArg() != 0 {
		return errors.New("factory approve: one argument, the item's id, and then any flags")
	}
	for _, required := range []struct{ name, value string }{
		{"secrets", *secrets}, {"targets", *targets},
	} {
		if required.value == "" {
			return fmt.Errorf("factory approve: -%s is required", required.name)
		}
	}

	return withPath(pathFlags{secrets: *secrets, targets: *targets, human: *human},
		func(ctx context.Context, p *path) error {
			return p.approveThrough(ctx, id, gate.Verdict(*verdict), *reason)
		})
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
}

// withPath composes the path for a subcommand that drives one step of it rather than
// the whole thing. The model is nil, which is what says these commands author nothing:
// a stage that reached for one would fail here rather than spending a token.
//
// The services are read out of the store, which is what a subcommand acting on
// existing records needs and what makes these commands work on an install of any
// number of services. A factory with no service record yet has nothing for one of
// these to act on, and the error says so.
func withPath(f pathFlags, command func(context.Context, *path) error) error {
	if _, err := secretsResolver(f.secrets); err != nil {
		return err
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
			credential:       deployCredential(),
			in:               strings.NewReader(""),
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
