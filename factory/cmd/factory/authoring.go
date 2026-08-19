package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorypolicy"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/pin"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/service"
)

// The four subcommands of duty 8 and duty 9. An owner authors a parameter,
// places a pin, withdraws one, and reads what is in force — which is the whole
// of what the design has an owner do with gate policy, on a terminal until the
// Factory surface replaces it.
//
// Every one of them writes as a human, because gate policy is authored by an
// owner and package policy refuses a component. -human names which.

// areaCommand declares an area, which is what a pin or an item-size target is
// drawn on. It is duty 9's other write: an owner declares the groupings the rest
// of the factory is scoped against.
func areaCommand(args []string) error {
	flags := flag.NewFlagSet("area", flag.ContinueOnError)
	inside := flags.String("inside", "", "the area this one lies inside, by name; empty at the outermost")
	human := flags.String("human", "owner", "the owner declaring it")

	// The name is taken off the front before the flags are parsed, because
	// `area payments -inside greeting` is what a person types and Go's flag
	// package stops parsing at the first argument that is not a flag.
	name := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, args = args[0], args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if name == "" || flags.NArg() != 0 {
		return errors.New("factory area: one argument, the area's name, and then any flags")
	}

	return withPool(func(ctx context.Context, pool *pgxpool.Pool) error {
		insideID := ""
		if *inside != "" {
			outer, found, err := area.ByName(ctx, pool, *inside)
			if err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("factory area: no area is named %q, so %q cannot lie inside it", *inside, name)
			}
			insideID = outer.ID
		}
		declared, err := area.NewWriter(pool).Declare(ctx, owner(*human), name, insideID)
		if err != nil {
			return err
		}
		fmt.Printf("Area %s declared as %s\n", declared.Name, declared.ID)
		return nil
	})
}

// authorCommand authors one parameter. Which subject flags are read follows from
// the parameter, because the record a parameter is a field of is a fact of the
// parameter and not a choice: a threshold is authored on an environment for one
// gate row, a bound on the factory policy record for one stage, a target on an
// area, and the window's four on a service.
func authorCommand(args []string) error {
	flags := flag.NewFlagSet("author", flag.ContinueOnError)
	name := flags.String("parameter", "", "the parameter to author (required); factory policy lists them")
	value := flags.String("value", "", "the number to author, or a comma-separated list for the predicate catalog")
	serviceName := flags.String("service", "", "the service, for a parameter that is a field of one")
	areaName := flags.String("area", "", "the area, for a parameter that is a field of one")
	gateRow := flags.String("gate", string(gate.MergeToMaster), "the gate row a threshold applies at, or brief_or_skill for the factory's own row")
	stage := flags.String("stage", string(item.StageImplementation), "the stage an attempt bound applies to")
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

	return withPool(func(ctx context.Context, pool *pgxpool.Pool) error {
		factory := policy.NewFactory(pool)
		actor := owner(*human)

		if parameter == gatepolicy.PredicateCatalog {
			version, err := factory.AuthorPredicateCatalog(ctx, actor, strings.Split(*value, ","))
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
			if *gateRow == "brief_or_skill" {
				version, err = factory.AuthorBriefOrSkillThreshold(ctx, actor, number)
				break
			}
			production, found, err2 := environment.ByName(ctx, pool, environment.ProductionName)
			if err2 != nil {
				return err2
			}
			if !found {
				return errors.New("factory author: production's environment record does not exist yet — run the path once, which installs it")
			}
			version, err = factory.AuthorGateThreshold(ctx, actor, production.ID, *gateRow, number)
		case gatepolicy.AttemptBound:
			version, err = factory.AuthorAttemptBound(ctx, actor, item.Stage(*stage), int(number))
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
				version, err = factory.AuthorWindowSize(ctx, actor, svc.ID, number)
			case gatepolicy.WindowConfidence:
				version, err = factory.AuthorWindowConfidence(ctx, actor, svc.ID, number)
			case gatepolicy.WindowCap:
				version, err = factory.AuthorWindowCap(ctx, actor, svc.ID, number)
			case gatepolicy.K:
				version, err = factory.AuthorK(ctx, actor, svc.ID, number)
			}
		}
		if err != nil {
			return err
		}
		return authored(parameter, *value, version)
	})
}

