package notifier

import (
	"context"
	"errors"
	"fmt"

	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
)

// Actor is who a page event is written as. The notifier delivers the wait; who
// created it is on the wait's own record, and the page event says which wait it is
// about rather than repeating that record's actor.
var Actor = record.Actor{Kind: record.KindComponent, Key: "notifier", Basis: record.BasisClaimed}

// componentPrincipal is who this component's reads of the decision log are made
// as: the notifier calls as itself, and the read event names it.
var componentPrincipal = principal.OfComponent("notifier")

// Channel is one of the three the notifier delivers on.
type Channel string

const (
	// ChannelMail is mail.
	ChannelMail Channel = "mail"
	// ChannelChat is chat.
	ChannelChat Channel = "chat"
	// ChannelPage is the page, the one channel that widens.
	ChannelPage Channel = "page"
)

// Channels is every channel, in the order [Notifier.Notify] delivers on them.
// The page is last, so a wait that qualifies for one has already reached its
// holder by the other two when the record is written.
var Channels = []Channel{ChannelMail, ChannelChat, ChannelPage}

// Event is which of the four a page event is.
type Event string

const (
	// EventReached is the first delivery, written once per human holding the duty.
	EventReached Event = "reached"
	// EventAcknowledged is a human saying they have the row. It ends no wait
	// and decides nothing; it stops only the widening.
	EventAcknowledged Event = "acknowledged"
	// EventWidened is the one widening, to the owner. There is no second.
	EventWidened Event = "widened"
	// EventAnswered is the wait having stopped waiting.
	EventAnswered Event = "answered"
)

// Events is every kind of page event. The four are a sequence on one wait and
// not four records.
var Events = []Event{EventReached, EventAcknowledged, EventWidened, EventAnswered}

// Pages is what the page's condition answers for a kind of wait: whether the
// deployed software is worse until a human ends one of these.
type Pages int

const (
	// PagesNever is a kind the condition cannot be met by.
	PagesNever Pages = iota
	// PagesAlways is a kind the condition is met by, by definition.
	PagesAlways
	// PagesIfWorse is a kind where it depends on what the wait is about, and
	// [Wait.Worse] is the caller's answer.
	PagesIfWorse
)

// Kind is what waits on a human. Each is one of the callers the design names,
// and each carries what the page's condition answers for it — doc.go sets out the
// three answers.
type Kind string

const (
	// KindGateDecision is a gate having opened a decision a human must close.
	// Confirming criteria and performing UAT are the duties at a gate, and neither
	// makes anything live worse: the item waits and nothing is running that should
	// not be.
	KindGateDecision Kind = "gate_decision"
	// KindGateRevertDecision is a human deciding at a gate on a revert while
	// the rollback that removed the defect still holds: the service is
	// running the build that was restored, master still contains the
	// defect, and nothing ships past that human. Unlike an ordinary gate
	// decision this can leave production worse, so its answer depends on
	// the caller — whether the rollback still holds — and it is a kind of
	// its own rather than KindGateDecision made conditional.
	KindGateRevertDecision Kind = "gate_revert_decision"
	// KindInterview is intake having written a round of interview questions. An
	// owner's silence there consumes no compute, which is why no bound applies to
	// that wait — and a page would be the same mistake made more loudly.
	KindInterview Kind = "interview"
	// KindIntentEscalated is an intent whose interview rounds exceeded the limit.
	// Whether it pages depends on where the intent came from, the same way an item's
	// escalation does.
	KindIntentEscalated Kind = "intent_escalated"
	// KindItemEscalated is an item stopped at the attempt limit. It pages where the
	// intent behind it was not an owner's: the factory giving up on a defect that is
	// live is production staying worse until a human takes it over, and giving up on
	// a feature is not.
	KindItemEscalated Kind = "item_escalated"
	// KindDriftMismatch is a record the drift detector found disagreeing with what
	// runs. It holds that service's production deploys and does not lift itself, so
	// the service cannot receive its own fixes until a human ends it.
	KindDriftMismatch Kind = "drift_mismatch"
	// KindOwnerFired is a page a human fired on their own judgment from Ops, which
	// is the parallel that undoing a change after it shipped already has. Nothing
	// scores it and no bound applies to it; what limits it is that a page nobody
	// needed makes its recipient slower to answer the next one.
	KindOwnerFired Kind = "owner_fired"
	// KindRollbackPerformed is a rollback the factory performed on its own. It is
	// reported, not requested, and reporting is not paging.
	KindRollbackPerformed Kind = "rollback_performed"
	// KindCredentialUnreachable is a credential a component could not reach. Whether
	// it pages depends on which: a deploy target's while the health monitor is calling for
	// a rollback leaves production worse, and a provider account that has run out
	// stops work rather than making anything live worse.
	KindCredentialUnreachable Kind = "credential_unreachable"
	// KindFailedWithNoRollback is a failed analysis window that rolled
	// nothing back: production is running a release the factory has just
	// failed and no mechanism the factory has will improve it. The health
	// monitor's rollback.go is its one caller.
	KindFailedWithNoRollback Kind = "failed_with_no_rollback"
	// KindWindowCapUnevaluated is a window past its cap that nothing has
	// evaluated — what a stopped health monitor looks like from outside,
	// read by the drift detector's third comparison off the health
	// monitor's own last check.
	KindWindowCapUnevaluated Kind = "window_cap_unevaluated"
	// KindRollbackIncomplete is a rollback the health monitor called for
	// whose deploy record is still not complete on every target at the
	// deployer's next last check for that environment.
	KindRollbackIncomplete Kind = "rollback_incomplete"
	// KindIncidentNoOpenWindow is an open incident whose crossing has not
	// stopped, with no open window: the deployed software is worse until a
	// human ends it, whatever the factory has since raised from it.
	KindIncidentNoOpenWindow Kind = "incident_no_open_window"
	// KindConstraintDeadline is a deadline a constraint put on an intent,
	// where a law started a clock. Unbuilt: nothing raises this yet.
	KindConstraintDeadline Kind = "constraint_deadline"
	// KindAdvisoryRemediation is an advisory's remediation period passing
	// while the intent it raised is still open. Unbuilt: nothing raises
	// this yet.
	KindAdvisoryRemediation Kind = "advisory_remediation"
	// KindHarmMarkedReport is a report marked as describing harm to a
	// person. Unbuilt: nothing raises this yet, and [Notifier.Notify]
	// still reads the factory-wide settings' off switch and cap for it.
	KindHarmMarkedReport Kind = "harm_marked_report"
	// KindIncidentBoundExceeded is an incident-raised item worked past the
	// bound an owner authors on the service record. Unbuilt: nothing raises
	// this yet.
	KindIncidentBoundExceeded Kind = "incident_bound_exceeded"
	// KindDriftDetectorStale is the drift detector's own last check past
	// its interval, read by the notifier itself: each of the two processes
	// watches the other, and this is the notifier's half.
	KindDriftDetectorStale Kind = "drift_detector_stale"
	// KindDriftDetectorOwnDelivery is a delivery the detector made to its
	// own address while the factory's process was down, caught up at the
	// factory's next start. The delivery already happened, so what the
	// factory writes is the page event the log missed — and the kind is
	// here because a page event carries a kind and a kind is a line in
	// [Kinds].
	KindDriftDetectorOwnDelivery Kind = "drift_detector_own_delivery"
)

