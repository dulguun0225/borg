package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/agentrun"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
)

// The two rows a credential itself stands as, in the words the payload stores.
// Each is a wait row of the log naming the credential name and never the fleet
// entry, so an owner may re-credential or delete an entry without changing what
// a past row says, and Work routes each by resolving that name against the
// People declaration.
//
// They are apart from the per-item rows [HoldKind] marks: dispatch's own rows
// are about the stages that never started and these are about the credential —
// so a credential twelve items wait on is twelve rows and one, and neither
// stands for the other.
const (
	// KindCredentialUnreachable is a credential a run failed to reach: an
	// account exhausted, or one taken back. Which of the two it is is not
	// something the provider's answer says, and the design does the same thing
	// with both.
	KindCredentialUnreachable = "credential_unreachable"
	// KindCredentialAtCeiling is a credential whose spend in the period
	// reached the ceiling authored on it.
	KindCredentialAtCeiling = "credential_at_ceiling"
)

var (
	// ErrNoCeilingHold is returned by [Dispatch.ClearCeiling] where no ceiling
	// row stands open on that credential in the period in force. A clear
	// authorises an overage against a hold that is holding, and there is nothing
	// to authorise before one is written — nor where the only row open is a past
	// period's, which is a row nothing is holding now.
	ErrNoCeilingHold = errors.New("dispatch: no spend-ceiling row stands open on that credential")
	// ErrNotTheOwner is returned by [Dispatch.ClearCeiling] for a clear by
	// anybody but a human. What a clear authorises is spend, and the factory
	// authorising its own spend is the authority the ceiling keeps with the
	// owner.
	ErrNotTheOwner = errors.New("dispatch: clearing a spend ceiling is the owner's")
)

// CredentialWait is what one of those two rows says.
type CredentialWait struct {
	Kind           string `json:"kind"`
	CredentialName string `json:"credential_name"`
	// PeriodStart is the start of the period the sum was taken over, on a
	// ceiling's row and on the row that clears one. It is what makes a clear
	// authorise an overage for that period alone: the next period's start is
	// another value, and a close naming the old one clears nothing in the new
	// one.
	PeriodStart string `json:"period_start,omitempty"`
	// OpenedFor is the item, or the intent, whose own run could not reach the
	// credential. It is on the unreachable row alone, and it is the one
	// dispatch that reaches for that credential again: every other is declined
	// with a hold of its own, and a credential nothing ever tried again would
	// hold for ever, no record ever saying it came back.
	OpenedFor string `json:"opened_for,omitempty"`
}

// WantsARate is one run whose converted amount is absent because a kind it
// returned has no rate: the kinds, and the model version and effort a rate for
// them is authored under. It is what the ceiling's row names where the ceiling
// failed closed on an unpriced run, and authoring the rate is what clears it.
type WantsARate struct {
	Kinds        []string `json:"kinds"`
	ModelVersion string   `json:"model_version"`
	Effort       string   `json:"effort"`
}

// credentialRows is the credential rows this component reads off the log at
// one read: the ones standing with the row each stands as, and the payload of
// every close. The closes are read as well as the openings because a ceiling
// cleared for one period is a close whose payload names that period's start,
// and nothing else says so.
//
// It is read once per dispatch and once per re-match and passed to whatever
// asks, because every read of the log appends a read event: a condition that
// read it for itself would write one event per condition per dispatch.
type credentialRows struct {
	open     []CredentialWait
	openRows []decisionlog.Row
	cleared  []CredentialWait
}

// credentialWaits reads them, the same read [Dispatch.Open] makes of the
// per-item rows.
func (d *Dispatch) credentialWaits(ctx context.Context) (credentialRows, error) {
	rows, err := d.c.Reader.ByShape(ctx, componentPrincipal, decisionlog.ShapeWait)
	if err != nil {
		return credentialRows{}, err
	}
	closed := map[string]bool{}
	var read credentialRows
	for _, row := range rows {
		if row.Part != decisionlog.PartClose {
			continue
		}
		closed[row.Closes] = true
		if wait, ok := credentialPayload(row); ok {
			read.cleared = append(read.cleared, wait)
		}
	}
	for _, row := range rows {
		if row.Part != decisionlog.PartOpen || closed[row.ID] {
			continue
		}
		if wait, ok := credentialPayload(row); ok {
			read.open = append(read.open, wait)
			read.openRows = append(read.openRows, row)
		}
	}
	return read, nil
}

