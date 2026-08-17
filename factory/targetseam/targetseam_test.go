package targetseam

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/secretref"
)

func TestFakeRecordsEveryNamedOperation(t *testing.T) {
	ctx := context.Background()
	credential := secretref.MustNew("deploy.staging")
	fake := NewFake()

	var target Target = fake
	if err := target.Deploy(ctx, Deployment{Service: "checkout", Release: "r-7", Credential: credential}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	running, err := target.ReadRunning(ctx, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	if running.Release != "r-7" {
		t.Fatalf("ReadRunning = %+v, want release r-7", running)
	}
	if err := target.Stop(ctx, "checkout", credential); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	stopped, err := target.ReadRunning(ctx, "checkout", credential)
	if err != nil {
		t.Fatalf("ReadRunning: %v", err)
	}
	if stopped.Release != "" {
		t.Fatalf("ReadRunning = %+v, want nothing running", stopped)
	}

	want := []Call{
		{Op: OpDeploy, Service: "checkout", Release: "r-7", Credential: credential},
		{Op: OpReadRunning, Service: "checkout", Credential: credential},
		{Op: OpStop, Service: "checkout", Credential: credential},
		{Op: OpReadRunning, Service: "checkout", Credential: credential},
	}
	got := fake.Calls()
	if len(got) != len(want) {
		t.Fatalf("the fake recorded %d calls, want %d: %+v", len(got), len(want), got)
	}
	for n := range want {
		if got[n] != want[n] {
			t.Errorf("call %d = %+v, want %+v", n+1, got[n], want[n])
		}
	}
}

// TestARecordedCallHoldsAReferenceAndNoValue is where seam 3 and seam 4 meet:
// the credential crosses the seam as a name, so nothing the seam records can
// hold a value even when the caller has one in hand.
func TestARecordedCallHoldsAReferenceAndNoValue(t *testing.T) {
	const value = "sk-the-value-nothing-else-may-see"
	ctx := context.Background()
	credential := secretref.MustNew("deploy.staging")
	fake := NewFake()

	if err := fake.Deploy(ctx, Deployment{Service: "checkout", Release: "r-7", Credential: credential}); err != nil {
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
		"no service":    func() error { return fake.Deploy(ctx, Deployment{Release: "r-7", Credential: credential}) },
		"no release":    func() error { return fake.Deploy(ctx, Deployment{Service: "checkout", Credential: credential}) },
		"no credential": func() error { return fake.Deploy(ctx, Deployment{Service: "checkout", Release: "r-7"}) },
		"stop with no credential": func() error {
			return fake.Stop(ctx, "checkout", secretref.Ref{})
		},
		"read with no credential": func() error {
			_, err := fake.ReadRunning(ctx, "checkout", secretref.Ref{})
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
