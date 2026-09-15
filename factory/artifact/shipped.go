package artifact

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// Shipped is one fleet artifact the product ships. Role prompts name Role,
// skills name Subject, and the selection rule names neither.
type Shipped struct {
	Kind    Kind
	Role    string
	Subject string
	Content string
}

// EnterChangedShipped enters every shipped fleet artifact whose newest shipped
// entry does not already carry the same words. The store decides whether this
// is the install's first start from whether any shipped entry exists: only
// that start's entries stand in force ungated; every later upgrade awaits its
// gate, including a newly started chain.
func (s *Store) EnterChangedShipped(ctx context.Context, actor record.Actor,
	bundle string, artifacts []Shipped) ([]Shipped, error) {
	install, err := s.firstShippedStart(ctx)
	if err != nil {
		return nil, err
	}
	if install {
		return s.enterInstalledShipped(ctx, actor, bundle, artifacts)
	}
	var entered []Shipped
	for _, one := range artifacts {
		last, found, err := NewestShipped(ctx, s.pool, one.Kind, one.Role, one.Subject)
		if err != nil {
			return nil, err
		}
		if found && last.Content == one.Content {
			continue
		}
		if _, err := s.EnterShipped(ctx, actor, one.Kind, one.Role, one.Subject,
			one.Content, EnteredByUpgradeFirstStart, bundle); err != nil {
			return nil, err
		}
		entered = append(entered, one)
	}
	return entered, nil
}

func (s *Store) enterInstalledShipped(ctx context.Context, actor record.Actor,
	bundle string, artifacts []Shipped) ([]Shipped, error) {
	if err := refuseFactoryStart(actor); err != nil {
		return nil, err
	}
	if bundle == "" {
		return nil, ErrShippedBundleIdentityEmpty
	}
	for _, one := range artifacts {
		if _, err := fleetKey(one.Kind, one.Role, one.Subject); err != nil {
			return nil, err
		}
	}
	if len(artifacts) == 0 {
		return nil, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("artifact: beginning the shipped install: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, s.token); err != nil {
		return nil, err
	}

	entered := make([]Shipped, 0, len(artifacts))
	for _, one := range artifacts {
		key, _ := fleetKey(one.Kind, one.Role, one.Subject)
		if _, err := insertVersion(ctx, tx, actor, By{}, key, one.Kind, one.Content, "",
			shipped{EnteredBy: EnteredByInstall, BundleIdentity: bundle}); err != nil {
			return nil, err
		}
		entered = append(entered, one)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("artifact: committing the shipped install: %w", err)
	}
	return entered, nil
}

func (s *Store) firstShippedStart(ctx context.Context) (bool, error) {
	var found bool
	err := s.pool.QueryRow(ctx, `select exists(select 1 from `+Table+` where item_id = '' and entered_by <> '')`).Scan(&found)
	if err != nil {
		return false, err
	}
	return !found, nil
}
