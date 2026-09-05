package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/contractcheck"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/service"
)

// contractsCommand prints the graph a contract and a consumer contract make,
// which is the milestone's own claim read off the records: what does this break
// is a query rather than an estimate. Four things, in this order.
//
// Every contract with its versions, the elements of the newest, and the
// deprecation mark on each — which is what a producer promises and to which
// version. The consumer contracts in force per service with the release range they
// were derived over, so a reader sees what made them binding rather than only what
// they say. The deprecation list per marked element, which nothing writes and
// which cannot go stale. And, for one item, what its candidate would break and
// whom.
//
// It reaches no target and needs no model, so it composes the path the way every
// other read-only subcommand does.
func contractsCommand(args []string) error {
	flags := flag.NewFlagSet("contracts", flag.ContinueOnError)
	secrets := flags.String("secrets", "", "path of the secrets file (required)")
	targets := flags.String("targets", "", "the directory the local target runs releases from (required)")
	breaks := flags.String("breaks", "", "an item id: print what that candidate would break and whom, instead of the whole graph")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("factory contracts: no arguments, and then any flags")
	}
	for _, required := range []struct{ name, value string }{
		{"secrets", *secrets}, {"targets", *targets},
	} {
		if required.value == "" {
			return fmt.Errorf("factory contracts: -%s is required", required.name)
		}
	}
	if _, err := secretsResolver(*secrets); err != nil {
		return err
	}

	// The graph is the factory's and not one service's: a contract's consumers are
	// other services, whichever project each is in — so this reads every service
	// record, which is what withPath already does.
	return withPath(pathFlags{secrets: *secrets, targets: *targets, human: "owner"},
		func(ctx context.Context, p *path) error {
			services, err := service.All(ctx, p.d.pool)
			if err != nil {
				return err
			}
			if *breaks != "" {
				return printBreaks(ctx, p, *breaks)
			}
			return printContracts(ctx, p, services)
		})
}

// printContracts is the whole graph: the contracts, the consumer contracts in
// force, and the deprecation list.
func printContracts(ctx context.Context, p *path, services []service.Service) error {
	names := map[string]string{}
	for _, svc := range services {
		names[svc.ID] = svc.Name
	}

	all, err := contract.All(ctx, p.d.pool)
	if err != nil {
		return err
	}
	if len(all) == 0 {
		fmt.Fprintln(p.d.out, "No release has published a contract yet, so there is nothing bound to anything")
	}
	for _, con := range all {
		fmt.Fprintf(p.d.out, "contract %s of %s (%s), id %s\n",
			con.Name, names[con.ServiceID], con.Kind, con.ID)
		promise := "backward: the new build reads what the old one wrote"
		if con.Kind.Forward() {
			promise = "backward and forward: the build being restored reads what the newer one wrote too"
		}
		fmt.Fprintf(p.d.out, "  it promises %s\n", promise)

		versions, err := contract.VersionsOf(ctx, p.d.pool, con.ID)
		if err != nil {
			return err
		}
		for _, v := range versions {
			fmt.Fprintf(p.d.out, "  %s at release %d (item %s), version id %s\n",
				v.Semver, v.ReleaseNumber, v.ItemID, v.ID)
		}
		newest, hasOne, err := contract.NewestVersion(ctx, p.d.pool, con.ID)
		if err != nil {
			return err
		}
		if !hasOne {
			continue
		}
		form, err := contract.FormOf(ctx, p.d.pool, con, newest.ID)
		if err != nil {
			return err
		}
		for _, e := range form.Elements {
			said := []string{e.Type}
			if e.Populated {
				said = append(said, "always populated")
			}
			if e.Deprecated {
				said = append(said, "marked deprecated")
			}
			fmt.Fprintf(p.d.out, "    %s: %s\n", e.Name, strings.Join(said, ", "))
		}
		// What is running, which is what a producer's own diff is against. Newest
		// and current are different facts and this is where an owner sees it.
		current, running, err := deploy.Current(ctx, p.d.pool, con.ServiceID, p.production.ID)
		if err != nil {
			return err
		}
		if !running {
			fmt.Fprintf(p.d.out, "  %s is running nothing, so this contract's promise is to nobody yet\n",
				names[con.ServiceID])
			continue
		}
		rel, err := release.Get(ctx, p.d.pool, current.ReleaseID)
		if err != nil {
			return err
		}
		inProduction, hadOne, err := contract.VersionAt(ctx, p.d.pool, con.ID, rel.Number)
		if err != nil {
			return err
		}
		if !hadOne {
			fmt.Fprintf(p.d.out, "  release %d is running and published no version of this contract at or below it\n", rel.Number)
			continue
		}
		fmt.Fprintf(p.d.out, "  production runs release %d, which publishes %s — the version a producer's own diff is against\n",
			rel.Number, inProduction.Semver)
	}

	for _, svc := range services {
		in, err := p.contracts.ConsumerContractsInForce(ctx, svc.ID)
		if err != nil {
			return err
		}
		lastGood := "no last known-good release: no window of this service has closed passed or timed out, so the range is every release it has"
		if in.HasLastKnownGood {
			lastGood = fmt.Sprintf("last known-good release %d, set by window %s", in.LastKnownGoodNumber, in.LastKnownGoodWindowID)
		}
		fmt.Fprintf(p.d.out, "%s declares %d predicate(s) in force, derived over releases %d..%d (%s)\n",
			svc.Name, len(in.Predicates), lowestOf(in), in.HighestNumber, lastGood)
		for _, pr := range in.Predicates {
			producer := pr.ProducerService
			if pr.ProducerServiceID == "" {
				producer += " (no service record: it has published nothing the factory has seen)"
			}
			fmt.Fprintf(p.d.out, "  %s on %s.%s.%s, from item %s\n",
				pr.Kind, producer, pr.Interface, pr.Element, pr.ItemID)
		}
	}

	marked, err := p.contracts.Deprecated(ctx)
	if err != nil {
		return err
	}
	if len(marked) == 0 {
		fmt.Fprintln(p.d.out, "No element is marked deprecated, so no removal is waiting on anything")
		return nil
	}
	for _, m := range marked {
		state := fmt.Sprintf("%d consumer(s) still declare it: %v", len(m.Consumers()), m.Consumers())
		if m.Empty() {
			state = "nothing derived still names it, so the removal may ship"
		}
		fmt.Fprintf(p.d.out, "%s.%s of %s is marked deprecated in %s — %s\n",
			m.Contract.Name, m.Element.Name, m.ServiceName, m.Version.Semver, state)
		for _, s := range m.Safeguards {
			fmt.Fprintf(p.d.out, "  safeguard %s, placed by %s %s, asserts %s on it: the removal item exists and is rejected at its Merge to master gate\n",
				s.SafeguardID, s.Actor.Kind, s.Actor.Key, s.Kind)
		}
	}
	return nil
}

