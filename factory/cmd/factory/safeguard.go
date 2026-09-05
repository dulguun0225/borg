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

	return withPool(func(ctx context.Context, pool *pgxpool.Pool, token lease.Token) error {
		factory := policy.NewFactory(pool, token)
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
