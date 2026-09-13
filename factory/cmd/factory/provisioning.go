package main

import "context"

// Provisioned is the dispatch composition's read of the service record: the
// owner-written field says the repository and the store exist.
func (p *path) Provisioned(ctx context.Context, serviceID string) (bool, error) {
	svc, err := p.serviceOf(ctx, serviceID)
	if err != nil {
		return false, err
	}
	return svc.Provisioned.Written(), nil
}