// lowestOf is the bottom of the range consumer contracts in force were derived
// over, which is the last known-good release where there is one and the service's
// first release where there is not.
func lowestOf(in contractcheck.InForce) int64 {
	if in.HasLastKnownGood {
		return in.LastKnownGoodNumber
	}
	if in.HighestNumber == 0 {
		return 0
	}
	return 1
}

// printBreaks is what one candidate would break and whom, which is the query
// enforcement answers at the merge row, asked outside a run. It reads the candidate
// out of the records: the item, its service, its newest build, and the environment
// its build ran on.
func printBreaks(ctx context.Context, p *path, itemID string) error {
	it, err := item.Get(ctx, p.d.pool, itemID)
	if err != nil {
		return err
	}
	c, err := p.candidateFor(ctx, itemID)
	if err != nil {
		return err
	}
	if c.environmentID == "" {
		return fmt.Errorf("factory contracts: item %s has no candidate environment, so there is no run to decide a consumer contract against", itemID)
	}
	newest, err := newestBuildOf(ctx, p, itemID)
	if err != nil {
		return err
	}
	checked, err := p.contracts.Enforce(ctx, contractcheck.Candidate{
		ItemID:        it.ID,
		ServiceID:     c.svc.ID,
		ServiceName:   c.svc.Name,
		BuildID:       newest,
		EnvironmentID: c.environmentID,
	}, p.production.ID)
	if err != nil {
		return err
	}
	reportContracts(p.d.out, checked)
	if checked.Passed() {
		fmt.Fprintln(p.d.out, "Nothing this candidate does breaks a promise anything still depends on")
		return nil
	}
	fmt.Fprintf(p.d.out, "%s would reject this candidate: %s\n", checked.Check(), checked.Why())
	return nil
}

// newestBuildOf is the item's newest build, which is the one a check outside a run
// reads. A run holds the build it just made; this does not, so it asks the records.
func newestBuildOf(ctx context.Context, p *path, itemID string) (string, error) {
	bl, found, err := build.Newest(ctx, p.d.pool, itemID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("factory contracts: item %s has no build, so there is nothing to check its contracts against", itemID)
	}
	return bl.ID, nil
}
