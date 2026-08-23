package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/score"
)

// learnCommand is the score's own pass: it reads every outcome in the store,
// computes the values the score supplies, prints what moved and what moved it,
// and appends a score version where the table has stopped matching the one in
// force. It is the same pass a run performs when it composes, run against an
// existing database — the arrangement `watch` already has, and for the same
// reason: what a pass needs is in the store rather than in what a process
// remembers.
//
// It is a command of its own because the movement is worth reading on its own. A
// run appends the version and prints two lines about it; this prints the table,
// the evidence behind each moved value, and which items the score has held out —
// which is what an owner disagreeing with a learned number has to see.
func learnCommand(args []string) error {
	flags := flag.NewFlagSet("learn", flag.ContinueOnError)
	dry := flags.Bool("dry", false, "read the outcomes and print what would move, appending no version")
	if err := flags.Parse(args); err != nil {
		return err
	}

	return withPool(func(ctx context.Context, pool *pgxpool.Pool) error {
		inForce, found, err := score.Newest(ctx, pool)
		if err != nil {
			return err
		}
		if found {
			fmt.Printf("Score version %s in force: formula %s\n", inForce.ID, inForce.FormulaVersion)
		} else {
			fmt.Println("No score version has been appended yet")
		}

		learned, err := score.Learn(ctx, pool)
		if err != nil {
			return err
		}
		printSupplied(os.Stdout, learned, inForce)

		if err := printHeldOut(ctx, os.Stdout, pool); err != nil {
			return err
		}

		if *dry {
			fmt.Println("\nNothing was appended: -dry reads the outcomes and writes nothing.")
			return nil
		}
		appended, err := score.NewWriter(pool).Ensure(ctx, scoreActor)
		if err != nil {
			return err
		}
		if found && appended.ID == inForce.ID {
			fmt.Printf("\nNo version was appended: %s already says what the outcomes in this store supply.\n", appended.ID)
			return nil
		}
		superseded := "nothing, this being the first"
		if appended.Supersedes != "" {
			superseded = appended.Supersedes
		}
		fmt.Printf("\nScore version %s appended, superseding %s: every decision from here on names it.\n",
			appended.ID, superseded)
		return nil
	})
}

// printSupplied prints the whole table and marks each row that differs from the
// version in force, which is what makes a movement readable: the value now, the
// value the decisions already taken were decided against, and the evidence between
// them.
//
// It walks the learned rows and then the moved rows of the version in force that
// the learned table no longer holds. A value can move back — the window limit rises
// on a service
// and falls at the next rollback that sweeps — and a row that has gone is a
// movement a reader would otherwise never see.
func printSupplied(out io.Writer, learned score.SuppliedValues, inForce score.Version) {
	fmt.Fprintln(out, "\nWhat the score supplies, where an owner authored nothing:")
	for _, s := range learned {
		where := "every subject"
		if s.Moved() {
			where = s.Subject
		}
		fmt.Fprintf(out, "  %s on %s = %v", s.Parameter, where, s.Value)
		if before, had := inForce.Value(s.Parameter, s.Subject); !had || before.Value != s.Value {
			fmt.Fprintf(out, " — moved from %v", before.Value)
		}
		fmt.Fprintln(out)
		fmt.Fprintf(out, "      %s\n", s.Why)
	}
	for _, was := range inForce.Supplied {
		if !was.Moved() || heldRow(learned, was) {
			continue
		}
		now, _ := learned.Value(was.Parameter, was.Subject)
		fmt.Fprintf(out, "  %s on %s = %v — moved from %v\n", was.Parameter, was.Subject, now.Value, was.Value)
		fmt.Fprintf(out, "      the outcomes no longer move it: %s\n", now.Why)
	}
}

// heldRow is whether the learned table holds a row for this subject. It is not
// SuppliedValues.Value, which answers the starting value for a subject with no row
// of its own — so asking that alone could not tell a value still moved from one
// that has moved back.
func heldRow(learned score.SuppliedValues, s score.Supplied) bool {
	for _, row := range learned {
		if row.Parameter == s.Parameter && row.Subject == s.Subject {
			return true
		}
	}
	return false
}

// printHeldOut prints the items the score has selected into its sample, which is
// the one thing the learning does that changes what the factory decides rather
// than what it supplies.
func printHeldOut(ctx context.Context, out io.Writer, pool *pgxpool.Pool) error {
	items, err := score.HeldOutItems(ctx, pool)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Fprintf(out, "\nThe score has held no item out, so every threshold it supplies can fall and cannot rise: the only evidence for raising one is a change the score wanted gated and did not gate. It holds out %.0f in every 100 firings it would have gated.\n",
			score.SampleRate*100)
		return nil
	}
	fmt.Fprintln(out, "\nHeld out of the gate the score would have gated:")
	for _, id := range items {
		fmt.Fprintf(out, "  %s\n", id)
	}
	return nil
}
