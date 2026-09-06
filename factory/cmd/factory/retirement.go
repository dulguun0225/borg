package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/service"
)

// What ends a service and what ends a project, and the removal each calls for,
// composed here because the deployer is: package policy writes retired on the
// service record and calls the removal, reaching no deploy target itself.

// removeService is [policy.Factory.Removal]: the deployer ends every instance of
// the service on every target of a persistent environment and writes a deploy
// record per environment naming no release, which is what makes the service's
// current release nothing wherever it ran.
//
// environmentID bounds it to one environment, which is the removal an owner has
// performed before an environment may be withdrawn; empty is every persistent
// environment, which is what a retirement reaches. The environments this install
// knows are production's alone — a customer's is a record nothing here creates —
// so an environment named that is not production's is refused rather than
// reaching nothing and reading as a removal that happened.
func (p *path) removeService(ctx context.Context, serviceID, environmentID string) error {
	svc, err := p.serviceOf(ctx, serviceID)
	if err != nil {
		return err
	}
	if environmentID != "" && environmentID != p.production.ID {
		return fmt.Errorf("factory: this install knows production's environment alone, and the removal names %s", environmentID)
	}
	_, err = deploy.Remove(ctx, p.deploys, deploy.Removal{
		Actor:       deployActor,
		Principal:   deployerPrincipal,
		ServiceID:   svc.ID,
		ServiceName: svc.Name,
		From: []deploy.Environment{{
			EnvironmentID: p.production.ID,
			Credential:    p.d.credential,
			Reaches:       p.reaches(p.production),
		}},
	})
	return err
}

// retireCommand is `factory retire <service>`: the owner's write of retired on
// the service record, which is the one thing that ends a service and what calls
// the deployer's removal. The three counts that refuse it are read here —
// package policy takes them as arguments, each being a read of a package it may
// not import — so what the owner sees is which of the three still names the
// service.
//
// With `-environment`, it performs the removal for that one environment and
// writes nothing on the service record: that is the step an owner takes before
// an environment other than production may be withdrawn, and the order is by
// hand — remove, then withdraw.
func retireCommand(args []string) error {
	flags := flag.NewFlagSet("retire", flag.ContinueOnError)
	secrets := flags.String("secrets", "", "path of the secrets file (required)")
	targets := flags.String("targets", "", "the directory the local target runs releases from (required)")
	human := flags.String("human", "owner", "the owner retiring the service")
	projectName := flags.String("project", defaultProjectName, "the project the service is in")
	environmentName := flags.String("environment", "",
		"perform the removal for this one environment and write nothing on the service record, which is what precedes an environment's withdrawal")

	name := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, args = args[0], args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if name == "" || flags.NArg() != 0 {
		return errors.New("factory retire: one argument, the service's name, and then any flags")
	}
	for _, required := range []struct{ name, value string }{{"secrets", *secrets}, {"targets", *targets}} {
		if required.value == "" {
			return fmt.Errorf("factory retire: -%s is required", required.name)
		}
	}

	return withPath(pathFlags{secrets: *secrets, targets: *targets, human: *human, project: *projectName},
		func(ctx context.Context, p *path) error {
			svc, found, err := service.ByName(ctx, p.d.pool, name)
			if err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("factory retire: no service named %q", name)
			}
			if *environmentName != "" {
				return p.removeFromEnvironment(ctx, svc, *environmentName)
			}
			return p.retire(ctx, svc)
		})
}

// retire is the owner's write and what it reports. The removal runs after the
// version and the field commit, which package policy performs, so a removal that
// stopped leaves a service retired whose removal is performed again.
func (p *path) retire(ctx context.Context, svc service.Service) error {
	binding, _, err := p.contracts.Binding(ctx, svc.ID)
	if err != nil {
		return err
	}
	naming, dependingOn, err := unmergedItemsNaming(ctx, p, svc.ID)
	if err != nil {
		return err
	}
	version, err := p.factory.RetireService(ctx, p.human, svc.ID, len(binding), naming, dependingOn)
	if err != nil {
		return fmt.Errorf("%w\n  %d consumer contract(s) in force name it, %d unmerged item(s) name it, and %d unmerged item(s) depend on one of its items",
			err, len(binding), naming, dependingOn)
	}
	fmt.Fprintf(p.d.out, "Service %s is retired by %s; policy version %s\n", svc.Name, p.d.human, version.ID)
	fmt.Fprintln(p.d.out, "  the deployer removed it from every persistent environment, so it has no current release and every reader of that record reads it as such")
	fmt.Fprintln(p.d.out, "  every record of it stays, and the repository and the store are the owner's to remove, as creating them was")
	return nil
}

