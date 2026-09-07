package screens

// Ops is [Views.Ops]'s view: every service on every environment, answering
// what is running rather than what needs a human.
type Ops struct {
	Services []ServiceSummary
}

// ServiceSummary is one row of the Ops board: a service on an environment,
// enough to route to [Views.ServiceOn] for the rest.
type ServiceSummary struct {
	ServiceID      string
	EnvironmentID  string
	CurrentRelease int64
	// Health is a short reading: "measured", "unmeasured", or "watched with
	// no way to reach passed".
	Health string
}

// Service is [Views.ServiceOn]'s view: one service's own view on one
// environment.
type Service struct {
	ServiceID     string
	EnvironmentID string
	Targets       []TargetRelease
	// DriftMismatch is what the drift detector's own record over this
	// environment names, or empty where it names none.
	DriftMismatch      string
	ContractsPublished []string
	OpenIncidents      []Incident
	Rollouts           []Rollout
	LastChecks         []LastCheck
	Windows            []WindowParameters
	// EmissionVersion is the version this service's newest record carries,
	// which is how an owner sees a service still emitting the shape an
	// earlier factory shipped.
	EmissionVersion string
	// Unmeasured is true where this service is missing one of the four
	// fields the deployer populates at decomposition.
	Unmeasured bool
	// Mitigation is nil where none stands on any target of this service on
	// this environment.
	Mitigation *Mitigation
}

// TargetRelease is one target of the environment: which release of the
// service is running on it now, the deploy record that release came from,
// when that target's own deploy completed, and how far a deploy still in
// progress has reached on it. There is one row per target and not one per
// deploy ever completed, so a rollout stalled on one target reads as two
// targets running two releases.
type TargetRelease struct {
	TargetID string
	// ReleaseNumber is zero and DeployID and CompletedAt are empty where
	// nothing is running on the target: nothing has completed there, or a
	// removal took the service off it.
	ReleaseNumber int64
	DeployID      string
	CompletedAt   string // RFC 3339 UTC
	// Deploying is how far the deploy in progress on this target has reached
	// — not_reached until the deployer completes it — and empty where no
	// deploy is in progress on it.
	Deploying string
}

// Incident is one open incident on this service and environment.
type Incident struct {
	ID       string
	Quantity string
	OpenedAt string // RFC 3339 UTC
}

// Rollout is one rollout in progress: the release it is rolling out and the
// control target it is measured against, where the strategy keeps one.
type Rollout struct {
	ReleaseNumber int64
	// ControlTargetID is empty where the strategy keeps no control.
	ControlTargetID string
}

// WindowParameters is what this service is being watched for, per quantity:
// the size in force, the finest size its traffic reaches, the confidence and
// power that reading was taken at, whether passed is reachable at all, and
// the average run length with the crossings it admits on a service where
// nothing changed.
type WindowParameters struct {
	Quantity          string
	Size              int64
	FinestSizeReached int64
	Confidence        float64
	Power             float64
	PassedReachable   bool
	AverageRunLength  float64
	AdmittedCrossings float64
}

// Mitigation is one mitigation standing on a target, and the hours it has
// stood.
type Mitigation struct {
	ID            string
	TargetID      string
	Operation     string
	StandingHours float64
}
