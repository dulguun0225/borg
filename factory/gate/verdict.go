package gate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
)

// What closes a firing opened by [Gate.Fire] or [Gate.FireSet]: a human's
// verdict, the factory's own pass, and the factory's own reject — the three
// calls that append the close event — and the payload each of them writes.
// refer.go is the fourth verdict, which is a close and a re-firing together.

var (
	// ErrReferGivenHere is returned by [Gate.Decide] for a refer. A refer closes
	// the row and re-fires it to a holder who has not referred it, so it is two
	// appends and one refusal of its own; [Gate.Refer] is where it is given.
	ErrReferGivenHere = errors.New("gate: a refer is given through Refer, which re-fires the row")
	// ErrSelfApproval is returned for a close whose actor wrote the artifact
	// version its open event names — the same per-person key on both — where
	// another holder of the row's duty exists. The row re-fires to that holder
	// rather than closing to its own author.
	ErrSelfApproval = errors.New("gate: this close is by the author of the version under decision, and another holder of the row's duty exists")
	// ErrClosedByTheActor is returned for a close by the human a record's own
	// routing says may not decide it: the actor on a withdrawal is never the
	// human its row waits on, and the human who authored a shorter retention
	// value is not the one who decides it.
	ErrClosedByTheActor = errors.New("gate: this row does not route to the human who wrote the record it decides")
)

// Given is a human's verdict as it reaches the gate: who gave it, which verdict,
// the reason a reject and a hold carry, the stage a reject names, the set of
// holds an approve goes through, and when the actor opened the row in Work.
type Given struct {
	Actor   record.Actor
	Verdict Verdict
	// Reason is the text the human wrote. It is required on a reject and on a
	// hold, and is a note on an approve that nothing reads.
	Reason string
	// ReturnsTo is the stage a reject sends the item to. Where it is empty the
	// row's own default is written, which [DefaultReturnsTo] gives.
	ReturnsTo ReturnsTo
	// Holds is the set an approve goes through, each named. A bare approve while
	// one stands is the case with nothing named, and it is refused.
	Holds []string
	// OpenedInWorkAt is when the actor opened the row in Work, in
	// [record.TimeLayout], written with Work as the caller so that it is the
	// screen's report and not the human's. It is empty on the factory's own
	// verdict, which nobody opened.
	OpenedInWorkAt string
}

// ClosingPayload is what the close event says: the verdict, what the human
// wrote, the stage a reject returns the item to, the holds an approve went
// through, and what auto-passed or auto-rejected the firing where the factory
// decided for itself.
//
// [score.CloseEvent] is embedded for the reason [score.OpenEvent] is: the
// verdict and what auto-passed the firing are both read back by the score when
// it learns, and the threshold's own calibration turns on telling an auto-pass
// on the number apart from one its own sample made, so those two field names are
// declared once in the package that reads them.
type ClosingPayload struct {
	score.CloseEvent
	Reason    string    `json:"reason"`
	ReturnsTo ReturnsTo `json:"returns_to,omitempty"`
	// Holds is the set the approve went through, each hold named, so a later
	// reader reads which the human accepted rather than that they approved.
	Holds []string `json:"holds,omitempty"`
	// AutoRejectedBy is which mechanical check rejected, and is empty on every
	// close event but [Gate.AutoReject]'s.
	AutoRejectedBy string `json:"auto_rejected_by,omitempty"`
	// SelfApproval is a close by the author of the version under decision where
	// no second holder of the row's duty exists. An install with one owner is
	// allowed, and its trail says what it is.
	SelfApproval bool `json:"self_approval,omitempty"`
}

// Decide gives a human's verdict: it recomputes the holds standing, refuses the
// three shapes an approve may not take, appends the close event as the deciding
// human, and names the open event it closes. A verdict the row does not offer is
// refused, and so are a reject and a hold with no reason. A second verdict over
// one opening is refused by the log's store.
func (g *Gate) Decide(ctx context.Context, opened Opened, given Given) (decisionlog.Row, error) {
	if given.Verdict == VerdictRefer {
		return decisionlog.Row{}, ErrReferGivenHere
	}
	if err := permits(opened.Gate, given.Verdict); err != nil {
		return decisionlog.Row{}, err
	}
	if (given.Verdict == VerdictReject || given.Verdict == VerdictHold) && given.Reason == "" {
		return decisionlog.Row{}, fmt.Errorf("%w: the %s of %s carries none",
			ErrReasonMissing, given.Verdict, opened.Row.ID)
	}

	returnsTo, err := returnsToOf(opened.Gate, given)
	if err != nil {
		return decisionlog.Row{}, err
	}
	if given.Verdict == VerdictApprove {
		standing, err := g.standingHolds(ctx, opened.Subject)
		if err != nil {
			return decisionlog.Row{}, err
		}
		if err := checkApproveNamesTheSet(standing, given.Holds); err != nil {
			return decisionlog.Row{}, err
		}
	}

	return g.close(ctx, opened, given.Actor, given.OpenedInWorkAt, ClosingPayload{
		CloseEvent: score.CloseEvent{Verdict: string(given.Verdict)},
		Reason:     given.Reason,
		ReturnsTo:  returnsTo,
		Holds:      given.Holds,
	})
}