// credentialPayload is the row read as one of the two credential rows, and
// false for every other wait — this component's per-item holds among them.
func credentialPayload(row decisionlog.Row) (CredentialWait, bool) {
	var wait CredentialWait
	if err := json.Unmarshal([]byte(row.Payload), &wait); err != nil {
		return CredentialWait{}, false
	}
	if wait.Kind != KindCredentialUnreachable && wait.Kind != KindCredentialAtCeiling {
		return CredentialWait{}, false
	}
	return wait, true
}

// standing reports whether a row of that kind stands open on that credential.
func (r credentialRows) standing(kind, credentialName string) bool {
	for _, one := range r.open {
		if one.Kind == kind && one.CredentialName == credentialName {
			return true
		}
	}
	return false
}

// declines reports whether an open unreachable row declines this dispatch.
//
// The run whose own failure opened the row is not declined: its next dispatch
// is the retry the design has the stage resume by, and a call that succeeds
// closes the credential row and every per-item hold written against it. Every
// other run is declined with a hold of its own, so twelve items waiting on one
// credential are twelve rows in Work and a count at Factory.
func (r credentialRows) declines(on On, credentialName string) bool {
	declined := false
	for _, one := range r.open {
		if one.Kind != KindCredentialUnreachable || one.CredentialName != credentialName {
			continue
		}
		if one.OpenedFor == subjectOf(on) {
			return false
		}
		declined = true
	}
	return declined
}

// clearedFor reports whether an owner cleared this credential's ceiling for the
// period starting at periodStart. A clear authorises an overage for that period
// alone and does not reset the sum, so the same units stop the next period at
// the count authored for it.
func (r credentialRows) clearedFor(credentialName, periodStart string) bool {
	for _, one := range r.cleared {
		if one.Kind == KindCredentialAtCeiling && one.CredentialName == credentialName &&
			one.PeriodStart == periodStart {
			return true
		}
	}
	return false
}

// rowsOf is the open rows of one kind on one credential and one period, which
// is what a close names.
//
// The period is part of the match because a ceiling's row is about the credential
// and the period both: a row a past period left open is not a row anything is
// holding now, and a clear that closed it would authorise an overage in a period
// nothing has yet reached the ceiling in. The unreachable rows carry no period,
// so they match on the empty string at both ends.
func (r credentialRows) rowsOf(kind, credentialName, periodStart string) []decisionlog.Row {
	var found []decisionlog.Row
	for n, one := range r.open {
		if one.Kind == kind && one.CredentialName == credentialName && one.PeriodStart == periodStart {
			found = append(found, r.openRows[n])
		}
	}
	return found
}

// subjectOf is what a credential row names as the run it was opened for: the
// item where there is one, and the intent otherwise.
func subjectOf(on On) string {
	if on.ItemID != "" {
		return on.ItemID
	}
	return on.IntentID
}

// unreachable reports whether the provider's answer says the credential itself
// could not be used: unauthorised, forbidden, payment required, or the quota
// spent. Those four are the account exhausted and the credential taken back —
// the failures whose answer will not change on another sample, which is why
// [Dispatch.attempts] returns on the first of them rather than spending the
// limit.
//
// A model that is not serving and an upstream that refused the request are not
// among them, though the same endpoint reports both: neither says anything
// about the credential, and marking one unreachable on either would decline
// every item on that credential for a fault the next call would not have.
func unreachable(err error) bool {
	var status *agent.StatusError
	if !errors.As(err, &status) {
		return false
	}
	switch status.Status {
	case 401, 402, 403, 429:
		return true
	default:
		return false
	}
}

// couldNotReach opens the credential row a run's own failure leaves, with
// whoever could not reach as the caller and the actor — the agent's own
// principal, this dispatch having made the call as it — and returns the row it
// stands as. A row already open on that credential is returned as it stands:
// the row is the credential's and not this run's, so a second failure writes no
// second row.
func (d *Dispatch) couldNotReach(ctx context.Context, as principal.Principal, on On,
	credentialName string) (string, error) {
	read, err := d.credentialWaits(ctx)
	if err != nil {
		return "", err
	}
	if standing := read.rowsOf(KindCredentialUnreachable, credentialName, ""); len(standing) > 0 {
		return standing[0].ID, nil
	}
	payload, err := json.Marshal(CredentialWait{
		Kind: KindCredentialUnreachable, CredentialName: credentialName, OpenedFor: subjectOf(on),
	})
	if err != nil {
		return "", fmt.Errorf("dispatch: marshalling the row for %s: %w", credentialName, err)
	}
	row, err := d.c.Log.AppendWaitOpen(ctx, decisionlog.Entry{
		Actor: as.Actor, Payload: string(payload), FormatVersion: HoldFormatVersion,
	})
	if err != nil {
		return "", err
	}
	return row.ID, nil
}

