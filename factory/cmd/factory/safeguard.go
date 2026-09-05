package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/safeguard"
)

// -route-duty and -route-human are the safeguard's own routing, which the rows
// it puts a human at and the row that withdraws it are routed by. A safeguard
// naming neither routes to the owner, which is where every unheld row goes.

// safeguardCommand places a safeguard or withdraws one. The direction is not a
// flag: it differs per parameter and points the same way in each, so an owner
// chooses the subject and the bound and never which way the bound points.
func safeguardCommand(args []string) error {
	flags := flag.NewFlagSet("safeguard", flag.ContinueOnError)
	name := flags.String("parameter", "", "the parameter to bind")
	subject := flags.String("subject", "", "what the safeguard is drawn on, as kind:name — stage:x, service:x, project:x, area:x, gate_row:merge_to_master, contract_element:<service>/<contract>/<element>, design_system_component:x, factory_settings:, report_store:, drift_detector_last_check:")
	serviceName := flags.String("service", "", "the service, for a safeguard on the risk threshold — a row-scoped safeguard is drawn on the service the row fires for")
	bound := flags.String("bound", "", "the number the safeguard bounds by, a comma-separated list for the list of allowed predicate kinds, or kind[=argument] for a safeguard's predicate; a safeguard on the risk threshold takes none")
	withdraw := flags.String("withdraw", "", "the id of a safeguard to withdraw instead of placing one")
	human := flags.String("human", "owner", "the owner placing it")
	routeDuty := flags.Int("route-duty", 0, "route this safeguard's rows to one of the owner's twelve duties")
	routeHuman := flags.String("route-human", "", "route this safeguard's rows to this human, by name")
	if err := flags.Parse(args); err != nil {
		return err
	}

	return withPool(func(ctx context.Context, pool *pgxpool.Pool, token lease.Token) error {
		factory := policy.NewFactory(pool, token)
		actor, err := humanNamed(ctx, pool, token, *human)
		if err != nil {
			return err
		}
		// The routing names a human by their per-person key and an owner types a
		// name, so the name is resolved through the People mapping the way -human
		// is — the same crossing, at the one place a safeguard names a person.
		routing := safeguard.Routing{Duty: *routeDuty}
		if *routeHuman != "" {
			routed, err := humanNamed(ctx, pool, token, *routeHuman)
			if err != nil {
				return err
			}
			routing.HumanKey = routed.Key
		}
		if err := routing.Validate(); err != nil {
			return err
		}

		// Withdrawing writes the withdrawal record and takes the safeguard out of
		// nothing: a safeguard leaves force at the gate row A safeguard's
		// withdrawal, decided by a human always and routed away from whoever wrote
		// it. `factory approve` is where that row fires.
		if *withdraw != "" {
			written, version, err := factory.WriteSafeguardWithdrawal(ctx, actor, *withdraw)
			if err != nil {
				return err
			}
			fmt.Printf("Withdrawal %s written for safeguard %s; policy version %s\n",
				written.ID, *withdraw, version.ID)
			fmt.Printf("The safeguard stands until the row that decides it closes: `factory approve -safeguard-withdrawal %s`\n", written.ID)
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
		on, err := safeguardSubject(ctx, pool, *subject, *serviceName)
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

		placed, version, err := factory.AddSafeguard(ctx, actor, parameter, on, of, routing)
		if err != nil {
			return err
		}
		fmt.Printf("Safeguard %s placed: %s on %s as a %s; policy version %s\n",
			placed.ID, parameter, on, placed.Direction, version.ID)
		return nil
	})
}

// safeguardSubject reads a subject written as kind:name and resolves the name
// to the record's id where the kind names one. The nine kinds package
// safeguard defines are the "reaching" subjects the design names; "gate_row"
// is not a tenth kind but this command's own shorthand for the risk
// threshold's subject — package policy's own [Reader] reads a row-scoped
// safeguard drawn on the service the row fires for, keyed by the row (its
// effective.go: "a row-scoped safeguard needs a service to be keyed on"), so a
// safeguard on a gate row is drawn on -service with the row as
// [safeguard.Subject.Key], and refuses where -service names none.
func safeguardSubject(ctx context.Context, pool *pgxpool.Pool, written, serviceName string) (safeguard.Subject, error) {
	kind, name, found := strings.Cut(written, ":")
	if !found {
		return safeguard.Subject{}, fmt.Errorf("factory safeguard: a subject is written kind:name, not %q", written)
	}
	if kind == "gate_row" {
		row, err := gate.RowFrom(name)
		if err != nil {
			return safeguard.Subject{}, err
		}
		if _, err := gate.Actions(row); err != nil {
			return safeguard.Subject{}, err
		}
		svc, err := namedService(ctx, pool, serviceName)
		if err != nil {
			return safeguard.Subject{}, err
		}
		return safeguard.Subject{Kind: safeguard.SubjectService, ID: svc.ID, Key: name}, nil
	}
	switch safeguard.SubjectKind(kind) {
	case safeguard.SubjectStage:
		// A stage names no record of its own; the design reaches the gate row
		// that decides a role prompt version for it by the name alone.
		if name == "" {
			return safeguard.Subject{}, fmt.Errorf("factory safeguard: a stage subject names the stage, not %q", written)
		}
		return safeguard.Subject{Kind: safeguard.SubjectStage, ID: name}, nil
	case safeguard.SubjectService:
		svc, err := namedService(ctx, pool, name)
		if err != nil {
			return safeguard.Subject{}, err
		}
		return safeguard.Subject{Kind: safeguard.SubjectService, ID: svc.ID}, nil
	case safeguard.SubjectProject:
		prj, err := namedProject(ctx, pool, name)
		if err != nil {
			return safeguard.Subject{}, err
		}
		return safeguard.Subject{Kind: safeguard.SubjectProject, ID: prj.ID}, nil
	case safeguard.SubjectArea:
		ar, err := namedArea(ctx, pool, name)
		if err != nil {
			return safeguard.Subject{}, err
		}
		return safeguard.Subject{Kind: safeguard.SubjectArea, ID: ar.ID}, nil
	case safeguard.SubjectDesignSystemComponent:
		// Nothing derives the design system's own components yet — package
		// safeguard's doc.go says this kind's value is stored and read by
		// nothing at this milestone — so the name is stored as given.
		if name == "" {
			return safeguard.Subject{}, fmt.Errorf("factory safeguard: a design-system-component subject names the component, not %q", written)
		}
		return safeguard.Subject{Kind: safeguard.SubjectDesignSystemComponent, ID: name}, nil
	case safeguard.SubjectReportStore:
		// The report store is not built and takes no name, the way the
		// factory-wide settings record's own subject below does.
		return safeguard.Subject{Kind: safeguard.SubjectReportStore}, nil
	case safeguard.SubjectDriftDetectorLastCheck:
		// The drift detector's store is outside this module and nothing
		// derives a safeguard reading it yet; the name, where given, is a
		// target address or a service id and is stored as given.
		return safeguard.Subject{Kind: safeguard.SubjectDriftDetectorLastCheck, ID: name}, nil
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
	case safeguard.SubjectPredicateKindsList:
		// The record's own id, and not the word: there is one factory-wide settings
		// record and it takes no name, but a mechanism reading safeguards on it reads
		// them by that id, so a safeguard naming anything else applies to nothing.
		settings, err := factorysettings.Get(ctx, pool)
		if err != nil {
			return safeguard.Subject{}, err
		}
		return safeguard.Subject{Kind: safeguard.SubjectPredicateKindsList, ID: settings.ID}, nil
	default:
		return safeguard.Subject{}, fmt.Errorf("%w: %q", safeguard.ErrSubjectKindUnknown, kind)
	}
}
