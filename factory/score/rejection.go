package score

import (
	"sort"

	"github.com/dulguun0225/borg/factory/record"
)

// How a rejection resolved, in the words the version publishes. A human's
// rejection is an input the threshold falls on, and nothing in the record can
// bear one out the way a window bears out an approval: the rejected version
// never ships, so a rework that then passed is consistent with the rejection
// having been right and with it having been a false alarm. What the record does
// hold is how the rejection resolved, and the threshold reads a rejection only
// once it has.
const (
	// ResolvedReAuthoredApproved is the re-authored version approved, differing
	// by content digest in what the rejection named.
	ResolvedReAuthoredApproved = "the re-authored version was approved and differs by content digest"
	// ResolvedApprovedUnchanged is approval without differing there, which is
	// read as a false alarm: it moves nothing and is published per human.
	ResolvedApprovedUnchanged = "the same version was approved without differing in what the rejection named"
	// ResolvedRejectedAgain is a second rejection.
	ResolvedRejectedAgain = "the version was rejected again"
	// ResolvedAttemptLimit is the item reaching the attempt limit.
	ResolvedAttemptLimit = "the item reached the attempt limit"
)

// Rejection is one human's rejection and how it resolved. An unresolved
// rejection moves nothing: the threshold moves late, at the rejection's
// resolution rather than at the rejection.
type Rejection struct {
	ItemID     string       `json:"item_id"`
	ArtifactID string       `json:"artifact_id"`
	Gate       string       `json:"gate"`
	By         record.Actor `json:"by"`
	// Named is what the human named in the rejection, which is what the
	// re-authored version is compared against.
	Named string `json:"named"`
	// Resolution is one of the four above, and empty while the rejection has
	// resolved no way at all.
	Resolution string `json:"resolution"`
}

// MovesTheThreshold reports whether this rejection is read as a gate the factory
// needed. The first, third and fourth resolutions are; the second is a false
// alarm and moves nothing.
func (r Rejection) MovesTheThreshold() bool {
	switch r.Resolution {
	case ResolvedReAuthoredApproved, ResolvedRejectedAgain, ResolvedAttemptLimit:
		return true
	}
	return false
}

// FalseAlarm reports whether this rejection resolved as one.
func (r Rejection) FalseAlarm() bool { return r.Resolution == ResolvedApprovedUnchanged }

// FalseAlarm is how many rejections of one human resolved as false alarms,
// published on the version beside what each approved and how often it was
// undone. Without it, rejecting would be the response that costs the person
// nothing, and at the same time a lever on the one parameter that decides how
// much human work the factory removes.
type FalseAlarm struct {
	Human string `json:"human"`
	Count int    `json:"count"`
	// Rejections is how many rejections by this human have resolved at all,
	// which is what the count is a share of.
	Rejections int `json:"rejections"`
}

// resolvedRejections is every human rejection in the evidence with the way it
// resolved, in the order the rejections were closed. A rejection resolves one of
// four ways and moves nothing until it has.
func (e *Evidence) resolvedRejections() []Rejection {
	ordered := append([]Firing{}, e.firings...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].At < ordered[j].At })

	var rejections []Rejection
	for i, f := range ordered {
		if !f.HumanClosed || f.CloseEvent.Verdict != VerdictRejected || f.OpenEvent.ItemID == "" {
			continue
		}
		r := Rejection{
			ItemID: f.OpenEvent.ItemID, ArtifactID: f.OpenEvent.ArtifactID,
			Gate: f.OpenEvent.Gate, By: f.ClosedBy, Named: f.CloseEvent.RejectionNamed,
		}
		r.Resolution = e.resolutionOf(r, ordered[i+1:])
		rejections = append(rejections, r)
	}
	return rejections
}

// resolutionOf is how one rejection resolved, read off what happened on that
// item after it. The digest is what says the re-authored version differs from
// the one the rejection named: a version approved under the same digest is the
// same text, which is the false alarm the design measures per human.
func (e *Evidence) resolutionOf(r Rejection, later []Firing) string {
	rejectedDigest := e.digests[r.ArtifactID]
	for _, f := range later {
		if f.OpenEvent.ItemID != r.ItemID || !f.HumanClosed {
			continue
		}
		switch f.CloseEvent.Verdict {
		case VerdictRejected:
			return ResolvedRejectedAgain
		case VerdictApproved:
			digest := e.digests[f.OpenEvent.ArtifactID]
			if digest != "" && rejectedDigest != "" && digest != rejectedDigest {
				return ResolvedReAuthoredApproved
			}
			return ResolvedApprovedUnchanged
		}
	}
	if e.reachedTheAttemptLimit(r.ItemID) {
		return ResolvedAttemptLimit
	}
	return ""
}

// falseAlarms is how many rejections resolved as false alarms, per human, over
// the rejections of that human that have resolved at all.
func (e *Evidence) falseAlarms() []FalseAlarm {
	counts := map[string]*FalseAlarm{}
	for _, r := range e.resolvedRejections() {
		if r.Resolution == "" {
			continue
		}
		key := r.By.Key
		if counts[key] == nil {
			counts[key] = &FalseAlarm{Human: key}
		}
		counts[key].Rejections++
		if r.FalseAlarm() {
			counts[key].Count++
		}
	}
	var published []FalseAlarm
	for _, key := range sortedKeys(counts) {
		published = append(published, *counts[key])
	}
	return published
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
