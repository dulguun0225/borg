package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
)

// The lease this process holds, per ../../../end-goal/one-process.md: exactly
// one instance of the factory runs, and what enforces it is the factory's own
// store. Every subcommand takes it before it touches the store and releases it
// when it returns; "serve" holds it for the life of the process, which is the
// difference between a pass and a process.

// leaseTTL is how long an acquired lease stands before it lapses, and
// leaseRenewEvery is how often the renewal goroutine renews it — a third of the
// ttl, so a delay of a couple of renewals still lands before the lease would
// lapse. Both are this interface's own choice: the design names the lease and
// the fencing token and leaves the numbers to whoever runs the process.
const (
	leaseTTL        = 30 * time.Second
	leaseRenewEvery = leaseTTL / 3
)

// defaultInstance is this process's own identity for the lease: the machine's
// hostname and this process's id, which tells one instance from another without
// any configuration.
func defaultInstance() string {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown-host"
	}
	return fmt.Sprintf("%s:%d", host, os.Getpid())
}

// acquireLease creates package lease's own table and takes the lease for this
// process, per ../../../end-goal/one-process.md: every subcommand reaches the
// store while it runs, whether it writes or only reads — a read appends a read
// event, which is itself a write of the log — so every subcommand acquires it
// before anything else touches the store. The lease's own table is the one
// thing created first, because a lease cannot be taken in a store whose lease
// table does not exist; every other table is created by [postgres.Start] after
// this returns. A held lease is a start failure.
//
// It returns the token every writer and every reader below this point carries,
// the channel that says the lease is gone, and a stop function, deferred by
// every caller, that ends the goroutine renewing the lease every
// leaseRenewEvery and then releases the lease, so the next subcommand starts
// rather than waiting out the ttl this one left behind. Releasing after the
// lease was taken releases nothing: [lease.Release] refuses a token that is no
// longer the lease's number.
//
// Exactly one instance runs, so a process whose lease is gone has to stop:
// another instance holds it, every write this one makes from then on is
// refused at the fence, and two processes advancing one path is what the lease
// exists to prevent. The channel carries one error and is buffered, so the
// renewal goroutine reports and returns whether or not anyone is reading. A
// subcommand that makes one pass and exits ignores it — it holds the lease for
// a pass and its next write is fenced anyway — and "serve", which holds the
// lease for the life of the process, ends on it.
func acquireLease(ctx context.Context, pool *pgxpool.Pool) (lease.Token, <-chan error, func(), error) {
	if err := postgres.ApplyLease(ctx, pool); err != nil {
		return 0, nil, nil, err
	}
	token, err := lease.Acquire(ctx, pool, defaultInstance(), leaseTTL)
	if err != nil {
		if errors.Is(err, lease.ErrHeld) {
			return 0, nil, nil, fmt.Errorf("another instance holds the lease: %w", err)
		}
		return 0, nil, nil, err
	}
	stop := make(chan struct{})
	lost := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(leaseRenewEvery)
		defer ticker.Stop()
		var failing renewals
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if gone := failing.after(lease.Renew(ctx, pool, token, leaseTTL), time.Now()); gone != nil {
					lost <- gone
					return
				}
			}
		}
	}()
	return token, lost, func() {
		close(stop)
		_ = lease.Release(context.WithoutCancel(ctx), pool, token)
	}, nil
}

// renewals is what the renewal goroutine keeps between two ticks: when the
// renewals started failing for a reason that is not the lease having been
// taken.
type renewals struct{ failingSince time.Time }

// after is what one renewal's result means for the process: nil where it may
// go on, and the error it stops on otherwise.
//
// A lease another instance holds stops it at once — every write this process
// makes from then on is refused at the fence, so going on would be work that
// cannot commit. Any other failure is the store unreachable or a query
// cancelled, and those are retried until leaseTTL has passed: the lease stands
// for that long whether or not a renewal lands, so a renewal landing inside it
// costs nothing, and past it the lease has lapsed and another instance may
// already have taken it.
func (r *renewals) after(err error, now time.Time) error {
	switch {
	case err == nil:
		r.failingSince = time.Time{}
	case errors.Is(err, lease.ErrFenced), errors.Is(err, lease.ErrNoLease):
		return fmt.Errorf("the lease this process holds was taken by another instance: %w", err)
	case r.failingSince.IsZero():
		r.failingSince = now
	case now.Sub(r.failingSince) >= leaseTTL:
		return fmt.Errorf("the lease could not be renewed for %v, which is longer than the %v it stands: %w",
			now.Sub(r.failingSince).Truncate(time.Second), leaseTTL, err)
	}
	return nil
}
