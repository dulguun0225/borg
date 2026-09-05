package contractcheck

import (
	"context"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/window"
)

// The brownout is why the emptied list raises no removal by itself. The list is
// derived from builds, and a read through reflection or configuration empties a
// list its consumer is still on — so the last inference before something is
// destroyed is checked against the running world instead of trusted.
//
// The brownout is an item of the producer whose release disables the marked
// element in behaviour while leaving the form unchanged. It ships, deploys and
// rolls back like any release, and its window runs to the cap rather than stopping
// where the boundary would allow, its evidence being the point. What the detector
// reads here is that window.
//
// Nothing writes a link from the element to the release: the walk is the intent's
// own evidence. The brownout intent is keyed by the contract and the element, an
// item names the intent it was decomposed from, and a release names the item — so
// the producer's oldest release whose item came from that intent is the brownout's,
// and the window over it is the one this reads.

// Brownout is what the record says about the brownout of one marked element.
type Brownout struct {
	// Ran is whether a release of the producer came from the intent raised on
	// this element's evidence. False is an element whose brownout has not shipped,
	// which is what the detector raises.
	Ran       bool
	ReleaseID string
	WindowID  string
	// Open is whether its window has not closed. A brownout still open establishes
	// nothing yet, so no removal follows it.
	Open bool
	// Failed is whether the window closed failed, which is a consumer read the
	// derivation missed. The detector raises neither item again for such an
	// element.
	Failed bool
	// Establishes is whether the window reached its cap uncrossed having received
	// volume, which is the one reading that licenses the removal: a comparison
	// that received none says nothing about the element.
	Establishes bool
}

// brownout is what the record says about the brownout of one marked element.
func (c *Check) brownout(ctx context.Context, m Marked) (Brownout, error) {
	key, err := intent.Evidence{ContractID: m.Contract.ID, Element: m.Element.Name}.Key()
	if err != nil {
		return Brownout{}, err
	}
	highest, found, err := release.Highest(ctx, c.pool, m.Contract.ServiceID)
	if err != nil || !found {
		return Brownout{}, err
	}
	releases, err := release.Between(ctx, c.pool, m.Contract.ServiceID, 1, highest.Number)
	if err != nil {
		return Brownout{}, err
	}
	for _, rel := range releases {
		if rel.ItemID == "" {
			// A release minted over an accepted commit is one no gate decided,
			// and every traversal from it ends at the acceptance rather than at
			// an intent.
			continue
		}
		it, err := item.Get(ctx, c.pool, rel.ItemID)
		if err != nil {
			return Brownout{}, err
		}
		if it.IntentID == "" {
			continue
		}
		in, err := intent.Get(ctx, c.pool, it.IntentID)
		if err != nil {
			return Brownout{}, err
		}
		if in.Evidence != key {
			continue
		}
		// The oldest release on this evidence is the brownout's: the removal is
		// raised on the same evidence and only after the brownout has shipped.
		return c.window(ctx, rel.ID)
	}
	return Brownout{}, nil
}

// window is what the window over the brownout's release says.
func (c *Check) window(ctx context.Context, releaseID string) (Brownout, error) {
	ran := Brownout{Ran: true, ReleaseID: releaseID}
	w, found, err := window.ForRelease(ctx, c.pool, releaseID)
	if err != nil || !found {
		// A brownout that shipped and whose window has not opened yet establishes
		// nothing, which is the same answer as one still open.
		ran.Open = true
		return ran, err
	}
	ran.WindowID = w.ID
	ran.Open = w.Open()
	ran.Failed = w.Exit == window.ExitFailed
	// Volume is nothing only where both arms received none: a release arm
	// silent beside a control that is serving is itself a crossing, not an
	// absence of volume. Summed across every quantity the window read, either
	// arm counting anything at all is what "received volume" means.
	var counts boundary.Counts
	for _, c := range w.ClosedOn.Quantities {
		counts = counts.Add(c)
	}
	ran.Establishes = w.Exit == window.ExitTimedOut && (counts.Units > 0 || counts.BaselineUnits > 0)
	return ran, nil
}
