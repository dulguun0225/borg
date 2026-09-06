package criterion

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// MutationToolchain is the toolchain this extractor mutates. A build in
// another toolchain has no extractor here and reads could not derive, the
// arrangement [Derive] already has for the encodings.
const MutationToolchain = "go"

// Mutation is what mutating a build produced: which toolchain's extractor ran,
// which tool it ran, what the coverage of the run under mutation was, how many
// mutants were tested and how many the encodings detected, and the reason
// nothing could be derived.
//
// It is a record and not a number, because "the encodings caught none of the
// seeded defects" and "nothing was mutated" call for opposite responses: the
// first is a mutation score of zero, which the floor rejects on the terms an
// undecided criterion is rejected on, and the second is could not derive,
// which never passes and takes the same treatment at Merge to master.
type Mutation struct {
	// Toolchain is the extractor that ran, and is empty where none did.
	Toolchain string
	// Tool is the mutation tool the checkout named, and is empty where it
	// named none or none ran.
	Tool string
	// Coverage is what the run under mutation covered, in the words the
	// build's readers are shown. It is the coverage of the checkout's own test
	// run, which is what the mutants are re-run under, and it stands whether
	// or not the mutation itself could be derived.
	Coverage string
	// MutantsTested is how many mutants the tool produced and re-ran the
	// encodings against, and MutantsDetected is how many of them an encoding
	// failed on. Both are nought where the derivation could not be made.
	MutantsTested   int
	MutantsDetected int
	// CouldNotDerive is why no score was produced, and is empty where the
	// mutation ran.
	CouldNotDerive string
}

// Derived reports whether the mutation produced a score at all.
func (m Mutation) Derived() bool { return m.CouldNotDerive == "" }

// Score is the share of the mutants the encodings detected, derived at the
// read and never stored, so the number cannot disagree with the two counts it
// is made of. It is nought on a derivation that could not be made, which is
// not a reading of zero: [Mutation.Derived] is what tells the two apart.
func (m Mutation) Score() float64 {
	if !m.Derived() || m.MutantsTested == 0 {
		return 0
	}
	return float64(m.MutantsDetected) / float64(m.MutantsTested)
}

// Blocks says whether the reading stops the item at the Merge to master gate,
// where the mutation floor is read. A score below the floor rejects there on
// the terms an undecided criterion does, and a build the factory could not
// mutate reads could not derive, which never passes and takes that same
// treatment.
func (m Mutation) Blocks(floor float64) bool {
	if !m.Derived() {
		return true
	}
	return m.Score() < floor
}

// mutationCounts is how a mutation tool states what it did. The factory writes
// the services it builds, so the tool a checkout names states its two counts in
// lines of this form, and a tool whose output states neither reads as could not
// derive rather than as a score of zero.
var mutationCounts = map[string]*regexp.Regexp{
	"tested":   regexp.MustCompile(`(?i)mutants tested:[ \t]*([0-9]+)`),
	"detected": regexp.MustCompile(`(?i)mutants detected:[ \t]*([0-9]+)`),
}

// toolDirective is a tool directive of go.mod, which is how a Go checkout names
// a tool it runs: `tool <module path>`, alone or inside a `tool ( … )` block.
// The name `go tool` runs it under is the last element of that path.
var toolDirective = regexp.MustCompile(`(?m)^[ \t]*(?:tool[ \t]+)?([A-Za-z0-9._~-]+(?:/[A-Za-z0-9._~-]+)+)[ \t]*$`)

// coverageOf reads what `go test -cover` reported over the checkout, which is
// what the mutants are re-run under.
var coverageOf = regexp.MustCompile(`coverage:[ \t]*([0-9.]+)% of statements`)

