package mergequeue

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/securitypredicate"
)

func (q *Queue) reverify(ctx context.Context, it item.Item, ahead []item.Item) (Verified, error) {
	verified, err := q.repo.Reverify(ctx, it, ahead)
	if err != nil {
		return Verified{}, err
	}
	return q.applySecurityPredicates(verified), nil
}

func (q *Queue) reverifyCommit(ctx context.Context, serviceID, commit string) (Verified, error) {
	verified, err := q.repo.VerifyCommit(ctx, serviceID, commit)
	if err != nil {
		return Verified{}, err
	}
	return q.applySecurityPredicates(verified), nil
}

func (q *Queue) applySecurityPredicates(verified Verified) Verified {
	if verified.BuildID == "" || verified.Checkout == "" {
		return verified
	}
	verified.SecurityPredicates = securitypredicate.Decide(q.securityPredicates,
		securitypredicate.Run{ID: verified.BuildID, Checkout: securitypredicate.Checkout{Dir: verified.Checkout}})
	if rejected := verified.SecurityPredicates.Rejected(); len(rejected) > 0 {
		verified.Passed = false
		verified.Why = fmt.Sprintf("security predicate %s did not hold against build %s",
			rejected[0].Kind, verified.BuildID)
	}
	return verified
}
