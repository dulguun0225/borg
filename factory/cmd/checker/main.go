package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/checker"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/targetseam"
	"github.com/dulguun0225/borg/factory/window"
)

// deployCredentialName is the credential the target seam requires on every
// operation, read from the same secrets file the factory's own run reads. The
// independent checker resolves it and hands the reference across the seam; what
// sits behind the seam is what would read a value, and on this substrate
// nothing does.
const deployCredentialName = "deploy.local"

const subcommands = "pass, show, clear <mismatch-id>"

func main() {
	if err := dispatch(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func dispatch(args []string) error {
	if len(args) == 0 {
		return errors.New("checker: a subcommand is required — " + subcommands)
	}
	switch args[0] {
	case "pass":
		return passCommand(args[1:])
	case "show":
		return showCommand(args[1:])
	case "clear":
		return clearCommand(args[1:])
	default:
		return fmt.Errorf("checker: %q is none of %s", args[0], subcommands)
	}
}

// stores is the two pools one command holds: the factory's, which it only reads,
// and its own, which nothing in the factory writes.
type stores struct {
	factory *pgxpool.Pool
	own     *pgxpool.Pool
}

// open opens both and applies the independent checker's own schema. The
// factory's is not applied here: a store the independent checker applied would
// be a store the independent checker writes, and it reads that one.
func open(ctx context.Context) (stores, func(), error) {
	factory, err := postgres.Open(ctx, postgres.URL())
	if err != nil {
		return stores{}, nil, err
	}
	own, err := checker.Open(ctx, checker.URL())
	if err != nil {
		factory.Close()
		return stores{}, nil, err
	}
	if err := checker.Apply(ctx, own); err != nil {
		factory.Close()
		own.Close()
		return stores{}, nil, err
	}
	return stores{factory: factory, own: own}, func() { factory.Close(); own.Close() }, nil
}

// passCommand is one comparison of every service on every production target.
func passCommand(args []string) error {
	flags := flag.NewFlagSet("pass", flag.ContinueOnError)
	secrets := flags.String("secrets", "", "path of the secrets file (required; the seam needs a credential reference on every operation)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *secrets == "" {
		return errors.New("checker pass: -secrets is required")
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
	return pass(ctx, s, os.Stdout, secretref.MustNew(deployCredentialName),
		func(dir string) targetseam.Target { return localtarget.New(dir) })
}

// pass is the comparison itself, taking what it reaches a target with so that a
// test drives the same code with a target of its own.
func pass(ctx context.Context, s stores, out io.Writer, credential secretref.Ref,
	targetAt func(dir string) targetseam.Target) error {
	production, found, err := environment.ByName(ctx, s.factory, environment.ProductionName)
	if err != nil {
		return err
	}
	if !found {
		fmt.Fprintln(out, "The factory has no production environment record; there is nothing to check")
		return nil
	}
	services, err := service.All(ctx, s.factory)
	if err != nil {
		return err
	}
	if len(services) == 0 {
		fmt.Fprintln(out, "The factory has no services; there is nothing to check")
		return nil
	}

	writer := checker.NewWriter(s.own)
	for _, svc := range services {
		recorded, err := recordedFor(ctx, s.factory, svc.ID, production.ID)
		if err != nil {
			return err
		}
		excused, err := excusedBuilds(ctx, s.factory, svc.ID)
		if err != nil {
			return err
		}
		for _, target := range production.Targets {
			p := checker.Pass{
				ServiceID:         svc.ID,
				Target:            target,
				RecordedReleaseID: recorded.ReleaseID,
				RecordedBuildID:   recorded.BuildID,
			}
			running, err := targetAt(target).ReadRunning(ctx, svc.Name, credential)
			if err != nil {
				// Failing to reach a target is not a mismatch: a network blip would
				// otherwise hold every production deploy, which is why the last check
				// exists at all.
				p.Why = err.Error()
			} else {
				p.Reached = true
				p.RunningBuild = running.Build
				p.Excused = running.Build != "" && excused[running.Build]
			}

			written, err := writer.Record(ctx, p)
			if err != nil {
				return err
			}
			report(out, svc.Name, p, written)
		}
	}
	return nil
}

// recordedFor is what the factory recorded as the service's current release in
// production: the release and the build its production deploy record names, and
// nothing where no deploy of it has completed. It is the one factory record the
// independent checker reads in the other direction.
func recordedFor(ctx context.Context, pool *pgxpool.Pool, serviceID, environmentID string) (deploy.Deploy, error) {
	current, running, err := deploy.Current(ctx, pool, serviceID, environmentID)
	if err != nil || !running {
		return deploy.Deploy{}, err
	}
	return current, nil
}

// excusedBuilds is every build an open watch window accounts for: the build of
// the release under watch, and — where a substrate keeps one — the build of the
// control that window's deploy record names. A build running beside the current
// release is a mismatch only where no open window names it, or the independent
// checker would page on every rollout it sees.
//
// On this substrate the set is never the reason a pass agrees. One directory runs one
// process, so what a target reports is either the current release's build or a
// disagreement, and a release under watch is the current release. The read is here
// because the rule is the design's and not the substrate's, and because a control
// running after its window closed is meant to be a mismatch like any other — which
// is how a failed teardown is caught.
func excusedBuilds(ctx context.Context, pool *pgxpool.Pool, serviceID string) (map[string]bool, error) {
	open, err := window.AllOpen(ctx, pool, serviceID)
	if err != nil {
		return nil, err
	}
	excused := map[string]bool{}
	for _, w := range open {
		rel, err := release.Get(ctx, pool, w.ReleaseID)
		if err != nil {
			return nil, err
		}
		excused[rel.BuildID] = true
		// The control the window's deploy record names would be added here. There are
		// no columns for one, because this substrate starts none — package deploy's
		// doc.go says so where the fields would be.
	}
	return excused, nil
}

func report(out io.Writer, serviceName string, p checker.Pass, written checker.Recorded) {
	switch {
	case !p.Reached:
		fmt.Fprintf(out, "%s on %s: the target could not be reached, which is no mismatch — %s\n",
			serviceName, p.Target, p.Why)
	case written.Raised != "":
		fmt.Fprintf(out, "%s on %s: MISMATCH %s — the target runs %q and the factory recorded %q\n",
			serviceName, p.Target, written.Raised, p.RunningBuild, p.RecordedBuildID)
		fmt.Fprintln(out, "  it holds that service's production deploys until a human clears it here, and the factory cannot")
	case written.Agreed != "":
		fmt.Fprintf(out, "%s on %s: agrees now, and mismatch %s still stands — a later agreement is recorded on it as evidence\n",
			serviceName, p.Target, written.Agreed)
	case p.Excused:
		fmt.Fprintf(out, "%s on %s: the target runs %q, which an open watch window accounts for\n",
			serviceName, p.Target, p.RunningBuild)
	default:
		fmt.Fprintf(out, "%s on %s: agrees — build %q\n", serviceName, p.Target, p.RunningBuild)
	}
}

// showCommand prints the store: every mismatch, and the last check per
// service and target. The second is what says whether this process is still running.
func showCommand(args []string) error {
	if len(args) != 0 {
		return errors.New("checker show: no arguments")
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
	all, err := checker.All(ctx, s.own)
	if err != nil {
		return err
	}
	if len(all) == 0 {
		fmt.Fprintln(out, "No mismatch has ever been raised")
	}
	for _, m := range all {
		state := "UNCLEARED — it holds that service's production deploys"
		if m.Cleared() {
			state = "cleared at " + m.ClearedAt + " by " + m.ClearedBy
		}
		fmt.Fprintf(out, "mismatch %s at %s: %s\n  %s\n", m.ID, m.At, m.Why(), state)
		if m.LaterAgreements > 0 {
			fmt.Fprintf(out, "  %d later comparison(s) agreed, and it stands all the same\n", m.LaterAgreements)
		}
	}

	checks, err := checker.LastChecks(ctx, s.own, "")
	if err != nil {
		return err
	}
	if len(checks) == 0 {
		fmt.Fprintln(out, "No check has ever been recorded, so this independent checker has never run")
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
		return errors.New("checker clear: one argument, the mismatch id")
	}
	if *human == "" {
		return errors.New("checker clear: -human is required")
	}

	ctx := context.Background()
	s, shut, err := open(ctx)
	if err != nil {
		return err
	}
	defer shut()

	cleared, err := checker.NewWriter(s.own).Clear(ctx, flags.Arg(0), *human)
	if err != nil {
		return err
	}
	fmt.Printf("Mismatch %s cleared at %s by %s; the hold it set on %s is lifted\n",
		cleared.ID, cleared.ClearedAt, cleared.ClearedBy, cleared.ServiceID)
	fmt.Println("The page about it is answered by the factory's next pass over this store, which is what writes that event")
	return nil
}
