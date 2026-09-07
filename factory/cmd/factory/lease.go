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
// and a stop function, deferred by every caller, that ends the goroutine
// renewing the lease every leaseRenewEvery and then releases the lease, so the
// next subcommand starts rather than waiting out the ttl this one left behind.
func acquireLease(ctx context.Context, pool *pgxpool.Pool) (lease.Token, func(), error) {
	if err := postgres.ApplyLease(ctx, pool); err != nil {
		return 0, nil, err
	}
	token, err := lease.Acquire(ctx, pool, defaultInstance(), leaseTTL)
	if err != nil {
		if errors.Is(err, lease.ErrHeld) {
			return 0, nil, fmt.Errorf("another instance holds the lease: %w", err)
		}
		return 0, nil, err
	}
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(leaseRenewEvery)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				_ = lease.Renew(ctx, pool, token, leaseTTL)
			}
		}
	}()
	return token, func() {
		close(stop)
		_ = lease.Release(context.WithoutCancel(ctx), pool, token)
	}, nil
}
