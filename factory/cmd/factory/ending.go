package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/legalhold"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/service"
)

// The four ways a human ends something: an item or an intent dropped for good,
// a commit accepted that the queue did not make, a mitigation performed and
// ended on a target, and the log's retention enforced.

// dropCommand is `factory drop <item-id|intent-id>`: a human ending work for
// good. Work ends an item that escalated and nobody took over, or the intent
// above it; Ops ends a revert item a mark made unnecessary. The value is
// written by dispatch on an item and by intake on an intent, each being the
// component that owns the record.
func dropCommand(args []string) error {
	flags := flag.NewFlagSet("drop", flag.ContinueOnError)
	human := flags.String("human", "owner", "the human ending the work")

	id := ""
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		id, args = args[0], args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if id == "" || flags.NArg() != 0 {
		return errors.New("factory drop: one argument, the item or the intent, and then any flags")
	}

	return withPool(func(ctx context.Context, pool *pgxpool.Pool, token lease.Token) error {
		actor, err := humanNamed(ctx, pool, token, *human)
		if err != nil {
			return err
		}
		switch {
		case strings.HasPrefix(id, item.IDPrefix+"_"):
			dropped, err := item.NewDispatch(pool, token).Drop(ctx, actor, id)
			if err != nil {
				return err
			}
			fmt.Printf("Item %s is dropped: work on it ends for good, and its branch is not merged\n", dropped.ID)
			fmt.Println("Every row of its own the gate left open is abandoned by the next firing that reads them")
			return nil
		case strings.HasPrefix(id, intent.IDPrefix+"_"):
			// Dropping leaves nothing waiting on a human, so this intake
			// reaches none: the two calls intake makes are at a round of the
			// interview and at an escalation, and this is neither.
			if err := intent.NewIntake(pool, token, intent.NoNotifier{}).Drop(ctx, actor, id); err != nil {
				return err
			}
			fmt.Printf("Intent %s is dropped: no item of it is dispatched and nothing below it moves\n", id)
			return nil
		default:
			return fmt.Errorf("factory drop: %q is neither an item nor an intent", id)
		}
	})
}

// acceptCommitCommand is `factory accept-commit <service> <commit>`: a human at
// Work accepting a commit master holds that the queue did not make. It is what
// ends the stop a commit the queue did not put there leaves — the queue mints
// nothing for the service while it stands — and the release it mints is one no
// gate decided.
func acceptCommitCommand(args []string) error {
	flags := flag.NewFlagSet("accept-commit", flag.ContinueOnError)
	secrets := flags.String("secrets", "", "path of the secrets file (required)")
	targets := flags.String("targets", "", "the directory the local target runs releases from (required)")
	human := flags.String("human", "owner", "the human accepting the commit")

	name, commit := "", ""
	for len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		if name == "" {
			name = args[0]
		} else if commit == "" {
			commit = args[0]
		} else {
			break
		}
		args = args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if name == "" || commit == "" || flags.NArg() != 0 {
		return errors.New("factory accept-commit: two arguments, the service and the commit, and then any flags")
	}

	return withPath(pathFlags{secrets: *secrets, targets: *targets, human: *human},
		func(ctx context.Context, p *path) error {
			svc, found, err := service.ByName(ctx, p.d.pool, name)
			if err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("factory accept-commit: no service named %q", name)
			}
			accepted, err := p.queue.AcceptCommit(ctx, p.human, svc.ID, commit)
			if err != nil {
				return err
			}
			if accepted.Why != "" {
				fmt.Fprintf(p.d.out, "Commit %s was not accepted: %s (rejection row %s)\n",
					commit, accepted.Why, accepted.RejectionRow)
				return nil
			}
			fmt.Fprintf(p.d.out, "Commit %s accepted on %s by %s; release %d minted and the queue's stop is ended\n",
				commit, svc.Name, p.d.human, accepted.Release.Number)
			fmt.Fprintln(p.d.out, "  the release is one no gate decided, and its record says so")
			return nil
		})
}

