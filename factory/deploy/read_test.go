// The reads of this package tested against the database: what a reader reads
// as running, which is the highest-numbered record complete on every target,
// and the two records no reader moves onto — a rollout complete on some
// targets and a record marked failed. The fixtures are db_test.go's, this file
// being one subject of the same external test package.
package deploy_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/release"
)

// TestCurrentIsTheHighestNumberCompleteOnEveryTarget: current is the
// highest-numbered release whose deploy completed on every production target,
// and not the most recently started — rollouts overlap and differ in length, so
// a short one completing above a longer one below it must not make an older
// release current.
func TestCurrentIsTheHighestNumberCompleteOnEveryTarget(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	addresses := addressesOf(twoTargets)

	first := mintRelease(t, ctx, pool, token, serviceID)
	second := mintRelease(t, ctx, pool, token, serviceID)

	begin := func(r release.Release) deploy.Deploy {
		t.Helper()
		d, err := w.Start(ctx, deployer, deploy.Beginning{
			ServiceID: serviceID, EnvironmentID: productionID,
			What: deploy.OfRelease(r.ID, r.BuildID), Targets: twoTargets,
			IntoProduction: true, StrategyPicked: deploy.StrategyWithoutControl,
		})
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		return d
	}

	// The higher release is deployed first and completes on every target; the
	// lower one is deployed after it and completes later. Recency would name the
	// lower one, and the number names the higher.
	high := begin(second)
	completeOn(t, ctx, w, high.ID, addresses...)
	if err := w.Complete(ctx, high.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	low := begin(first)
	completeOn(t, ctx, w, low.ID, addresses...)
	if err := w.Complete(ctx, low.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	current, found, err := deploy.Current(ctx, pool, serviceID, productionID, addresses)
	if err != nil || !found {
		t.Fatalf("Current = found %v, %v", found, err)
	}
	if current.ReleaseID != second.ID {
		t.Errorf("Current names release %s, want the highest-numbered one, %s", current.ReleaseID, second.ID)
	}
}

// TestARolloutOnSomeTargetsIsNotCurrent: a release is current only once every
// production target is marked complete, and until then the previous release is.
func TestARolloutOnSomeTargetsIsNotCurrent(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	addresses := addressesOf(twoTargets)

	first := mintRelease(t, ctx, pool, token, serviceID)
	second := mintRelease(t, ctx, pool, token, serviceID)

	landed, err := w.Start(ctx, deployer, deploy.Beginning{
		ServiceID: serviceID, EnvironmentID: productionID,
		What: deploy.OfRelease(first.ID, first.BuildID), Targets: twoTargets,
		IntoProduction: true, StrategyPicked: deploy.StrategyWithoutControl,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	completeOn(t, ctx, w, landed.ID, addresses...)
	if err := w.Complete(ctx, landed.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	widening, err := w.Start(ctx, deployer, deploy.Beginning{
		ServiceID: serviceID, EnvironmentID: productionID,
		What: deploy.OfRelease(second.ID, second.BuildID), Targets: twoTargets,
		IntoProduction: true, StrategyPicked: deploy.StrategyWithoutControl,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	completeOn(t, ctx, w, widening.ID, "/srv/one")

	current, found, err := deploy.Current(ctx, pool, serviceID, productionID, addresses)
	if err != nil || !found {
		t.Fatalf("Current = found %v, %v", found, err)
	}
	if current.ReleaseID != first.ID {
		t.Errorf("Current names %s while the newer release is on one target of two, want the previous release %s",
			current.ReleaseID, first.ID)
	}

	on, err := deploy.CompleteOnEvery(ctx, pool, widening.ID, addresses)
	if err != nil || on {
		t.Errorf("CompleteOnEvery = %v, %v, want false with one target of two complete", on, err)
	}
}

// TestCurrentIsCompletionOnTheServicesOwnTargets: a service's current release is
// the one its deploy record marks complete on every production target the
// service runs on. Which of the environment's targets that is is a field of the
// service record, so a service running on a subset is current once its own
// targets are complete, with the record itself still started because a target
// it does not run on is not.
func TestCurrentIsCompletionOnTheServicesOwnTargets(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	runsOn := []string{"/srv/one"}
	r := mintRelease(t, ctx, pool, token, serviceID)

	d, err := w.Start(ctx, deployer, deploy.Beginning{
		ServiceID: serviceID, EnvironmentID: productionID,
		What: deploy.OfRelease(r.ID, r.BuildID), Targets: twoTargets,
		IntoProduction: true, StrategyPicked: deploy.StrategyWithoutControl,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	completeOn(t, ctx, w, d.ID, runsOn...)

	read, err := deploy.Get(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Status != deploy.StatusStarted {
		t.Fatalf("the record is %s, and the reading below is the one that must not depend on it", read.Status)
	}

	current, found, err := deploy.Current(ctx, pool, serviceID, productionID, runsOn)
	if err != nil || !found {
		t.Fatalf("Current over the service's own targets = found %v, %v", found, err)
	}
	if current.ID != d.ID {
		t.Errorf("Current names %s, want the record complete on every target the service runs on, %s",
			current.ID, d.ID)
	}

	// Over the environment's whole list the same record is not current: the
	// second target is not complete, and the rule is completion on every target
	// read.
	if _, found, err := deploy.Current(ctx, pool, serviceID, productionID, addressesOf(twoTargets)); err != nil || found {
		t.Errorf("Current over both targets = found %v, %v, want none", found, err)
	}
}

// TestARemovalClearsTheCurrentRelease: a removal names no release at all, and
// the service's current release is none once its record is complete on every
// target.
func TestARemovalClearsTheCurrentRelease(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	addresses := addressesOf(twoTargets)
	r := mintRelease(t, ctx, pool, token, serviceID)

	live, err := w.Start(ctx, deployer, deploy.Beginning{
		ServiceID: serviceID, EnvironmentID: productionID,
		What: deploy.OfRelease(r.ID, r.BuildID), Targets: twoTargets,
		IntoProduction: true, StrategyPicked: deploy.StrategyWithoutControl,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	completeOn(t, ctx, w, live.ID, addresses...)
	if err := w.Complete(ctx, live.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if _, found, err := deploy.Current(ctx, pool, serviceID, productionID, addresses); err != nil || !found {
		t.Fatalf("Current after the deploy = found %v, %v", found, err)
	}

	removal, err := w.Start(ctx, deployer, deploy.Beginning{
		ServiceID: serviceID, EnvironmentID: productionID,
		What: deploy.OfRemoval(), Targets: twoTargets,
		IntoProduction: true, StrategyPicked: deploy.StrategyWithoutControl,
	})
	if err != nil {
		t.Fatalf("Start of a removal: %v", err)
	}
	completeOn(t, ctx, w, removal.ID, addresses...)
	if err := w.Complete(ctx, removal.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if _, found, err := deploy.Current(ctx, pool, serviceID, productionID, addresses); err != nil || found {
		t.Errorf("Current after a removal = found %v, %v, want none", found, err)
	}
}

// TestAFailedRecordNamesTheStepAndMovesNoReader: the record as a whole is
// marked failed where the deployer stopped it before any target was complete,
// naming the step; a deploy with a target complete is a recorded partial deploy
// and is not marked failed at all.
func TestAFailedRecordNamesTheStepAndMovesNoReader(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	addresses := addressesOf(twoTargets)
	r := mintRelease(t, ctx, pool, token, serviceID)

	begin := func() deploy.Deploy {
		t.Helper()
		d, err := w.Start(ctx, deployer, deploy.Beginning{
			ServiceID: serviceID, EnvironmentID: productionID,
			What: deploy.OfRelease(r.ID, r.BuildID), Targets: twoTargets,
			IntoProduction: true, StrategyPicked: deploy.StrategyWithoutControl,
			SchemaChanges: []string{"0003-drop-the-old-column"},
		})
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		return d
	}

	stopped := begin()
	if err := w.MarkFailed(ctx, stopped.ID, deploy.StepSchemaChange); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	read, err := deploy.Get(ctx, pool, stopped.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Status != deploy.StatusFailed || read.FailedStep != deploy.StepSchemaChange {
		t.Errorf("the record is %s at %q, want failed at the step that stopped it", read.Status, read.FailedStep)
	}
	if _, found, err := deploy.Current(ctx, pool, serviceID, productionID, addresses); err != nil || found {
		t.Errorf("a failed record moved the current release: found %v, %v", found, err)
	}

	partial := begin()
	completeOn(t, ctx, w, partial.ID, "/srv/one")
	if err := w.MarkFailed(ctx, partial.ID, deploy.StepStopped); !errors.Is(err, deploy.ErrATargetCompleted) {
		t.Errorf("MarkFailed with a target complete = %v, want ErrATargetCompleted", err)
	}
}