// Kinds is every kind of wait, with what the page's condition answers for each.
// A kind is added by writing a line here, which is where the question "does the
// deployed software stay worse until a human ends this?" has to be answered.
var Kinds = map[Kind]Pages{
	KindGateDecision:             PagesNever,
	KindGateRevertDecision:       PagesIfWorse,
	KindInterview:                PagesNever,
	KindRollbackPerformed:        PagesNever,
	KindDriftMismatch:            PagesAlways,
	KindOwnerFired:               PagesAlways,
	KindIntentEscalated:          PagesIfWorse,
	KindItemEscalated:            PagesIfWorse,
	KindCredentialUnreachable:    PagesIfWorse,
	KindFailedWithNoRollback:     PagesAlways,
	KindWindowCapUnevaluated:     PagesAlways,
	KindRollbackIncomplete:       PagesAlways,
	KindIncidentNoOpenWindow:     PagesAlways,
	KindConstraintDeadline:       PagesAlways,
	KindAdvisoryRemediation:      PagesAlways,
	KindHarmMarkedReport:         PagesAlways,
	KindIncidentBoundExceeded:    PagesAlways,
	KindDriftDetectorStale:       PagesAlways,
	KindDriftDetectorOwnDelivery: PagesAlways,
}

// anyHour is every kind admitted beside the ordinary condition rather than
// under it — a constraint's deadline, an advisory's remediation period, and
// a harm mark — which page at whatever hour the trigger happened to arrive
// and are never deferred to a service's paging hours the way every other
// kind that names a service is.
var anyHour = map[Kind]bool{
	KindConstraintDeadline:  true,
	KindAdvisoryRemediation: true,
	KindHarmMarkedReport:    true,
}

