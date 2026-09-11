package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/halt"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/legalhold"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/screens"
)

// The five rows that decide a record rather than an item, decided at Factory:
// the four withdrawals and shortenings, and the row every version of what an
// agent is told fires. Each fires like any other row, reads no threshold and no
// factor set, and waits on a human always.
//
// Each of the four is two appends and then one write: the gate fires the row,
// the human closes it, and the record's own approval is made from that close
// and names it. Nothing here approves a record without firing the row first,
// and package policy refuses an approval naming no close event, so the order
// cannot be taken the other way round.

// recordRow is one of the five as a caller says it: the row, the record it
// decides, the verdict, the reason a reject carries, and when the actor opened
// the row in Work — the one field only a screen fills.
//
// recordID is the withdrawal's or the shortening's own id and not the record it
// removes a protection from: the withdrawal is what an owner decides, and the
// record the row's open event names is read off it.
type recordRow struct {
	kind           string
	recordID       string
	verdict        gate.Verdict
	reason         string
	openedInWorkAt string
}

// decideOutsideEveryItemAt decides one of the four rows outside every item. On
// an approve the record's own approval is written from the close event and
// names it; on a reject the record stands, which is what not deciding the row
// already does, and the close event is what says a human refused it.
func decideOutsideEveryItemAt(ctx context.Context, pool *pgxpool.Pool, token lease.Token,
	actor record.Actor, r recordRow) error {
	if r.verdict == "" {
		r.verdict = gate.VerdictApprove
	}
	g, scoreVersion, err := rowGate(ctx, pool, token)
	if err != nil {
		return err
	}
	factory := newFactory(pool, token)
	given := gate.Given{
		Actor: actor, Verdict: r.verdict, Reason: r.reason, OpenedInWorkAt: r.openedInWorkAt,
	}

	switch r.kind {
	case gate.SafeguardWithdrawal.String():
		routed, safeguardID, err := safeguardWithdrawalRouting(ctx, pool, r.recordID)
		if err != nil {
			return err
		}
		closed, err := decideOutsideEveryItem(ctx, g, gate.Firing{
			Row: gate.SafeguardWithdrawal, RecordID: safeguardID, RoutedTo: routed,
		}, given)
		if err != nil {
			return err
		}
		if r.verdict != gate.VerdictApprove {
			return nil
		}
		_, err = factory.ApproveSafeguardWithdrawal(ctx, actor, r.recordID, closed.ID)
		return err

	case gate.HaltWithdrawal.String():
		written, err := halt.GetWithdrawal(ctx, pool, r.recordID)
		if err != nil {
			return err
		}
		closed, err := decideOutsideEveryItem(ctx, g, gate.Firing{
			Row: gate.HaltWithdrawal, RecordID: written.HaltID,
			RoutedTo: gate.RoutedTo{NotHuman: written.Actor.Key},
		}, given)
		if err != nil {
			return err
		}
		if r.verdict != gate.VerdictApprove {
			return nil
		}
		_, err = factory.ApproveHaltWithdrawal(ctx, actor, r.recordID, closed.ID)
		return err

	case gate.LegalHoldWithdrawal.String():
		written, err := legalhold.GetWithdrawal(ctx, pool, r.recordID)
		if err != nil {
			return err
		}
		closed, err := decideOutsideEveryItem(ctx, g, gate.Firing{
			Row: gate.LegalHoldWithdrawal, RecordID: written.HoldID,
			RoutedTo: gate.RoutedTo{NotHuman: written.Actor.Key},
		}, given)
		if err != nil {
			return err
		}
		if r.verdict != gate.VerdictApprove {
			return nil
		}
		_, err = factory.ApproveLegalHoldWithdrawal(ctx, actor, r.recordID, closed.ID)
		return err

	case gate.DecisionLogRetentionShortening.String():
		proposed, err := factorysettings.GetShortening(ctx, pool, r.recordID)
		if err != nil {
			return err
		}
		settings, err := factorysettings.Get(ctx, pool)
		if err != nil {
			return err
		}
		priors, err := priorsRestartedBy(ctx, pool, token, scoreVersion, actor, proposed.Seconds)
		if err != nil {
			return err
		}
		closed, err := decideOutsideEveryItem(ctx, g, gate.Firing{
			Row: gate.DecisionLogRetentionShortening, RecordID: settings.ID,
			RoutedTo:        gate.RoutedTo{NotHuman: proposed.Actor.Key},
			PriorsRestarted: priors,
		}, given)
		if err != nil {
			return err
		}
		if r.verdict != gate.VerdictApprove {
			return nil
		}
		_, err = factory.ApproveRetentionShortening(ctx, actor, r.recordID, closed.ID)
		return err

	default:
		return fmt.Errorf("%w: %q is none of the four rows that decide a record", screens.ErrRefused, r.kind)
	}
}

