// The diff and the version a form moves on, which are arithmetic over forms
// rather than rows: neither reaches a database.
package contract_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/contract"
)

// input is one element of what an interface accepts, and output one of what it
// returns. Which a diff is depends on that and on nothing else.
func input(name, kind string, required bool) contract.Element {
	return contract.Element{
		Name: name, Kind: contract.ElementField, Position: contract.PositionInput,
		Type: kind, Required: required,
	}
}

func output(name, kind string, populated bool) contract.Element {
	return contract.Element{
		Name: name, Kind: contract.ElementField, Position: contract.PositionOutput,
		Type: kind, Populated: populated,
	}
}

func TestInOutputPositionAnAdditionIsCompatibleAndARemovalBreaks(t *testing.T) {
	before := form(contract.KindInterface,
		output("Health.Status", "string", true),
		output("Health.Detail", "string", false),
	)
	for name, after := range map[string]contract.Form{
		"an element removed": form(contract.KindInterface, output("Health.Status", "string", true)),
		"a type changed": form(contract.KindInterface,
			output("Health.Status", "int64", true), output("Health.Detail", "string", false)),
		"a populated element weakened": form(contract.KindInterface,
			output("Health.Status", "string", false), output("Health.Detail", "string", false)),
	} {
		t.Run(name, func(t *testing.T) {
			change := contract.Diff(before, after)
			if !change.Moved() {
				t.Fatal("the form moved and the diff says it did not")
			}
			if len(change.Breaking) == 0 {
				t.Fatalf("%s is not breaking, and every consumer reading that element does: %s",
					name, change.Describe())
			}
		})
	}

	added := contract.Diff(before, form(contract.KindInterface,
		output("Health.Status", "string", true),
		output("Health.Detail", "string", false),
		output("Health.Count", "int64", true),
	))
	if len(added.Breaking) != 0 {
		t.Fatalf("adding an element to what an interface returns breaks %v, and it breaks nobody", added.Breaking)
	}
}

func TestInInputPositionCompatibilityRunsTheOtherWay(t *testing.T) {
	before := form(contract.KindInterface,
		input("Ask.Deep", "bool", false),
		contract.Element{
			Name: "Ask.Status", Kind: contract.ElementField, Position: contract.PositionInput,
			Type: "string", Domain: []string{"ok", "error"},
		},
		contract.Element{
			Name: "Ask.Since", Kind: contract.ElementField, Position: contract.PositionInput,
			Type: "int64", Range: &contract.Range{Low: 0, High: 100},
		},
		contract.Element{
			Name: "Fetch.id", Kind: contract.ElementArgument, Position: contract.PositionInput,
			Type: "string", Required: true,
		},
	)
	with := func(change func(*contract.Form)) contract.Form {
		after := form(contract.KindInterface, slices.Clone(before.Elements)...)
		change(&after)
		return after
	}
	for name, after := range map[string]contract.Form{
		"an element added as required": with(func(f *contract.Form) {
			f.Elements = append(f.Elements, input("Ask.Reason", "string", true))
		}),
		"an accepted value withdrawn": with(func(f *contract.Form) {
			f.Elements[1].Domain = []string{"ok"}
		}),
		"an accepted range narrowed": with(func(f *contract.Form) {
			f.Elements[2].Range = &contract.Range{Low: 0, High: 50}
		}),
		"an argument dropped": with(func(f *contract.Form) {
			f.Elements = f.Elements[:3]
		}),
		"an optional element made required": with(func(f *contract.Form) {
			f.Elements[0].Required = true
		}),
	} {
		t.Run(name, func(t *testing.T) {
			change := contract.Diff(before, after)
			if len(change.Breaking) == 0 {
				t.Fatalf("%s is not breaking, and every caller that sends it breaks: %s", name, change.Describe())
			}
		})
	}

	for name, after := range map[string]contract.Form{
		"an element added as optional": with(func(f *contract.Form) {
			f.Elements = append(f.Elements, input("Ask.Reason", "string", false))
		}),
		"a value admitted": with(func(f *contract.Form) {
			f.Elements[1].Domain = []string{"ok", "error", "unknown"}
		}),
		"a range widened": with(func(f *contract.Form) {
			f.Elements[2].Range = &contract.Range{Low: 0, High: 1000}
		}),
	} {
		t.Run(name, func(t *testing.T) {
			change := contract.Diff(before, after)
			if len(change.Breaking) != 0 {
				t.Fatalf("%s breaks %v, and a caller sending what it sent still fits", name, change.Breaking)
			}
			if !change.Moved() {
				t.Fatalf("%s did not move the form, so the version would not follow it", name)
			}
		})
	}
}

