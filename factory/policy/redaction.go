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
// appendErasure is what appends the erasure-list row this record names:
// [redaction.Insert] calls it before the record itself, keyed by
// [redaction.Key] over the same actor and the same writing, so the row lands
// first and the two cannot be keyed differently. It is the caller's, because
// the erasure list has one writer and it is the report store, which this
// package does not import — cmd/factory's own erasure hands it that store's
// AppendErasure. A stop between the row and the record leaves the row and no
// record, which is the event visibly owing rather than visibly done, and the
// row is what a restore replays.
//
// A step taken again finds the record the first one wrote, through
// [redaction.ByErasureKey], before appendErasure is even called, and appends
// neither a second record nor a second version: the erasure is one event with
// two steps, and the second is keyed.
//
// reaches is the caller's own reading of whether a legal hold reaches the
// target through the records it hangs from, which no package here can walk;
// package redaction reads the hold over the whole install itself. Either
// reading refuses the write, and this is where the refusal is recorded: the
// same call writes the version naming it and returns the error, so a caller
// need not remember [Factory.RecordRedactionRefusal] on top of this one.
func (f *Factory) WriteRedaction(ctx context.Context, actor record.Actor, writing redaction.Writing,
	reaches func(ctx context.Context) (bool, error), appendErasure redaction.ErasureAppender,
) (redaction.Redaction, Version, error) {
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
		version, err := f.recordRedactionRefusal(ctx, actor, writing,
			"a legal hold reaches the target of this erasure")
		if err != nil {
			return redaction.Redaction{}, Version{}, err
		}
		return redaction.Redaction{}, version,
			fmt.Errorf("%w: %s", redaction.ErrLegalHoldReaches, writing.Target)
	}

	id := record.NewID(redaction.IDPrefix)
	var performed redaction.Redaction
	version, err := f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionRedacted,
		scope:    Scope{Kind: string(writing.Target.Kind), ID: writing.Target.ID},
		keyExtra: key, minted: Created{RedactionID: id},
		apply: func(ctx context.Context, tx pgx.Tx) error {
			var err error
			performed, err = redaction.Insert(ctx, tx, f.token, actor, id, writing, appendErasure)
			return err
		},
	})
	return performed, version, err
}

// RecordRedactionRefusal records that an erasure was refused because a legal
// hold reaches one of its targets, which is what the design asks for where a
// hold stands against an erasure the owner owes: the words stay, and the
// refusal is on the record.
//
// [Factory.WriteRedaction] calls this itself where its own reading of the hold
// refuses the write, so a caller has it to make where its own, narrower reading
// refuses an erasure before any erasure-list row has landed — the case
// [Factory.WriteRedaction] cannot make this write for, because calling it would
// perform the redaction that row is meant to precede.
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
	if refusal == "" {
		return Version{}, fmt.Errorf("%w: %s", redaction.ErrReasonEmpty, writing.Target)
	}
	return f.recordRedactionRefusal(ctx, actor, writing, refusal)
}

// recordRedactionRefusal is the write [Factory.RecordRedactionRefusal] and
// [Factory.WriteRedaction]'s own held branch both make: the two differ only in
// where the reason comes from and whether the actor was checked already.
func (f *Factory) recordRedactionRefusal(ctx context.Context, actor record.Actor,
	writing redaction.Writing, refusal string) (Version, error) {
	if err := ownerOnly(actor); err != nil {
		return Version{}, err
	}
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionRedactionRefused,
		scope:    Scope{Kind: string(writing.Target.Kind), ID: writing.Target.ID},
		keyExtra: redaction.Key(actor, writing),
		refusal:  refusal,
	})
}
