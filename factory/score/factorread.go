package score

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
)

// reading is one factor as it was read: the level, the quantity in words, and
// what resolved it where something did. unavailable is the factor the score
// could not compute; resolved is a value the design resolves rather than weighs,
// with the cause it names.
type reading struct {
	level       float64
	words       string
	unavailable string
	resolved    string
	cause       Cause
	// evidence is the exposure list, and empty on every other factor.
	evidence []string
	// width, closes, claimed and verified are the per-author prior's, and are
	// nothing on every other factor.
	width    float64
	closes   int
	claimed  int
	verified int
}

// size reads the change under decision: the lines the diff changes, or at
// Decomposition the count of the intent's requirements the proposed set answers,
// which is the unit the item-size target is authored in. A proposed set is never
// unavailable — the set exists and was read — which is what keeps the four rows
// above a build from resolving on a diff that was never going to be there.
func (s *Score) size(_ context.Context, c Change) (reading, error) {
	m := c.Measurement
	if m.FromProposedSet() {
		return reading{
			level: level(float64(m.RequirementsProposed), requirementsBreakpoints, 1.0),
			words: fmt.Sprintf("%d requirement(s) answered by the set decomposition proposed", m.RequirementsProposed),
		}, nil
	}
	if m.Unavailable != "" {
		return reading{unavailable: m.Unavailable}, nil
	}
	return reading{
		level: level(float64(m.LinesChanged), sizeBreakpoints, 1.0),
		words: fmt.Sprintf("%d lines changed", m.LinesChanged),
	}, nil
}

// reach reads how much of the system the change can affect: the share of the
// service's files the diff touches, or at Decomposition the services the
// proposed set spans.
func (s *Score) reach(_ context.Context, c Change) (reading, error) {
	m := c.Measurement
	if m.FromProposedSet() {
		return reading{
			level: level(float64(m.ServicesProposed), consumersBreakpoints, 1.0),
			words: fmt.Sprintf("%d service(s) the set decomposition proposed spans", m.ServicesProposed),
		}, nil
	}
	if m.Unavailable != "" {
		return reading{unavailable: m.Unavailable}, nil
	}
	if m.FilesInTree <= 0 {
		return reading{unavailable: "the build's tree holds no files, so the share one change touches is undefined"}, nil
	}
	share := float64(m.FilesChanged) / float64(m.FilesInTree)
	return reading{
		level: level(share, reachBreakpoints, 1.0),
		words: fmt.Sprintf("%d of the service's %d files", m.FilesChanged, m.FilesInTree),
	}, nil
}

// churn reads what else has been changing in the item's area lately. This
// item's own releases are left out: a change is not its own churn, and at the
// production deploy row its release already exists.
func (s *Score) churn(ctx context.Context, c Change) (reading, error) {
	if c.AreaID == "" {
		return reading{unavailable: "the item names no area, so nothing says what else has been changing around it"}, nil
	}
	items, err := item.IDsInArea(ctx, s.pool, c.AreaID)
	if err != nil {
		return reading{}, err
	}
	since := record.FormatTime(time.Now().Add(-ChurnWindow))
	releases, err := release.CountForItemsSince(ctx, s.pool, items, c.ItemID, since)
	if err != nil {
		return reading{}, err
	}
	return reading{
		level: level(float64(releases), churnBreakpoints, 1.0),
		words: fmt.Sprintf("%d releases in this area in the last %s", releases, ChurnWindow),
	}, nil
}

// reversibility reads whether the service has a release to return to, this
// item's own excluded, and resolves where the diff destroys stored data. A first
// release has none, which is what the design says of one: no control, nothing
// able to close a window passed, and no rollback target.
func (s *Score) reversibility(ctx context.Context, c Change) (reading, error) {
	if c.Measurement.DestroysStoredData && c.AtImplementation {
		return reading{
			resolved: "the diff destroys stored data, so a human decides at Implementation with the diff in front of them",
			cause:    CauseDestroysStoredData,
			words:    "the diff destroys stored data",
		}, nil
	}
	earlier, err := release.CountForService(ctx, s.pool, c.ServiceID, c.ItemID)
	if err != nil {
		return reading{}, err
	}
	if earlier == 0 {
		return reading{level: 1.0, words: "no earlier release of this service to return to"}, nil
	}
	return reading{
		level: 0.3,
		words: fmt.Sprintf("%d earlier releases of this service", earlier),
	}, nil
}

