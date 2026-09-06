package screenstatemachine_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/screenstatemachine"
)

const driverModule = "module example.com/screen\n\ngo 1.24\n"

// TestDeriveDriversReadsWhatTheBuildNames: a driver is picked out by the screen
// and the state it names, so naming them is the whole of how the build says a
// state is driven, and each pair is read once however many times it is named.
func TestDeriveDriversReadsWhatTheBuildNames(t *testing.T) {
	dir := checkout(t, map[string]string{
		"go.mod": driverModule,
		"drive.go": "package main\n\n" +
			"// drives " + oneScreen + ":empty\n" +
			"func driveEmpty() {}\n\n" +
			"// drives " + oneScreen + ":loading\n" +
			"func driveLoading() {}\n\n" +
			"// drives " + oneScreen + ":loading again\n" +
			"func driveLoadingAgain() {}\n",
		"more/drive.go": "package more\n\n// drives " + oneScreen + ":loaded\nfunc driveLoaded() {}\n",
	})
	derived, err := screenstatemachine.DeriveDrivers(dir)
	if err != nil {
		t.Fatalf("DeriveDrivers: %v", err)
	}
	if derived.CouldNotDerive != "" {
		t.Fatalf("DeriveDrivers could not derive: %s", derived.CouldNotDerive)
	}
	want := []screenstatemachine.Driver{
		{Screen: oneScreen, State: "empty"},
		{Screen: oneScreen, State: "loading"},
		{Screen: oneScreen, State: "loaded"},
	}
	if len(derived.Drivers) != len(want) {
		t.Fatalf("Drivers = %+v, want %+v", derived.Drivers, want)
	}
	for n, one := range want {
		if derived.Drivers[n] != one {
			t.Errorf("Drivers[%d] = %+v, want %+v", n, derived.Drivers[n], one)
		}
	}
	if err := screenstatemachine.CheckDrivers(derived, []screenstatemachine.Machine{oneMachine()}); err != nil {
		t.Errorf("CheckDrivers over a build that drives every state = %v", err)
	}
}

// TestCheckDriversRejectsInBothDirections: a state in force for the build that
// nothing drives, and a driver naming a state no machine in force declares.
func TestCheckDriversRejectsInBothDirections(t *testing.T) {
	dir := checkout(t, map[string]string{
		"go.mod": driverModule,
		"drive.go": "package main\n\n" +
			"// drives " + oneScreen + ":empty\n" +
			"func driveEmpty() {}\n\n" +
			"// drives " + oneScreen + ":printing\n" +
			"func drivePrinting() {}\n",
	})
	derived, err := screenstatemachine.DeriveDrivers(dir)
	if err != nil {
		t.Fatalf("DeriveDrivers: %v", err)
	}
	err = screenstatemachine.CheckDrivers(derived, []screenstatemachine.Machine{oneMachine()})
	if err == nil {
		t.Fatal("CheckDrivers accepted a build that drives neither loading nor loaded and drives printing")
	}
	var notDriven *screenstatemachine.StateNotDrivenError
	if !errors.As(err, &notDriven) {
		t.Errorf("CheckDrivers = %v, want a StateNotDrivenError among the defects", err)
	}
	var notDeclared *screenstatemachine.DriverNotDeclaredError
	if !errors.As(err, &notDeclared) {
		t.Errorf("CheckDrivers = %v, want a DriverNotDeclaredError among the defects", err)
	}
}

// TestDriversCouldNotDeriveIsNotNoDrivers: a build no extractor covers rejects
// as could not derive, because reading it as an empty list would reject every
// state in force for a build nothing was read from.
func TestDriversCouldNotDeriveIsNotNoDrivers(t *testing.T) {
	dir := checkout(t, map[string]string{"main.rs": "fn main() {}\n"})
	derived, err := screenstatemachine.DeriveDrivers(dir)
	if err != nil {
		t.Fatalf("DeriveDrivers: %v", err)
	}
	if derived.CouldNotDerive == "" {
		t.Fatal("a build with no go.mod derived drivers")
	}
	var could *screenstatemachine.DriversCouldNotDeriveError
	if err := screenstatemachine.CheckDrivers(derived, []screenstatemachine.Machine{oneMachine()}); !errors.As(err, &could) {
		t.Errorf("CheckDrivers = %v, want a DriversCouldNotDeriveError and nothing else", err)
	}
}

// TestALongerRunOfHexadecimalIsNotAScreenID: every id is exactly 32 characters
// long, so reading one out of a longer run would name a screen nothing in force
// has.
func TestALongerRunOfHexadecimalIsNotAScreenID(t *testing.T) {
	dir := checkout(t, map[string]string{
		"go.mod":   driverModule,
		"drive.go": "package main\n\n// drives ssm_000000000000000000000000000000012:empty\nfunc drive() {}\n",
	})
	derived, err := screenstatemachine.DeriveDrivers(dir)
	if err != nil {
		t.Fatalf("DeriveDrivers: %v", err)
	}
	if len(derived.Drivers) != 0 {
		t.Errorf("Drivers = %+v, want none", derived.Drivers)
	}
}
