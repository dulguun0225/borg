package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/lastcheck"
	"github.com/dulguun0225/borg/factory/service"
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
// which is what makes a stopped factory component reach a human. A last check
// past the interval it names with a further pass owed is a mismatch of the shape
// the two above have, holding what the stopped component reaches — the health
// monitor's that service's production deploys, the deployer's that
// environment's, which is one row per service in it.
//
// The notifier's own staleness, and every factory last check stale at once, have
// no carrier inside the factory — a mismatch about the notifier would be
// delivered by the notifier — so for those two the detector delivers to its own
// address instead, naming what it found.
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
	for _, c := range others {
		// Every stale component is raised, not the first of them: a second one
		// behind the first would otherwise be invisible until the first is
		// cleared, and each holds what it reaches rather than what the others do.
		if err := raiseStale(ctx, s, writer, c, out); err != nil {
			return err
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

// raiseStale writes the mismatch one stale last check calls for, over the
// services its component's stopping holds. The health monitor keeps one record
// per service, so its subject is the service. The deployer keeps one per target
// of a persistent environment, so its subject is a target and what it holds is
// every service in the environment that names that target. A component whose
// record is per neither holds every service the factory has, which is the
// widest reading of "what the stopped component reaches" and the safe one.
func raiseStale(ctx context.Context, s stores, writer *driftdetector.Writer,
	c lastcheck.LastCheck, out io.Writer) error {
	why := fmt.Sprintf("%s's own last check is stale: it named an interval of %s and owes a further pass",
		c.Component, c.Interval)
	for _, held := range holdsWhat(ctx, s, c) {
		raised, err := writer.RaiseStaleComponent(ctx, driftdetector.StaleComponent{
			Component: c.Component, ServiceID: held.serviceID, Target: held.target, Why: why,
		})
		if err != nil {
			return err
		}
		if raised != "" {
			fmt.Fprintf(out, "STALE COMPONENT %s — %s, holding %s's production deploys\n",
				raised, why, held.serviceID)
		}
	}
	return nil
}

// held is one service a stopped component's mismatch holds, and the target where
// the record that stopped is kept per target.
type held struct {
	serviceID string
	target    string
}

func holdsWhat(ctx context.Context, s stores, c lastcheck.LastCheck) []held {
	if c.Component == lastcheck.ComponentHealthMonitor && c.Subject != "" {
		return []held{{serviceID: c.Subject}}
	}
	services, err := service.All(ctx, s.factory)
	if err != nil {
		return nil
	}
	var holding []held
	for _, svc := range services {
		if svc.Retired() {
			continue
		}
		if c.Component != lastcheck.ComponentDeployer || c.Subject == "" {
			holding = append(holding, held{serviceID: svc.ID})
			continue
		}
		production, found, err := environment.Production(ctx, s.factory, svc.ProjectID)
		if err != nil || !found {
			continue
		}
		// The service's own set and no other: a stopped deployer on a target
		// this service does not run on holds nothing of this service.
		for _, address := range runsOn(production, svc) {
			if address == c.Subject {
				holding = append(holding, held{serviceID: svc.ID, target: c.Subject})
				break
			}
		}
	}
	return holding
}