func TestAStoreBreaksOnAPopulatedAdditionAndOnAConstraint(t *testing.T) {
	before := form(contract.KindStore, element("Ledger.ID", "string", true, false))
	added := contract.Diff(before, form(contract.KindStore,
		element("Ledger.ID", "string", true, false),
		element("Ledger.Amount", "int64", true, false),
	))
	if !slices.Equal(added.Breaking, []string{"Ledger.Amount"}) {
		t.Fatalf("breaking is %v, want Ledger.Amount — a store promises forward, and the build being restored does not write it",
			added.Breaking)
	}
	// The same addition on an interface breaks nothing, which is the whole
	// difference between the two promises.
	asInterface := contract.Diff(form(contract.KindInterface, output("Ledger.ID", "string", true)),
		form(contract.KindInterface, output("Ledger.ID", "string", true), output("Ledger.Amount", "int64", true)))
	if len(asInterface.Breaking) != 0 {
		t.Fatalf("the same addition breaks %v on a published interface, and a consumer reads what it reads",
			asInterface.Breaking)
	}

	constrained := form(contract.KindStore, element("Ledger.ID", "string", true, false))
	constrained.Elements[0].NotNull = true
	change := contract.Diff(before, constrained)
	if !slices.Equal(change.Constrained, []string{"Ledger.ID"}) || len(change.Breaking) == 0 {
		t.Fatalf("a not-null constraint added to a store element is %s and breaks %v, and a write declared inside the old range violates it",
			change.Describe(), change.Breaking)
	}

	narrowed := form(contract.KindStore, element("Ledger.ID", "string", true, false))
	narrowed.Elements[0].Domain = []string{"ok"}
	if len(contract.Diff(before, narrowed).Breaking) == 0 {
		t.Fatal("a domain check added to a store element breaks nothing, and the rows a rollback restores need not fit it")
	}
}

func TestTheFirstFormBreaksNothing(t *testing.T) {
	after := form(contract.KindStore,
		element("Ledger.ID", "string", true, false),
		element("Ledger.Amount", "int64", true, false),
	)
	change := contract.Diff(contract.Form{}, after)
	if len(change.Breaking) != 0 {
		t.Fatalf("a contract's first form breaks %v, and there is no earlier build to break", change.Breaking)
	}
	if !change.Moved() {
		t.Fatal("a first form did not move, and the release that publishes it mints a version")
	}
}

func TestAVersionMovesOnlyWhereTheFormDoes(t *testing.T) {
	one := form(contract.KindInterface, output("Health.Status", "string", true))
	// The same elements in the other order are the same form: the order is not
	// part of the identity, so two builds that declare them differently publish
	// one form.
	same := contract.Form{Name: "health", Kind: contract.KindInterface, Elements: []contract.Element{
		output("Health.Status", "string", true),
	}}
	if contract.Diff(one, same).Moved() {
		t.Fatal("an unchanged form moved, so every release would mint a version")
	}
	if got := contract.Diff(one, same).Describe(); got != "the form is unchanged" {
		t.Fatalf("an unchanged form describes itself as %q", got)
	}
}

func TestAMarkMintsAMinor(t *testing.T) {
	before := form(contract.KindInterface, output("Health.Status", "string", true))
	after := form(contract.KindInterface, output("Health.Status", "string", true))
	after.Elements[0].Deprecated = true
	change := contract.Diff(before, after)
	if !slices.Equal(change.Marked, []string{"Health.Status"}) || len(change.Breaking) != 0 {
		t.Fatalf("a mark is %s and breaks %v, and it changes no shape", change.Describe(), change.Breaking)
	}
	if next := contract.FirstVersion.Next(len(change.Breaking) > 0); next != (contract.Semver{Major: 1, Minor: 1}) {
		t.Fatalf("the version moves to %s, want 1.1.0 — a mark is a minor", next)
	}
}

func TestSemverRoundTripsAndThePatchNeverMoves(t *testing.T) {
	v := contract.FirstVersion
	for range 3 {
		v = v.Next(false)
	}
	v = v.Next(true)
	if v != (contract.Semver{Major: 2}) {
		t.Fatalf("three minors and a major from 1.0.0 is %s, want 2.0.0", v)
	}
	parsed, err := contract.ParseSemver(v.String())
	if err != nil || parsed != v {
		t.Fatalf("parsing %s gave %s, %v", v, parsed, err)
	}
	if v.Patch != 0 {
		t.Fatal("the patch moved, and nothing in a form is a patch-level change")
	}
}

func TestDDLListsEveryKindEveryElementKindAndEveryPosition(t *testing.T) {
	joined := strings.Join(contract.DDL, "")
	for _, kind := range contract.Kinds {
		if !strings.Contains(joined, "'"+string(kind)+"'") {
			t.Errorf("the schema does not list kind %q, so the store would admit a contract this package refuses", kind)
		}
	}
	for _, kind := range contract.ElementKinds {
		if !strings.Contains(joined, "'"+string(kind)+"'") {
			t.Errorf("the schema does not list element kind %q", kind)
		}
	}
	for _, position := range contract.Positions {
		if !strings.Contains(joined, "'"+string(position)+"'") {
			t.Errorf("the schema does not list position %q", position)
		}
	}
}

func TestElementSubjectIsStableAcrossVersions(t *testing.T) {
	if got := contract.ElementSubject("con_abc", "Health.Status"); got != "con_abc.Health.Status" {
		t.Fatalf("the subject of an element is %q", got)
	}
}