var (
	// ErrKindUnknown is returned for a kind outside [Kinds].
	ErrKindUnknown = errors.New("notifier: not a kind of wait this component knows")
	// ErrWorseRefused is returned for a wait asserting the page's condition on a
	// kind that cannot meet it, and for one denying it on a kind that meets it by
	// definition. The condition is the test, and these two are where the design has
	// already applied it.
	ErrWorseRefused = errors.New("notifier: the page's condition is already settled for this kind of wait")
	// ErrWaitIncomplete is returned for a wait naming nothing that waits or saying
	// nothing about what it is waiting for.
	ErrWaitIncomplete = errors.New("notifier: a wait names what waits and what it is waiting for")
	// ErrAlreadyWidened is returned by [Notifier.Widen] for a wait that has
	// widened. A page widens exactly once, to the owner, and there is no second
	// widening.
	ErrAlreadyWidened = errors.New("notifier: the page has widened already, and there is no second widening")
	// ErrAcknowledged is returned by [Notifier.Widen] for a wait a human has
	// acknowledged: the row still waits, but a human is already on it, so
	// widening it further would make somebody already working the problem
	// indistinguishable from one asleep.
	ErrAcknowledged = errors.New("notifier: a human has acknowledged this wait, so it does not widen")
	// ErrAlreadyAnswered is returned by [Notifier.Acknowledge] for a wait
	// already answered: there is nothing left to say "I have this row"
	// about.
	ErrAlreadyAnswered = errors.New("notifier: this wait is already answered")
	// ErrNothingReached is returned by [Notifier.Widen], [Notifier.Answered]
	// and [Notifier.Acknowledge] for a wait no page ever reached anybody
	// about. A page is the sequence of events on one wait, and widening,
	// answering or acknowledging one that never started would leave a
	// sequence with no beginning.
	ErrNothingReached = errors.New("notifier: no page was ever delivered about this wait")
)

// Wait is one thing waiting on a human.
type Wait struct {
	// Row is what waits, by id: a gate's open event, an item, an intent, a
	// mismatch. It is what the page's events are the sequence on, so two waits with
	// one id would be one page.
	Row  string
	Kind Kind
	// Waiting is what it is waiting for, in words a human reads. It is the whole of
	// what a delivery says, there being no screen to link to yet.
	Waiting string
	// Holding is whose wait it is: a duty, or an obligation outside the twelve. Its
	// zero value is a wait belonging to neither, which routes to the owner — the
	// same answer as a duty nobody holds.
	Holding people.Holding
	// Worse is whether the deployed software is worse until a human ends this wait.
	// It is read for a kind whose answer is [PagesIfWorse] and refused on the other
	// two, where the design has already applied the condition.
	Worse bool
	// ServiceID is the service this wait is about, where it is about one. It
	// is what a wait of the second kind is held to the service's own paging
	// hours by; empty is a wait that pages at any hour, the same as a
	// service authoring none.
	ServiceID string
	// RollbackOutstanding is whether production serves a release the health
	// monitor has called for a rollback on and that rollback has not run. It
	// is the design's own test for which of the two kinds a wait is: where it
	// holds, the software is worse now and worse for every hour of the wait,
	// so the page fires at whatever hour the condition arose and is never
	// deferred to the service's paging hours; where it does not, the software
	// is no worse for the hour a human ends the wait at.
	//
	// It is the caller's answer for the reason [Wait.Worse] is: the two facts
	// it is computed from — the release the production deploy record names,
	// and whether a rollback the health monitor called for on it has run —
	// are deploy records this package does not read. doc.go names the callers
	// that answer it.
	RollbackOutstanding bool
}

// pages reports whether this wait fires a page, and refuses a caller that
// contradicted what the condition already answers for the kind.
func (w Wait) pages() (bool, error) {
	answer, known := Kinds[w.Kind]
	if !known {
		return false, fmt.Errorf("%w: %q", ErrKindUnknown, w.Kind)
	}
	switch answer {
	case PagesNever:
		if w.Worse {
			return false, fmt.Errorf("%w: nothing live is worse until a human ends a %s", ErrWorseRefused, w.Kind)
		}
		return false, nil
	case PagesAlways:
		if !w.Worse {
			return false, fmt.Errorf("%w: a %s leaves production worse until a human ends it", ErrWorseRefused, w.Kind)
		}
		return true, nil
	default:
		return w.Worse, nil
	}
}

func (w Wait) validate() error {
	if w.Row == "" {
		return fmt.Errorf("%w: it names nothing that waits", ErrWaitIncomplete)
	}
	if w.Waiting == "" {
		return fmt.Errorf("%w: %s says nothing about what it waits for", ErrWaitIncomplete, w.Row)
	}
	_, err := w.pages()
	return err
}

// Deliverer is where a delivery goes. What mail and chat are on a self-hosted
// install is the owner's arrangement, so the notifier composes one of these rather
// than reaching anything itself.
type Deliverer interface {
	// Deliver hands one delivery to one channel. An error stops the notification:
	// a page half delivered with no record of the failure is worse than a caller
	// that knows its page did not go out.
	Deliver(ctx context.Context, d Delivery) error
}

// Delivery is one thing delivered on one channel to one human.
type Delivery struct {
	Channel Channel
	// To is the human reached, by per-person key — the same field a page
	// event's recipient carries, never a name. It is the owner where the
	// wait belongs to no duty or where nobody holds it, and the owner's own
	// identifier is not a People key: the design gives the owner no record.
	To   string
	Wait Wait
	// Event is which page event this delivery is, and is empty on mail and chat.
	Event Event
}