// pinCommand places a pin or withdraws one. The direction is not a flag: it
// differs per parameter and points the same way in each, so an owner chooses the
// subject and the bound and never which way the bound points.
func pinCommand(args []string) error {
	flags := flag.NewFlagSet("pin", flag.ContinueOnError)
	name := flags.String("parameter", "", "the parameter to bind")
	subject := flags.String("subject", "", "what the pin is drawn on, as kind:name — service:x, area:y, gate_row:merge_to_master, factory_policy:")
	bound := flags.String("bound", "", "the number the pin bounds by, or a comma-separated list for the predicate catalog; a pin on the risk threshold takes none")
	withdraw := flags.String("withdraw", "", "the id of a pin to withdraw instead of placing one")
	human := flags.String("human", "owner", "the owner placing it")
	if err := flags.Parse(args); err != nil {
		return err
	}

	return withPool(func(ctx context.Context, pool *pgxpool.Pool) error {
		factory := policy.NewFactory(pool)
		actor := owner(*human)

		if *withdraw != "" {
			version, err := factory.WithdrawPin(ctx, actor, *withdraw)
			if err != nil {
				return err
			}
			fmt.Printf("Pin %s withdrawn; policy version %s\n", *withdraw, version.ID)
			return nil
		}
		if *name == "" || *subject == "" {
			return errors.New("factory pin: -parameter and -subject are required, or -withdraw <pin-id>")
		}

		parameter := gatepolicy.Parameter(*name)
		definition, err := gatepolicy.Define(parameter)
		if err != nil {
			return err
		}
		on, err := pinSubject(ctx, pool, *subject)
		if err != nil {
			return err
		}

		var number float64
		var list []string
		switch {
		case definition.Direction == gatepolicy.DirectionAddsAHuman:
			// A pin on the risk threshold adds a human and bounds no value.
		case definition.Kind == gatepolicy.KindList:
			list = strings.Split(*bound, ",")
		default:
			number, err = strconv.ParseFloat(*bound, 64)
			if err != nil {
				return fmt.Errorf("factory pin: a pin on %s takes a number as its bound, not %q", parameter, *bound)
			}
		}

		placed, version, err := factory.Pin(ctx, actor, parameter, on, number, list)
		if err != nil {
			return err
		}
		fmt.Printf("Pin %s placed: %s on %s as a %s; policy version %s\n",
			placed.ID, parameter, on, placed.Direction, version.ID)
		return nil
	})
}

