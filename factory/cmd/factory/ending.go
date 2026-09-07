package main

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/legalhold"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
)

// The four ways a human ends something: an item or an intent dropped for good,
// a commit accepted that the queue did not make, a mitigation performed and
// ended on a target, and the log's retention enforced.

// dropItem ends one item for good and tears its candidate environment down
// with it. Two callers make the act — the subcommand above, and Work's own
// EndItem — because the design has the environment stay the item's until it
// merges, is dropped, or is superseded, so an item dropped anywhere reaches
// the deployer the same way.
func (p *path) dropItem(ctx context.Context, actor record.Actor, id string) error {
	dropped, err := item.NewDispatch(p.d.pool, p.d.token).Drop(ctx, actor, id)
	if errors.Is(err, item.ErrEnded) {
		// An item already dropped is not dropped twice, and its environment
		// may still stand from a drop that could not reach the deployer; what
		// follows tears that down.
		if dropped, err = item.Get(ctx, p.d.pool, id); err != nil {
			return err
		}
		if dropped.Stage != item.StageDropped {
			return fmt.Errorf("factory: %s is %s, and only work still open or already dropped is dropped", id, dropped.Stage)
		}
		fmt.Fprintf(p.d.out, "Item %s is already dropped\n", dropped.ID)
	} else if err != nil {
		return err
	} else {
		fmt.Fprintf(p.d.out, "Item %s is dropped: work on it ends for good, and its branch is not merged\n", dropped.ID)
		fmt.Fprintln(p.d.out, "Every row of its own the gate left open is abandoned by the next firing that reads them")
	}
	env, found, err := environment.ForItem(ctx, p.d.pool, dropped.ID)
	if err != nil {
		return err
	}
	if !found || !env.Live() || len(env.Targets) == 0 {
		return nil
	}
	svc, err := p.serviceOf(ctx, dropped.ServiceID)
	if err != nil {
		return err
	}
	// Stopping comes first, so a record saying torn down never stands over a
	// process still running — the order the merge's teardown keeps.
	if _, err := p.d.targets.at(env.Targets[0].Address).Stop(ctx, deployerPrincipal, svc.Name, p.d.credential); err != nil {
		return err
	}
	if err := p.candidates.TearDown(ctx, deployActor, env.ID, environment.ReasonDropped, environment.Rate{}); err != nil {
		return err
	}
	fmt.Fprintf(p.d.out, "Candidate environment %s torn down as dropped; the record is kept\n", env.ID)
	return nil
}

// truncateCommand is `factory truncate`: the decision log's retention pass. It
// appends the truncation row naming the boundary and the retention value being
// enforced, and then removes every row before that boundary — the one write in
// the factory that destroys evidence, which is why the row that records it is
// written first and stays.
//
// The row's actor is whoever authored the retention value, read off the policy
// versions, and not the human running the pass: what the row says is under what
// authority the rows went, and the pass is the factory enforcing a value a
// human authored. -human is who the reads of the log are made as, which every
// read of it names.
//
// It is refused where a legal hold stands: a hold suspends every retention
// clock it names, and the pass reads the holds before it reads a row. It is
// refused too where nothing is authored — the log is then kept for the life of
// the install — and where the boundary is inside the retention, which package
// decisionlog checks against the value the cut names.
func truncateCommand(args []string) error {
	flags := flag.NewFlagSet("truncate", flag.ContinueOnError)
	human := flags.String("human", "owner", "the human running the retention pass, as whom the log is read")
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

		// The retention value in force. It is authored and is not one of gate
		// policy's eleven rows, so it is read one parameter at a time and not
		// through the set those rows make: this pass is the factory's own, and
		// what it enforces is what the factory-wide record holds. The reader is
		// composed with the score version in force, which is what supplies a
		// value an owner authored none for.
		version, err := score.NewWriter(pool, token, marksOf(pool)).Ensure(ctx, scoreActor)
		if err != nil {
			return err
		}
		reader := policy.NewReader(pool, token, version)
		inForce, err := reader.InForce(ctx, gatepolicy.DecisionLogRetention, policy.Subjects{})
		if err != nil {
			return err
		}
		if inForce.Source != policy.FromAuthored {
			return errors.New("factory truncate: nobody has authored decision-log retention, " +
				"so the log is kept for the life of the install and there is nothing to enforce")
		}
		retention := int64(inForce.Number)

		// Who authored the value, which is the actor the truncation row names.
		// A value in force was authored by some version, so a pass that cannot
		// find one is a pass whose row would name the wrong person.
		settings, err := factorysettings.Get(ctx, pool)
		if err != nil {
			return err
		}
		author, found, err := reader.AuthoredBy(ctx, asPrincipal(actor), gatepolicy.DecisionLogRetention,
			policy.Scope{Kind: policy.ScopeFactorySettings, ID: settings.ID})
		if err != nil {
			return err
		}
		if !found {
			return errors.New("factory truncate: no policy version authored the retention value in force, " +
				"and the truncation row names who authored it")
		}

		// The two versions in force at the cut, which the truncation row names
		// beside the value and the boundary: a decision after the cut naming a
		// version before it is read against what it was decided under, and the
		// log refuses a truncation that names neither.
		inForceNow, err := reader.Newest(ctx, asPrincipal(actor))
		if err != nil {
			return err
		}
		row, err := decisionlog.NewWriter(pool, token).Truncate(ctx, decisionlog.Cut{
			Actor:            author,
			RetentionSeconds: retention,
			Boundary:         *boundary,
			PolicyVersion:    inForceNow.ID,
			ScoreVersion:     version.ID,
		}, holds)
		if err != nil {
			return err
		}
		fmt.Printf("Truncation %s written: %s is the log's new checkpoint, under the %d second(s) %s authored\n",
			row.ID, *boundary, retention, author.Key)
		fmt.Println("What the log no longer holds is gone; the truncation row is what says a cut happened and where")
		return nil
	})
}