// reached closes every unreachable row standing on this name, which is what a
// call that succeeded says: the credential is reachable again. It returns
// whether it closed one, so the caller re-matches only where something moved.
//
// The rows are the ones read at the dispatch's own start and not read again: a
// call that could not reach the credential returns before it, so a row opened
// between the two would be one this run itself did not open, and one process
// holds the lease.
//
// Dispatching again is what ends the row, rather than the agent that stopped
// closing it: an agent whose account ran out part-way through a stage renews
// nothing and reports nothing.
func (d *Dispatch) reached(ctx context.Context, read credentialRows, credentialName string) (bool, error) {
	rows := read.rowsOf(KindCredentialUnreachable, credentialName, "")
	for _, row := range rows {
		payload, err := json.Marshal(CredentialWait{
			Kind: KindCredentialUnreachable, CredentialName: credentialName,
		})
		if err != nil {
			return false, fmt.Errorf("dispatch: marshalling the close for %s: %w", credentialName, err)
		}
		if _, err := d.c.Log.AppendWaitClose(ctx, decisionlog.Entry{
			Actor: Actor, Payload: string(payload), FormatVersion: HoldFormatVersion, Closes: row.ID,
		}); err != nil {
			return false, err
		}
	}
	return len(rows) > 0, nil
}

// ceilingReading is what the spend ceiling on one credential comes to: whether
// it is reached, the start of the period the sum was taken over, and the runs a
// rate is wanted for where that is what it failed closed on.
type ceilingReading struct {
	reached     bool
	periodStart string
	wantsARate  []WantsARate
}

// atCeiling is whether a credential has reached the spend ceiling authored on
// it: the sum of the converted amounts of the runs naming it since the start of
// the period in force, against the amount authored, reading nothing at the
// provider.
//
// A credential nobody lent, and one with no ceiling authored, are unbounded —
// the provider's quota is the only stop, which is what every install had before
// one was authored. A run whose converted amount is absent because a kind it
// returned has no rate gives the ceiling nothing to sum, and a credential under
// one fails closed on it, naming the kinds, the model version and the effort
// that want a rate.
//
// Which period a run falls in is on no record: it is derived here from the
// run's own time against the start date and the length in force, so a period
// lengthened or re-anchored later re-buckets every run already written rather
// than stranding it in a period that no longer exists.
func (d *Dispatch) atCeiling(ctx context.Context, credentialName string,
	read credentialRows) (ceilingReading, error) {
	if credentialName == "" {
		return ceilingReading{}, nil
	}
	lent, found, err := people.CredentialNamed(ctx, d.c.Pool, credentialName)
	if err != nil {
		return ceilingReading{}, err
	}
	if !found || !lent.Ceiling.Authored() {
		return ceilingReading{}, nil
	}
	periodStart, err := lent.Ceiling.PeriodStartAt(time.Now())
	if err != nil {
		return ceilingReading{}, fmt.Errorf("dispatch: the period in force on %s: %w", credentialName, err)
	}
	reading := ceilingReading{periodStart: periodStart}
	spend, err := agentrun.SpendByCredentialSince(ctx, d.c.Pool, credentialName, periodStart)
	if err != nil {
		return ceilingReading{}, err
	}
	// The unpriced hold is decided before any clear is honoured, because the
	// design clears that one by authoring the rate and not by authorising an
	// overage: an overage authorised against a sum the rates do not cover would
	// authorise a number nobody has.
	if wants := wantsARate(spend.Unpriced); len(wants) > 0 {
		reading.reached, reading.wantsARate = true, wants
		return reading, nil
	}
	if err := d.nearingTheCeiling(ctx, credentialName, periodStart, spend.Amount, lent.Ceiling); err != nil {
		return ceilingReading{}, err
	}
	if read.clearedFor(credentialName, periodStart) {
		return reading, nil
	}
	reading.reached = spend.Amount >= lent.Ceiling.Amount
	return reading, nil
}

// NotifiedAtFraction is the fraction of an authored ceiling the factory
// notifies at. The design fixes the fraction rather than leaving it to an
// owner, so it is a constant and not a parameter: a fraction an owner could
// author would be a second place a ceiling is decided.
const NotifiedAtFraction = 0.8