// DeriveMutation is the mutation score of a checkout of the build: the share of
// the seeded defects the encodings caught. Go is the one toolchain with an
// extractor, recognised by a go.mod at the root of the checkout, and every
// other build is could not derive.
//
// It reads the coverage of the checkout's own test run with `go test -cover`,
// and it mutates only where the checkout names a mutation tool — a tool
// directive of go.mod, which is what `go tool <name>` runs. A checkout that
// names none is a service the factory cannot mutate, which reads could not
// derive and never passes, rather than a score of zero the floor would reject
// as if the encodings had missed every defect.
//
// Its caller is the deployer at the candidate run, which mutates the lines the
// diff touches and re-runs the encodings; that caller is not built, so the
// mutant cap authored on the service record is not read here — this derivation
// runs what the tool runs, and the cap bounds what the deployer deploys.
func DeriveMutation(ctx context.Context, dir string) (Mutation, error) {
	if dir == "" {
		return Mutation{}, fmt.Errorf("criterion: the mutation derivation needs a directory")
	}
	goMod := filepath.Join(dir, "go.mod")
	declared, err := os.ReadFile(goMod)
	if err != nil {
		return Mutation{
			CouldNotDerive: "no extractor covers this build: the factory ships one for Go, " +
				"recognised by a go.mod at the root of the checkout",
		}, nil
	}

	m := Mutation{Toolchain: MutationToolchain, Coverage: coverage(ctx, dir)}

	tool, named := mutationTool(string(declared))
	if !named {
		m.CouldNotDerive = "the checkout names no mutation tool in a tool directive of go.mod, " +
			"so this build cannot be mutated"
		return m, nil
	}
	m.Tool = tool

	run := exec.CommandContext(ctx, "go", "tool", tool, "./...")
	run.Dir = dir
	out, err := run.CombinedOutput()
	if err != nil {
		m.CouldNotDerive = fmt.Sprintf("the mutation tool %s did not run in %s: %v: %s",
			tool, dir, err, strings.TrimSpace(string(out)))
		return m, nil
	}

	tested, statedTested := count(string(out), mutationCounts["tested"])
	detected, statedDetected := count(string(out), mutationCounts["detected"])
	switch {
	case !statedTested || !statedDetected:
		m.CouldNotDerive = fmt.Sprintf("the mutation tool %s stated no mutants tested and detected: %s",
			tool, strings.TrimSpace(string(out)))
	case tested == 0:
		m.CouldNotDerive = fmt.Sprintf("the mutation tool %s tested no mutants", tool)
	case detected > tested:
		m.CouldNotDerive = fmt.Sprintf("the mutation tool %s stated %d mutants detected of %d tested",
			tool, detected, tested)
	default:
		m.MutantsTested, m.MutantsDetected = tested, detected
	}
	return m, nil
}

// mutationTool is the name of the first tool the checkout names that this
// extractor knows to run for mutation, and false where it names none. The name
// is the last element of the module path, which is what `go tool` runs it
// under.
//
// Which tools those are is [MutationTools], a list of names and not a
// derivation: what a checkout names is the checkout's, and a tool this
// extractor has never heard of would be run with arguments it does not take.
func mutationTool(goMod string) (string, bool) {
	for _, match := range toolDirective.FindAllStringSubmatch(goMod, -1) {
		name := match[1]
		if slash := strings.LastIndexByte(name, '/'); slash >= 0 {
			name = name[slash+1:]
		}
		for _, known := range MutationTools {
			if name == known {
				return name, true
			}
		}
	}
	return "", false
}

// MutationTools is every mutation tool the Go extractor knows to run, by the
// name `go tool` runs it under — the last element of the module path in a tool
// directive of go.mod. A build whose checkout names none of them is one the
// factory cannot mutate.
var MutationTools = []string{"mutate", "go-mutesting", "ooze"}

// count is one of the two counts a mutation tool states, and false where the
// output states none.
func count(out string, pattern *regexp.Regexp) (int, bool) {
	match := pattern.FindStringSubmatch(out)
	if match == nil {
		return 0, false
	}
	n, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

// coverage is what `go test -cover` reported over the checkout, in one
// sentence. A test that fails is still a coverage reading, so the command's
// exit status is not read: what is reported is what it printed, and a run that
// printed no coverage at all says so with the command's own words.
func coverage(ctx context.Context, dir string) string {
	cmd := exec.CommandContext(ctx, "go", "test", "-cover", "./...")
	cmd.Dir = dir
	out, _ := cmd.CombinedOutput()

	readings := coverageOf.FindAllStringSubmatch(string(out), -1)
	if len(readings) == 0 {
		return "go test -cover reported no coverage over this checkout: " +
			strings.TrimSpace(string(out))
	}
	lowest, highest := readings[0][1], readings[0][1]
	for _, reading := range readings {
		if less(reading[1], lowest) {
			lowest = reading[1]
		}
		if less(highest, reading[1]) {
			highest = reading[1]
		}
	}
	if lowest == highest {
		return fmt.Sprintf("go test -cover over the checkout: %d package(s) reported %s%% of statements covered",
			len(readings), lowest)
	}
	return fmt.Sprintf("go test -cover over the checkout: %d package(s) reported coverage, from %s%% to %s%% of statements",
		len(readings), lowest, highest)
}

// less compares two coverage readings as numbers, and as text where either is
// not one.
func less(a, b string) bool {
	x, errA := strconv.ParseFloat(a, 64)
	y, errB := strconv.ParseFloat(b, 64)
	if errA != nil || errB != nil {
		return a < b
	}
	return x < y
}
