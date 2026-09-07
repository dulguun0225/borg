package main

import (
	"context"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/legalhold"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/screens"
)

// People's own writes: the duties and the obligations a human holds, the
// credentials each lent, the ceiling and the rates on one, and the mapping from
// a per-person key to a name — the one record kept outside the chain, so an
// erasure deletes it alone.
//
// Every write here but the mapping appends a policy version, with People as
// caller: what the chain buys is that who held a duty at any row is a read of
// the chain and not of today's declaration.

// declarations is the People writer, which appends a policy version per write.
func (c *calls) declarations() *people.Writer {
	return people.NewWriter(c.p.d.pool, c.p.d.token, policy.NewFactory(c.p.d.pool, c.p.d.token))
}

// DeclareDuty holds one of the owner's twelve duties on a per-person key.
func (c *calls) DeclareDuty(ctx context.Context, who principal.Principal, args screens.DeclareDutyArgs) error {
	return c.declare(ctx, who, args.HumanKey, people.OfDuty(people.Duty(args.Duty)))
}

// WithdrawDuty withdraws a duty from a per-person key. The row is kept: who
// held a duty at any row is a read of the chain, so removing a holder leaves a
// row and does not delete one.
func (c *calls) WithdrawDuty(ctx context.Context, who principal.Principal, args screens.WithdrawDutyArgs) error {
	return c.withdrawHolding(ctx, who, args.HumanKey, people.OfDuty(people.Duty(args.Duty)))
}

// DeclareObligation names an obligation outside the twelve: hosting,
// installing the drift detector, or composing the fleet.
func (c *calls) DeclareObligation(ctx context.Context, who principal.Principal, args screens.DeclareObligationArgs) error {
	return c.declare(ctx, who, args.HumanKey, people.OfObligation(people.Obligation(args.Obligation)))
}

// WithdrawObligation withdraws an obligation from a per-person key.
func (c *calls) WithdrawObligation(ctx context.Context, who principal.Principal, args screens.WithdrawObligationArgs) error {
	return c.withdrawHolding(ctx, who, args.HumanKey, people.OfObligation(people.Obligation(args.Obligation)))
}

// declare is one holding written, and withdrawHolding is one ended. Both are
// here rather than in each call because the four differ only in which of the
// two a holding names.
func (c *calls) declare(ctx context.Context, who principal.Principal, key string, holding people.Holding) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if _, err := c.declarations().Declare(ctx, actor, key, holding); err != nil {
		return err
	}
	c.changed("people", listAddressID)
	return nil
}

func (c *calls) withdrawHolding(ctx context.Context, who principal.Principal, key string, holding people.Holding) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	standing, err := people.ByHolding(ctx, c.p.d.pool, key, holding)
	if err != nil {
		return err
	}
	if _, err := c.declarations().Withdraw(ctx, actor, standing.ID); err != nil {
		return err
	}
	c.changed("people", listAddressID)
	return nil
}

// LendCredential lends a credential the fleet runs on, naming whether the
// account is a person's own or an organisation's.
func (c *calls) LendCredential(ctx context.Context, who principal.Principal, args screens.LendCredentialArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if _, err := c.declarations().Lend(ctx, actor, args.HumanKey, args.Credential,
		people.AccountKind(args.Kind)); err != nil {
		return err
	}
	c.changed("people", listAddressID)
	c.changed("factory", listAddressID)
	return nil
}

// TakeBackCredential takes a lent credential back. Nothing lifts the hold that
// leaves until an owner attaches another, which is why it is a row in Work and
// not a wait the factory recomputes away.
func (c *calls) TakeBackCredential(ctx context.Context, who principal.Principal, args screens.TakeBackCredentialArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if _, err := c.declarations().TakeBack(ctx, actor, args.Credential); err != nil {
		return err
	}
	c.changed("people", listAddressID)
	c.changed("home", listAddressID)
	// The lent credentials are a list at Factory and a taken-back one is
	// rendered there as taken back, which is what [calls.LendCredential]
	// announces the same address for.
	c.changed("factory", listAddressID)
	return nil
}

