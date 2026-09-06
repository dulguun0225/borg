package contract

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/record"
)

// Publication is one contract of one release: the release, its number, the item
// it was cut from, and the form that release's build publishes. The service is the
// form's publisher, which is the service the release is of.
//
// ItemID is empty on a release minted over an accepted commit, which names a build
// and no item. The queue writes that release's contract versions in the same write
// as the release, as it does at a fast-forward, so the release is the key and the
// item is what a version has where a gate decided one.
type Publication struct {
	ServiceID     string
	ReleaseID     string
	ReleaseNumber int64
	ItemID        string
	Form          Form
}

// Published is what one publication did: the contract, the version where one was
// minted, what the form did to the version below it, and whether anything was
// written at all. Most releases publish no new version, so Moved is false and the
// version is the one already in force.
type Published struct {
	Contract Contract
	Version  Version
	Change   Change
	// Moved is whether this publication minted a version. False is a release
	// whose form is identical to the version in force below it, which is most
	// releases and is not a failure.
	Moved bool
	// Created is whether this publication is the contract's first, which is the
	// merge the contract exists from.
	Created bool
}

// Publish writes one contract's version inside tx. Its one caller is the merge
// queue, which calls it inside the transaction that mints the release's number,
// so a number and the versions its release publishes commit together or not at
// all — one merge cannot leave a number with no version, or a version under a
// number nothing minted.
//
// The contract row is written where this is the first release publishing the
// interface, in the same write as that release's first version, because a contract
// changes only inside its service's items and every write to it happens at a
// release. A form whose kind is not the kind the contract already has is
// [ErrKindChanged]: the kind has to be single-valued across versions.
//
// A form identical to the version in force below this release mints nothing and
// is not an error. That is the ordinary case — most releases publish no new
// contract version at all — and it is what keeps the version tracking the form
// rather than the release.
func Publish(ctx context.Context, tx pgx.Tx, actor record.Actor, p Publication) (Published, error) {
	if err := actor.Validate(); err != nil {
		return Published{}, err
	}
	if err := p.validate(); err != nil {
		return Published{}, err
	}
	form := p.Form.Sorted()

	c, found, err := ByName(ctx, tx, p.ServiceID, form.Name)
	if err != nil {
		return Published{}, err
	}
	published := Published{}
	if !found {
		c = Contract{
			ID:        record.NewID(IDPrefix),
			Actor:     actor,
			At:        record.Now(),
			ServiceID: p.ServiceID,
			Name:      form.Name,
			Kind:      form.Kind,
		}
		if _, err := tx.Exec(ctx, `insert into `+Table+`
			(id, format_version, actor_kind, actor_key, actor_key_basis, at, service_id, name, kind)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			c.ID, FormatVersion, string(c.Actor.Kind), c.Actor.Key, string(c.Actor.Basis), c.At, c.ServiceID, c.Name, string(c.Kind),
		); err != nil {
			return Published{}, fmt.Errorf("contract: creating %s of %s: %w", form.Name, p.ServiceID, err)
		}
		published.Created = true
	} else if c.Kind != form.Kind {
		return Published{}, fmt.Errorf("%w: %s of %s is a %s and the build publishes a %s",
			ErrKindChanged, c.Name, c.ServiceID, c.Kind, form.Kind)
	}
	published.Contract = c

	// The version below this release, and the form it publishes. Strictly below,
	// because the release this publication is for is the one being minted and has
	// no version yet — and a re-publication of a release that already has one is
	// refused by the unique constraint rather than diffed against itself.
	before := Form{}
	next := FirstVersion
	inForce, hadOne, err := VersionAt(ctx, tx, c.ID, p.ReleaseNumber-1)
	if err != nil {
		return Published{}, err
	}
	if hadOne {
		before, err = FormOf(ctx, tx, c, inForce.ID)
		if err != nil {
			return Published{}, err
		}
		published.Version = inForce
	}

	change := Diff(before, form)
	published.Change = change
	if hadOne && !change.Moved() {
		return published, nil
	}
	if hadOne {
		// Major means a consumer breaks, and [Change.Breaking] is what breaks
		// whatever any declaration says: the store rule's addable pair — a
		// not-null constraint and a domain check — is not in it, and one that
		// reached this write is one enforcement found no declaration in force
		// violates. So a release that only constrains mints a minor here, which
		// is the version enforcement reported it would mint.
		next = inForce.Semver.Next(len(change.Breaking) > 0)
	}

	v := Version{
		ID:            record.NewID(VersionIDPrefix),
		Actor:         actor,
		At:            record.Now(),
		ContractID:    c.ID,
		ServiceID:     p.ServiceID,
		ReleaseID:     p.ReleaseID,
		ReleaseNumber: p.ReleaseNumber,
		ItemID:        p.ItemID,
		Semver:        next,
	}
	if _, err := tx.Exec(ctx, `insert into `+VersionTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, contract_id, service_id, release_id, release_number,
		item_id, major, minor, patch)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		v.ID, FormatVersionVersion, string(v.Actor.Kind), v.Actor.Key, string(v.Actor.Basis), v.At, v.ContractID, v.ServiceID,
		v.ReleaseID, v.ReleaseNumber, v.ItemID, v.Semver.Major, v.Semver.Minor, v.Semver.Patch,
	); err != nil {
		return Published{}, fmt.Errorf("contract: minting %s of %s at release %d: %w",
			v.Semver, c.Name, p.ReleaseNumber, err)
	}
	for _, e := range form.Elements {
		var low, high *float64
		if e.Range != nil {
			low, high = &e.Range.Low, &e.Range.High
		}
		if _, err := tx.Exec(ctx, `insert into `+ElementTable+`
			(id, format_version, actor_kind, actor_key, actor_key_basis, at, contract_version_id, contract_id, name,
			kind, element_position, declared_type, required, populated, deprecated, accepted_domain,
			range_low, range_high, not_null, unique_rule)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)`,
			record.NewID(ElementIDPrefix), FormatVersionElement, string(actor.Kind), actor.Key, string(actor.Basis), v.At,
			v.ID, c.ID, e.Name, string(e.Kind), string(e.Position), e.Type, e.Required, e.Populated, e.Marked,
			DomainText(e.Domain), low, high, e.NotNull, e.Unique,
		); err != nil {
			return Published{}, fmt.Errorf("contract: writing element %s of %s %s: %w",
				e.Name, c.Name, v.Semver, err)
		}
	}
	published.Version = v
	published.Moved = true
	return published, nil
}

// PublishAll is every form one release publishes, in the order they are given.
// It is the call the merge queue makes: a release publishes as many contracts as
// its build declares, and each is diffed against its own version in force.
func PublishAll(ctx context.Context, tx pgx.Tx, actor record.Actor,
	serviceID, releaseID string, releaseNumber int64, itemID string, forms []Form) ([]Published, error) {
	published := make([]Published, 0, len(forms))
	for _, form := range forms {
		one, err := Publish(ctx, tx, actor, Publication{
			ServiceID:     serviceID,
			ReleaseID:     releaseID,
			ReleaseNumber: releaseNumber,
			ItemID:        itemID,
			Form:          form,
		})
		if err != nil {
			return nil, err
		}
		published = append(published, one)
	}
	return published, nil
}

// validate refuses a publication missing something every one names. The item is
// not among them: a release minted over an accepted commit names a build and no
// item, and the version it publishes is keyed by the release.
func (p Publication) validate() error {
	for _, required := range []struct{ what, value string }{
		{"service", p.ServiceID}, {"release", p.ReleaseID},
	} {
		if required.value == "" {
			return fmt.Errorf("%w: it names no %s", ErrPublishIncomplete, required.what)
		}
	}
	if p.ReleaseNumber < 1 {
		return fmt.Errorf("%w: the release's number is %d", ErrPublishIncomplete, p.ReleaseNumber)
	}
	return p.Form.Validate()
}
