package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
)

// authorCommand authors one parameter. Which subject flags are read follows from
// the parameter, because the record a parameter is a field of is a fact of the
// parameter and not a choice: a threshold is authored on an environment for one
// gate row, a limit on the factory-wide settings record for one stage, a target on an
// area, and the window's four on a service.
func authorCommand(args []string) error {
	flags := flag.NewFlagSet("author", flag.ContinueOnError)
	name := flags.String("parameter", "", "the parameter to author (required); factory policy lists them")
	value := flags.String("value", "", "the number to author, or a comma-separated list for the list of allowed predicate kinds")
	serviceName := flags.String("service", "", "the service, for a parameter that is a field of one")
	areaName := flags.String("area", "", "the area, for a parameter that is a field of one")
	projectName := flags.String("project", defaultProjectName, "the project, for the risk threshold, read on production's environment for it")
	gateRow := flags.String("gate", gate.MergeToMaster.String(),
		"the gate row a threshold applies at, "+gate.RolePromptOrSkill.String()+" being the one row with no environment to read it from")
	stage := flags.String("stage", string(item.StageImplementation), "the stage an attempt limit applies to")
	quantity := flags.String("quantity", string(gatepolicy.QuantityErrorRate), "the quantity the window size applies to")
	human := flags.String("human", "owner", "the owner authoring it")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *name == "" || *value == "" {
		return errors.New("factory author: -parameter and -value are required")
	}
	parameter := gatepolicy.Parameter(*name)
	if _, err := gatepolicy.Define(parameter); err != nil {
		return err
	}

	return withPool(func(ctx context.Context, pool *pgxpool.Pool, token lease.Token) error {
		factory := policy.NewFactory(pool, token)
		actor, err := humanNamed(ctx, pool, token, *human)
		if err != nil {
			return err
		}

		if parameter == gatepolicy.AllowedPredicateKinds {
			version, err := factory.AuthorAllowedPredicateKinds(ctx, actor, strings.Split(*value, ","))
			if err != nil {
				return err
			}
			return authored(parameter, *value, version)
		}

		number, err := strconv.ParseFloat(*value, 64)
		if err != nil {
			return fmt.Errorf("factory author: %s takes a number, not %q", parameter, *value)
		}

		var version policy.Version
		switch parameter {
		case gatepolicy.RiskThreshold:
			if *gateRow == gate.RolePromptOrSkill.String() {
				version, err = factory.AuthorRolePromptOrSkillThreshold(ctx, actor, number)
				break
			}
			prj, err2 := namedProject(ctx, pool, *projectName)
			if err2 != nil {
				return err2
			}
			production, found, err2 := environment.Production(ctx, pool, prj.ID)
			if err2 != nil {
				return err2
			}
			if !found {
				return errors.New("factory author: production's environment record does not exist yet — run the path once, which installs it")
			}
			version, err = factory.AuthorGateThreshold(ctx, actor, production.ID, *gateRow, number)
		case gatepolicy.AttemptLimit:
			version, err = factory.AuthorAttemptLimit(ctx, actor, item.Stage(*stage), int(number))
		case gatepolicy.DecisionLogRetention:
			// Lengthening is in force on write. Shortening is decided at a gate
			// row of its own, routed away from whoever authored the value, so
			// what this writes there is the value pending and the row that
			// approves it is where it comes into force.
			version, err = factory.AuthorDecisionLogRetention(ctx, actor, int64(number))
			if errors.Is(err, policy.ErrShorteningIsDecided) {
				return writeShortening(ctx, factory, actor, int64(number))
			}
		case gatepolicy.ItemSizeTarget:
			ar, err2 := namedArea(ctx, pool, *areaName)
			if err2 != nil {
				return err2
			}
			version, err = factory.AuthorItemSizeTarget(ctx, actor, ar.ID, number)
		default:
			svc, err2 := namedService(ctx, pool, *serviceName)
			if err2 != nil {
				return err2
			}
			switch parameter {
			case gatepolicy.WindowSize:
				version, err = factory.AuthorWindowSize(ctx, actor, svc.ID, gatepolicy.Quantity(*quantity), number)
			case gatepolicy.WindowConfidence:
				version, err = factory.AuthorWindowConfidence(ctx, actor, svc.ID, number)
			case gatepolicy.WindowCap:
				version, err = factory.AuthorWindowCap(ctx, actor, svc.ID, number)
			case gatepolicy.WindowLimit:
				version, err = factory.AuthorWindowLimit(ctx, actor, svc.ID, number)
			}
		}
		if err != nil {
			return err
		}
		return authored(parameter, *value, version)
	})
}
func authored(parameter gatepolicy.Parameter, value string, version policy.Version) error {
	fmt.Printf("Authored %s = %s on %s; policy version %s\n", parameter, value, version.Scope, version.ID)
	return nil
}

// writeShortening writes a shorter decision-log retention value pending and
// says what decides it. The value is not in force here: the row that decides a
// shortening is routed away from whoever authored it, which is what the record
// this writes makes possible, and `factory approve -retention-shortening` is
// that row.
func writeShortening(ctx context.Context, factory *policy.Factory, actor record.Actor,
	seconds int64) error {
	written, version, err := factory.WriteRetentionShortening(ctx, actor, seconds)
	if err != nil {
		return err
	}
	fmt.Printf("Shortening %s written pending: decision-log retention goes to %d second(s) once the row that decides it approves it; policy version %s\n",
		written.ID, seconds, version.ID)
	fmt.Printf("  `factory approve -retention-shortening %s` is that row, and it is routed away from %s\n",
		written.ID, actor.Key)
	return nil
}
