// hours_test.go is [notifier.Wait.PageAt]: the next hour a wait of the
// second kind held back may page, computed once at the deferral rather than
// read again from the service's own record at every pass.
package notifier_test

import (
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/notifier"
)

// TestPageDeferredKeepsTheHourComputedAtTheDeferral is finding 5's own fix:
// an owner widening the service's paging hours after the deferral does not
// move an hour already computed and stored, so a pass finding the current
// hours now cover the moment still does not page before the stored instant.
func TestPageDeferredKeepsTheHourComputedAtTheDeferral(t *testing.T) {
	ctx, pool, token, n, channels := newNotifier(t)
	serviceID := aServiceWithNoPagingHoursNow(t, ctx, pool, token)

	held := notifier.Wait{
		Row: "mis_frozen_hour", Kind: notifier.KindDriftMismatch,
		Waiting: "a record disagrees with what runs", Worse: true, ServiceID: serviceID,
	}
	if _, err := n.Notify(ctx, held); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if channels.on(notifier.ChannelPage) != 0 {
		t.Fatalf("the wait paged outside the service's hours")
	}

	// The owner widens the hours to cover this instant, after the deferral.
	// The hour [notifier.Notifier.PageDeferred] pages at was already computed
	// and stored, and does not move.
	authorHoursCovering(t, ctx, pool, token, serviceID, time.Now())

	paged, err := n.PageDeferred(ctx, nil)
	if err != nil {
		t.Fatalf("PageDeferred: %v", err)
	}
	if len(paged) != 0 || channels.on(notifier.ChannelPage) != 0 {
		t.Fatalf("PageDeferred paged %v after the hours were widened, want the stored instant kept", paged)
	}
}