// hazardSeverity reads the hazard severity in force on the item's area, which is
// the context group's one declared input and the only term in the vector an
// owner writes rather than the factory derives. An irreversible value is
// resolved rather than weighed, at Implementation and at no gate above it: a
// human at all four would stop the factory authoring in such an area at all.
func (s *Score) hazardSeverity(ctx context.Context, c Change) (reading, error) {
	if c.AreaID == "" {
		return reading{unavailable: "the item names no area, so nothing says what harm its software can do"}, nil
	}
	grade, err := area.SeverityInForce(ctx, s.pool, c.AreaID)
	if err != nil {
		return reading{}, err
	}
	switch grade {
	case area.GradeIrreversible:
		if !c.AtImplementation {
			return reading{level: 1.0, words: "the area is graded irreversible"}, nil
		}
		return reading{
			resolved: "the hazard severity in force on this area is irreversible, so a human decides at Implementation whatever the formula returns",
			cause:    CauseIrreversibleHazard,
			words:    "the area is graded irreversible",
		}, nil
	case area.GradeRecoverable:
		return reading{level: 0.5, words: "the area is graded recoverable"}, nil
	case area.GradeNegligible:
		return reading{level: 0.1, words: "the area is graded negligible"}, nil
	}
	return reading{level: 0.1, words: "no area on this item's chain names a hazard severity"}, nil
}

// intentSource reads where the intent this item answers came from. An intent
// grouped from reports carries text the factory did not author and no gate has
// admitted, so it resolves at Spec: that channel is the one way in the factory
// cannot authenticate.
func (s *Score) intentSource(ctx context.Context, c Change) (reading, error) {
	it, err := item.Get(ctx, s.pool, c.ItemID)
	if err != nil {
		return reading{unavailable: fmt.Sprintf("the item could not be read, so nothing says where its intent came from: %v", err)}, nil
	}
	if it.IntentID == "" {
		return reading{unavailable: "the item names no intent, so nothing says where the request came from"}, nil
	}
	in, err := intent.Get(ctx, s.pool, it.IntentID)
	if err != nil {
		return reading{unavailable: fmt.Sprintf("the intent could not be read: %v", err)}, nil
	}
	switch in.Source {
	case intent.SourceReports:
		if !c.AtSpec {
			return reading{level: 1.0, words: "the intent was grouped from reports"}, nil
		}
		return reading{
			resolved: "the intent was grouped from reports, whose text the factory did not author and no gate has admitted, so a human decides at Spec",
			cause:    CauseReportSourcedIntent,
			words:    "the intent was grouped from reports",
		}, nil
	case intent.SourceDetector:
		return reading{level: 0.4, words: "the factory raised this intent itself"}, nil
	default:
		return reading{level: 0.2, words: "an owner typed this request"}, nil
	}
}

