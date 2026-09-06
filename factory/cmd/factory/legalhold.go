package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/legalhold"
	"github.com/dulguun0225/borg/factory/policy"
)

// legalHoldCommand sets a legal hold, or writes a withdrawal of one. Both go
// through [policy.Factory], the record's one writer, so each appends a policy
// version. A withdrawal is written pending: the hold stands until the gate row
// A legal hold's withdrawal approves it, which is `factory approve
// -legal-hold-withdrawal`, decided by a human other than the one who wrote it.
func legalHoldCommand(args []string) error {
	flags := flag.NewFlagSet("legal-hold", flag.ContinueOnError)
	subject := flags.String("subject", "", "what the hold reaches, as kind:name — service:x, project:y, or factory: for the whole install")
	reason := flags.String("reason", "", "why the hold was set (required unless -withdraw)")
	withdraw := flags.String("withdraw", "", "the id of a legal hold to withdraw instead of setting one")
	human := flags.String("human", "owner", "the owner acting")
	if err := flags.Parse(args); err != nil {
		return err
	}

	return withPool(func(ctx context.Context, pool *pgxpool.Pool, token lease.Token) error {
		actor, err := humanNamed(ctx, pool, token, *human)
		if err != nil {
			return err
		}
		factory := policy.NewFactory(pool, token)
		if *withdraw != "" {
			wd, version, err := factory.WriteLegalHoldWithdrawal(ctx, actor, *withdraw)
			if err != nil {
				return err
			}
			fmt.Printf("Withdrawal %s of legal hold %s written, pending; policy version %s\n",
				wd.ID, *withdraw, version.ID)
			fmt.Printf("The hold stands until the row that decides it closes: `factory approve -legal-hold-withdrawal %s`\n", wd.ID)
			return nil
		}
		if *subject == "" || *reason == "" {
			return errors.New("factory legal-hold: -subject and -reason are required, or -withdraw <hold-id>")
		}
		on, err := legalHoldSubject(ctx, pool, *subject)
		if err != nil {
			return err
		}
		h, version, err := factory.SetLegalHold(ctx, actor, on, *reason)
		if err != nil {
			return err
		}
		fmt.Printf("Legal hold %s set on %s by %s %s: %s; policy version %s\n",
			h.ID, h.Subject, actor.Kind, *human, h.Reason, version.ID)
		fmt.Println("It is refused wherever it reaches: a hold on a project reaches the project's environment and every service in it")
		return nil
	})
}

// legalHoldSubject reads a subject written as kind:name and resolves the name
// to the record's id where the kind names one; [legalhold.SubjectFactory]
// names none.
func legalHoldSubject(ctx context.Context, pool *pgxpool.Pool, written string) (legalhold.Subject, error) {
	kind, name, _ := strings.Cut(written, ":")
	switch legalhold.SubjectKind(kind) {
	case legalhold.SubjectService:
		svc, err := namedService(ctx, pool, name)
		if err != nil {
			return legalhold.Subject{}, err
		}
		return legalhold.Subject{Kind: legalhold.SubjectService, ID: svc.ID}, nil
	case legalhold.SubjectProject:
		prj, err := namedProject(ctx, pool, name)
		if err != nil {
			return legalhold.Subject{}, err
		}
		return legalhold.Subject{Kind: legalhold.SubjectProject, ID: prj.ID}, nil
	case legalhold.SubjectFactory:
		return legalhold.Subject{Kind: legalhold.SubjectFactory}, nil
	default:
		return legalhold.Subject{}, fmt.Errorf("%w: %q", legalhold.ErrSubjectKindUnknown, kind)
	}
}
