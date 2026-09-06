package principal_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
)

// TestTheThreeCallersEachValidate: a component calls as itself, a human as the
// row they say they are, and an agent as its fleet entry plus the dispatch and
// the scope it was dispatched under.
func TestTheThreeCallersEachValidate(t *testing.T) {
	for _, p := range []principal.Principal{
		principal.OfComponent("deployer"),
		principal.OfHuman("hk_one", record.BasisClaimed),
		principal.OfAgent("model/1", "dsp_one", "the item's own repository"),
	} {
		if err := p.Validate(); err != nil {
			t.Errorf("Validate(%s) = %v, want no error", p, err)
		}
	}
}

// TestEveryKindCarriesABasis: the claimed-or-verified field is on every actor,
// so the two constructors that supply the key themselves supply a basis with
// it, and nothing here writes verified.
func TestEveryKindCarriesABasis(t *testing.T) {
	for _, p := range []principal.Principal{
		principal.OfComponent("deployer"),
		principal.OfAgent("model/1", "dsp_one", "the item's own repository"),
	} {
		if p.Actor.Basis != record.BasisClaimed {
			t.Errorf("%s carries basis %q, want %q", p, p.Actor.Basis, record.BasisClaimed)
		}
	}
}

// TestOnlyAnAgentCarriesADispatch: the scope travels with an agent's calls and
// with nobody else's, so a component or a human carrying one is refused rather
// than recorded as though a scope had been checked.
func TestOnlyAnAgentCarriesADispatch(t *testing.T) {
	withScope := principal.OfComponent("deployer")
	withScope.Scope = "everything"
	if err := withScope.Validate(); !errors.Is(err, principal.ErrDispatchOnlyOnAnAgent) {
		t.Errorf("Validate = %v, want ErrDispatchOnlyOnAnAgent", err)
	}

	withoutDispatch := principal.OfAgent("model/1", "", "the item's own repository")
	if err := withoutDispatch.Validate(); !errors.Is(err, principal.ErrAgentNamesNoDispatch) {
		t.Errorf("Validate = %v, want ErrAgentNamesNoDispatch", err)
	}
}

// TestTheRenderingNamesTheScope: what a seam records beside what was asked for
// is this string, so an agent's scope has to be in it.
func TestTheRenderingNamesTheScope(t *testing.T) {
	rendered := principal.OfAgent("model/1", "dsp_one", "the item's own repository").String()
	if !strings.Contains(rendered, "dsp_one") || !strings.Contains(rendered, "the item's own repository") {
		t.Errorf("String() = %q, want the dispatch and the scope in it", rendered)
	}
	if (principal.Principal{}).String() != "nobody" {
		t.Errorf("the zero principal renders %q, want nobody", (principal.Principal{}).String())
	}
}