// AutoPass gives the factory's own verdict, which is what closes a firing that
// put no human at the row and has no hold standing. The close event's actor is
// the gate component, and the payload says what auto-passed it.
//
// A firing that put a human there is refused with [ErrHumanDecides], and one
// with a hold standing is refused too: the row stays open while a hold stands,
// and closing it would decide the event the hold exists to stop.
func (g *Gate) AutoPass(ctx context.Context, opened Opened) (decisionlog.Row, error) {
	if opened.HumanDecides {
		return decisionlog.Row{}, fmt.Errorf("%w: %v", ErrHumanDecides, opened.Marks)
	}
	standing, err := g.standingHolds(ctx, opened.Subject)
	if err != nil {
		return decisionlog.Row{}, err
	}
	if len(standing) > 0 {
		return decisionlog.Row{}, fmt.Errorf("%w: %v", ErrApproveLeavesAHoldOut, standing)
	}
	return g.close(ctx, opened, component(opened.Gate), "", ClosingPayload{
		CloseEvent: score.CloseEvent{
			Verdict:         string(VerdictApprove),
			WhyItAutoPassed: whyItAutoPassed(opened),
		},
	})
}

// AutoReject gives the factory's own reject, which is what a mechanical check
// that failed closes a firing with: the close event's actor is the gate
// component, the payload names which of [MechanicalChecks] rejected, and the
// reason is what it found.
//
// It is allowed whatever the firing decided about a human, and [Gate.AutoPass]
// is not. That asymmetry is the whole of the difference between the two: the
// factory may not approve over a human, because nothing in the design removes a
// human from a gate; and it rejects before a human is asked, because a
// mechanical check rejects on its own terms before anyone gives a verdict.
func (g *Gate) AutoReject(ctx context.Context, opened Opened, check, found string) (decisionlog.Row, error) {
	if err := permits(opened.Gate, VerdictReject); err != nil {
		return decisionlog.Row{}, err
	}
	if check == "" || found == "" {
		return decisionlog.Row{}, fmt.Errorf("%w: check %q, what it found %q", ErrCheckMissing, check, found)
	}
	if !slices.Contains(MechanicalChecks, check) {
		return decisionlog.Row{}, fmt.Errorf("%w: %q", ErrCheckUnknown, check)
	}
	returnsTo, _ := DefaultReturnsTo(opened.Gate)
	return g.close(ctx, opened, component(opened.Gate), "", ClosingPayload{
		CloseEvent:     score.CloseEvent{Verdict: string(VerdictReject)},
		Reason:         found,
		ReturnsTo:      returnsTo,
		AutoRejectedBy: check,
	})
}

// returnsToOf is the stage a reject names: the one the verdict gave where it
// gave one, and the row's own default otherwise. A verdict naming a target the
// row may not send an item to is refused, and so is one naming a target at a row
// whose reject sends nothing back.
func returnsToOf(row Row, given Given) (ReturnsTo, error) {
	if given.Verdict != VerdictReject {
		if given.ReturnsTo != "" {
			return "", fmt.Errorf("%w: a %s named %q", ErrReturnsToUnknown, given.Verdict, given.ReturnsTo)
		}
		return "", nil
	}
	fallback, sendsBack := DefaultReturnsTo(row)
	if given.ReturnsTo == "" {
		return fallback, nil
	}
	if !sendsBack {
		return "", fmt.Errorf("%w: %s sends nothing back and named %q", ErrReturnsToUnknown, row, given.ReturnsTo)
	}
	if !slices.Contains(ReturnsToTargets, given.ReturnsTo) {
		return "", fmt.Errorf("%w: %q", ErrReturnsToUnknown, given.ReturnsTo)
	}
	return given.ReturnsTo, nil
}

// close appends the close event, supplying the log's writer with the two
// refusals it cannot evaluate on its own. The verdict and the reason go onto the
// entry's own columns, so decisionlog's refusal of a reject or a hold with no
// reason applies here without a second field to keep in step with the payload's.
//
// The refusals are supplied per close rather than once on the writer, because
// what each of them reads — the author of the version under decision, who holds
// the row's duty, and who has already referred it — is what the gate computed
// for this close and no other. The writer is copied for that call alone.
func (g *Gate) close(ctx context.Context, opened Opened, actor record.Actor,
	openedInWorkAt string, closing ClosingPayload) (decisionlog.Row, error) {
	refusals, err := g.refusalsFor(ctx, opened, actor)
	if err != nil {
		return decisionlog.Row{}, err
	}
	if refusals.selfApproval() {
		closing.SelfApproval = true
	}

	payload, err := json.Marshal(closing)
	if err != nil {
		return decisionlog.Row{}, fmt.Errorf("gate: marshalling the closing payload: %w", err)
	}

	writer := *g.log
	writer.RefuseClose = func(_ context.Context, _ pgx.Tx, _ decisionlog.Entry) error {
		return refusals.refuse(Verdict(closing.CloseEvent.Verdict))
	}
	return writer.AppendDecisionClose(ctx, decisionlog.Entry{
		Actor:          actor,
		Payload:        string(payload),
		FormatVersion:  decisionFormatVersion,
		Closes:         opened.Row.ID,
		Verdict:        closing.CloseEvent.Verdict,
		Reason:         closing.Reason,
		OpenedInWorkAt: openedInWorkAt,
		SelfApproval:   closing.SelfApproval,
	})
}

// whyItAutoPassed is what the close event says passed the firing. It reads the
// threshold at a gate the score would have passed anyway, whether or not the
// item is held out, and the sample only where the number was at or above the
// threshold — which is the one case the sample is evidence about, and the only
// case the threshold's own calibration counts.
func whyItAutoPassed(opened Opened) string {
	if opened.HeldOut && opened.Assessment.Number >= opened.Applied.Threshold {
		return score.AutoPassSample
	}
	return score.AutoPassThreshold
}
