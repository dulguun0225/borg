package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// deployCredentialName is the credential the target seam requires on every
// operation, read from the same secrets file the factory's own run reads. The
// drift detector resolves it and hands the reference across the seam; what
// sits behind the seam is what would read a value, and on this platform
// nothing does.
const deployCredentialName = "deploy.local"

const subcommands = "pass, show, clear <mismatch-id>, install -address <address>"

func main() {
	if err := dispatch(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func dispatch(args []string) error {
	if len(args) == 0 {
		return errors.New("driftdetector: a subcommand is required — " + subcommands)
	}
	switch args[0] {
	case "pass":
		return passCommand(args[1:])
	case "show":
		return showCommand(args[1:])
	case "clear":
		return clearCommand(args[1:])
	case "install":
		return installCommand(args[1:])
	default:
		return fmt.Errorf("driftdetector: %q is none of %s", args[0], subcommands)
	}
}

// stores is the two pools one command holds: the factory's, which it only reads,
// and its own, which nothing in the factory writes.
type stores struct {
	factory *pgxpool.Pool
	own     *pgxpool.Pool
}

// open opens both and applies the drift detector's own schema. The
// factory's is not applied here: a store the drift detector applied would
// be a store the drift detector writes, and it reads that one.
func open(ctx context.Context) (stores, func(), error) {
	factory, err := postgres.Open(ctx, postgres.URL())
	if err != nil {
		return stores{}, nil, err
	}
	own, err := driftdetector.Open(ctx, driftdetector.URL())
	if err != nil {
		factory.Close()
		return stores{}, nil, err
	}
	if err := driftdetector.Apply(ctx, own); err != nil {
		factory.Close()
		own.Close()
		return stores{}, nil, err
	}
	return stores{factory: factory, own: own}, func() { factory.Close(); own.Close() }, nil
}

// passCommand is one comparison of every service on every production target,
// plus the second comparison over the log's chain and the third over the
// factory's own last checks.
func passCommand(args []string) error {
	flags := flag.NewFlagSet("pass", flag.ContinueOnError)
	secrets := flags.String("secrets", "", "path of the secrets file (required; the seam needs a credential reference on every operation)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *secrets == "" {
		return errors.New("driftdetector pass: -secrets is required")
	}
	if _, err := secretref.Load(*secrets); err != nil {
		return err
	}

	ctx := context.Background()
	s, shut, err := open(ctx)
	if err != nil {
		return err
	}
	defer shut()
	if err := pass(ctx, s, os.Stdout, secretref.MustNew(deployCredentialName),
		func(dir string) targetseam.Target { return localtarget.New(dir) }); err != nil {
		return err
	}
	if err := chainCheck(ctx, s, os.Stdout); err != nil {
		return err
	}
	return staleCheck(ctx, s, os.Stdout)
}

// showCommand prints the store: every mismatch, and the last check per
// target. The second is what says whether this process is still running.
func showCommand(args []string) error {
	if len(args) != 0 {
		return errors.New("driftdetector show: no arguments")
	}
	ctx := context.Background()
	s, shut, err := open(ctx)
	if err != nil {
		return err
	}
	defer shut()
	return show(ctx, s, os.Stdout)
}

func show(ctx context.Context, s stores, out io.Writer) error {
	all, err := driftdetector.All(ctx, s.own)
	if err != nil {
		return err
	}
	if len(all) == 0 {
		fmt.Fprintln(out, "No mismatch has ever been raised")
	}
	for _, m := range all {
		state := "UNCLEARED — it holds that service's production deploys"
		if m.Kind == driftdetector.MismatchKindChain {
			state = "UNCLEARED — it holds every service's production deploys"
		}
		if m.Cleared() {
			state = "cleared at " + m.ClearedAt + " by " + m.ClearedBy
		}
		fmt.Fprintf(out, "mismatch %s (%s) at %s: %s\n  %s\n", m.ID, m.Kind, m.At, m.Why(), state)
		if m.LaterAgreements > 0 {
			fmt.Fprintf(out, "  %d later comparison(s) agreed, and it stands all the same\n", m.LaterAgreements)
		}
	}

	checks, err := driftdetector.LastChecks(ctx, s.own, "")
	if err != nil {
		return err
	}
	if len(checks) == 0 {
		fmt.Fprintln(out, "No check has ever been recorded, so this drift detector has never run")
		return nil
	}
	for _, c := range checks {
		if !c.Reached {
			fmt.Fprintf(out, "last check of %s on %s at %s: unreached — %s\n",
				c.ServiceID, c.Target, c.At, c.Why)
			continue
		}
		fmt.Fprintf(out, "last check of %s on %s at %s: agreed=%t, running %q against recorded %q\n",
			c.ServiceID, c.Target, c.At, c.Agreed, c.RunningBuild, c.RecordedBuildID)
	}
	return nil
}

// clearCommand is the human act this store keeps and the factory refuses.
func clearCommand(args []string) error {
	flags := flag.NewFlagSet("clear", flag.ContinueOnError)
	human := flags.String("human", "", "the human clearing it (required; the record says whose act it was)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("driftdetector clear: one argument, the mismatch id")
	}
	if *human == "" {
		return errors.New("driftdetector clear: -human is required")
	}

	ctx := context.Background()
	s, shut, err := open(ctx)
	if err != nil {
		return err
	}
	defer shut()

	cleared, err := driftdetector.NewWriter(s.own).Clear(ctx, flags.Arg(0), *human)
	if err != nil {
		return err
	}
	fmt.Printf("Mismatch %s cleared at %s by %s\n", cleared.ID, cleared.ClearedAt, cleared.ClearedBy)
	fmt.Println("The page about it is answered by the factory's notifier the next time it reads this store and finds it cleared")
	return nil
}

// installCommand writes the one address — mail or chat — the detector
// delivers its own page to: the owner's and nobody's duty, done once when
// the detector is installed beside the factory.
func installCommand(args []string) error {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	address := flags.String("address", "", "mail or chat address the detector delivers its own page to (required)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *address == "" {
		return errors.New("driftdetector install: -address is required")
	}

	ctx := context.Background()
	s, shut, err := open(ctx)
	if err != nil {
		return err
	}
	defer shut()

	if err := driftdetector.NewWriter(s.own).SetAddress(ctx, *address); err != nil {
		return err
	}
	fmt.Printf("Address set to %s: the detector delivers its own page there when the notifier's last check\n", *address)
	fmt.Println("or every factory last check is stale at once.")
	return nil
}
