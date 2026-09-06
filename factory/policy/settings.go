package policy

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
)

// ErrShorteningIsDecided is returned by [Factory.AuthorDecisionLogRetention]
// for a value shorter than the one in force. Shortening decision-log retention
// is decided rather than written: it takes a human at a gate row of its own,
// routed to a human other than the one who authored the shorter value, and the
// shorter value is not in force until that row approves it —
// [Factory.WriteRetentionShortening] writes the value pending and
// [Factory.ApproveRetentionShortening] is what the row's approval calls.
var ErrShorteningIsDecided = errors.New("policy: shortening decision-log retention is decided at a gate row")

// ErrNotAShortening is returned by [Factory.ApproveRetentionShortening] for a
// value that is not shorter than the one in force. Lengthening adds protection
// and is in force on write, so it does not come through the row.
var ErrNotAShortening = errors.New("policy: this value is not a shortening, and is in force on write")

// AuthorDecisionLogRetention authors how long the decision log is kept.
// Lengthening it is in force on write. Shortening it is refused here with
// [ErrShorteningIsDecided]: the protection a shorter value destroys is the
// evidence itself, so the write takes a gate row of its own.
//
// The first authored value is a shortening too. Where an owner has authored
// none the log is kept for the life of the install, so any finite value is
// shorter than what is in force and takes the row — a factory whose first
// authoring escaped it would cut the log on one owner's write, which is the one
// thing that row exists to stop.
//
// Neither an authored value nor a safeguard may take it under the retention
// floor, which package factorysettings refuses in the same transaction.
func (f *Factory) AuthorDecisionLogRetention(ctx context.Context, actor record.Actor,
	seconds int64) (Version, error) {
	settings, err := factorysettings.Get(ctx, f.pool)
	if err != nil {
		return Version{}, err
	}
	held := settings.DecisionLogRetentionSeconds
	if !held.Present {
		return Version{}, fmt.Errorf("%w: %d against the life of the install", ErrShorteningIsDecided, seconds)
	}
	if float64(seconds) < held.Number {
		return Version{}, fmt.Errorf("%w: %d under the %v in force", ErrShorteningIsDecided, seconds, held.Number)
	}
	return f.setDecisionLogRetention(ctx, actor, settings.ID, seconds, "")
}

// WriteRetentionShortening writes a shorter decision-log retention value
// pending, which is what the gate row that decides a shortening then decides.
// The value is not in force until [Factory.ApproveRetentionShortening]: what is
// written here is a record of its own naming who authored the value, so the row
// can be routed away from them — a row that decided a value no record carried
// an author for could only be closed by whoever proposed it.
//
// A value that is not shorter than the one in force is refused with
// [ErrNotAShortening]: lengthening adds protection and is in force on write,
// through [Factory.AuthorDecisionLogRetention]. Where nothing is authored the
// log is kept for the life of the install, so every finite value is shorter and
// the first authoring comes through here.
func (f *Factory) WriteRetentionShortening(ctx context.Context, actor record.Actor,
	seconds int64) (factorysettings.Shortening, Version, error) {
	settings, err := factorysettings.Get(ctx, f.pool)
	if err != nil {
		return factorysettings.Shortening{}, Version{}, err
	}
	held := settings.DecisionLogRetentionSeconds
	if held.Present && float64(seconds) >= held.Number {
		return factorysettings.Shortening{}, Version{},
			fmt.Errorf("%w: %d against the %v in force", ErrNotAShortening, seconds, held.Number)
	}
	var written factorysettings.Shortening
	version, err := f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionShorteningWritten,
		parameter: gatepolicy.DecisionLogRetention,
		scope:     Scope{Kind: ScopeFactorySettings, ID: settings.ID},
		number:    float64(seconds),
		mint: func(ctx context.Context, tx pgx.Tx) (Created, error) {
			written, err = factorysettings.InsertShortening(ctx, tx, f.token, actor, seconds)
			if err != nil {
				return Created{}, err
			}
			return Created{ShorteningID: written.ID}, nil
		},
	})
	return written, version, err
}

