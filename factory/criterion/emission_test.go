package criterion_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/criterion"
)

func emissionSource(names []string, hazardousOperation string) string {
	var fields []string
	for _, name := range names {
		fields = append(fields, "\tField_"+strings.ReplaceAll(name, "_", "")+" string `json:\""+name+"\"`")
	}
	source := "package main\n\ntype emission struct {\n" + strings.Join(fields, "\n") + "\n}\n"
	if hazardousOperation != "" {
		source += "\nvar hazardous = emission{Kind: \"hazardous_operation\", Operation: \"" + hazardousOperation + "\"}\n"
	}
	return source
}

func TestEmissionDerivationAcceptsCompleteReadableShape(t *testing.T) {
	names := []string{"version", "kind", "time", "service", "build", "deploy", "target", "operation", "outcome", "duration"}
	dir := checkout(t, map[string]string{"emission.go": emissionSource(names, "charge")})
	derived := derive(t, dir)

	if got := derived.Emission.Names; len(got) != len(names) {
		t.Fatalf("Derive emission names = %v, want %v", got, names)
	}
	previous := criterion.Emission{Names: names}
	if err := criterion.CheckEmission(derived, previous, names, "charge"); err != nil {
		t.Fatalf("CheckEmission = %v, want nil", err)
	}
}

func TestEmissionGateDoesNotRequireEveryReadableName(t *testing.T) {
	names := []string{"version", "kind", "time", "service", "build", "deploy", "target", "operation"}
	dir := checkout(t, map[string]string{"emission.go": emissionSource(names, "")})

	readable := append(append([]string{}, names...), "outcome")
	if err := criterion.CheckEmission(derive(t, dir), criterion.Emission{Names: names}, readable, ""); err != nil {
		t.Fatalf("CheckEmission = %v, want nil when a readable name is not emitted", err)
	}
	previous := criterion.Emission{Names: append(append([]string{}, names...), "legacy")}
	if err := criterion.CheckEmission(derive(t, dir), previous, names, ""); err != nil {
		t.Fatalf("CheckEmission = %v, want nil when the previous-only name is unreadable", err)
	}
	if err := criterion.CheckEmission(derive(t, dir), criterion.Emission{Names: append(names, "outcome")}, readable, ""); err == nil {
		t.Fatal("CheckEmission = nil, want a stopped-emission error")
	}
}

func TestEmissionGateUsesReadableDirectionWithoutPreviousEmission(t *testing.T) {
	names := []string{"version", "kind", "time", "service", "build", "deploy", "target", "operation"}
	declared := append(append([]string{}, names...), "extra")
	dir := checkout(t, map[string]string{"emission.go": emissionSource(declared, "")})

	err := criterion.CheckEmission(derive(t, dir), criterion.Emission{CouldNotDerive: "previous unavailable"}, names, "")
	var unreadable *criterion.EmissionUnreadableError
	if !errors.As(err, &unreadable) {
		t.Fatalf("CheckEmission = %v, want an unreadable-name error", err)
	}
}

func TestEmissionGateRejectsStoppedEmission(t *testing.T) {
	names := []string{"version", "kind", "time", "service", "build", "deploy", "target", "operation"}
	dir := checkout(t, map[string]string{"emission.go": emissionSource(names, "")})

	readable := append(append([]string{}, names...), "outcome")
	err := criterion.CheckEmission(derive(t, dir), criterion.Emission{Names: append(names, "outcome")}, readable, "")
	var stopped *criterion.EmissionStoppedError
	if !errors.As(err, &stopped) {
		t.Fatalf("CheckEmission = %v, want a stopped-emission error", err)
	}
	if stopped.Name != "outcome" {
		t.Errorf("stopped name = %q, want outcome", stopped.Name)
	}
}

func TestEmissionGateRejectsUnreadableName(t *testing.T) {
	names := []string{"version", "kind", "time", "service", "build", "deploy", "target", "operation"}
	declared := append(append([]string{}, names...), "extra")
	dir := checkout(t, map[string]string{"emission.go": emissionSource(declared, "")})

	err := criterion.CheckEmission(derive(t, dir), criterion.Emission{Names: names}, names, "")
	var unreadable *criterion.EmissionUnreadableError
	if !errors.As(err, &unreadable) {
		t.Fatalf("CheckEmission = %v, want an unreadable-name error", err)
	}
	if unreadable.Name != "extra" {
		t.Errorf("unreadable name = %q, want extra", unreadable.Name)
	}
}

func TestEmissionGateRejectsMissingHazardousOperation(t *testing.T) {
	names := []string{"version", "kind", "time", "service", "build", "deploy", "target", "operation"}
	dir := checkout(t, map[string]string{"emission.go": emissionSource(names, "")})

	err := criterion.CheckEmission(derive(t, dir), criterion.Emission{Names: names}, names, "charge")
	var missing *criterion.HazardousOperationMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("CheckEmission = %v, want a hazardous-operation error", err)
	}
	if missing.Operation != "charge" {
		t.Errorf("missing hazardous operation = %q, want charge", missing.Operation)
	}
}

func TestEmissionDerivationTreatsNoShapeAsEmptyEmission(t *testing.T) {
	dir := checkout(t, map[string]string{"service.go": "package main\n\ntype service struct{}\n"})
	derived := derive(t, dir)

	if derived.Emission.CouldNotDerive != "" {
		t.Fatalf("Derive emission could-not-derive = %q, want empty", derived.Emission.CouldNotDerive)
	}
	if len(derived.Emission.Names) != 0 || len(derived.Emission.HazardousOperations) != 0 {
		t.Fatalf("Derive empty emission = %+v, want no names or hazardous operations", derived.Emission)
	}
	if err := criterion.CheckEmission(derived, criterion.Emission{}, nil, ""); err != nil {
		t.Fatalf("CheckEmission for an empty emission = %v, want nil", err)
	}
}

func TestEmissionDerivationReadsNoTestFile(t *testing.T) {
	names := []string{"version", "kind", "time", "service", "build", "deploy", "target", "operation"}
	dir := checkout(t, map[string]string{"emission_test.go": emissionSource(names, "charge")})
	derived := derive(t, dir)

	if derived.Emission.CouldNotDerive != "" || len(derived.Emission.Names) != 0 || len(derived.Emission.HazardousOperations) != 0 {
		t.Fatalf("Derive emission over a _test.go file = %+v, want an empty emission", derived.Emission)
	}
}

func TestEmissionDerivationCouldNotDeriveOnParseFailure(t *testing.T) {
	dir := checkout(t, map[string]string{
		"emission.go": emissionSource([]string{"version", "kind", "time", "service", "build", "deploy", "target", "operation"}, ""),
		"broken.go":   "package main\nfunc broken( {\n",
	})
	derived := derive(t, dir)
	if derived.Emission.CouldNotDerive == "" {
		t.Fatal("Derive emission could-not-derive = empty, want a parse failure")
	}
	if err := criterion.CheckEmission(derived, criterion.Emission{}, nil, ""); err == nil {
		t.Fatal("CheckEmission = nil, want could-not-derive")
	}
}

func TestEmissionDerivationRefusesTwoShapes(t *testing.T) {
	names := []string{"version", "kind", "time", "service", "build", "deploy", "target", "operation"}
	dir := checkout(t, map[string]string{
		"a.go": emissionSource(names, ""),
		"b.go": emissionSource(names, ""),
	})
	derived := derive(t, dir)
	if derived.Emission.CouldNotDerive == "" {
		t.Fatal("Derive emission could-not-derive = empty, want two-shape failure")
	}
}
