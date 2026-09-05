package gate

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
)

// What closes a firing opened by [Gate.Fire] or [Gate.FireSet]: a human's
// verdict, the factory's own pass, and the factory's own reject — the three
// calls that append the close event — and the payload each of them writes.

// ClosingPayload is what the close event says: the verdict, what the human typed
// with it, the stage a reject returns the item to, and what auto-passed or
// auto-rejected the firing where the factory decided for itself.
//
// Feedback is required on a reject, that action being "Reject with feedback",
// and is a note on a hold — what a human held for is worth showing beside the
// wait, and nothing reads it.
//
// [score.CloseEvent] is embedded for the reason [score.OpenEvent] is: the verdict and
// what auto-passed the firing are both read back by the score when it learns, and
// the threshold's own calibration turns on telling an auto-pass on the number
// apart from one its own sample made, so those two field names are declared once
// in the package that reads them.
type ClosingPayload struct {
	score.CloseEvent
	Feedback  string `json:"feedback"`
	ReturnsTo string `json:"returns_to"`
	// AutoRejectedBy is which mechanical check rejected, and is empty on every
	// close event but [Gate.AutoReject]'s.
	AutoRejectedBy string `json:"auto_rejected_by,omitempty"`
}

// The mechanical checks that reject on their own terms at the merge row, in the
// words a close event names one by. They are constants here so that a caller
// cannot report a rejection under a name of its own, which is the arrangement the
// five holds already have; what computes each of them reads the contracts and the
// consumer contracts, and this package imports neither.
const (
	// AutoRejectedByContractDiff is the producer's own diff: the form the
	// candidate publishes against the version its service's current release
	// publishes, breaking, with the migration not shipped ahead of it.
	AutoRejectedByContractDiff = "the producer's own contract diff"
	// AutoRejectedByConsumerContract is a consumer contract in force
	// that the candidate does not satisfy, decided against the candidate's own
	// run.
	AutoRejectedByConsumerContract = "a consumer contract"
	// AutoRejectedBySafeguardPredicate is a safeguard's predicate naming an
	// element the candidate removes. It is told apart from a consumer contract
	// because an owner placed it and a derivation did not, and what a reader of
	// the rejection needs is the safeguard and its author.
	AutoRejectedBySafeguardPredicate = "a safeguard's predicate"
)

// Decide gives a human's verdict: it appends the close event as the deciding
// human, naming the open event it closes. A verdict the row does not offer is
// refused, and so is a reject with no feedback. A second verdict over one opening
// is refused by the log's store, not here.
func (g *Gate) Decide(ctx context.Context, opened Opened, actor record.Actor, verdict Verdict, feedback string) (decisionlog.Row, error) {
	if err := permits(opened.Gate, verdict); err != nil {
		return decisionlog.Row{}, err
	}
	if verdict == VerdictReject && feedback == "" {
		return decisionlog.Row{}, fmt.Errorf("%w: the reject of %s carries none", ErrFeedbackMissing, opened.Row.ID)
	}

	returnsTo := ""
	if verdict == VerdictReject && opened.Gate != Decomposition {
		// Decomposition names nothing: its reject re-decomposes the set rather than
		// sending an item anywhere, which is the one reject in the design with no
		// stage on the other end of it.
		returnsTo = ReturnsTo
	}
	return g.close(ctx, opened, actor, ClosingPayload{
		CloseEvent: score.CloseEvent{Verdict: string(verdict)},
		Feedback:   feedback,
		ReturnsTo:  returnsTo,
	})
}

// AutoPass gives the factory's own verdict, which is what closes a firing that
// put no human at the row. The close event's actor is the gate component, and
// the payload says what auto-passed it. A firing that did put a human there is
// refused with [ErrHumanDecides].
func (g *Gate) AutoPass(ctx context.Context, opened Opened) (decisionlog.Row, error) {
	if opened.HumanDecides {
		return decisionlog.Row{}, fmt.Errorf("%w: %s", ErrHumanDecides, opened.WhyHuman)
	}
	return g.close(ctx, opened, component(opened.Gate), ClosingPayload{
		CloseEvent: score.CloseEvent{
			Verdict:         string(VerdictApprove),
			WhyItAutoPassed: whyItAutoPassed(opened),
		},
	})
}

// AutoReject gives the factory's own reject, which is what a mechanical check
// that failed closes a firing with: the close event's actor is the gate component,
// the payload names which check rejected, and the feedback is what it found — which
// is what goes back up the pipeline, a reject being "Reject with feedback".
//
// It is allowed whatever the firing decided about a human, and [Gate.AutoPass] is
// not. That asymmetry is the whole of the difference between the two: the factory
// may not approve over a human, because nothing in the design removes a human from a
// gate; and it rejects before a human is asked, because a mechanical check rejects
// on its own terms before anyone gives a verdict. A human at the row who was going
// to approve is not being overruled — there is nothing left to approve, and the
// check is not a judgment they could have made differently.
//
// A row that does not offer reject refuses this, which is the production deploy
// row: by then the merge has happened and the number is assigned, so there is
// nothing left to reject to.
func (g *Gate) AutoReject(ctx context.Context, opened Opened, check, found string) (decisionlog.Row, error) {
	if err := permits(opened.Gate, VerdictReject); err != nil {
		return decisionlog.Row{}, err
	}
	if check == "" || found == "" {
		return decisionlog.Row{}, fmt.Errorf("%w: check %q, what it found %q", ErrCheckMissing, check, found)
	}
	returnsTo := ReturnsTo
	if opened.Gate == Decomposition {
		// Decomposition names nothing at all: its reject re-decomposes the set rather
		// than sending an item anywhere, so the field its close event would carry
		// stays unwritten.
		returnsTo = ""
	}
	return g.close(ctx, opened, component(opened.Gate), ClosingPayload{
		CloseEvent:     score.CloseEvent{Verdict: string(VerdictReject)},
		Feedback:       found,
		ReturnsTo:      returnsTo,
		AutoRejectedBy: check,
	})
}

func (g *Gate) close(ctx context.Context, opened Opened, actor record.Actor, closing ClosingPayload) (decisionlog.Row, error) {
	payload, err := json.Marshal(closing)
	if err != nil {
		return decisionlog.Row{}, fmt.Errorf("gate: marshalling the closing payload: %w", err)
	}
	return g.log.AppendDecisionClose(ctx, decisionlog.Entry{
		Actor:   actor,
		Payload: string(payload),
		Closes:  opened.Row.ID,
	})
}

// whyItAutoPassed is what the close event says passed the firing. It reads the
// threshold at a gate the score would have passed anyway, whether or not the item
// is held out, and the sample only where the number was at or above the threshold
// — which is the one case the sample is evidence about, and the only case the
// threshold's own calibration counts.
func whyItAutoPassed(opened Opened) string {
	if opened.HeldOut && opened.Assessment.Number >= opened.Applied.Threshold {
		return score.AutoPassSample
	}
	return score.AutoPassThreshold
}