// mitigateCommand is `factory mitigate <deploy-id>`: a human at Ops
// instructing the deployer to perform one of the three operations on a target,
// and ending one already standing. The factory performs none of the three on
// its own — a mitigation is a human's instruction and the record says which
// human.
func mitigateCommand(args []string) error {
	flags := flag.NewFlagSet("mitigate", flag.ContinueOnError)
	secrets := flags.String("secrets", "", "path of the secrets file (required)")
	targets := flags.String("targets", "", "the directory the local target runs releases from (required)")
	human := flags.String("human", "owner", "the named human at Ops instructing the deployer")
	operation := flags.String("operation", "", "one of shift_traffic, set_instance_count, end_every_instance")
	share := flags.Float64("share", 0, "the share a traffic shift asks for")
	count := flags.Int("count", 0, "the instance count a set_instance_count asks for")
	end := flags.String("end", "", "the id of a standing mitigation to end, instead of performing one")

	deployID := ""
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		deployID, args = args[0], args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("factory mitigate: one argument, the deploy the mitigation is on, and then any flags")
	}
	if *end == "" && (deployID == "" || *operation == "") {
		return errors.New("factory mitigate: a deploy id and -operation, or -end naming a mitigation to end")
	}

	return withPath(pathFlags{secrets: *secrets, targets: *targets, human: *human},
		func(ctx context.Context, p *path) error {
			if *end != "" {
				if err := p.deploys.EndMitigation(ctx, *end); err != nil {
					return err
				}
				fmt.Fprintf(p.d.out, "Mitigation %s ended; what it did to the target stands until a deploy replaces it\n", *end)
				return nil
			}
			dep, err := deploy.Get(ctx, p.d.pool, deployID)
			if err != nil {
				return err
			}
			svc, err := p.serviceOf(ctx, dep.ServiceID)
			if err != nil {
				return err
			}
			address := p.d.dir
			if len(p.productionAddresses()) > 0 {
				address = p.productionAddresses()[0]
			}
			performed, err := deploy.Mitigate(ctx, p.deploys, deploy.Mitigating{
				Actor:       p.human,
				Principal:   deployerPrincipal,
				Operation:   deploy.Operation(*operation),
				Address:     address,
				Target:      p.d.targets.at(address),
				DeployID:    deployID,
				ServiceName: svc.Name,
				Build:       dep.BuildID,
				Share:       *share,
				Count:       *count,
				Credential:  p.d.credential,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(p.d.out, "Mitigation %s performed on %s by %s: %s\n",
				performed.ID, address, p.d.human, *operation)
			fmt.Fprintln(p.d.out, "  it stands until a human ends it, and the drift detector reads the target against the deploy record meanwhile")
			return nil
		})
}

// truncateCommand is `factory truncate`: the decision log's retention pass. It
// appends the truncation row naming the boundary and the retention value being
// enforced, and then removes every row before that boundary — the one write in
// the factory that destroys evidence, which is why the row that records it is
// written first and stays.
//
// It is refused where a legal hold stands: a hold suspends every retention
// clock it names, and the pass reads the holds before it reads a row.
func truncateCommand(args []string) error {
	flags := flag.NewFlagSet("truncate", flag.ContinueOnError)
	human := flags.String("human", "owner", "the human running the retention pass")
	boundary := flags.String("boundary", "", "the id of the oldest row that will remain")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("factory truncate: no arguments, and then any flags")
	}
	if *boundary == "" {
		return errors.New("factory truncate: -boundary names the row that becomes the checkpoint")
	}

	return withPool(func(ctx context.Context, pool *pgxpool.Pool, token lease.Token) error {
		actor, err := humanNamed(ctx, pool, token, *human)
		if err != nil {
			return err
		}
		// The legal holds standing, read here and handed to the cut: package
		// decisionlog refuses the truncation where one stands and may not
		// import the record that says so, so the caller reads them.
		standing, err := legalhold.Standing(ctx, pool)
		if err != nil {
			return err
		}
		holds := make([]string, 0, len(standing))
		for _, one := range standing {
			holds = append(holds, one.ID)
		}

		// The retention value in force, read through the whole set rather than
		// one parameter: package policy's reader answers per subject and this
		// pass is the factory's own, so what it enforces is what the
		// factory-wide row holds. The reader is composed with the score version
		// in force, which is what supplies a value an owner authored none for.
		version, err := score.NewWriter(pool, token, marksOf(pool)).Ensure(ctx, scoreActor)
		if err != nil {
			return err
		}
		all, err := policy.NewReader(pool, token, version).All(ctx, policy.Subjects{})
		if err != nil {
			return err
		}
		retention := 0.0
		for _, one := range all {
			if one.Parameter == gatepolicy.DecisionLogRetention {
				retention = one.Number
			}
		}
		row, err := decisionlog.NewWriter(pool, token).Truncate(ctx, decisionlog.Cut{
			Actor:     actor,
			Retention: fmt.Sprintf("%.0f second(s)", retention),
			Boundary:  *boundary,
		}, holds)
		if err != nil {
			return err
		}
		fmt.Printf("Truncation %s written: %s is the log's new checkpoint, under retention of %.0f second(s)\n",
			row.ID, *boundary, retention)
		fmt.Println("What the log no longer holds is gone; the truncation row is what says a cut happened and where")
		return nil
	})
}
