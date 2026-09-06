package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/safeguard"
	"github.com/dulguun0225/borg/factory/score"
)

// policyCommand prints what is in force: every parameter, where its value came
// from, the safeguards that reached it, and what reads it at this milestone. It
// is the one place an owner sees that four of the eight are read by nothing yet.
func policyCommand(args []string) error {
	flags := flag.NewFlagSet("policy", flag.ContinueOnError)
	serviceName := flags.String("service", "", "read the service-scoped parameters of this service")
	areaName := flags.String("area", "", "read the area-scoped parameters of this area")
	projectName := flags.String("project", defaultProjectName, "read the risk threshold of production's environment for this project")
	human := flags.String("human", "owner", "the human this read is made as, which the read event names")
	gateRow := flags.String("gate", gate.MergeToMaster.String(), "read the threshold of this gate row")
	stage := flags.String("stage", string(item.StageImplementation), "read the attempt limit of this stage")
	if err := flags.Parse(args); err != nil {
		return err
	}

	return withPool(func(ctx context.Context, pool *pgxpool.Pool, token lease.Token) error {
		row, err := gate.RowFrom(*gateRow)
		if err != nil {
			return err
		}
		subjects := policy.Subjects{GateRow: row.String(), Stage: item.Stage(*stage)}
		if *serviceName != "" {
			svc, err := namedService(ctx, pool, *serviceName)
			if err != nil {
				return err
			}
			subjects.ServiceID = svc.ID
		}
		if *areaName != "" {
			ar, err := namedArea(ctx, pool, *areaName)
			if err != nil {
				return err
			}
			subjects.AreaID = ar.ID
		}
		prj, err := namedProject(ctx, pool, *projectName)
		if err != nil {
			return err
		}

		production, found, err := environment.Production(ctx, pool, prj.ID)
		if err != nil {
			return err
		}
		if found {
			subjects.EnvironmentID = production.ID
		}

		// The newest score version and not an ensured one: this command prints what
		// is in force and authors nothing, and an ensure here would have a read
		// append a record.
		scoreVersion, found, err := score.Newest(ctx, pool, token)
		if err != nil {
			return err
		}
		if found {
			fmt.Printf("Score version %s in force: formula %s\n", scoreVersion.ID, scoreVersion.FormulaVersion)
		} else {
			fmt.Println("No score version has been appended, so every supplied value is where the formula was calibrated")
		}

		actor, err := humanNamed(ctx, pool, token, *human)
		if err != nil {
			return err
		}
		reader := policy.NewReader(pool, token, scoreVersion)
		version, err := reader.Newest(ctx, asPrincipal(actor))
		if err != nil {
			return err
		}
		fmt.Printf("Policy version %s in force: %s %s on %s by %s\n",
			version.ID, version.Action, version.Parameter, version.Scope, version.Actor.Key)

		effectives, err := reader.All(ctx, subjects)
		if err != nil {
			return err
		}
		printEffectives(os.Stdout, effectives)

		safeguards, err := safeguard.All(ctx, pool)
		if err != nil {
			return err
		}
		if len(safeguards) == 0 {
			fmt.Println("\nNo safeguards are placed.")
			return nil
		}
		fmt.Println("\nThe safeguards:")
		for _, p := range safeguards {
			state := "in force"
			if p.Withdrawn {
				state = "withdrawn"
			}
			fmt.Printf("  %s: %s on %s as a %s, %s\n", p.ID, p.Parameter, p.Subject, p.Direction, state)
		}
		return nil
	})
}

// printEffectives writes the effective value of every parameter, grouped as gate
// policy's own table groups them: one row per row, however many parameters it
// carries.
func printEffectives(out io.Writer, effectives []policy.Effective) {
	row := ""
	for _, e := range effectives {
		if e.Row != row {
			fmt.Fprintf(out, "\n%s\n", e.Row)
			row = e.Row
		}
		value := fmt.Sprintf("%v", e.Number)
		if len(e.List) > 0 || e.Parameter == gatepolicy.AllowedPredicateKinds {
			value = strings.Join(e.List, ", ")
			if value == "" {
				value = "(empty)"
			}
		}
		fmt.Fprintf(out, "  %s = %s (%s)", e.Parameter, value, e.Source)
		if e.Supplied.Moved() {
			fmt.Fprintf(out, ", moved by outcomes on %s", e.Supplied.Subject)
		}
		if e.Clamped {
			fmt.Fprint(out, ", clamped by a safeguard")
		}
		if e.HumanBySafeguard {
			fmt.Fprint(out, ", a safeguard adds a human")
		}
		if e.ReadBy == "" {
			fmt.Fprint(out, "; read by nothing at this milestone")
		} else {
			fmt.Fprintf(out, "; read by %s", e.ReadBy)
		}
		fmt.Fprintln(out)
		if e.Source == policy.FromSupplied && e.Supplied.Why != "" {
			fmt.Fprintf(out, "      the score supplies it: %s\n", e.Supplied.Why)
		}
	}
}
