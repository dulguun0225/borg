package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/release"
)

func (p *path) shippedRevertSiblings(ctx context.Context, in intent.Intent) ([]item.Item, bool, error) {
	if in.Evidence == "" {
		return nil, false, nil
	}
	var evidence intent.Evidence
	if err := json.Unmarshal([]byte(in.Evidence), &evidence); err != nil {
		return nil, false, fmt.Errorf("factory: reading revert evidence on intent %s: %w", in.ID, err)
	}
	if evidence.ReleaseID == "" {
		return nil, false, nil
	}

	failed, err := release.Get(ctx, p.d.pool, evidence.ReleaseID)
	if err != nil {
		return nil, false, err
	}
	if failed.ItemID == "" {
		return nil, false, nil
	}
	original, err := item.Get(ctx, p.d.pool, failed.ItemID)
	if err != nil {
		return nil, false, err
	}
	originalSiblings, err := item.ForIntent(ctx, p.d.pool, original.IntentID)
	if err != nil {
		return nil, false, err
	}
	addresses := make(map[string][]string)
	for _, sibling := range originalSiblings {
		if _, found := addresses[sibling.ServiceID]; found {
			continue
		}
		addresses[sibling.ServiceID], err = p.addressesOf(ctx, sibling.ServiceID)
		if err != nil {
			return nil, false, err
		}
	}
	shipped, err := item.ShippedSiblings(ctx, p.d.pool, evidence.ReleaseID, p.production.ID, addresses)
	return shipped, true, err
}

// revertServices is the service names of the shipped siblings the item package
// selected. The caller's proposed services remain the fallback for an intent
// without revert evidence or without a shipped sibling.
func (p *path) revertServices(ctx context.Context, in intent.Intent, proposed []string) ([]string, error) {
	shipped, found, err := p.shippedRevertSiblings(ctx, in)
	if err != nil {
		return nil, err
	}
	if !found {
		return proposed, nil
	}
	services := make([]string, 0, len(shipped))
	seen := make(map[string]bool, len(shipped))
	for _, sibling := range shipped {
		svc, err := p.serviceOf(ctx, sibling.ServiceID)
		if err != nil {
			return nil, err
		}
		if !seen[svc.Name] {
			seen[svc.Name] = true
			services = append(services, svc.Name)
		}
	}
	if len(services) == 0 {
		return proposed, nil
	}
	return services, nil
}