// ApproveRetentionShortening puts one pending shortening in force. Its caller is
// the close of the gate row that decides it, the row naming each author whose
// per-author prior the cut would remove; this package does not fire it.
// decision is that close event, required with [ErrNotDecidedAtARow], and
// shorteningID is the record the row decided, which is where the value comes
// from: the approval carries no value of its own, so what is put in force is
// what the row was fired over and not what the approving call says.
//
// A shortening whose value is no longer shorter than the one in force is
// refused, which is the state a lengthening between the two writes leaves.
func (f *Factory) ApproveRetentionShortening(ctx context.Context, actor record.Actor,
	shorteningID, decision string) (Version, error) {
	if decision == "" {
		return Version{}, fmt.Errorf("%w: the shortening %s", ErrNotDecidedAtARow, shorteningID)
	}
	proposed, err := factorysettings.GetShortening(ctx, f.pool, shorteningID)
	if err != nil {
		return Version{}, err
	}
	settings, err := factorysettings.Get(ctx, f.pool)
	if err != nil {
		return Version{}, err
	}
	held := settings.DecisionLogRetentionSeconds
	if held.Present && float64(proposed.Seconds) >= held.Number {
		return Version{}, fmt.Errorf("%w: %d against the %v in force",
			ErrNotAShortening, proposed.Seconds, held.Number)
	}
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionAuthored,
		parameter: gatepolicy.DecisionLogRetention,
		scope:     Scope{Kind: ScopeFactorySettings, ID: settings.ID},
		number:    float64(proposed.Seconds), authored: true,
		shortening: shorteningID, decision: decision,
		apply: func(ctx context.Context, tx pgx.Tx) error {
			if err := factorysettings.ApproveShortening(ctx, tx, f.token, shorteningID); err != nil {
				return err
			}
			return factorysettings.SetDecisionLogRetention(ctx, tx, settings.ID, proposed.Seconds)
		},
	})
}

func (f *Factory) setDecisionLogRetention(ctx context.Context, actor record.Actor,
	settingsID string, seconds int64, decision string) (Version, error) {
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionAuthored,
		parameter: gatepolicy.DecisionLogRetention,
		scope:     Scope{Kind: ScopeFactorySettings, ID: settingsID},
		number:    float64(seconds), authored: true, decision: decision,
		apply: func(ctx context.Context, tx pgx.Tx) error {
			return factorysettings.SetDecisionLogRetention(ctx, tx, settingsID, seconds)
		},
	})
}

// AuthorReportRetention authors how long the report store keeps a report.
func (f *Factory) AuthorReportRetention(ctx context.Context, actor record.Actor, seconds int64) (Version, error) {
	return f.authorOnSettings(ctx, actor, gatepolicy.ReportRetention, "", float64(seconds),
		func(ctx context.Context, tx pgx.Tx, settingsID string) error {
			return factorysettings.SetReportRetention(ctx, tx, settingsID, seconds)
		})
}

// AuthorBackupRetention authors how far back a backup may reach, authored
// outright with nothing supplied.
func (f *Factory) AuthorBackupRetention(ctx context.Context, actor record.Actor, seconds int64) (Version, error) {
	return f.authorOnSettings(ctx, actor, gatepolicy.BackupRetention, "", float64(seconds),
		func(ctx context.Context, tx pgx.Tx, settingsID string) error {
			return factorysettings.SetBackupRetention(ctx, tx, settingsID, seconds)
		})
}

// SetRetentionFloor writes how low an authored value or a safeguard may ever
// take decision-log retention. It has two callers and no third: the gate row
// that decides a shortening, and intake on the arrival of a records-retention
// constraint. Neither is built, and this is the write each will make.
func (f *Factory) SetRetentionFloor(ctx context.Context, actor record.Actor, seconds int64) (Version, error) {
	return f.authorOnSettings(ctx, actor, gatepolicy.RetentionFloor, "", float64(seconds),
		func(ctx context.Context, tx pgx.Tx, settingsID string) error {
			return factorysettings.SetRetentionFloor(ctx, tx, settingsID, seconds)
		})
}