// consumers reads which sibling services declare they consume what this one
// publishes. It is a query over the graph a contract and a consumer contract
// make: the contracts this service publishes, and the other services whose
// consumer contracts name one of them.
//
// Two filters and both are deliberate. A consumer contract whose item has no
// release is left out, because a consumer contract is written at the
// implementation stage and a candidate that never merges leaves one behind; what
// says it is a release's is a release naming the same item. And this service's
// own consumer contracts are left out, because a service declaring against its
// own store contract is its own past and not a sibling.
//
// A consumer nobody could read makes this unknowable rather than zero: the
// derivation records that it could not run, nothing bounds what such a consumer
// consumes, and a count that left it out would score a service whose consumers
// cannot be read as one nothing consumes — the reading that made the changes most
// likely to break a consumer the ones most likely to auto-pass. So one standing
// could-not-derive record anywhere in the install resolves this factor for every
// candidate whose service publishes a contract, and the resolution names the
// consumer nobody could read.
func (s *Score) consumers(ctx context.Context, c Change) (reading, error) {
	published, err := contract.OfService(ctx, s.pool, c.ServiceID)
	if err != nil {
		return reading{}, err
	}
	if len(published) == 0 {
		return reading{
			level: level(0, consumersBreakpoints, 1.0),
			words: "this service publishes no contract, so nothing declares it consumes one",
		}, nil
	}

	unreadable, err := consumercontract.StandingCouldNotDerive(ctx, s.pool)
	if err != nil {
		return reading{}, err
	}
	if len(unreadable) > 0 {
		return reading{unavailable: fmt.Sprintf(
			"%d consumer(s) could not be derived, the first being item %s of service %s: %s",
			len(unreadable), unreadable[0].ItemID, unreadable[0].ServiceID, unreadable[0].Describe()),
		}, nil
	}

	predicates, err := consumercontract.AgainstProducer(ctx, s.pool, c.ServiceID)
	if err != nil {
		return reading{}, err
	}
	var items []string
	for _, p := range predicates {
		if p.ServiceID != c.ServiceID && !slices.Contains(items, p.ItemID) {
			items = append(items, p.ItemID)
		}
	}
	released, err := release.ItemsWithRelease(ctx, s.pool, items)
	if err != nil {
		return reading{}, err
	}
	var services []string
	for _, p := range predicates {
		if p.ServiceID == c.ServiceID || !slices.Contains(released, p.ItemID) {
			continue
		}
		if !slices.Contains(services, p.ServiceID) {
			services = append(services, p.ServiceID)
		}
	}
	return reading{
		level: level(float64(len(services)), consumersBreakpoints, 1.0),
		words: fmt.Sprintf("%d sibling service(s) declare they consume one of the %d contract(s) this service publishes",
			len(services), len(published)),
	}, nil
}

// fleetShare, fleetDeparture and fleetReversibility are the three factors that
// replace the change group on the row that decides a version of what an agent is
// told. The two readings are the caller's: the fleet's records are that row's
// own, and no component writes one yet, so a factory that fires this row without
// them resolves both rather than reading them as nothing.
func (s *Score) fleetShare(_ context.Context, c Change) (reading, error) {
	if c.Fleet.Unavailable != "" {
		return reading{unavailable: c.Fleet.Unavailable}, nil
	}
	if !c.Fleet.Derived {
		return reading{unavailable: fleetUnread}, nil
	}
	return reading{
		level: c.Fleet.ShareWorkingFromIt,
		words: fmt.Sprintf("%.0f%% of the factory works from the version this one replaces", c.Fleet.ShareWorkingFromIt*100),
	}, nil
}

func (s *Score) fleetDeparture(_ context.Context, c Change) (reading, error) {
	if c.Fleet.Unavailable != "" {
		return reading{unavailable: c.Fleet.Unavailable}, nil
	}
	if !c.Fleet.Derived {
		return reading{unavailable: fleetUnread}, nil
	}
	return reading{
		level: c.Fleet.Departure,
		words: fmt.Sprintf("%.0f%% of this version differs from the version in force", c.Fleet.Departure*100),
	}, nil
}

// fleetUnread is why both fleet readings resolve where nothing read the fleet's
// records: no component writes one yet, and a share of nothing is not a reading.
const fleetUnread = "nothing read the fleet's records for this firing, so what works from the version this one replaces and how far it departs are unknown"

func (s *Score) fleetReversibility(_ context.Context, _ Change) (reading, error) {
	return reading{
		level: 0.3,
		words: "withdrawal is a second record and nothing was deployed, so every version of what an agent is told is reversible",
	}, nil
}
