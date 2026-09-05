package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/lastcheck"
)

// chainCheck is the second comparison: the log's chain against the head
// this store recorded last pass, extended and nothing else. A mismatch
// found here holds every service's production deploys, because the log
// reaches every decision.
func chainCheck(ctx context.Context, s stores, out io.Writer) error {
	head, mismatch, why, err := driftdetector.VerifyChain(ctx, s.own, s.factory)
	if err != nil {
		return err
	}
	writer := driftdetector.NewWriter(s.own)
	if mismatch {
		raised, err := writer.RaiseChainMismatch(ctx, why)
		if err != nil {
			return err
		}
		if raised != "" {
			fmt.Fprintf(out, "CHAIN MISMATCH %s — %s\n", raised, why)
			fmt.Fprintln(out, "  it holds every service's production deploys until a human clears it here, and the factory cannot")
		} else {
			fmt.Fprintf(out, "the chain still disagrees with the recorded head — %s\n", why)
		}
		return nil
	}
	if _, err := writer.RecordHead(ctx, head.Hash, head.Seq); err != nil {
		return err
	}
	fmt.Fprintf(out, "the log's chain still holds the head recorded last pass, extended to sequence %d\n", head.Seq)
	return nil
}

// staleCheck is the third comparison: the factory's own last check records,
// which is what makes a stopped factory component reach a human. The
// notifier's own staleness, and every factory last check stale at once, have
// no carrier inside the factory — the detector delivers to its own address
// itself, naming what it found, rather than raising a mismatch nothing
// inside the stopped process could ever answer. Every other stale component
// is folded into the same chain-shaped mismatch [chainCheck] raises,
// because this package does not carry a per-service mismatch for a last
// check the way it does for a target that disagrees — an open point this
// dispatch's report names.
func staleCheck(ctx context.Context, s stores, out io.Writer) error {
	all, err := lastcheck.All(ctx, s.factory)
	if err != nil {
		return err
	}
	if len(all) == 0 {
		return nil
	}
	stale, err := lastcheck.Stale(ctx, s.factory, time.Now())
	if err != nil {
		return err
	}
	if len(stale) == 0 {
		return nil
	}

	notifierStale := false
	var others []lastcheck.LastCheck
	for _, c := range stale {
		if c.Component == lastcheck.ComponentNotifier {
			notifierStale = true
		} else {
			others = append(others, c)
		}
	}
	everyStale := len(stale) == len(all)

	writer := driftdetector.NewWriter(s.own)
	if len(others) > 0 {
		why := fmt.Sprintf("%s's own last check is stale", others[0].Component)
		raised, err := writer.RaiseChainMismatch(ctx, why)
		if err != nil {
			return err
		}
		if raised != "" {
			fmt.Fprintf(out, "STALE COMPONENT %s — %s\n", raised, why)
		}
	}

	if notifierStale || everyStale {
		address, err := driftdetector.Address(ctx, s.own)
		if errors.Is(err, driftdetector.ErrNoAddress) {
			fmt.Fprintln(out, "the notifier's own last check is stale, and no address is set to deliver to — run install -address first")
			return nil
		} else if err != nil {
			return err
		}
		why := "the notifier's own last check is stale"
		if everyStale {
			why = "every factory last check is stale at once; the whole process has stopped"
		}
		if _, err := writer.Deliver(ctx, why); err != nil {
			return err
		}
		fmt.Fprintf(out, "DELIVERED to %s — %s\n", address, why)
	}
	return nil
}
