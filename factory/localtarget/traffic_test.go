package localtarget_test

import (
	"errors"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// TestThePlatformRunsAControlAndKeepsItForRollback: the target writes one
// traffic file both builds receive, and a controlled deploy leaves the replaced
// build beside the release. Repeated and full shifts reuse that process.
func TestThePlatformRunsAControlAndKeepsItForRollback(t *testing.T) {
	ctx := t.Context()
	local, dir := newTarget(t, "checkout")
	buildProgram(t, dir, "rel_one", sleeperSource)
	buildProgram(t, dir, "rel_two", sleeperSource)
	if _, err := local.Deploy(ctx, deployer, targetseam.Deployment{
		Service: "checkout", Build: "rel_one", Credential: credential, Configuration: deployIDConfig("dep_1"),
	}); err != nil {
		t.Fatalf("Deploy rel_one: %v", err)
	}
	oldRunning, err := os.ReadFile(localtarget.RunningFile(dir, "checkout"))
	if err != nil {
		t.Fatalf("reading the running control before deploy: %v", err)
	}
	if _, err := local.DeployWithControl(ctx, deployer, targetseam.Deployment{
		Service: "checkout", Build: "rel_two", Credential: credential, Configuration: deployIDConfig("dep_2"),
	}); err != nil {
		t.Fatalf("DeployWithControl rel_two: %v", err)
	}
	for _, file := range []string{localtarget.ControlFile(dir, "checkout"), localtarget.KeptFile(dir, "checkout")} {
		got, err := os.ReadFile(file)
		if err != nil || string(got) != string(oldRunning) {
			t.Errorf("%s = %q, %v, want the already-running control %q", file, got, err, oldRunning)
		}
	}
	if err := local.ShiftTraffic(ctx, deployer, targetseam.Shift{
		Service: "checkout", Build: "rel_two", Share: 0.1, Credential: credential,
	}); err != nil {
		t.Fatalf("ShiftTraffic: %v", err)
	}
	traffic, err := os.ReadFile(localtarget.TrafficFile(dir, "checkout"))
	fields := strings.Fields(string(traffic))
	var share, controlShare float64
	var parseErr, controlParseErr error
	if len(fields) >= 2 {
		share, parseErr = strconv.ParseFloat(fields[1], 64)
	}
	if len(fields) >= 4 {
		controlShare, controlParseErr = strconv.ParseFloat(fields[3], 64)
	}
	if err != nil || parseErr != nil || controlParseErr != nil || len(fields) != 4 || fields[0] != "rel_two" || fields[2] != "rel_one" || math.Abs(share-0.1) > 1e-12 || math.Abs(controlShare-0.9) > 1e-12 {
		t.Fatalf("traffic file = %q, %v, want rel_two at share 0.1", traffic, err)
	}
	if _, err := os.Stat(localtarget.ControlFile(dir, "checkout")); err != nil {
		t.Fatalf("control process was not recorded: %v", err)
	}
	if _, err := os.Stat(localtarget.KeptFile(dir, "checkout")); err != nil {
		t.Fatalf("kept process was not recorded: %v", err)
	}
	controlBefore, err := os.ReadFile(localtarget.ControlFile(dir, "checkout"))
	if err != nil {
		t.Fatalf("reading control before second shift: %v", err)
	}
	if err := local.ShiftTraffic(ctx, deployer, targetseam.Shift{
		Service: "checkout", Build: "rel_two", Share: 0.2, Credential: credential,
	}); err != nil {
		t.Fatalf("ShiftTraffic second: %v", err)
	}
	controlAfter, err := os.ReadFile(localtarget.ControlFile(dir, "checkout"))
	if err != nil || string(controlAfter) != string(controlBefore) {
		t.Errorf("control after second shift = %q, %v, want the same process %q", controlAfter, err, controlBefore)
	}
	if err := local.ShiftTraffic(ctx, deployer, targetseam.Shift{
		Service: "checkout", Build: "rel_two", Share: 1, Credential: credential,
	}); err != nil {
		t.Fatalf("ShiftTraffic full: %v", err)
	}
	if _, err := os.Stat(localtarget.ControlFile(dir, "checkout")); err != nil {
		t.Errorf("control after full shift = %v, want still recorded", err)
	}
	if _, err := os.Stat(localtarget.KeptFile(dir, "checkout")); err != nil {
		t.Errorf("kept process after full shift = %v, want still running", err)
	}
	if _, err := local.Reconfigure(ctx, deployer, targetseam.Reconfiguration{
		Service: "checkout", Build: "rel_one", Credential: credential, Configuration: deployIDConfig("dep_3"),
	}); err != nil {
		t.Fatalf("Reconfigure kept: %v", err)
	}
	if err := local.ShiftTraffic(ctx, deployer, targetseam.Shift{
		Service: "checkout", Build: "rel_one", Share: 1, Credential: credential,
	}); err != nil {
		t.Fatalf("ShiftTraffic rollback: %v", err)
	}
	if _, err := os.Stat(localtarget.ControlFile(dir, "checkout")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("control after rollback = %v, want ended", err)
	}
	running, err := local.ReadRunning(ctx, deployer, "checkout", credential)
	if err != nil || running.Build != "rel_one" {
		t.Errorf("running after rollback shift = %+v, %v, want kept rel_one", running, err)
	}
	if err := local.SetInstanceCount(ctx, deployer, targetseam.InstanceCount{
		Service: "checkout", Build: "rel_one", Count: 3, Credential: credential,
	}); !errors.Is(err, localtarget.ErrOneInstance) {
		t.Errorf("SetInstanceCount(3) = %v, want ErrOneInstance", err)
	}
	if err := local.SetInstanceCount(ctx, deployer, targetseam.InstanceCount{
		Service: "checkout", Build: "rel_one", Count: 1, Credential: credential,
	}); err != nil {
		t.Errorf("SetInstanceCount(1) = %v, want the count this platform already runs", err)
	}
}

func TestStopControlWithoutARecordedControlIsIdempotent(t *testing.T) {
	local, _ := newTarget(t, "checkout")
	if err := local.StopControl(t.Context(), deployer, "checkout"); err != nil {
		t.Fatalf("StopControl without a recorded control: %v", err)
	}
}
