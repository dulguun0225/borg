package decisionlog

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// Reader is every way of reading the log. It takes the pool and a fencing
// token rather than a [Writer], because reading the log is not a reason to
// be handed the thing that appends to it — but a read event is itself an
// append, the log's tenth shape, so a reader carries the same token a writer
// does.
//
// Every method appends exactly one read event before it answers, naming
// principal as the actor and what was asked for as the payload: "the log
// itself is read, or ... stored report text a redaction could reach is
// read." A caller wanting the log's own read, and not a report's, always
// passes through here.
type Reader struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewReader returns a reader over pool, appending its read events with
// token.
func NewReader(pool *pgxpool.Pool, token lease.Token) *Reader {
	return &Reader{pool: pool, token: token}
}

// appendReadEvent is the one read event every method below appends.
func (r *Reader) appendReadEvent(ctx context.Context, principal record.Actor, asked string) error {
	_, err := commitAppend(ctx, r.pool, r.token, ShapeReadEvent, "", Entry{
		Actor:         principal,
		Payload:       `{"read":"` + asked + `"}`,
		FormatVersion: "read_event/1",
	}, nil)
	return err
}

// Read is the whole log in row order, after appending a read event naming
// principal. It holds every row in memory, which is what a reader of a log
// this package never deletes from — [Writer.Truncate] aside — should know;
// [Reader.Verify] does not, and is what a caller wanting only the answer
// should use.
func (r *Reader) Read(ctx context.Context, principal record.Actor) ([]Row, error) {
	if err := r.appendReadEvent(ctx, principal, "whole log"); err != nil {
		return nil, err
	}
	return readAll(ctx, r.pool)
}

// Verify walks the log in row order and returns the first row that breaks
// the chain, as a [*BrokenError], or nil for a log that is whole, after
// appending a read event naming principal. An empty log is whole. See
// verify.go for what it does and does not catch.
func (r *Reader) Verify(ctx context.Context, principal record.Actor) error {
	if err := r.appendReadEvent(ctx, principal, "verify"); err != nil {
		return err
	}
	return verify(ctx, r.pool)
}

// ClosedDecisions is every decision both of whose rows are in the log, in
// the order the openings were appended, after appending a read event naming
// principal.
//
// It reads the whole log to pair the rows, so a caller asking per firing
// reads the log per firing. That is what an outcome count over one author's
// decisions costs while the log is small, and it is the honest place for the
// cost: a query narrowed by what the payload names would put the payload's
// shape — which is the gate's and carries the vector — inside the log.
func (r *Reader) ClosedDecisions(ctx context.Context, principal record.Actor) ([]Closed, error) {
	if err := r.appendReadEvent(ctx, principal, "closed decisions"); err != nil {
		return nil, err
	}
	rows, err := readAll(ctx, r.pool)
	if err != nil {
		return nil, err
	}
	return pairClosedDecisions(rows), nil
}

// Pending is every decision opening with neither a closing nor an
// abandonment — "a predicate over the log alone that needs no join against
// the item's stage" — after appending a read event naming principal.
func (r *Reader) Pending(ctx context.Context, principal record.Actor) ([]Row, error) {
	if err := r.appendReadEvent(ctx, principal, "pending decisions"); err != nil {
		return nil, err
	}
	rows, err := readAll(ctx, r.pool)
	if err != nil {
		return nil, err
	}
	ended := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.Shape == ShapeDecision && (row.Part == PartClose || row.Part == PartAbandonment) {
			ended[row.Closes] = true
		}
	}
	var pending []Row
	for _, row := range rows {
		if row.Shape == ShapeDecision && row.Part == PartOpen && !ended[row.ID] {
			pending = append(pending, row)
		}
	}
	return pending, nil
}

// ByShape is every row of one shape, in row order, after appending a read
// event naming principal and the shape asked for. It is how a caller that
// stores a record as a row of the log — the policy version and the score
// version are the two — reads its own rows back, rather than by a query of
// its own against the table.
//
// It reads the whole log and filters in memory, the way
// [Reader.ClosedDecisions] does, and for the same reason: a query narrowed by
// what a payload names would put that payload's shape inside this package.
func (r *Reader) ByShape(ctx context.Context, principal record.Actor, shape Shape) ([]Row, error) {
	if err := r.appendReadEvent(ctx, principal, string(shape)+" rows"); err != nil {
		return nil, err
	}
	rows, err := readAll(ctx, r.pool)
	if err != nil {
		return nil, err
	}
	var of []Row
	for _, row := range rows {
		if row.Shape == shape {
			of = append(of, row)
		}
	}
	return of, nil
}
