package policy

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/redaction"
)

// WriteRedaction writes the redaction one erasure performs: the record naming
// the target, the reason and the spans that went, with the policy version
// appended in the same transaction, the way a legal hold's write is made.
//
// The erasure-list row this record names has already been appended when this
// is called — the row lands first, and redaction.Key over the same actor and
// the same writing is what it was keyed under. So a step taken again finds
// the record the first one wrote, through [redaction.ByErasureKey], and
// appends neither a second record nor a second version: the erasure is one
// event with two steps, and the second is keyed.
//
// reaches is the caller's own reading of whether a legal hold reaches the
// target through the records it hangs from, which no package here can walk;
// package redaction reads the hold over the whole install itself. Either
// reading refuses the write, and nothing is recorded where it does.
func (f *Factory) WriteRedaction(ctx context.Context, actor record.Actor, writing redaction.Writing,
	reaches func(ctx context.Context) (bool, error)) (redaction.Redaction, Version, error) {
	if err := ownerOnly(actor); err != nil {
		return redaction.Redaction{}, Version{}, err
	}
	key := redaction.Key(actor, writing)
	written, found, err := redaction.ByErasureKey(ctx, f.pool, key)
	if err != nil {
		return redaction.Redaction{}, Version{}, err
	}
	if found {
		version, err := f.newest(ctx, actor)
		return written, version, err
	}

	held, err := redaction.Reaching(ctx, f.pool, reaches)
	if err != nil {
		return redaction.Redaction{}, Version{}, err
	}
	if held {
		return redaction.Redaction{}, Version{},
			fmt.Errorf("%w: %s", redaction.ErrLegalHoldReaches, writing.Target)
	}

	var performed redaction.Redaction
	version, err := f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionRedacted,
		scope:    Scope{Kind: string(writing.Target.Kind), ID: writing.Target.ID},
		keyExtra: key,
		mint: func(ctx context.Context, tx pgx.Tx) (Created, error) {
			var err error
			performed, err = redaction.Insert(ctx, tx, f.token, actor, writing)
			if err != nil {
				return Created{}, err
			}
			return Created{RedactionID: performed.ID}, nil
		},
	})
	return performed, version, err
}

// RecordRedactionRefusal records that an erasure was refused because a legal
// hold reaches one of its targets, which is what the design asks for where a
// hold stands against an erasure the owner owes: the words stay, and the
// refusal is on the record.
//
// It is the one write here that performs nothing. No erasure-list row is
// appended and no redaction is written, so no restore is needed to undo it —
// the version names the target, the action, the actor and the instant, and
// [Version.Refusal] says which reading refused it.
//
// It is keyed by the erasure key of the writing it refused, so an owner who
// performs the same erasure again while the hold still stands is refused
// again and the log carries one row for it.
func (f *Factory) RecordRedactionRefusal(ctx context.Context, actor record.Actor,
	writing redaction.Writing, refusal string) (Version, error) {
	if err := ownerOnly(actor); err != nil {
		return Version{}, err
	}
	if refusal == "" {
		return Version{}, fmt.Errorf("%w: %s", redaction.ErrReasonEmpty, writing.Target)
	}
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionRedactionRefused,
		scope:    Scope{Kind: string(writing.Target.Kind), ID: writing.Target.ID},
		keyExtra: redaction.Key(actor, writing),
		refusal:  refusal,
	})
}