// nearingTheCeiling delivers the one notice a ceiling carries: at
// [NotifiedAtFraction] of the amount authored, through the notifier as a
// delivery and never a page, so the hold is not the first anyone hears of it.
//
// It is evaluated where the sum is read, which is every place the ceiling is
// compared, and delivered once per credential and period — the implementation
// keys it on the two, this component holding no delivery record of its own.
// The sum it compares is the priced one: a period with an unpriced run has
// already failed closed above, and a fraction of a sum the rates do not cover
// would be a fraction of a number nobody has.
func (d *Dispatch) nearingTheCeiling(ctx context.Context, credentialName, periodStart string,
	spent float64, ceiling people.Ceiling) error {
	if spent < ceiling.Amount*NotifiedAtFraction {
		return nil
	}
	if err := d.c.Notifier.NearingASpendCeiling(ctx, credentialName, periodStart,
		spent, ceiling.Amount, ceiling.Currency); err != nil {
		return fmt.Errorf("dispatch: reporting that %s is at %v of its ceiling: %w",
			credentialName, NotifiedAtFraction, err)
	}
	return nil
}

// wantsARate is the unpriced runs as the ceiling's row names them: one entry
// per model version and effort, naming the kinds that want a rate, so twelve
// runs of one entry are one line and not twelve.
func wantsARate(unpriced []agentrun.Run) []WantsARate {
	var wants []WantsARate
	for _, run := range unpriced {
		kinds := run.UnpricedKinds()
		at := -1
		for n, already := range wants {
			if already.ModelVersion == run.ModelVersion && already.Effort == run.Effort {
				at = n
				break
			}
		}
		if at < 0 {
			wants = append(wants, WantsARate{Kinds: kinds, ModelVersion: run.ModelVersion, Effort: run.Effort})
			continue
		}
		for _, kind := range kinds {
			if !slices.Contains(wants[at].Kinds, kind) {
				wants[at].Kinds = append(wants[at].Kinds, kind)
			}
		}
	}
	return wants
}

// atCeilingRow opens the credential's own ceiling row where none stands on that
// period, so what an owner clears is a row about the credential and the period,
// beside the per-item rows the items it declines each get. It is the row
// [Dispatch.ClearCeiling] closes.
func (d *Dispatch) atCeilingRow(ctx context.Context, read credentialRows, credentialName, periodStart string) error {
	for _, one := range read.open {
		if one.Kind == KindCredentialAtCeiling && one.CredentialName == credentialName &&
			one.PeriodStart == periodStart {
			return nil
		}
	}
	payload, err := json.Marshal(CredentialWait{
		Kind: KindCredentialAtCeiling, CredentialName: credentialName, PeriodStart: periodStart,
	})
	if err != nil {
		return fmt.Errorf("dispatch: marshalling the ceiling row for %s: %w", credentialName, err)
	}
	_, err = d.c.Log.AppendWaitOpen(ctx, decisionlog.Entry{
		Actor: Actor, Payload: string(payload), FormatVersion: HoldFormatVersion,
	})
	return err
}

// ClearCeiling is the owner authorising an overage on one credential for the
// period in force: it closes the credential's ceiling row with the start of
// that period on the closing, and re-matches, which lifts every per-item hold
// the ceiling was holding. It does not reset the sum, and it authorises nothing
// in the next period, whose start is another value.
//
// It is the owner's and nobody else's, so an actor that is not a human is
// [ErrNotTheOwner].
func (d *Dispatch) ClearCeiling(ctx context.Context, actor record.Actor, credentialName string) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if actor.Kind != record.KindHuman {
		return fmt.Errorf("%w: %s %q", ErrNotTheOwner, actor.Kind, actor.Key)
	}
	lent, found, err := people.CredentialNamed(ctx, d.c.Pool, credentialName)
	if err != nil {
		return err
	}
	if !found || !lent.Ceiling.Authored() {
		return fmt.Errorf("%w: %s carries no authored ceiling", ErrNoCeilingHold, credentialName)
	}
	periodStart, err := lent.Ceiling.PeriodStartAt(time.Now())
	if err != nil {
		return fmt.Errorf("dispatch: the period in force on %s: %w", credentialName, err)
	}

	read, err := d.credentialWaits(ctx)
	if err != nil {
		return err
	}
	rows := read.rowsOf(KindCredentialAtCeiling, credentialName, periodStart)
	if len(rows) == 0 {
		return fmt.Errorf("%w: %s in the period beginning %s", ErrNoCeilingHold, credentialName, periodStart)
	}
	for _, row := range rows {
		payload, err := json.Marshal(CredentialWait{
			Kind: KindCredentialAtCeiling, CredentialName: credentialName, PeriodStart: periodStart,
		})
		if err != nil {
			return fmt.Errorf("dispatch: marshalling the clear for %s: %w", credentialName, err)
		}
		if _, err := d.c.Log.AppendWaitClose(ctx, decisionlog.Entry{
			Actor: actor, Payload: string(payload), FormatVersion: HoldFormatVersion, Closes: row.ID,
		}); err != nil {
			return err
		}
	}
	_, err = d.Rematch(ctx)
	return err
}
