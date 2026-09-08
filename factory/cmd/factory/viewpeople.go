package main

import (
	"context"

	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/screens"
)

// People: the owner's row first, and then every row of the declaration — the
// duties each key holds, the obligations outside the twelve, the credentials
// each lent with the ceiling and the rates on them, and whether the row acts
// anywhere at all.
//
// The owner's row is no entry of the declaration: its key is the one this
// process was made as and its name is the mapping the install wrote for it. A
// fresh install's declaration is empty, so the key every acting call is
// exempted on would otherwise be readable nowhere. It is first because a human
// who has not yet declared anything is looking for it.
//
// A name is resolved through the mapping and never held: the declaration is
// the one place a per-person key maps to a name, and the key is carried beside
// it because a key whose mapping was erased still routes.

// People is every row of the People declaration.
func (v *views) People(ctx context.Context, _ principal.Principal) (screens.People, error) {
	declarations, err := people.All(ctx, v.p.d.pool)
	if err != nil {
		return screens.People{}, err
	}
	credentials, err := people.Credentials(ctx, v.p.d.pool)
	if err != nil {
		return screens.People{}, err
	}
	rates, err := people.AllRates(ctx, v.p.d.pool)
	if err != nil {
		return screens.People{}, err
	}

	rows := map[string]*screens.PersonRow{}
	var order []string
	rowFor := func(key string) *screens.PersonRow {
		if row, found := rows[key]; found {
			return row
		}
		row := &screens.PersonRow{Key: key, Name: v.nameOf(ctx, key)}
		rows[key], order = row, append(order, key)
		return row
	}

	// The owner's row before any other, so what the rest of this adds to it is
	// added to a row that is already first. It acts anywhere whatever the
	// declaration holds, which is what the exemption in ./calls.go reads.
	ownerRow := rowFor(v.p.human.Key)
	ownerRow.Owner, ownerRow.ActsAnywhere = true, true

	for _, one := range declarations {
		if !one.Holds() {
			continue
		}
		row := rowFor(one.Key)
		switch {
		case one.Obligation != "":
			row.Obligations = append(row.Obligations, string(one.Obligation))
		default:
			row.Duties = append(row.Duties, int64(one.Duty))
		}
		row.ActsAnywhere = true
	}

	for _, one := range credentials {
		row := rowFor(one.Key)
		lent := screens.LentCredential{Name: one.Name, Kind: string(one.Kind)}
		if one.Ceiling.Authored() {
			lent.Ceiling = &screens.Ceiling{
				Amount:    one.Ceiling.Amount,
				Currency:  one.Ceiling.Currency,
				Period:    periodOf(one.Ceiling),
				StartDate: one.Ceiling.StartDate,
				Zone:      one.Ceiling.StartZone,
			}
		}
		for _, rate := range rates {
			if rate.CredentialName != one.Name {
				continue
			}
			lent.Rates = append(lent.Rates, screens.Rate{
				Kind: rate.Unit, ModelVersion: rate.ModelVersion, Effort: rate.Effort,
				Amount: rate.Rate, Currency: one.Ceiling.Currency,
			})
		}
		row.Credentials = append(row.Credentials, lent)
		if one.Lent() {
			row.ActsAnywhere = true
		}
	}

	view := screens.People{Rows: make([]screens.PersonRow, 0, len(order))}
	for _, key := range order {
		view.Rows = append(view.Rows, *rows[key])
	}
	return view, nil
}

// periodOf is the ceiling's period as one word: the length and the unit the
// declaration holds, which is what a reader compares a burn rate against.
func periodOf(ceiling people.Ceiling) string {
	if ceiling.Length == 1 {
		return string(ceiling.Unit)
	}
	return formatNumber(float64(ceiling.Length)) + " " + string(ceiling.Unit)
}

// actsAnywhere reports whether one per-person key holds a duty, holds an
// obligation, or lends a credential — which is what says a call from that row
// is not the design's read-only row. It is the one reading every acting call at
// Work, Ops, Factory and People makes before any screen-specific check.
func actsAnywhere(ctx context.Context, v *views, key string) (bool, error) {
	declarations, err := people.All(ctx, v.p.d.pool)
	if err != nil {
		return false, err
	}
	for _, one := range declarations {
		if one.Key == key && one.Holds() {
			return true, nil
		}
	}
	credentials, err := people.Credentials(ctx, v.p.d.pool)
	if err != nil {
		return false, err
	}
	for _, one := range credentials {
		if one.Key == key && one.Lent() {
			return true, nil
		}
	}
	return false, nil
}
