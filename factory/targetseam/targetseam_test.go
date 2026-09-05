package targetseam

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/secretref"
)

// deployer is the principal every call at this seam is made as. No agent
// reaches a deploy target: deploying is not a stage an agent is dispatched to.
var deployer = principal.OfComponent("deployer")

func TestFakeRecordsEveryNamedOperation(t *testing.T) {
	ctx := context.Background()
	credential := secretref.MustNew("deploy.staging")
	fake := NewFake()

	var target Target = fake
	if _, err := target.Deploy(ctx, deployer, Deployment{
		Service: "checkout", Build: "r-7", Credential: credential,
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
	if _, err := target.Snapshot(ctx, deployer, SnapshotRequest{
		Service: "checkout", Name: "before-the-drop", Credential: credential,
	}); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := target.ApplySchemaChange(ctx, deployer, SchemaChange{
		Service: "checkout", Change: "0003-drop-the-old-column", Text: "drop", Destroys: true,
		Credential: credential,
	}); err != nil {
		t.Fatalf("ApplySchemaChange: %v", err)
	}
	if err := target.Stop(ctx, deployer, "checkout", credential); err != nil {
		t.Fatalf("Stop: %v", err)
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

// TestTheSchemaHistoryIsReadFromTheTarget: which changes a store carries is
// read from the store's own history and never from the deploy record, so the
// read operation is what answers it.
func TestTheSchemaHistoryIsReadFromTheTarget(t *testing.T) {
	ctx := context.Background()
	credential := secretref.MustNew("deploy.staging")
	fake := NewFake()
	fake.Instances = 4

	if _, err := fake.Deploy(ctx, deployer, Deployment{
		Service: "checkout", Build: "r-7", Credential: credential,
	}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if err := fake.ApplySchemaChange(ctx, deployer, SchemaChange{
		Service: "checkout", Change: "0001-add-the-column", Text: "add", Credential: credential,
	}); err != nil {
		t.Fatalf("ApplySchemaChange: %v", err)
	}

	running, err := fake.ReadRunning(ctx, deployer, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	if len(running.SchemaHistory) != 1 || running.SchemaHistory[0].Change != "0001-add-the-column" {
		t.Errorf("the history reads %+v, want the one change applied", running.SchemaHistory)
	}
	if !running.SchemaHistory[0].Widened {
		t.Error("a change that destroys nothing reads as not having widened the store")
	}
	if running.Instances != 4 {
		t.Errorf("Instances = %d, want the capacity the platform reports", running.Instances)
	}
}

// TestADeployReportsADrainOrACut: the operation that replaces an instance
// reports whether it drained, and a platform that cannot hold a request open
// performs a cut the factory records as one.
func TestADeployReportsADrainOrACut(t *testing.T) {
	ctx := context.Background()
	credential := secretref.MustNew("deploy.staging")

	drained := NewFake()
	placed, err := drained.Deploy(ctx, deployer, Deployment{Service: "checkout", Build: "r-7", Credential: credential})
	if err != nil || placed.Replacement != ReplacementDrained {
		t.Fatalf("Deploy = %+v, %v, want a drain", placed, err)
	}

	cutting := NewFake()
	cutting.Drains = false
	cut, err := cutting.Deploy(ctx, deployer, Deployment{Service: "checkout", Build: "r-7", Credential: credential})
	if err != nil || cut.Replacement != ReplacementCut {
		t.Fatalf("Deploy = %+v, %v, want a cut", cut, err)
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
		Service: "checkout", Build: "r-7", Credential: credential, WayInToken: value,
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
		"stop with no credential": func() error {
			return fake.Stop(ctx, deployer, "checkout", secretref.Ref{})
		},
		"read with no credential": func() error {
			_, err := fake.ReadRunning(ctx, deployer, "checkout", secretref.Ref{})
			return err
		},
		"a schema change naming no change": func() error {
			return fake.ApplySchemaChange(ctx, deployer, SchemaChange{Service: "checkout", Credential: credential})
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
		Service: "checkout", Build: "r-7", Credential: credential,
	}); !errors.Is(err, ErrNoPrincipal) {
		t.Fatalf("Deploy with no principal = %v, want ErrNoPrincipal", err)
	}
	if err := fake.Stop(ctx, principal.Principal{}, "checkout", credential); !errors.Is(err, ErrNoPrincipal) {
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
