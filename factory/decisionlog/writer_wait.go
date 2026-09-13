package decisionlog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/record"
)

// ErrWaitOpenNotComponent is returned by [Writer.AppendWaitOpen] for an actor
// that is not a component or the agent that could not reach its own model
// credential. The latter must carry its matching principal and name the
// unreachable credential in the payload.
var ErrWaitOpenNotComponent = errors.New("decisionlog: a wait opens as a component or the agent unable to reach its credential")

// ErrWaitCloseNotComponent refuses a wait's closing except by a component or
// by a human at Work ending one of the two waits the design has a human end:
// the owner authorising an overage on a credential at its ceiling, and a human
// accepting a commit master holds that the queue did not make.
var ErrWaitCloseNotComponent = errors.New("decisionlog: a wait closes as a component or a human at Work clearing a ceiling or accepting a commit")

// AppendWaitOpen appends a wait's opening, written when the factory meets a
// condition it could not compute at a firing. It closes nothing and names
// neither version.
func (w *Writer) AppendWaitOpen(ctx context.Context, e Entry) (Row, error) {
	if err := expectShape(e, ShapeWait); err != nil {
		return Row{}, err
	}
	if e.Closes != "" {
		return Row{}, fmt.Errorf("%w: %q", ErrClosesRefused, e.Closes)
	}
	if err := e.Actor.Validate(); err != nil {
		return Row{}, err
	}
	if e.Actor.Kind != record.KindComponent && !agentCredentialWait(e) {
		return Row{}, fmt.Errorf("%w: actor kind %q", ErrWaitOpenNotComponent, e.Actor.Kind)
	}
	if err := refuseVersionsAndClosingOnlyFields("a wait's opening", e); err != nil {
		return Row{}, err
	}
	return commitAppend(ctx, w.pool, w.token, ShapeWait, PartOpen, e, nil)
}

// AppendWaitClose appends the row written when the condition a wait's
// opening named is found gone. It names the opening it closes. It fails with
// [ErrNotAnOpening] when the named row is not a wait's opening, and with
// [ErrAlreadyEnded] when a closing already ends it.
func (w *Writer) AppendWaitClose(ctx context.Context, e Entry) (Row, error) {
	if err := expectShape(e, ShapeWait); err != nil {
		return Row{}, err
	}
	if e.Closes == "" {
		return Row{}, fmt.Errorf("%w: a wait's closing", ErrClosesMissing)
	}
	if err := refuseVersionsAndClosingOnlyFields("a wait's closing", e); err != nil {
		return Row{}, err
	}

	return commitAppend(ctx, w.pool, w.token, ShapeWait, PartClose, e,
		func(ctx context.Context, tx pgx.Tx) error {
			shape, part, err := lookupRow(ctx, tx, e.Closes)
			if err != nil {
				return err
			}
			if shape != ShapeWait || part != PartOpen {
				return fmt.Errorf("%w: %q is shape %q, part %q", ErrNotAnOpening, e.Closes, shape, part)
			}
			if e.Actor.Kind != record.KindComponent {
				var payload string
				if err := tx.QueryRow(ctx, `select payload from `+Table+` where id = $1`, e.Closes).Scan(&payload); err != nil {
					return err
				}
				if !humanWaitClose(e, payload) {
					return ErrWaitCloseNotComponent
				}
			}
			ended, err := alreadyEnded(ctx, tx, e.Closes)
			if err != nil {
				return err
			}
			if ended {
				return fmt.Errorf("%w: %q", ErrAlreadyEnded, e.Closes)
			}
			return nil
		})
}

// refuseVersionsAndClosingOnlyFields refuses an entry naming either version
// or any of the fields only a decision's closing may carry — every field a
// wait's opening or closing carries none of.
func refuseVersionsAndClosingOnlyFields(what string, e Entry) error {
	if e.PolicyVersion != "" || e.ScoreVersion != "" {
		return fmt.Errorf("%w: %s named policy %q, score %q", ErrVersionsRefused, what, e.PolicyVersion, e.ScoreVersion)
	}
	return refuseClosingOnlyFields(what, e)
}

// agentCredentialWait recognises the credential failure exception to the
// component actor rule. Other payloads remain opaque to this package.
func agentCredentialWait(e Entry) bool {
	if e.Actor.Kind != record.KindAgent || e.Principal.Actor != e.Actor || e.Principal.Validate() != nil {
		return false
	}
	var payload struct {
		Kind           string `json:"kind"`
		CredentialName string `json:"credential_name"`
	}
	return json.Unmarshal([]byte(e.Payload), &payload) == nil &&
		payload.Kind == "credential_unreachable" && payload.CredentialName != ""
}

// humanWaitClose admits the two waits a human ends at Work, keeping the human
// as actor and Work as caller: an overage authorised on a credential at its
// ceiling, and a commit accepted that master holds and the queue did not make.
// The closing repeats the opening's payload, which is what ties the
// authorisation to that credential and period, or to that commit. Any other
// human closing is refused.
func humanWaitClose(e Entry, openingPayload string) bool {
	if e.Actor.Kind != record.KindHuman || e.Principal.Validate() != nil ||
		e.Principal.Actor.Kind != record.KindComponent || e.Principal.Actor.Key != "work" {
		return false
	}
	var opening, closing map[string]any
	if json.Unmarshal([]byte(openingPayload), &opening) != nil || json.Unmarshal([]byte(e.Payload), &closing) != nil {
		return false
	}
	kind, _ := opening["kind"].(string)
	switch kind {
	case "credential_at_ceiling":
		name, _ := opening["credential_name"].(string)
		period, _ := opening["period_start"].(string)
		if name == "" || period == "" {
			return false
		}
	case "master holds a commit the queue did not make":
		commit, _ := opening["commit"].(string)
		if commit == "" {
			return false
		}
	default:
		return false
	}
	// json.Marshal writes a map's keys sorted, so equal maps marshal alike.
	a, errA := json.Marshal(opening)
	b, errB := json.Marshal(closing)
	return errA == nil && errB == nil && string(a) == string(b)
}
