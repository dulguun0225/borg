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
	agreed, err := writer.RecordChainAgreement(ctx)
	if err != nil {
		return err
	}
	if agreed != "" {
		fmt.Fprintf(out, "the log's chain still holds the head recorded last pass, extended to sequence %d — a later agreement is recorded on %s as evidence\n",
			head.Seq, agreed)
		return nil
	}
	fmt.Fprintf(out, "the log's chain still holds the head recorded last pass, extended to sequence %d\n", head.Seq)
	return nil
}

// staleCheck is the third comparison: the factory's own last check records,
// which is what makes a stopped factory component reach a human. A last check
// past the interval it names with a further pass owed is a mismatch of the shape
// the two above have, holding what the stopped component reaches — the health
// monitor's that service's production deploys, the deployer's that
// environment's, which is one row per service in it. [driftdetector.Holds] is
// where what a stopped component holds is decided, over the last check and the
// services and their production targets this command assembles;
// [driftdetector.MustDeliver] is where the detector's own delivery is decided,
// over every last check and every stale one. A component whose last check
// answers fresh again does not clear the mismatch its earlier staleness
// raised — [recordFreshAgain] records the agreement on it as evidence, the
// way a later agreeing target pass already does.
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

	writer := driftdetector.NewWriter(s.own)
	staleComponents := make(map[string]bool, len(stale))
	for _, c := range stale {
		staleComponents[c.Component] = true
		if c.Component == lastcheck.ComponentNotifier {
			// The notifier's own staleness has no carrier inside the factory — a
			// mismatch about the notifier would be delivered by the notifier — so
			// it raises no mismatch here; [driftdetector.MustDeliver] below is
			// where the detector's own delivery answers for it.
			continue
		}
		// Every stale component is raised, not the first of them: a second one
		// behind the first would otherwise be invisible until the first is
		// cleared, and each holds what it reaches rather than what the others do.
		if err := raiseStale(ctx, s, writer, c, out); err != nil {
			return err
		}
	}
	if err := recordFreshAgain(ctx, s, writer, staleComponents, out); err != nil {
		return err
	}

	if len(stale) == 0 {
		return nil
	}
	why, deliver := driftdetector.MustDeliver(all, stale)
	if !deliver {
		return nil
	}
	address, err := driftdetector.Address(ctx, s.own)
	if errors.Is(err, driftdetector.ErrNoAddress) {
		fmt.Fprintln(out, "the notifier's own last check is stale, and no address is set to deliver to — run install -address first")
		return nil
	} else if err != nil {
		return err
	}
	if _, err := writer.Deliver(ctx, why); err != nil {
		return err
	}
	fmt.Fprintf(out, "DELIVERED to %s — %s\n", address, why)
	return nil
}

// raiseStale writes the mismatch one stale last check calls for, over what its
// component's stopping holds — one mismatch per service held, and one holding
// nothing where the component reaches no deploy.
func raiseStale(ctx context.Context, s stores, writer *driftdetector.Writer,
	c lastcheck.LastCheck, out io.Writer) error {
	why := fmt.Sprintf("%s's own last check is stale: it named an interval of %s and owes a further pass",
		c.Component, c.Interval)
	running, err := servicesOnProductionTargets(ctx, s)
	if err != nil {
		return err
	}
	for _, hold := range driftdetector.Holds(c, running) {
		raised, err := writer.RaiseStaleComponent(ctx, driftdetector.StaleComponent{
			Component: c.Component, ServiceID: hold.ServiceID, Why: why,
		})
		if err != nil {
			return err
		}
		if raised == "" {
			continue
		}
		if hold.ServiceID == "" {
			fmt.Fprintf(out, "STALE COMPONENT %s — %s, holding nothing: the page is the whole of it\n",
				raised, why)
			continue
		}
		fmt.Fprintf(out, "STALE COMPONENT %s — %s, holding %s's production deploys\n",
			raised, why, hold.ServiceID)
	}
	return nil
}

// recordFreshAgain is the third comparison's later agreement: every uncleared
// [driftdetector.MismatchKindStaleComponent] mismatch whose component
// staleComponents does not name is a component whose last check answered
// fresh again this pass, and [driftdetector.Writer.RecordStaleComponentAgreement]
// records that on the standing row the way a later agreeing target pass
// already does — it does not clear the mismatch, so a human still reads it
// at the deploy row it holds.
func recordFreshAgain(ctx context.Context, s stores, writer *driftdetector.Writer,
	staleComponents map[string]bool, out io.Writer) error {
	standing, err := driftdetector.Uncleared(ctx, s.own, "")
	if err != nil {
		return err
	}
	for _, m := range standing {
		if m.Kind != driftdetector.MismatchKindStaleComponent || staleComponents[m.Component] {
			continue
		}
		agreed, err := writer.RecordStaleComponentAgreement(ctx, m.Component, m.ServiceID, m.Target)
		if err != nil {
			return err
		}
		if agreed == "" {
			continue
		}
		fmt.Fprintf(out, "%s's own last check is fresh again — a later agreement is recorded on %s as evidence\n",
			m.Component, agreed)
	}
	return nil
}

// servicesOnProductionTargets assembles [driftdetector.Holds]'s own input:
// every unretired service, its production environment's id, and the targets
// it runs on there. What each stopped component's mismatch holds is
// [driftdetector.Holds]'s decision over this, read off which thing that
// component keeps a last check per — the deployer's kept per production
// environment and not per target — and this command only reads what exists
// and where it runs.
func servicesOnProductionTargets(ctx context.Context, s stores) ([]driftdetector.ServiceOnTargets, error) {
	services, err := service.All(ctx, s.factory)
	if err != nil {
		return nil, err
	}
	running := make([]driftdetector.ServiceOnTargets, 0, len(services))
	for _, svc := range services {
		if svc.Retired() {
			continue
		}
		production, found, err := environment.Production(ctx, s.factory, svc.ProjectID)
		if err != nil || !found {
			continue
		}
		running = append(running, driftdetector.ServiceOnTargets{
			ServiceID: svc.ID, EnvironmentID: production.ID, Targets: runsOn(production, svc),
		})
	}
	return running, nil
}