// decideRolePrompt decides the row every version of what an agent is told
// fires, on the version awaiting it — the head of the chain that is not in
// force. The three actions are the row's own: Approve, Reject with feedback,
// and Edit in place, which is [calls.EditRecordRow].
//
// The version's author is empty, which is a factor the score cannot compute
// and is resolved the way an unavailable factor always is: a human decides
// whatever the formula returns.
func (c *calls) decideRolePrompt(ctx context.Context, actor record.Actor, args screens.DecideRecordRowArgs) error {
	version, err := artifact.Get(ctx, c.p.d.pool, args.RecordID)
	if err != nil {
		return err
	}
	verdict := gate.Verdict(args.Verdict)
	if verdict == "" {
		verdict = gate.VerdictApprove
	}
	// The row names the version under decision as the artifact and no record:
	// what it decides is a document, and package gate refuses a firing at a row
	// that decides no record which names one.
	if _, err := decideOutsideEveryItem(ctx, c.p.gate, gate.Firing{
		Row: gate.RolePromptOrSkill, ArtifactID: version.ID,
	}, gate.Given{
		Actor: actor, Verdict: verdict, Reason: args.Reason,
		OpenedInWorkAt: args.OpenedInWorkAt,
	}); err != nil {
		return err
	}
	// The version in force moves on an approve, and the store's own in-force
	// read is what moves it: the approved ids it is given are read off the
	// log's close events, so a decision written here is in force at the next
	// read rather than at the next start.
	c.p.prompts.forget()
	// A version entering force is one of the records the design has dispatch
	// re-match on, so the stages whose role had none are re-matched here, where
	// the decision that put it in force is written. Nothing polls: a hold left
	// to the next unrelated dispatch would outlive its condition.
	lifted, err := c.p.dispatch.RematchOnRolePromptInForce(ctx)
	if err != nil {
		return err
	}
	if len(lifted) > 0 {
		fmt.Fprintf(c.p.d.out, "The role prompt version in force lifted %d hold(s)\n", len(lifted))
		c.changed("work", listAddressID)
	}
	c.changed("factory", listAddressID)
	c.changed("home", listAddressID)
	return nil
}

// fireRolePromptRow fires the row again over a version a human authored at it,
// which is what an Edit in place at this row comes to: the row names a version
// and no item, so the firing is the version's own and there is no superseded
// row on an item's timeline to abandon.
func (c *calls) fireRolePromptRow(ctx context.Context, version artifact.Artifact) error {
	opened, err := c.p.gate.Fire(ctx, gate.Firing{
		Row: gate.RolePromptOrSkill, ArtifactID: version.ID,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(c.p.d.out, "Version %s of the %s's role prompt authored at the gate; row %s waits on %s\n",
		version.ID, version.Role, opened.Row.ID, waitedOn(opened.WaitsOn))
	return nil
}

// decideOutsideEveryItem fires one row that belongs to no item and takes the
// human's verdict at it. Two appends: the open event, which names the record
// under decision, who the row waits on and the human it may not be closed by,
// and the close event, which is what the record's own approval is then written
// from.
//
// A row already open on that record is decided rather than fired again. One gate
// on one subject has at most one pending row, so a verdict the gate turned away
// — a close by the human the record's own routing bars — leaves the row open,
// and a second firing over it is refused: without this, the first refusal at a
// row would be the last verdict it could ever take.
func decideOutsideEveryItem(ctx context.Context, g *gate.Gate, firing gate.Firing,
	given gate.Given) (decisionlog.Row, error) {
	opened, err := g.Fire(ctx, firing)
	if errors.Is(err, gate.ErrRowPending) {
		opened, err = alreadyOpen(ctx, g, firing)
	}
	if err != nil {
		return decisionlog.Row{}, err
	}
	if given.Verdict == gate.VerdictApprove {
		given.Holds = opened.Holds
	}
	return g.Decide(ctx, opened, given)
}

// alreadyOpen is the row a firing found pending: the one open event on that row
// whose subject is the record or the version the firing names. The subject is
// the record at the four rows that decide one and the version under decision at
// the row every version of what an agent is told fires, which is what package
// gate keys a pending row on at each.
func alreadyOpen(ctx context.Context, g *gate.Gate, firing gate.Firing) (gate.Opened, error) {
	pending, err := g.Pending(ctx)
	if err != nil {
		return gate.Opened{}, err
	}
	for _, opened := range pending {
		if opened.Gate.Kind != firing.Row.Kind {
			continue
		}
		if firing.RecordID != "" && opened.Subject.RecordID == firing.RecordID {
			return opened, nil
		}
		if firing.RecordID == "" && opened.ArtifactID == firing.ArtifactID {
			return opened, nil
		}
	}
	return gate.Opened{}, fmt.Errorf("%w: %s over %s%s", gate.ErrRowPending,
		firing.Row, firing.RecordID, firing.ArtifactID)
}

// autoPassRatesReader is the principal the read [score.RealizedAutoPass] makes
// is recorded as: a component and not the human whose write called for it, the
// way every internal read composed rather than made at a screen is.
var autoPassRatesReader = principal.OfComponent("policy")

// newFactory is package policy's writer, composed with the score's own read of
// the realized auto-pass rate: every construction needs it, [policy.NewFactory]
// refusing a nil one. Since is empty, which reads every closed firing at this
// row and this threshold there has ever been, because a threshold write freezes
// the rate as it stands at the write and not over a window the write chooses.
func newFactory(pool *pgxpool.Pool, token lease.Token) *policy.Factory {
	return policy.NewFactory(pool, token, autoPassRates(pool, token))
}

// autoPassRates reads the realized auto-pass rate at a threshold, per factor
// set, from every closed decision at this gate row and this threshold the log
// holds: [score.RealizedAutoPass] groups every closed firing by factor set,
// subject and threshold already, so this is a filter to the one row and number
// a threshold write names and never a second computation of the rate.
func autoPassRates(pool *pgxpool.Pool, token lease.Token) func(context.Context, policy.Scope, string, float64) ([]policy.AutoPassRate, error) {
	return func(ctx context.Context, _ policy.Scope, gateRow string, threshold float64) ([]policy.AutoPassRate, error) {
		realized, err := score.RealizedAutoPass(ctx, pool, token, autoPassRatesReader, "")
		if err != nil {
			return nil, err
		}
		var rates []policy.AutoPassRate
		for _, r := range realized {
			if r.Subject != gateRow || r.Threshold != threshold {
				continue
			}
			rates = append(rates, policy.AutoPassRate{FactorSet: string(r.FactorSet), Rate: r.RealizedRate})
		}
		return rates, nil
	}
}
