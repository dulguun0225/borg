package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/legalhold"
)

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
