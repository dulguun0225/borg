// paidfor.go is what the People declaration says about the credential a run
// ran on, read at the run and written onto the run's own record. It is split
// from credential.go by subject: that file is the two conditions a credential
// stops a dispatch on, and this is what a run that proceeded records about
// whose account it spent.
package dispatch

import (
	"context"

	"github.com/dulguun0225/borg/factory/agentrun"
	"github.com/dulguun0225/borg/factory/people"
)

// paidFor is what the People declaration says about the credential an entry
// runs on, read once per dispatch and written onto every agent run record that
// dispatch makes: the per-person key of whoever lent it, whether the account is
// a person's own or an organisation's, the rates each kind is converted at, and
// the currency they are authored in.
//
// It is copied onto each record rather than resolved at the read, because a
// rate corrected later must not reprice what a record already wrote and a
// declaration edited later must not change a past record. What that costs is
// what the design states: each record repeats what the declaration said.
//
// The currency is the credential's own column, which the declaration writes at
// the first rate authored on it: so a credential carrying a rate carries the
// currency that rate is in, and the one cause of an absent converted amount is
// a kind the rates do not cover.
//
// A credential nobody declared leaves every field empty. The agent run record
// refuses such a run — it names whose account it spent — so an entry naming a
// credential the declaration does not hold is a dispatch that fails at the
// record rather than one that records a run nobody paid for.
type paidFor struct {
	lenderKey   string
	accountKind agentrun.AccountKind
	rates       []people.Rate
	currency    string
}

// readPaidFor reads it. The account kind is the declaration's own word, and the
// two vocabularies are the same two words spelled in each package.
func (d *Dispatch) readPaidFor(ctx context.Context, credentialName string) (paidFor, error) {
	if credentialName == "" {
		return paidFor{}, nil
	}
	lent, found, err := people.CredentialNamed(ctx, d.c.Pool, credentialName)
	if err != nil {
		return paidFor{}, err
	}
	if !found {
		return paidFor{}, nil
	}
	rates, err := people.RatesFor(ctx, d.c.Pool, credentialName)
	if err != nil {
		return paidFor{}, err
	}
	return paidFor{
		lenderKey:   lent.Key,
		accountKind: agentrun.AccountKind(lent.Kind),
		rates:       rates,
		currency:    lent.Ceiling.Currency,
	}, nil
}

// ratesFor is the rate each kind the provider returned was converted at, which
// is what the run record carries beside the amount. A kind with no rate is left
// out, and its absence is why the amount is absent too.
func (p paidFor) ratesFor(modelVersion, effort string, units map[string]int64) map[string]float64 {
	if len(units) == 0 {
		return nil
	}
	at := map[string]float64{}
	for kind := range units {
		if rate, found := people.RateFor(p.rates, kind, modelVersion, effort); found {
			at[kind] = rate
		}
	}
	if len(at) == 0 {
		return nil
	}
	return at
}
