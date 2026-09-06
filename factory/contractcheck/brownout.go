package contractcheck

import (
	"context"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/contract"
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
	// Exit is what the window closed at, and empty while it is open.
	Exit window.Exit
	// EstablishesNothing is a closed window that neither failed nor establishes:
	// it closed passed before its cap, it was skipped, or it reached its cap
	// having received no volume. None of the three licenses the removal and none
	// is a consumer read the derivation missed, so the detector raises nothing
	// for the element and reports why rather than passing over it silently — no
	// later pass can change what this window closed at, so the removal is a
	// human's from there.
	EstablishesNothing bool
}

// Why is why a brownout that ran establishes nothing, in the words the detector
// reports the stall in. It is empty on a window that is open, one that failed,
// and one that establishes.
func (b Brownout) Why() string {
	if !b.EstablishesNothing {
		return ""
	}
	switch b.Exit {
	case window.ExitPassed:
		return "window " + b.WindowID + " closed passed before its cap, and a brownout's window runs to the cap, its evidence being the point"
	case window.ExitTimedOut:
		return "window " + b.WindowID + " reached its cap having received no volume, and a comparison that received none says nothing about the element"
	default:
		return "window " + b.WindowID + " closed " + string(b.Exit) + ", which establishes nothing about the element"
	}
}

// brownout is what the record says about the brownout of one marked element.
func (c *Check) brownout(ctx context.Context, m Marked) (Brownout, error) {
	key, err := intent.Evidence{ContractID: m.Contract.ID, Element: m.Element.Name}.Key()
	if err != nil {
		return Brownout{}, err
	}
	oldest, found, err := c.oldestOnEvidence(ctx, m.Contract.ServiceID, key)
	if err != nil || !found {
		return Brownout{}, err
	}
	return c.window(ctx, oldest)
}

// oldestOnEvidence is the service's oldest release whose item came from an
// intent raised on this evidence, and false where it has none. That release is
// the brownout's: the removal is raised on the same evidence and only after the
// brownout has shipped.
func (c *Check) oldestOnEvidence(ctx context.Context, serviceID, key string) (string, bool, error) {
	highest, found, err := release.Highest(ctx, c.pool, serviceID)
	if err != nil || !found {
		return "", false, err
	}
	releases, err := release.Between(ctx, c.pool, serviceID, 1, highest.Number)
	if err != nil {
		return "", false, err
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
			return "", false, err
		}
		if it.IntentID == "" {
			continue
		}
		in, err := intent.Get(ctx, c.pool, it.IntentID)
		if err != nil {
			return "", false, err
		}
		if in.Evidence == key {
			return rel.ID, true, nil
		}
	}
	return "", false, nil
}

// BrownoutOf is what a brownout's release is the brownout of: the contract and
// the marked element its evidence names.
type BrownoutOf struct {
	ContractID string
	Element    string
}

// IsBrownout is whether a release is a brownout of a marked element, and of
// which. It is the reading the health monitor needs of this component, because a
// brownout's window is not an ordinary window in two ways and neither is
// readable from the window's own record: it runs to the cap rather than stopping
// where the boundary would allow, the way the held-out sample's does, its
// evidence being the point; and it is the one window that reads more than the
// producer's own numbers, any service crossing the reading against its own
// recent history while it is open failing it.
//
// The walk is the same one [Check.brownout] takes, in the other direction:
// nothing writes a link from the element to the release, so what says a release
// is a brownout is the intent's evidence — keyed by the contract and the element
// — the item that names that intent, and the release that names the item. A
// later release on the same evidence is the removal and not the brownout, and a
// release whose element is no longer marked is neither.
func (c *Check) IsBrownout(ctx context.Context, releaseID string) (BrownoutOf, bool, error) {
	rel, err := release.Get(ctx, c.pool, releaseID)
	if err != nil || rel.ItemID == "" {
		return BrownoutOf{}, false, err
	}
	it, err := item.Get(ctx, c.pool, rel.ItemID)
	if err != nil || it.IntentID == "" {
		return BrownoutOf{}, false, err
	}
	in, err := intent.Get(ctx, c.pool, it.IntentID)
	if err != nil || in.Evidence == "" {
		return BrownoutOf{}, false, err
	}

	of, named, err := c.markedOnEvidence(ctx, rel.ServiceID, in.Evidence)
	if err != nil || !named {
		return BrownoutOf{}, false, err
	}
	oldest, found, err := c.oldestOnEvidence(ctx, rel.ServiceID, in.Evidence)
	if err != nil || !found || oldest != releaseID {
		return BrownoutOf{}, false, err
	}
	return of, true, nil
}

// markedOnEvidence is the marked element of this service whose evidence key is
// the one given, and false where no marked element has it — an intent the
// factory raised on some other evidence, or one whose element the removal has
// since dropped.
func (c *Check) markedOnEvidence(ctx context.Context, serviceID, key string) (BrownoutOf, bool, error) {
	contracts, err := contract.OfService(ctx, c.pool, serviceID)
	if err != nil {
		return BrownoutOf{}, false, err
	}
	for _, con := range contracts {
		version, hasOne, err := contract.NewestVersion(ctx, c.pool, con.ID)
		if err != nil {
			return BrownoutOf{}, false, err
		}
		if !hasOne {
			continue
		}
		form, err := contract.FormOf(ctx, c.pool, con, version.ID)
		if err != nil {
			return BrownoutOf{}, false, err
		}
		for _, element := range form.Marked() {
			marked, err := intent.Evidence{ContractID: con.ID, Element: element}.Key()
			if err != nil {
				return BrownoutOf{}, false, err
			}
			if marked == key {
				return BrownoutOf{ContractID: con.ID, Element: element}, true, nil
			}
		}
	}
	return BrownoutOf{}, false, nil
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
	ran.Exit = w.Exit
	ran.Establishes = w.Exit == window.ExitTimedOut && (counts.Units > 0 || counts.BaselineUnits > 0)
	ran.EstablishesNothing = !ran.Open && !ran.Failed && !ran.Establishes
	return ran, nil
}