// AuthorRemediationPeriod authors how long a matching advisory of one severity
// may stand before the intent it raised pages, authored outright with nothing
// supplied.
func (f *Factory) AuthorRemediationPeriod(ctx context.Context, actor record.Actor,
	severity float64, seconds int64) (Version, error) {
	return f.authorOnSettings(ctx, actor, gatepolicy.RemediationPeriod,
		fmt.Sprintf("severity %v", severity), float64(seconds),
		func(ctx context.Context, tx pgx.Tx, settingsID string) error {
			return factorysettings.SetRemediationPeriod(ctx, tx, actor, settingsID, severity, seconds)
		})
}

// AuthorReportChannelRate authors what bounds arrival at the way in
// factory-wide, and unauthored is unbounded.
func (f *Factory) AuthorReportChannelRate(ctx context.Context, actor record.Actor, rate int64) (Version, error) {
	return f.authorOnSettings(ctx, actor, gatepolicy.ReportChannelRate, "", float64(rate),
		func(ctx context.Context, tx pgx.Tx, settingsID string) error {
			return factorysettings.SetReportChannelRate(ctx, tx, settingsID, rate)
		})
}

// AuthorServiceReportChannelRate authors the same bound for one service, which
// is a field of the factory-wide settings record keyed by the service.
func (f *Factory) AuthorServiceReportChannelRate(ctx context.Context, actor record.Actor,
	serviceID string, rate int64) (Version, error) {
	return f.authorOnSettings(ctx, actor, gatepolicy.ReportChannelRate, serviceID, float64(rate),
		func(ctx context.Context, tx pgx.Tx, settingsID string) error {
			return factorysettings.SetServiceReportChannelRate(ctx, tx, actor, settingsID, serviceID, rate)
		})
}

// AuthorHarmMarkPageCap authors how many intents one service's marked reports
// may page per interval.
func (f *Factory) AuthorHarmMarkPageCap(ctx context.Context, actor record.Actor,
	serviceID string, pageCap int, intervalSeconds int64) (Version, error) {
	return f.authorOnSettings(ctx, actor, gatepolicy.HarmMarkPageCap, serviceID, float64(pageCap),
		func(ctx context.Context, tx pgx.Tx, settingsID string) error {
			return factorysettings.SetHarmMarkPageCap(ctx, tx, actor, settingsID, serviceID, pageCap, intervalSeconds)
		})
}

// SetHarmMarkPages writes whether a report marked as describing harm to a
// person pages at all. It ships on, so an owner who will not be woken by a
// stranger turns it off.
func (f *Factory) SetHarmMarkPages(ctx context.Context, actor record.Actor, pages bool) (Version, error) {
	return f.authorOnSettings(ctx, actor, gatepolicy.HarmMarkPageCap, "pages", boolValue(pages),
		func(ctx context.Context, tx pgx.Tx, settingsID string) error {
			return factorysettings.SetHarmMarkPages(ctx, tx, settingsID, pages)
		})
}

// SetSeam5Enforced turns enforcement of seam 5 on. An owner turns it on once
// and nothing turns it off again, which package factorysettings refuses.
func (f *Factory) SetSeam5Enforced(ctx context.Context, actor record.Actor) (Version, error) {
	settings, err := factorysettings.Get(ctx, f.pool)
	if err != nil {
		return Version{}, err
	}
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionAuthored,
		scope:  Scope{Kind: ScopeFactorySettings, ID: settings.ID, Key: "seam_5_enforced"},
		number: 1,
		apply: func(ctx context.Context, tx pgx.Tx) error {
			return factorysettings.SetSeam5Enforced(ctx, tx, settings.ID, true)
		},
	})
}

func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
