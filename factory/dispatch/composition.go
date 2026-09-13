package dispatch

import (
	"context"

	"github.com/dulguun0225/borg/factory/item"
)

// Provisioning reads whether a service's repository and store are provisioned.
// It is the eighth condition that can hold an implementation dispatch.
type Provisioning interface {
	Provisioned(ctx context.Context, serviceID string) (bool, error)
}

// NoAdmissionSafeguard is what a dispatch composed with no reading uses:
// nothing waits, which is an install where an owner placed no such safeguard.
type NoAdmissionSafeguard struct{}

// HoldsReportDerivedIntents reports that nothing waits.
func (NoAdmissionSafeguard) HoldsReportDerivedIntents(context.Context) (bool, error) {
	return false, nil
}

// NoNotifier is what a dispatch composed with no notifier uses: nothing is
// delivered, and an escalation reaches Work through the item's stage alone.
type NoNotifier struct{}

// Escalated delivers nothing.
func (NoNotifier) Escalated(context.Context, string, item.Stage, string) error { return nil }

// NearingASpendCeiling delivers nothing. The ceiling still holds: what is
// missing is the notice before it, not the comparison.
func (NoNotifier) NearingASpendCeiling(context.Context, string, string, float64, float64, string) error {
	return nil
}
