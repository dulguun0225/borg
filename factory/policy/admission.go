package policy

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/safeguard"
)

// Admissions is which of the two admissions the safeguards drawn on the report
// store require. Both are false where an owner has placed neither, which is an
// install where nothing waits and the channel behaves as it did before the
// safeguard existed.
type Admissions struct {
	// ArrivingReports is the safeguard that holds an arrived report ungrouped
	// until a human admits it at Work, so only an admitted report reaches the
	// grouper and no report's words leave the install unread.
	ArrivingReports bool
	// ReportDerivedIntents is the safeguard that holds an intent grouped from
	// reports until a human admits it at Work: dispatch puts no agent on it and
	// no interview round runs meanwhile.
	ReportDerivedIntents bool
}

// ReportStoreAdmissions is the two safeguards read in force. It is a read of
// its own rather than a filter over [Reader.All] for the reason
// [Reader.AllowedPredicateKinds] is: All is a printer's answer over every
// parameter, and this is two mechanisms' answer over two.
//
// A withdrawn safeguard is not in force and is not counted, which is how an
// owner takes an admission back: what was waiting on it moves on the next pass
// rather than waiting for a human who is no longer owed one.
//
// The subject is the factory-wide settings record's id and nothing else, the
// report store having no record of its own. A safeguard drawn on a service, a
// project or an area is a safeguard on a subject this mechanism never reads,
// which is the dangling safeguard the design already accounts for.
func (r *Reader) ReportStoreAdmissions(ctx context.Context) (Admissions, error) {
	settings, err := factorysettings.Get(ctx, r.pool)
	if err != nil {
		return Admissions{}, err
	}
	var in Admissions
	for _, one := range []struct {
		parameter gatepolicy.Parameter
		stands    *bool
	}{
		{gatepolicy.ReportAdmission, &in.ArrivingReports},
		{gatepolicy.ReportDerivedIntentAdmission, &in.ReportDerivedIntents},
	} {
		placed, err := safeguard.BySubjects(ctx, r.pool, one.parameter, []safeguard.Subject{
			{Kind: safeguard.SubjectReportStore, ID: settings.ID},
		})
		if err != nil {
			return Admissions{}, fmt.Errorf("policy: reading the safeguards on %s: %w", one.parameter, err)
		}
		*one.stands = len(placed) > 0
	}
	return in, nil
}
