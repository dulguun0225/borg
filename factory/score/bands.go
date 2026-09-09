package score

import (
	"math"
	"sort"

	"github.com/dulguun0225/borg/factory/window"
)

// ShippedBandWidth is how wide one band of the number is as the product ships
// it, and with it how far the risk threshold moves in one step: the two are one
// number, so what is fitted here is what an owner reads on the version. Ten
// bands over the scale is what makes a small install's bands hold anything at
// all, and the count beside each is what says how thin one is.
//
// It is a field of the score version [Writer.EnterShipped] writes, read back
// through [Version.BandWidthOrShipped]; this is the value that field starts at.
const ShippedBandWidth = 0.10

// Band is one band of the number with the share of held-out releases whose
// windows failed inside it, and the count of resolved held-out windows behind
// that share. It is the one reading over the number rather than over a factor,
// and the sample is the only place it can be computed: off the sample the gate
// the score asked for is the reason there is no outcome to count.
//
// What it answers is the question nothing else asks: whether the number ranks
// anything at all. Factors argued for, a formula published and every parameter
// calibrated are together consistent with a number no better than a constant,
// and bands whose failure shares do not rise with the number say exactly that.
type Band struct {
	FactorSet FactorSet `json:"factor_set"`
	// Service is the service the band is over, and empty for the factory-wide
	// band. It is published per factor set first, because each set's weights are
	// fitted apart and pooled bands would mix three populations.
	Service string  `json:"service,omitempty"`
	From    float64 `json:"from"`
	To      float64 `json:"to"`
	// Windows is how many resolved held-out windows the share is over: the
	// reading is weakest where the sample is thinnest, and this states it rather
	// than hiding it.
	Windows     int     `json:"windows"`
	FailedShare float64 `json:"failed_share"`
}

// heldOutRelease is one held-out release as the bands read it: the number its
// item was scored at, the set it was scored on, the service, and whether its
// window resolved failed or passed.
type heldOutRelease struct {
	set     FactorSet
	service string
	number  float64
	failed  bool
}

// heldOutReleases is every held-out release whose window closed on evidence. A
// window that timed out or was skipped ruled nothing out and is not one: the
// exit that reports no outcome reports none here either.
func (e *Evidence) heldOutReleases() []heldOutRelease {
	first := map[string]Firing{}
	order := []string{}
	ordered := append([]Firing{}, e.firings...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].At < ordered[j].At })
	for _, f := range ordered {
		if !f.OpenEvent.HeldOut || f.OpenEvent.ItemID == "" {
			continue
		}
		if _, seen := first[f.OpenEvent.ItemID]; seen {
			continue
		}
		first[f.OpenEvent.ItemID] = f
		order = append(order, f.OpenEvent.ItemID)
	}

	var releases []heldOutRelease
	for _, itemID := range order {
		r, released := e.releaseOfItem[itemID]
		if !released || e.marked[r.ID] {
			continue
		}
		w, watched := e.windowOfRelease[r.ID]
		if !watched || (w.Exit != window.ExitPassed && w.Exit != window.ExitFailed) {
			continue
		}
		releases = append(releases, heldOutRelease{
			set:     first[itemID].OpenEvent.FactorSet,
			service: e.serviceOfItem[itemID],
			number:  first[itemID].OpenEvent.Number,
			failed:  w.Exit == window.ExitFailed,
		})
	}
	return releases
}

// bands is the share of held-out releases whose windows failed within each band
// of the number, per factor set and within each set per service and
// factory-wide. A band with nothing in it is not published: an empty band is a
// share of nothing, and printing one would read as a share of zero.
//
// The width is the version's, not this file's: it is the step the threshold
// moves by as well, so a factory reading its bands at one width and stepping
// its threshold at another would publish a reading of one thing and act on
// another.
func (e *Evidence) bands(width float64) []Band {
	if width <= 0 {
		width = ShippedBandWidth
	}
	top := int(math.Ceil(1/width)) - 1
	type key struct {
		set     FactorSet
		service string
		band    int
	}
	counts := map[key]*Band{}
	for _, r := range e.heldOutReleases() {
		band := int(r.number / width)
		if band > top {
			band = top
		}
		scopes := []string{""}
		if r.service != "" {
			scopes = append(scopes, r.service)
		}
		for _, scope := range scopes {
			k := key{set: r.set, service: scope, band: band}
			if counts[k] == nil {
				counts[k] = &Band{
					FactorSet: r.set, Service: scope,
					From: float64(band) * width, To: float64(band+1) * width,
				}
			}
			counts[k].Windows++
			if r.failed {
				counts[k].FailedShare++
			}
		}
	}

	published := make([]Band, 0, len(counts))
	for _, b := range counts {
		b.FailedShare /= float64(b.Windows)
		published = append(published, *b)
	}
	sort.Slice(published, func(i, j int) bool {
		if published[i].FactorSet != published[j].FactorSet {
			return published[i].FactorSet < published[j].FactorSet
		}
		if published[i].Service != published[j].Service {
			return published[i].Service < published[j].Service
		}
		return published[i].From < published[j].From
	})
	return published
}
