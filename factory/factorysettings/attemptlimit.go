package factorysettings

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
)

// AttemptLimitSubject is what one attempt limit is authored for. It is one
// parameter and not three: the four stages that are retried, the interview's
// rounds, and decomposition's re-decompositions all count against the same limit,
// so the interview and decomposition are two more subjects here and not two more
// parameters.
type AttemptLimitSubject string

const (
	// SubjectSpec is the spec stage.
	SubjectSpec AttemptLimitSubject = "spec"
	// SubjectImplementationPlan is the implementation plan stage.
	SubjectImplementationPlan AttemptLimitSubject = "implementation_plan"
	// SubjectTasks is the tasks stage.
	SubjectTasks AttemptLimitSubject = "tasks"
	// SubjectImplementation is the implementation stage.
	SubjectImplementation AttemptLimitSubject = "implementation"
	// SubjectInterview is the interview's rounds, counted on the intent and
	// upstream of an item's first stage, the interview having no gate of its own.
	SubjectInterview AttemptLimitSubject = "interview"
	// SubjectDecomposition is decomposition's re-decompositions on a rejected set,
	// counted on the intent, decomposition's gate deciding a set rather than an
	// item.
	SubjectDecomposition AttemptLimitSubject = "decomposition"
)

// AttemptLimitSubjects is every subject an attempt limit may be authored for. The
// CHECK in [DDL] lists the same six, and TestDDLListsEveryAttemptLimitSubject
// fails if they stop agreeing.
var AttemptLimitSubjects = []AttemptLimitSubject{
	SubjectSpec, SubjectImplementationPlan, SubjectTasks, SubjectImplementation,
	SubjectInterview, SubjectDecomposition,
}

var (
	// ErrLimitNotPositive is returned by [SetAttemptLimit] for a limit that is
	// not above zero. A limit of zero would retry a stage no times and escalate
	// every item before an agent was asked anything.
	ErrLimitNotPositive = errors.New("factorysettings: an attempt limit is above zero")
	// ErrSubjectUnknown is returned by [SetAttemptLimit] for a subject outside
	// [AttemptLimitSubjects], and by [OfStage] for a stage that is not retried. A
	// limit authored where nothing counts attempts is a value nothing will ever
	// read.
	ErrSubjectUnknown = errors.New("factorysettings: the limit names something no attempt is counted at")
)

// OfStage is the attempt limit's subject for one stage of an item, and
// [ErrSubjectUnknown] for a stage that is not retried: nothing is dispatched to
// queued or merged, so no attempt is counted there and no limit is read.
func OfStage(stage item.Stage) (AttemptLimitSubject, error) {
	subject := AttemptLimitSubject(stage)
	if !slices.Contains(AttemptLimitSubjects, subject) {
		return "", fmt.Errorf("%w: %q", ErrSubjectUnknown, stage)
	}
	return subject, nil
}

// SetAttemptLimit writes the limit an owner authored for one subject, inside tx.
// Its one caller is package policy, which calls it inside the transaction that
// appends the policy version.
func SetAttemptLimit(ctx context.Context, tx pgx.Tx, actor record.Actor, settingsID string,
	subject AttemptLimitSubject, limit int) error {
	if !slices.Contains(AttemptLimitSubjects, subject) {
		return fmt.Errorf("%w: %q", ErrSubjectUnknown, subject)
	}
	if limit <= 0 {
		return fmt.Errorf("%w: %d", ErrLimitNotPositive, limit)
	}
	return insertKeyed(ctx, tx, actor, LimitTable, LimitIDPrefix, FormatVersionLimit,
		`factory_settings_id, subject, attempt_limit`, `$7, $8, $9`,
		`factory_settings_id, subject`, `attempt_limit = excluded.attempt_limit`,
		settingsID, string(subject), limit)
}

// AttemptLimit is the limit an owner authored for one subject, and absent where
// they authored none — where the value in force is what the score supplies.
func AttemptLimit(ctx context.Context, pool *pgxpool.Pool, settingsID string,
	subject AttemptLimitSubject) (gatepolicy.Authored, error) {
	return keyedValue(ctx, pool, LimitTable, "attempt_limit", "subject", settingsID, string(subject))
}
