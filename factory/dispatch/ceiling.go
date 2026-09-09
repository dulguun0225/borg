// ceiling.go is the spend ceiling an owner authors on a credential: the sum
// over the period in force, the two stops it fails closed on, the notice at a
// fraction of the amount authored, and the clear that authorises an overage for
// one period. It is split from credential.go by subject at the 500-line bound,
// that file being the two rows a credential itself stands as.
package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/dulguun0225/borg/factory/agentrun"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/record"
)

// WantsARate is one run whose converted amount is absent because a kind it
// returned has no rate: the kinds, and the model version and effort a rate for
// them is authored under. It is what the ceiling's row names where the ceiling
// failed closed on an unpriced run, and authoring the rate is what clears it.
type WantsARate struct {
	Kinds        []string `json:"kinds"`
	ModelVersion string   `json:"model_version"`
	Effort       string   `json:"effort"`
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
	spend, err := agentrun.SpendByCredentialIn(ctx, d.c.Pool, credentialName, periodOf(lent.Ceiling), time.Now())
	if err != nil {
		return ceilingReading{}, err
	}
	// The period the sum was taken over is the read's own: the query bounds it
	// at both ends, so a row naming a start the read did not use would be a row
	// about a period nothing was summed over.
	periodStart := spend.PeriodStart
	reading := ceilingReading{periodStart: periodStart}
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

// periodOf is the ceiling's period as the sum is bucketed against it. The two
// packages spell the period apart — the owner authors it on the People
// declaration and the sum is taken over the agent run records — so the
// conversion is made here, where the ceiling and the records meet.
func periodOf(ceiling people.Ceiling) agentrun.Period {
	return agentrun.Period{
		StartDate: ceiling.StartDate, StartZone: ceiling.StartZone,
		Length: ceiling.Length, Unit: agentrun.PeriodUnit(ceiling.Unit),
	}
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