// removeFromEnvironment is the removal performed for one environment and
// nothing else: the service record is not written, so the service still stands
// and may still be deployed. It is what makes an environment's withdrawal
// possible, and the withdrawal is a second act of the owner's.
func (p *path) removeFromEnvironment(ctx context.Context, svc service.Service, environmentName string) error {
	env, found, err := environment.ByName(ctx, p.d.pool, p.projectID, environmentName)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("factory retire: no environment named %q in this project", environmentName)
	}
	if !env.Kind.Persistent() {
		return fmt.Errorf("factory retire: %s is a candidate's environment, which is torn down and not withdrawn", env.Name)
	}
	if err := p.factory.RemoveFromEnvironment(ctx, p.human, svc.ID, env.ID); err != nil {
		return err
	}
	fmt.Fprintf(p.d.out, "Service %s is removed from environment %s by %s\n", svc.Name, env.Name, p.d.human)
	fmt.Fprintln(p.d.out, "  the service record is unwritten: it still stands, and the withdrawal of the environment is the owner's next act")
	return nil
}

// endProjectCommand is `factory end-project`: the project is ended at Factory
// once every service in it is retired, and its production environment is
// withdrawn with it, in the write that pairs the two the way creating them did.
//
// The count of deploy records marking a target complete for a release is read
// here — package environment refuses the withdrawal on it and may not count them
// — and it is the services with a current release on that environment, a removal
// being what takes one off.
func endProjectCommand(args []string) error {
	flags := flag.NewFlagSet("end-project", flag.ContinueOnError)
	secrets := flags.String("secrets", "", "path of the secrets file (required)")
	targets := flags.String("targets", "", "the directory the local target runs releases from (required)")
	human := flags.String("human", "owner", "the owner ending the project")
	projectName := flags.String("project", defaultProjectName, "the project to end")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("factory end-project: no arguments, and then any flags")
	}
	for _, required := range []struct{ name, value string }{{"secrets", *secrets}, {"targets", *targets}} {
		if required.value == "" {
			return fmt.Errorf("factory end-project: -%s is required", required.name)
		}
	}

	return withPath(pathFlags{secrets: *secrets, targets: *targets, human: *human, project: *projectName},
		func(ctx context.Context, p *path) error {
			running, err := servicesWithACurrentRelease(ctx, p)
			if err != nil {
				return err
			}
			version, err := p.factory.EndProject(ctx, p.human, p.projectID, running)
			if err != nil {
				return fmt.Errorf("%w\n  %d service(s) still have a current release on production's environment; retiring each is what removes it",
					err, running)
			}
			fmt.Fprintf(p.d.out, "Project %s is ended by %s; policy version %s\n", *projectName, p.d.human, version.ID)
			fmt.Fprintln(p.d.out, "  production's environment for it is withdrawn in the same write, the pairing that created the two")
			return nil
		})
}

// unmergedItemsNaming is two of the three counts an owner's retirement is
// refused on: how many unmerged items name the service, and how many unmerged
// items of any service declare a dependency on an item of this one. An item that
// has ended — merged, dropped, or superseded — is neither.
func unmergedItemsNaming(ctx context.Context, p *path, serviceID string) (naming, dependingOn int, err error) {
	every, err := item.All(ctx, p.d.pool)
	if err != nil {
		return 0, 0, err
	}
	of := map[string]bool{}
	for _, it := range every {
		if it.ServiceID == serviceID {
			of[it.ID] = true
		}
	}
	for _, it := range every {
		switch it.Stage {
		case item.StageMerged, item.StageDropped, item.StageSuperseded:
			continue
		}
		if it.ServiceID == serviceID {
			naming++
			continue
		}
		for _, on := range it.WaitsOn {
			if of[on] {
				dependingOn++
				break
			}
		}
	}
	return naming, dependingOn, nil
}

// servicesWithACurrentRelease is how many services still have a current release
// on production's environment, which is the count package environment refuses a
// withdrawal on. A removal, complete on every target and naming no release, is
// what makes a service's current release nothing.
func servicesWithACurrentRelease(ctx context.Context, p *path) (int, error) {
	services, err := service.All(ctx, p.d.pool)
	if err != nil {
		return 0, err
	}
	addresses := p.productionAddresses()
	running := 0
	for _, svc := range services {
		if svc.ProjectID != p.projectID {
			continue
		}
		_, found, err := deploy.Current(ctx, p.d.pool, svc.ID, p.production.ID, addresses)
		if err != nil {
			return 0, err
		}
		if found {
			running++
		}
	}
	return running, nil
}
