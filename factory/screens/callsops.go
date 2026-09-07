package screens

// RollBackArgs is duty 10's first form: the deployer returns production to
// the release below the one running.
type RollBackArgs struct {
	ServiceID string
	Reason    string
}

// RaiseRevertArgs is duty 10's second form: the revert intent, naming the
// release that failed.
type RaiseRevertArgs struct {
	ServiceID string
	ReleaseID string
	Reason    string
}

// StartMitigationArgs instructs one of the mitigation class's two
// operations on a target. Share is meaningful for a traffic-shifting
// operation and Count for an instance-count operation; the other is left
// zero.
type StartMitigationArgs struct {
	TargetID  string
	Operation string
	Share     float64
	Count     int64
}

// EndMitigationArgs ends a mitigation already standing; ending every
// instance of a service on a target is not this call, retirement being what
// calls for that.
type EndMitigationArgs struct {
	MitigationID string
}

// MarkRollbackNotCausedArgs excludes a release from the score and its
// learning pass from then on.
type MarkRollbackNotCausedArgs struct {
	DeployID string
	Reason   string
}

// FirePageArgs is a page a human fires on their own judgment — the one
// action of the twelve duties no subcommand makes today.
type FirePageArgs struct {
	ServiceID string
	Reason    string
}