// policyCommand prints what is in force: every parameter, where its value came
// from, the pins that reached it, and what reads it at this milestone. It is the
// one place an owner sees that four of the eight are read by nothing yet.
func policyCommand(args []string) error {
	flags := flag.NewFlagSet("policy", flag.ContinueOnError)
	serviceName := flags.String("service", "", "read the service-scoped parameters of this service")
	areaName := flags.String("area", "", "read the area-scoped parameters of this area")
	gateRow := flags.String("gate", string(gate.MergeToMaster), "read the threshold of this gate row")
	stage := flags.String("stage", string(item.StageImplementation), "read the attempt bound of this stage")
	if err := flags.Parse(args); err != nil {
		return err
	}

	return withPool(func(ctx context.Context, pool *pgxpool.Pool) error {
		subjects := policy.Subjects{GateRow: *gateRow, Stage: item.Stage(*stage)}
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
		production, found, err := environment.ByName(ctx, pool, environment.ProductionName)
		if err != nil {
			return err
		}
		if found {
			subjects.EnvironmentID = production.ID
		}

		version, err := policy.InForce(ctx, pool)
		if err != nil {
			return err
		}
		fmt.Printf("Policy version %s in force: %s %s on %s by %s\n",
			version.ID, version.Action, version.Parameter, version.Subject, version.Actor.Name)

		effectives, err := policy.NewReader(pool).All(ctx, subjects)
		if err != nil {
			return err
		}
		printEffectives(os.Stdout, effectives)

		pins, err := pin.All(ctx, pool)
		if err != nil {
			return err
		}
		if len(pins) == 0 {
			fmt.Println("\nNo pins are placed.")
			return nil
		}
		fmt.Println("\nThe pins:")
		for _, p := range pins {
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
		if len(e.List) > 0 || e.Parameter == gatepolicy.PredicateCatalog {
			value = strings.Join(e.List, ", ")
			if value == "" {
				value = "(empty)"
			}
		}
		fmt.Fprintf(out, "  %s = %s (%s)", e.Parameter, value, e.Source)
		if e.Clamped {
			fmt.Fprint(out, ", clamped by a pin")
		}
		if e.HumanPinned {
			fmt.Fprint(out, ", a pin adds a human")
		}
		if e.ReadBy == "" {
			fmt.Fprint(out, "; read by nothing at this milestone")
		} else {
			fmt.Fprintf(out, "; read by %s", e.ReadBy)
		}
		fmt.Fprintln(out)
	}
}

// pinSubject reads a subject written as kind:name and resolves the name to the
// record's id where the kind names a record. The factory policy record takes no
// name, being the one there is.
func pinSubject(ctx context.Context, pool *pgxpool.Pool, written string) (pin.Subject, error) {
	kind, name, found := strings.Cut(written, ":")
	if !found {
		return pin.Subject{}, fmt.Errorf("factory pin: a subject is written kind:name, not %q", written)
	}
	switch pin.SubjectKind(kind) {
	case pin.SubjectService:
		svc, err := namedService(ctx, pool, name)
		if err != nil {
			return pin.Subject{}, err
		}
		return pin.Subject{Kind: pin.SubjectService, ID: svc.ID}, nil
	case pin.SubjectArea:
		ar, err := namedArea(ctx, pool, name)
		if err != nil {
			return pin.Subject{}, err
		}
		return pin.Subject{Kind: pin.SubjectArea, ID: ar.ID}, nil
	case pin.SubjectGateRow:
		if _, err := gate.Actions(gate.Row(name)); err != nil {
			return pin.Subject{}, err
		}
		return pin.Subject{Kind: pin.SubjectGateRow, ID: name}, nil
	case pin.SubjectFactoryPolicy:
		// The record's own id, and not the word: there is one factory policy
		// record and it takes no name, but a mechanism reading pins on it reads
		// them by that id, so a pin naming anything else applies to nothing.
		policyRecord, err := factorypolicy.Get(ctx, pool)
		if err != nil {
			return pin.Subject{}, err
		}
		return pin.Subject{Kind: pin.SubjectFactoryPolicy, ID: policyRecord.ID}, nil
	default:
		return pin.Subject{}, fmt.Errorf("%w: %q", pin.ErrSubjectKindUnknown, kind)
	}
}

func namedService(ctx context.Context, pool *pgxpool.Pool, name string) (service.Service, error) {
	if name == "" {
		return service.Service{}, errors.New("factory: this parameter is a field of the service record, so -service is required")
	}
	svc, found, err := service.ByName(ctx, pool, name)
	if err != nil {
		return service.Service{}, err
	}
	if !found {
		return service.Service{}, fmt.Errorf("factory: no service is named %q", name)
	}
	return svc, nil
}

func namedArea(ctx context.Context, pool *pgxpool.Pool, name string) (area.Area, error) {
	if name == "" {
		return area.Area{}, errors.New("factory: this parameter is a field of the area record, so -area is required")
	}
	ar, found, err := area.ByName(ctx, pool, name)
	if err != nil {
		return area.Area{}, err
	}
	if !found {
		return area.Area{}, fmt.Errorf("factory: no area is named %q — declare it with `factory area %s`", name, name)
	}
	return ar, nil
}

func authored(parameter gatepolicy.Parameter, value string, version policy.Version) error {
	fmt.Printf("Authored %s = %s on %s; policy version %s\n", parameter, value, version.Subject, version.ID)
	return nil
}

// owner is the actor an authoring write is made as. Gate policy is authored by a
// human and package policy refuses anything else, so this is a human by
// construction and the name is whatever the owner typed.
func owner(name string) record.Actor {
	return record.Actor{Kind: record.KindHuman, Name: name}
}

// withPool opens the database, applies the schema, and runs one command against
// it. The schema is applied here for the reason the run applies it: these
// subcommands are the first thing an owner may reach on a fresh install, and a
// factory whose policy tables do not exist yet cannot be authored on.
//
// An error saying the factory has not been installed is answered with what to do
// about it. The two records an owner authors on are created by the run's first
// take, which is the one command that knows the targets production is reached at
// and the credential it is reached with; there is nothing to author on until
// then, and an error naming a missing version says that badly on its own.
func withPool(command func(context.Context, *pgxpool.Pool) error) error {
	ctx := context.Background()
	pool, err := postgres.Open(ctx, postgres.URL())
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := postgres.Apply(ctx, pool); err != nil {
		return err
	}
	err = command(ctx, pool)
	if errors.Is(err, policy.ErrNoVersion) || errors.Is(err, factorypolicy.ErrNotFound) {
		return fmt.Errorf("%w\nthe factory is not installed: the run's first take creates the factory policy record and production's environment, and there is nothing to author on until it has", err)
	}
	return err
}

// priorityCommand writes the priority an owner reorders a queue with. It is the
// design's settable order at every queue an item waits in as an item — the gates
// up to and including Merge to master, and the merge queue — so an owner who
// rushes an item to the front here has rushed it at every gate it has left, and
// has no way at all to reorder a deploy.
//
// It goes through dispatch and not beside it, the item having one writer after the
// cut. Work is the surface that will call this, and there is none yet.
func priorityCommand(args []string) error {
	flags := flag.NewFlagSet("priority", flag.ContinueOnError)
	priority := flags.Int("priority", 0, "the priority; a greater number goes first, and the cut writes nothing")
	human := flags.String("human", "owner", "the owner reordering the queue")

	// The item id is taken off the front before the flags are parsed, the way
	// `area <name>` is: it is what a person types first.
	id := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id, args = args[0], args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if id == "" || flags.NArg() != 0 {
		return errors.New("factory priority: one argument, the item's id, and then any flags")
	}

	return withPool(func(ctx context.Context, pool *pgxpool.Pool) error {
		owner := record.Actor{Kind: record.KindHuman, Name: *human}
		it, err := item.NewDispatch(pool).SetPriority(ctx, owner, id, *priority)
		if err != nil {
			return err
		}
		fmt.Printf("Item %s has priority %d, set by %s %s\n", it.ID, it.Priority, owner.Kind, owner.Name)
		fmt.Printf("It is at stage %s; the priority orders every queue it waits in as an item and no deploy\n", it.Stage)
		return nil
	})
}
