package score

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/dulguun0225/borg/factory/record"
)

// queueRejection is the merge queue's own rejection of a candidate, read off
// the decision log's queue_rejection row without importing package
// mergequeue: the payload is a fact of the log and not a type this package
// owns, the way [OpenEvent] and [CloseEvent] read a gate's rows the same way.
// It opens no gate row of its own — the Merge to master row already closed as
// an approval — so it carries no [Rejection]'s four-way resolution and what
// the score learns from it is stated on the row directly: [LearnsAs] is
// [VerdictRejected] where the candidate failed against the master it merges
// into, which is read here at the merge to master row the same way a human's
// resolved rejection is, and something else — a hold, on a dependency's
// release having moved — where it is not.
//
// TeachesNothing is a repeated failure whose criterion is unreliable over the
// two builds the queue's re-verification compared: an unreliable criterion
// teaches the score nothing, so the row is skipped whatever it learns as.
type queueRejection struct {
	ItemID         string `json:"item_id"`
	ServiceID      string `json:"service_id"`
	LearnsAs       string `json:"learns_as"`
	TeachesNothing bool   `json:"unreliable_criterion"`
}

// queueRejectionRow is the gate row a queue rejection is read at: the Merge to
// master row already approved the candidate, and the queue's re-verification
// is what caught the failure the score learns from as a reject there.
const queueRejectionRow = "merge_to_master"

// queueRejectionsNeeded is how many queue rejections resolved as a gate the
// factory needed at the merge to master row: read as from a reject, and its
// criterion not unreliable over the two builds compared.
func queueRejectionsNeeded(e *Evidence) int {
	needed := 0
	for _, q := range e.queueRejections {
		if !q.TeachesNothing && q.LearnsAs == VerdictRejected {
			needed++
		}
	}
	return needed
}

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
// item after it. What separates the first way from the second is the digest of
// what the rejection named, and not the digest of the whole version: the design
// reads the re-authored version as a gate the factory needed where it differs
// in what the rejection named, and any re-authoring at all moves the whole
// version's digest — so the whole digest would read every second approval as a
// gate that was needed and the false alarm would never be measured.
//
// A trail this cannot read leaves the rejection unresolved rather than resolved
// as a false alarm: the rejection named something, the words it named are gone
// with the version, and reading that silence as approval without differing
// would publish a false alarm against the human on evidence nobody holds.
func (e *Evidence) resolutionOf(r Rejection, later []Firing) string {
	for _, f := range later {
		if f.OpenEvent.ItemID != r.ItemID || !f.HumanClosed {
			continue
		}
		switch f.CloseEvent.Verdict {
		case VerdictRejected:
			return ResolvedRejectedAgain
		case VerdictApproved:
			was, readable := e.namedDigest(r.ArtifactID, r.Named)
			now, readableNow := e.namedDigest(f.OpenEvent.ArtifactID, r.Named)
			if !readable || !readableNow {
				return ""
			}
			if was != now {
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

// namedDigest is the digest of the part of one version the rejection named, and
// false where the trail cannot be read — the version's words are not held, or
// the rejection named nothing.
//
// What the named part is, is every line of the version that carries the words
// the human named, in the order they appear. That is a derivation and not a
// record: nothing in the graph divides a version into parts, so what the
// rejection named is located in the words themselves. A re-authoring that
// changed what was named changes one of those lines or removes it; one that
// changed something else leaves them all standing.
func (e *Evidence) namedDigest(artifactID, named string) (string, bool) {
	if artifactID == "" || named == "" {
		return "", false
	}
	content, held := e.contents[artifactID]
	if !held {
		return "", false
	}
	var part []string
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, named) {
			part = append(part, line)
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(part, "\n")))
	return hex.EncodeToString(sum[:]), true
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
