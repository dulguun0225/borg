package screens

// HostingHours is the hosting recorded for one service item and its release.
// Amount fields are meaningful only when their matching InForce field is true.
type HostingHours struct {
	ServiceID                string
	ItemID                   string
	ReleaseNumber            int64
	EnvironmentHours         float64
	EnvironmentAmount        float64
	EnvironmentAmountInForce bool
	InstanceHours            float64
	InstanceAmount           float64
	InstanceAmountInForce    bool
}

// MutationScore is the latest mutation reading for a service item.
type MutationScore struct {
	ServiceID       string
	ItemID          string
	BuildID         string
	Score           float64
	MutantsTested   int
	MutantsDetected int
	Derived         bool
}

// CriteriaCount is one service's criteria counts, with withdrawals grouped by
// the actor that wrote them.
type CriteriaCount struct {
	ServiceID  string
	Author     string
	Withdrawn  int64
	InForce    int64
	Unreliable int64
}
