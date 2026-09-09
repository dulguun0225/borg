package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/principal"
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
	// KindCeilingLifted is what a re-match writes over a ceiling row whose
	// condition has ended — a period that has passed, or a ceiling raised or
	// withdrawn. It is a kind of its own and not [KindCredentialAtCeiling],
	// because a close carrying that kind is the owner authorising an overage
	// for the period it names, which [credentialRows.clearedFor] reads, and a
	// condition that ended authorises nothing.
	KindCeilingLifted = "credential_ceiling_lifted"
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
