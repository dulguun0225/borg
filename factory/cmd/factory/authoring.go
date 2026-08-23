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
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/safeguard"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/service"
)

// The four subcommands of duty 8 and duty 9. An owner authors a parameter,
// places a safeguard, withdraws one, and reads what is in force — which is the
// whole of what the design has an owner do with gate policy, on a terminal
// until the Factory screen replaces it.
//
// Every one of them writes as a human, because gate policy is authored by an
// owner and package policy refuses a component. -human names which.

// areaCommand declares an area, which is what a safeguard or an item-size target
// is drawn on. It is duty 9's other write: an owner declares the groupings the
// rest of the factory is scoped against.
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
// gate row, a limit on the factory-wide settings record for one stage, a target on an
// area, and the window's four on a service.
func authorCommand(args []string) error {
	flags := flag.NewFlagSet("author", flag.ContinueOnError)
	name := flags.String("parameter", "", "the parameter to author (required); factory policy lists them")
	value := flags.String("value", "", "the number to author, or a comma-separated list for the list of allowed predicate kinds")
	serviceName := flags.String("service", "", "the service, for a parameter that is a field of one")
	areaName := flags.String("area", "", "the area, for a parameter that is a field of one")
	gateRow := flags.String("gate", string(gate.MergeToMaster), "the gate row a threshold applies at, or role_prompt_or_skill for the factory's own row")
	stage := flags.String("stage", string(item.StageImplementation), "the stage an attempt limit applies to")
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
			if *gateRow == "role_prompt_or_skill" {
				version, err = factory.AuthorRolePromptOrSkillThreshold(ctx, actor, number)
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
		case gatepolicy.AttemptLimit:
			version, err = factory.AuthorAttemptLimit(ctx, actor, item.Stage(*stage), int(number))
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

// safeguardCommand places a safeguard or withdraws one. The direction is not a
// flag: it differs per parameter and points the same way in each, so an owner
// chooses the subject and the bound and never which way the bound points.
func safeguardCommand(args []string) error {
	flags := flag.NewFlagSet("safeguard", flag.ContinueOnError)
	name := flags.String("parameter", "", "the parameter to bind")
	subject := flags.String("subject", "", "what the safeguard is drawn on, as kind:name — service:x, area:y, gate_row:merge_to_master, factory_settings:, contract_element:<service>/<contract>/<element>")
	bound := flags.String("bound", "", "the number the safeguard bounds by, a comma-separated list for the list of allowed predicate kinds, or kind[=argument] for a safeguard's predicate; a safeguard on the risk threshold takes none")
	withdraw := flags.String("withdraw", "", "the id of a safeguard to withdraw instead of placing one")
	human := flags.String("human", "owner", "the owner placing it")
	if err := flags.Parse(args); err != nil {
		return err
	}

	return withPool(func(ctx context.Context, pool *pgxpool.Pool) error {
		factory := policy.NewFactory(pool)
		actor := owner(*human)

		if *withdraw != "" {
			version, err := factory.WithdrawSafeguard(ctx, actor, *withdraw)
			if err != nil {
				return err
			}
			fmt.Printf("Safeguard %s withdrawn; policy version %s\n", *withdraw, version.ID)
			return nil
		}
		if *name == "" || *subject == "" {
			return errors.New("factory safeguard: -parameter and -subject are required, or -withdraw <safeguard-id>")
		}

		parameter := gatepolicy.Parameter(*name)
		definition, err := gatepolicy.Define(parameter)
		if err != nil {
			return err
		}
		on, err := safeguardSubject(ctx, pool, *subject)
		if err != nil {
			return err
		}

		var of safeguard.Bound
		switch {
		case definition.Direction == gatepolicy.DirectionAddsAHuman:
			// A safeguard on the risk threshold adds a human and bounds no value.
		case definition.Kind == gatepolicy.KindList:
			of.List = strings.Split(*bound, ",")
		case definition.Kind == gatepolicy.KindPredicate:
			kind, argument, _ := strings.Cut(*bound, "=")
			of.Predicate = safeguard.Predicate{
				Kind: gatepolicy.PredicateKind(strings.TrimSpace(kind)), Argument: strings.TrimSpace(argument),
			}
		default:
			of.Number, err = strconv.ParseFloat(*bound, 64)
			if err != nil {
				return fmt.Errorf("factory safeguard: a safeguard on %s takes a number as its bound, not %q", parameter, *bound)
			}
		}

		placed, version, err := factory.AddSafeguard(ctx, actor, parameter, on, of)
		if err != nil {
			return err
		}
		fmt.Printf("Safeguard %s placed: %s on %s as a %s; policy version %s\n",
			placed.ID, parameter, on, placed.Direction, version.ID)
		return nil
	})
}

// policyCommand prints what is in force: every parameter, where its value came
// from, the safeguards that reached it, and what reads it at this milestone. It
// is the one place an owner sees that four of the eight are read by nothing yet.
func policyCommand(args []string) error {
	flags := flag.NewFlagSet("policy", flag.ContinueOnError)
	serviceName := flags.String("service", "", "read the service-scoped parameters of this service")
	areaName := flags.String("area", "", "read the area-scoped parameters of this area")
	gateRow := flags.String("gate", string(gate.MergeToMaster), "read the threshold of this gate row")
	stage := flags.String("stage", string(item.StageImplementation), "read the attempt limit of this stage")
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

		// The newest score version and not an ensured one: this command prints what
		// is in force and authors nothing, and an ensure here would have a read
		// append a record.
		scoreVersion, found, err := score.Newest(ctx, pool)
		if err != nil {
			return err
		}
		if found {
			fmt.Printf("Score version %s in force: formula %s\n", scoreVersion.ID, scoreVersion.FormulaVersion)
		} else {
			fmt.Println("No score version has been appended, so every supplied value is where the formula was calibrated")
		}

		effectives, err := policy.NewReader(pool, scoreVersion).All(ctx, subjects)
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

// safeguardSubject reads a subject written as kind:name and resolves the name to the
// record's id where the kind names a record. The factory-wide settings record takes no
// name, being the one there is.
func safeguardSubject(ctx context.Context, pool *pgxpool.Pool, written string) (safeguard.Subject, error) {
	kind, name, found := strings.Cut(written, ":")
	if !found {
		return safeguard.Subject{}, fmt.Errorf("factory safeguard: a subject is written kind:name, not %q", written)
	}
	switch safeguard.SubjectKind(kind) {
	case safeguard.SubjectService:
		svc, err := namedService(ctx, pool, name)
		if err != nil {
			return safeguard.Subject{}, err
		}
		return safeguard.Subject{Kind: safeguard.SubjectService, ID: svc.ID}, nil
	case safeguard.SubjectArea:
		ar, err := namedArea(ctx, pool, name)
		if err != nil {
			return safeguard.Subject{}, err
		}
		return safeguard.Subject{Kind: safeguard.SubjectArea, ID: ar.ID}, nil
	case safeguard.SubjectGateRow:
		if _, err := gate.Actions(gate.Row(name)); err != nil {
			return safeguard.Subject{}, err
		}
		return safeguard.Subject{Kind: safeguard.SubjectGateRow, ID: name}, nil
	case safeguard.SubjectContractElement:
		// A contract element is named by its service, its contract, and the element —
		// three names an owner has — and stored as the contract's id and the element,
		// which is what outlives a version. Resolving it here rather than storing the
		// three names is what makes a safeguard follow the contract when the producer
		// publishes a new version of it.
		parts := strings.Split(name, "/")
		if len(parts) != 3 {
			return safeguard.Subject{}, fmt.Errorf(
				"factory safeguard: a contract element is written <service>/<contract>/<element>, not %q", name)
		}
		svc, err := namedService(ctx, pool, parts[0])
		if err != nil {
			return safeguard.Subject{}, err
		}
		con, found, err := contract.ByName(ctx, pool, svc.ID, parts[1])
		if err != nil {
			return safeguard.Subject{}, err
		}
		if !found {
			return safeguard.Subject{}, fmt.Errorf(
				"factory safeguard: %s publishes no contract named %q, and a contract exists from the merge that first published it",
				parts[0], parts[1])
		}
		return safeguard.Subject{Kind: safeguard.SubjectContractElement, ID: contract.ElementSubject(con.ID, parts[2])}, nil
	case safeguard.SubjectFactorySettings:
		// The record's own id, and not the word: there is one factory-wide settings
		// record and it takes no name, but a mechanism reading safeguards on it reads
		// them by that id, so a safeguard naming anything else applies to nothing.
		settings, err := factorysettings.Get(ctx, pool)
		if err != nil {
			return safeguard.Subject{}, err
		}
		return safeguard.Subject{Kind: safeguard.SubjectFactorySettings, ID: settings.ID}, nil
	default:
		return safeguard.Subject{}, fmt.Errorf("%w: %q", safeguard.ErrSubjectKindUnknown, kind)
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
	if errors.Is(err, policy.ErrNoVersion) || errors.Is(err, factorysettings.ErrNotFound) {
		return fmt.Errorf("%w\nthe factory is not installed: the run's first take creates the factory-wide settings record and production's environment, and there is nothing to author on until it has", err)
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
// decomposition. Work is the screen that will call this, and there is none yet.
func priorityCommand(args []string) error {
	flags := flag.NewFlagSet("priority", flag.ContinueOnError)
	priority := flags.Int("priority", 0, "the priority; a greater number goes first, and decomposition writes nothing")
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
