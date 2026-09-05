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
)

// legalHoldCommand sets a legal hold, or withdraws one. Package policy does
// not import package legalhold yet, so this writes directly through
// [legalhold.Writer] rather than through [policy.Factory] — the same
// exception [haltCommand] makes and for the same reason: nothing here
// appends a policy version.
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
		if *withdraw != "" {
			wd, err := legalhold.NewWriter(pool, token).InsertWithdrawal(ctx, actor, *withdraw)
			if err != nil {
				return err
			}
			fmt.Printf("Withdrawal %s of legal hold %s written, pending approval\n", wd.ID, *withdraw)
			fmt.Println("The gate row this withdrawal decides is not built, so this stands pending until a human approves it; nothing here fires that row")
			return nil
		}
		if *subject == "" || *reason == "" {
			return errors.New("factory legal-hold: -subject and -reason are required, or -withdraw <hold-id>")
		}
		on, err := legalHoldSubject(ctx, pool, *subject)
		if err != nil {
			return err
		}
		h, err := legalhold.NewWriter(pool, token).Insert(ctx, actor, on, *reason)
		if err != nil {
			return err
		}
		fmt.Printf("Legal hold %s set on %s by %s %s: %s\n", h.ID, h.Subject, actor.Kind, *human, h.Reason)
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
