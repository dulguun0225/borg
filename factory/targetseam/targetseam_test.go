package targetseam

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/secretref"
)

// deployer is the principal every call at this seam is made as. No agent
// reaches a deploy target: deploying is not a stage an agent is dispatched to.
var deployer = principal.OfComponent("deployer")

// deployIDOnly is a configuration naming only the deploy record's own
// identity, which [Deployment.Validate] requires of every deployment.
var deployIDOnly = ValueSet{Names: []string{DeployIDName}, Values: []string{"dep_1"}}

func TestFakeRecordsEveryNamedOperation(t *testing.T) {
	ctx := context.Background()
	credential := secretref.MustNew("deploy.staging")
	fake := NewFake()

	var target Target = fake
	if _, err := target.Deploy(ctx, deployer, Deployment{
		Service: "checkout", Build: "r-7", Credential: credential, Configuration: deployIDOnly,
	}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	running, err := target.ReadRunning(ctx, deployer, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	if running.Build != "r-7" {
		t.Fatalf("ReadRunning = %+v, want build r-7", running)
	}
	if err := target.ShiftTraffic(ctx, deployer, Shift{
		Service: "checkout", Build: "r-7", Share: 0.1, Credential: credential,
	}); err != nil {
		t.Fatalf("ShiftTraffic: %v", err)
	}
	if err := target.SetInstanceCount(ctx, deployer, InstanceCount{
		Service: "checkout", Build: "r-7", Count: 3, Credential: credential,
	}); err != nil {
		t.Fatalf("SetInstanceCount: %v", err)
	}
	taken, err := target.Snapshot(ctx, deployer, SnapshotRequest{
		Service: "checkout", Name: "before-the-drop", Credential: credential,
	})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := target.ApplySchemaChange(ctx, deployer, SchemaChange{
		Service: "checkout", Change: "0003-drop-the-old-column", Release: "rel_7", Text: "drop",
		Destroys: true, Snapshot: taken, Credential: credential,
	}); err != nil {
		t.Fatalf("ApplySchemaChange: %v", err)
	}
	stopped, err := target.Stop(ctx, deployer, "checkout", credential)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if stopped.Replacement != ReplacementDrained {
		t.Errorf("Stop reports %q, want the drain this fake performs", stopped.Replacement)
	}

	want := []Op{
		OpDeploy, OpReadRunning, OpShiftTraffic, OpSetInstanceCount,
		OpSnapshot, OpApplySchemaChange, OpStop,
	}
	var got []Op
	for _, call := range fake.Calls() {
		got = append(got, call.Op)
		if call.Principal != deployer {
			t.Errorf("call %s records principal %s, want the deployer", call.Op, call.Principal)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the fake recorded %v, want %v", got, want)
	}
}

// TestReconfigureHandsTheRunningInstancesAFreshConfiguration: the fast
// rollback mints a fresh way-in token for the kept instances rather than
// leaving them holding the one an earlier deploy minted, and this is the
// operation it hands that configuration through — no build put anywhere cold,
// unlike [Target.Deploy].
func TestReconfigureHandsTheRunningInstancesAFreshConfiguration(t *testing.T) {
	ctx := context.Background()
	credential := secretref.MustNew("deploy.production")
	fake := NewFake()

	placed, err := fake.Reconfigure(ctx, deployer, Reconfiguration{
		Service: "checkout", Build: "r-7", Credential: credential,
		Configuration: ValueSet{Names: []string{DeployIDName}, Values: []string{"dep_2"}},
	})
	if err != nil {
		t.Fatalf("Reconfigure: %v", err)
	}
	if placed.Replacement != ReplacementDrained {
		t.Errorf("Reconfigure reports %q, want the drain", placed.Replacement)
	}
	calls := fake.Calls()
	if len(calls) != 1 || calls[0].Op != OpReconfigure {
		t.Fatalf("the fake recorded %v, want one OpReconfigure call", calls)
	}
}

// TestReconfigureRefusesAnIncompleteOperationOrNoDeployID: [Reconfiguration]
// requires the same deploy id every deployment does, the instance being told
// its deploy the same way at either operation.
func TestReconfigureRefusesAnIncompleteOperationOrNoDeployID(t *testing.T) {
	ctx := context.Background()
	credential := secretref.MustNew("deploy.production")
	fake := NewFake()

	if _, err := fake.Reconfigure(ctx, deployer, Reconfiguration{
		Service: "checkout", Credential: credential, Configuration: deployIDOnly,
	}); !errors.Is(err, ErrIncomplete) {
		t.Errorf("Reconfigure with no build = %v, want ErrIncomplete", err)
	}
	if _, err := fake.Reconfigure(ctx, deployer, Reconfiguration{
		Service: "checkout", Build: "r-7", Credential: credential,
	}); !errors.Is(err, ErrIncomplete) {
		t.Errorf("Reconfigure with no deploy id = %v, want ErrIncomplete", err)
	}
	if calls := fake.Calls(); len(calls) != 0 {
		t.Fatalf("a refused Reconfigure was recorded: %+v", calls)
	}
}

// TestAPlatformThatCannotReconfigureRefuses: a platform unable to hand a
// running instance a fresh configuration without dropping a request refuses
// with [ErrCannotReconfigure] rather than reporting one, which is what makes
// the fast rollback fall back to [Restore], the slow way.
func TestAPlatformThatCannotReconfigureRefuses(t *testing.T) {
	ctx := context.Background()
	credential := secretref.MustNew("deploy.production")
	fake := NewFake()
	fake.RefuseReconfigure = ErrCannotReconfigure

	if _, err := fake.Reconfigure(ctx, deployer, Reconfiguration{
		Service: "checkout", Build: "r-7", Credential: credential, Configuration: deployIDOnly,
	}); !errors.Is(err, ErrCannotReconfigure) {
		t.Fatalf("Reconfigure on a platform that cannot = %v, want ErrCannotReconfigure", err)
	}
	if calls := fake.Calls(); len(calls) != 0 {
		t.Fatalf("a refused Reconfigure was recorded: %+v", calls)
	}
}

// TestOpsListsEveryOperation: [Ops] is what a caller enumerates the seam by, so
// it has to hold every method [Target] declares and no name that is not one.
func TestOpsListsEveryOperation(t *testing.T) {
	declared := reflect.TypeOf((*Target)(nil)).Elem().NumMethod()
	if len(Ops) != declared {
		t.Fatalf("Ops has %d operations and Target declares %d methods", len(Ops), declared)
	}
	seen := map[Op]bool{}
	for _, op := range Ops {
		if seen[op] {
			t.Errorf("Ops names %q twice", op)
		}
		seen[op] = true
	}
}

// TestAReplacementDropsNoRequest: neither rollout row drops a request, so the
// operation that replaces an instance reports the drain and there is no second
// outcome for a caller to write on a record.
func TestAReplacementDropsNoRequest(t *testing.T) {
	ctx := context.Background()
	credential := secretref.MustNew("deploy.staging")

	fake := NewFake()
	placed, err := fake.Deploy(ctx, deployer, Deployment{
		Service: "checkout", Build: "r-7", Credential: credential, Configuration: deployIDOnly,
	})
	if err != nil || placed.Replacement != ReplacementDrained {
		t.Fatalf("Deploy = %+v, %v, want a drain", placed, err)
	}
	ended, err := fake.Stop(ctx, deployer, "checkout", credential)
	if err != nil || ended.Replacement != ReplacementDrained {
		t.Fatalf("Stop = %+v, %v, want a drain", ended, err)
	}
	if len(Replacements) != 1 || Replacements[0] != ReplacementDrained {
		t.Fatalf("Replacements = %v, want the drain alone", Replacements)
	}
}

// TestAPlatformThatCannotDrainRefusesRatherThanReportingOne: neither Deploy nor
// Stop may report [ReplacementDrained] without having kept the promise, so a
// platform unable to hold a request open across the replacement refuses with
// [ErrCannotDrain] and records no call — a replacement that did not happen is
// never written on the record.
func TestAPlatformThatCannotDrainRefusesRatherThanReportingOne(t *testing.T) {
	ctx := context.Background()
	credential := secretref.MustNew("deploy.staging")
	fake := NewFake()
	fake.RefuseDrain = ErrCannotDrain

	if _, err := fake.Deploy(ctx, deployer, Deployment{
		Service: "checkout", Build: "r-7", Credential: credential, Configuration: deployIDOnly,
	}); !errors.Is(err, ErrCannotDrain) {
		t.Fatalf("Deploy on a platform that cannot drain = %v, want ErrCannotDrain", err)
	}
	if _, err := fake.Stop(ctx, deployer, "checkout", credential); !errors.Is(err, ErrCannotDrain) {
		t.Fatalf("Stop on a platform that cannot drain = %v, want ErrCannotDrain", err)
	}
	if calls := fake.Calls(); len(calls) != 0 {
		t.Fatalf("a refused replacement was recorded: %+v", calls)
	}
}

// TestTheMitigationIsAClassOfTwo: a mitigation is a named class at this seam
// with two operations and not three — ending every instance is retirement's and
// no human at Ops instructs it.
func TestTheMitigationIsAClassOfTwo(t *testing.T) {
	want := []Op{OpShiftTraffic, OpSetInstanceCount}
	if !reflect.DeepEqual(Mitigation, want) {
		t.Fatalf("Mitigation = %v, want %v", Mitigation, want)
	}
	for _, op := range Mitigation {
		if op == OpStop {
			t.Fatalf("the class names %q, which is retirement's and not a mitigation's", op)
		}
		if !slices.Contains(Ops, op) {
			t.Fatalf("the class names %q, which is no operation of the seam", op)
		}
	}
}

// TestARecordedCallHoldsAReferenceAndNoValue is where seam 3 and seam 4 meet:
// the credential crosses the seam as a name, so nothing the seam records can
// hold a value even when the caller has one in hand. The way-in token is the
// one value that crosses, and it is not recorded either.
func TestARecordedCallHoldsAReferenceAndNoValue(t *testing.T) {
	const value = "sk-the-value-nothing-else-may-see"
	ctx := context.Background()
	credential := secretref.MustNew("deploy.staging")
	fake := NewFake()

	if _, err := fake.Deploy(ctx, deployer, Deployment{
		Service: "checkout", Build: "r-7", Credential: credential,
		Configuration: ValueSet{Names: []string{"BORG_WAY_IN", DeployIDName}, Values: []string{value, "dep_1"}},
	}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	call := fake.Calls()[0]
	if call.Credential.Name() != "deploy.staging" {
		t.Fatalf("the recorded credential is %q, want the reference", call.Credential.Name())
	}
	if rendered := fmt.Sprintf("%+v", fake.Calls()); strings.Contains(rendered, value) {
		t.Fatalf("a recorded call renders a value: %s", rendered)
	}
}

func TestTheSeamRefusesAnIncompleteOperation(t *testing.T) {
	ctx := context.Background()
	credential := secretref.MustNew("deploy.staging")
	fake := NewFake()

	cases := map[string]func() error{
		"no service": func() error {
			_, err := fake.Deploy(ctx, deployer, Deployment{Build: "r-7", Credential: credential})
			return err
		},
		"no build": func() error {
			_, err := fake.Deploy(ctx, deployer, Deployment{Service: "checkout", Credential: credential})
			return err
		},
		"no credential": func() error {
			_, err := fake.Deploy(ctx, deployer, Deployment{Service: "checkout", Build: "r-7"})
			return err
		},
		"a value for no name": func() error {
			_, err := fake.Deploy(ctx, deployer, Deployment{
				Service: "checkout", Build: "r-7", Credential: credential,
				Configuration: ValueSet{Values: []string{"one"}},
			})
			return err
		},
		"no deploy id": func() error {
			_, err := fake.Deploy(ctx, deployer, Deployment{
				Service: "checkout", Build: "r-7", Credential: credential,
			})
			return err
		},
		"stop with no credential": func() error {
			_, err := fake.Stop(ctx, deployer, "checkout", secretref.Ref{})
			return err
		},
		"a change found applied naming no release": func() error {
			return fake.ApplySchemaChange(ctx, deployer, SchemaChange{
				Service: "checkout", Change: "0002-add-the-column", FoundApplied: true, Credential: credential,
			})
		},
		"read with no credential": func() error {
			_, err := fake.ReadRunning(ctx, deployer, "checkout", secretref.Ref{})
			return err
		},
		"a schema change naming no change": func() error {
			return fake.ApplySchemaChange(ctx, deployer, SchemaChange{
				Service: "checkout", Release: "rel_1", Credential: credential})
		},
		"a snapshot naming no copy": func() error {
			_, err := fake.Snapshot(ctx, deployer, SnapshotRequest{Service: "checkout", Credential: credential})
			return err
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, ErrIncomplete) {
				t.Fatalf("= %v, want ErrIncomplete", err)
			}
		})
	}
	if calls := fake.Calls(); len(calls) != 0 {
		t.Fatalf("a refused operation was recorded: %+v", calls)
	}
}

// TestTheSeamRefusesACallWithNoPrincipal: the principal is populated on every
// call and enforced on none, so an absent one is refused and nothing in a
// present one is read.
func TestTheSeamRefusesACallWithNoPrincipal(t *testing.T) {
	ctx := context.Background()
	credential := secretref.MustNew("deploy.staging")
	fake := NewFake()

	if _, err := fake.Deploy(ctx, principal.Principal{}, Deployment{
		Service: "checkout", Build: "r-7", Credential: credential, Configuration: deployIDOnly,
	}); !errors.Is(err, ErrNoPrincipal) {
		t.Fatalf("Deploy with no principal = %v, want ErrNoPrincipal", err)
	}
	if _, err := fake.Stop(ctx, principal.Principal{}, "checkout", credential); !errors.Is(err, ErrNoPrincipal) {
		t.Fatalf("Stop with no principal = %v, want ErrNoPrincipal", err)
	}
	if calls := fake.Calls(); len(calls) != 0 {
		t.Fatalf("a call with no principal was recorded: %+v", calls)
	}
}

// TestAShareIsAFraction: a shift asking for more than all of the traffic, or
// less than none, is refused before anything is reached.
func TestAShareIsAFraction(t *testing.T) {
	ctx := context.Background()
	credential := secretref.MustNew("deploy.staging")
	fake := NewFake()

	err := fake.ShiftTraffic(ctx, deployer, Shift{Service: "checkout", Share: 1.5, Credential: credential})
	if !errors.Is(err, ErrShareNotAFraction) {
		t.Errorf("ShiftTraffic(1.5) = %v, want ErrShareNotAFraction", err)
	}
	err = fake.SetInstanceCount(ctx, deployer, InstanceCount{
		Service: "checkout", Build: "r-7", Count: -1, Credential: credential,
	})
	if !errors.Is(err, ErrCountNegative) {
		t.Errorf("SetInstanceCount(-1) = %v, want ErrCountNegative", err)
	}
}
