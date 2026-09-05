package window

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dulguun0225/borg/factory/gatepolicy"
)

// OpenEvent is what [Writer.Open] is given: everything the window names at the
// open. It is a struct and not a list of arguments because most of its fields
// are ids, shares, or lists of names, and a caller that swapped two of either
// would compile.
type OpenEvent struct {
	DeployID string
	// ReleaseID is empty on the window over a deploy the search called for, whose
	// record names a build and no release.
	ReleaseID string
	BuildID   string
	ServiceID string
	// MeasuresNothing is a service missing one of the four fields the deployer
	// populates on its service record. Such an opening carries no parameters at
	// all, and [ErrMeasuresNothingCarriesNoParameters] is what refuses one that
	// does.
	MeasuresNothing bool
	// PassedAvailable is false where the release has nothing to be compared
	// against, where it was held out, and where the traffic cannot support the
	// power in force at the size in force inside the cap. All three are the
	// caller's to compute and this package's to store.
	PassedAvailable bool
	HeldOut         bool
	Size            map[gatepolicy.Quantity]float64
	Power           map[gatepolicy.Quantity]float64
	Confidence      float64
	CapSeconds      float64
	BoundaryVersion string
	// Targets is the production targets the rollout is planned to reach, which is
	// the set the boundary is allocated over. It is required on a window that
	// measures anything: the allocation is made at the open and does not move as
	// targets are reached.
	Targets                []string
	OperationsReadAlone    []string
	EmissionVersionRelease string
	EmissionVersionControl string
	QuantitiesOutside      []gatepolicy.Quantity
	OwnHistorySize         map[gatepolicy.Quantity]float64
	OwnHistoryRunLength    float64
	ThresholdSize          map[gatepolicy.Quantity]float64
	ThresholdRunLength     float64
	PolicyVersion          string
	ScoreVersion           string
}

func (o OpenEvent) validate() error {
	for _, required := range []struct{ what, value string }{
		{"deploy", o.DeployID}, {"build", o.BuildID}, {"service", o.ServiceID},
		{"boundary version", o.BoundaryVersion}, {"policy version", o.PolicyVersion},
		{"score version", o.ScoreVersion},
	} {
		if required.value == "" {
			return fmt.Errorf("%w: no %s", ErrOpeningIncomplete, required.what)
		}
	}
	if o.MeasuresNothing {
		return o.validateMeasuresNothing()
	}
	if len(o.Size) == 0 || len(o.Power) == 0 {
		return fmt.Errorf("%w: no size or no power, which are one value per quantity", ErrOpeningIncomplete)
	}
	for quantity, size := range o.Size {
		if size <= 0 || size > 1 {
			return fmt.Errorf("%w: the size %v for %s is not a share above nothing",
				ErrOpeningIncomplete, size, quantity)
		}
		if power, has := o.Power[quantity]; !has || power <= 0 || power >= 1 {
			return fmt.Errorf("%w: the power %v for %s is not a share below one",
				ErrOpeningIncomplete, power, quantity)
		}
	}
	if o.Confidence <= 0 || o.Confidence >= 1 {
		return fmt.Errorf("%w: the confidence %v is not a share below one", ErrOpeningIncomplete, o.Confidence)
	}
	if o.CapSeconds <= 0 {
		return fmt.Errorf("%w: the cap %v is not above nothing", ErrOpeningIncomplete, o.CapSeconds)
	}
	if len(o.Targets) == 0 {
		return fmt.Errorf("%w: no target set for the boundary to be allocated over", ErrOpeningIncomplete)
	}
	if o.EmissionVersionRelease == "" {
		return fmt.Errorf("%w: no emission version for the release's arm", ErrOpeningIncomplete)
	}
	return nil
}

// validateMeasuresNothing refuses a window that says it measures nothing and
// names a parameter anyway. Both would be on the record and a reader could not
// tell which of them the health monitor acted on.
func (o OpenEvent) validateMeasuresNothing() error {
	if len(o.Size) > 0 || len(o.Power) > 0 || o.Confidence != 0 || o.CapSeconds != 0 ||
		o.PassedAvailable || len(o.Targets) > 0 || len(o.OperationsReadAlone) > 0 ||
		len(o.OwnHistorySize) > 0 || len(o.ThresholdSize) > 0 ||
		o.OwnHistoryRunLength != 0 || o.ThresholdRunLength != 0 {
		return ErrMeasuresNothingCarriesNoParameters
	}
	return nil
}

// The window's per-quantity values are stored as JSON objects keyed by the
// quantity, and its lists of names one per line. Both are read back here, so the
// spelling of either is in one place.

func encodeShares(shares map[gatepolicy.Quantity]float64) (string, error) {
	if len(shares) == 0 {
		return "{}", nil
	}
	encoded, err := json.Marshal(shares)
	if err != nil {
		return "", fmt.Errorf("window: encoding a value per quantity: %w", err)
	}
	return string(encoded), nil
}

func decodeShares(stored string) (map[gatepolicy.Quantity]float64, error) {
	if stored == "" || stored == "{}" {
		return nil, nil
	}
	shares := map[gatepolicy.Quantity]float64{}
	if err := json.Unmarshal([]byte(stored), &shares); err != nil {
		return nil, fmt.Errorf("window: reading a value per quantity: %w", err)
	}
	return shares, nil
}

func encodeRead(r Read) (string, error) {
	if r.Empty() {
		return "", nil
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("window: encoding the read the window closed on: %w", err)
	}
	return string(encoded), nil
}

func decodeRead(stored string) (Read, error) {
	if stored == "" {
		return Read{}, nil
	}
	var r Read
	if err := json.Unmarshal([]byte(stored), &r); err != nil {
		return Read{}, fmt.Errorf("window: reading the read a window closed on: %w", err)
	}
	return r, nil
}

func encodeNames(names []string) string { return strings.Join(names, "\n") }

func decodeNames(stored string) []string {
	if stored == "" {
		return nil
	}
	return strings.Split(stored, "\n")
}

func encodeQuantities(quantities []gatepolicy.Quantity) string {
	names := make([]string, 0, len(quantities))
	for _, q := range quantities {
		names = append(names, string(q))
	}
	return encodeNames(names)
}

func decodeQuantities(stored string) []gatepolicy.Quantity {
	var quantities []gatepolicy.Quantity
	for _, name := range decodeNames(stored) {
		quantities = append(quantities, gatepolicy.Quantity(name))
	}
	return quantities
}