// AuthorCeiling authors a spend ceiling on a lent credential: the one field
// here the factory enforces. The period's start date carries the IANA zone it
// was authored in, and every reader computes in that zone and in no other.
func (c *calls) AuthorCeiling(ctx context.Context, who principal.Principal, args screens.AuthorCeilingArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if err := zoneNamed(args.Zone); err != nil {
		return err
	}
	if _, err := c.declarations().AuthorCeiling(ctx, actor, args.Credential, people.Ceiling{
		Amount:    args.Amount,
		Currency:  args.Currency,
		Length:    int(args.Length),
		Unit:      people.PeriodUnit(args.PeriodUnit),
		StartDate: args.StartDate,
		StartZone: args.Zone,
	}); err != nil {
		return err
	}
	c.changed("people", listAddressID)
	c.changed("factory", listAddressID)
	return nil
}

// AuthorRate authors what a provider's units convert at, per kind, model
// version and effort. A run whose converted amount is absent because a kind it
// returned has no rate fails closed, and authoring the rate is what clears it.
func (c *calls) AuthorRate(ctx context.Context, who principal.Principal, args screens.AuthorRateArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if _, err := c.declarations().AuthorRate(ctx, actor, args.Credential, args.Currency,
		args.Kind, args.ModelVersion, args.Effort, args.Amount); err != nil {
		return err
	}
	c.changed("people", listAddressID)
	c.changed("factory", listAddressID)
	return nil
}

// WriteMapping writes a per-person key's name. The mapping is kept outside the
// chain so an erasure can delete it alone, so this write appends no policy
// version.
func (c *calls) WriteMapping(ctx context.Context, who principal.Principal, args screens.WriteMappingArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if _, err := people.WriteMapping(ctx, c.p.d.pool, c.p.d.token, actor,
		args.HumanKey, args.Name); err != nil {
		return err
	}
	c.changed("people", listAddressID)
	return nil
}

// DeleteMapping erases a per-person key's name: the key stands on every record
// it was written to, so the chain, its links and its counts are undisturbed and
// what those records name is gone.
//
// It is refused, and the refusal recorded, while a legal hold reaches any
// record that key is written on — the mapping is what a hold over a decision
// preserves. Package people reads the hold over the whole install itself and
// takes the narrower reading from the caller, which is this: a hold on any
// service or project this key has a record under reaches it too.
func (c *calls) DeleteMapping(ctx context.Context, who principal.Principal, args screens.DeleteMappingArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if err := people.DeleteMapping(ctx, c.p.d.pool, c.p.d.token, args.HumanKey,
		func(ctx context.Context) (bool, error) {
			return c.holdReaching(ctx, actor, args.HumanKey)
		}); err != nil {
		return err
	}
	c.changed("people", listAddressID)
	return nil
}

// holdReaching is whether a legal hold reaches a record this key is written
// on. Package people reads the hold over the whole install itself; what this
// adds is the narrower reading it cannot make — a hold on a service or a
// project standing over a decision this key closed or acknowledged.
//
// The decisions this key is written on are read off the log, and the service
// each names is compared against every standing hold's subject. A hold on a
// project reaches every service in it, which is the reach the record itself
// declares and [legalhold.Reaching] answers for one subject at a time.
func (c *calls) holdReaching(ctx context.Context, actor record.Actor, key string) (bool, error) {
	standing, err := legalhold.Standing(ctx, c.p.d.pool)
	if err != nil {
		return false, err
	}
	if len(standing) == 0 {
		return false, nil
	}
	// The read is made as the human asking for the erasure and never as the
	// key being erased: the log's read event names the principal that asked.
	rows, err := decisionlog.NewReader(c.p.d.pool, c.p.d.token).Read(ctx, asPrincipal(actor))
	if err != nil {
		return false, err
	}
	openings := map[string]decisionlog.Row{}
	for _, row := range rows {
		if row.Shape == decisionlog.ShapeDecision && row.Part == decisionlog.PartOpen {
			openings[row.ID] = row
		}
	}
	for _, row := range rows {
		if row.Shape != decisionlog.ShapeDecision || row.Actor.Key != key {
			continue
		}
		opening, is := openings[row.Closes]
		if !is {
			continue
		}
		opened, readable := openingOn(opening, func(o gate.Opened) bool { return o.Subject.ServiceID != "" })
		if !readable {
			continue
		}
		reaches, err := legalhold.Reaching(ctx, c.p.d.pool,
			legalhold.Subject{Kind: legalhold.SubjectService, ID: opened.Subject.ServiceID})
		if err != nil {
			return false, err
		}
		if reaches {
			return true, nil
		}
	}
	return false, nil
}
