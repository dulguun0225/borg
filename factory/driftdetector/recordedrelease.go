package driftdetector

// RecordedDeploy is one target's own row of one of a service's production
// deploy records, as [RecordedRelease] reads the sequence its caller
// assembles for one target: what that record names, its place in the
// service's own order, whether it marks this target complete, and whether it
// names no release and no build, a removal.
type RecordedDeploy struct {
	ReleaseID, BuildID string
	// Number orders records in the service's own sequence: higher is newer.
	Number int
	// Complete is whether this record marks the target complete.
	Complete bool
	// Removal is whether the record names no release and no build, which is
	// what takes a service off an environment.
	Removal bool
}

// RecordedRelease is what a service's production deploy record marks for one
// target: the newest complete record's release and build, empty where that
// record is a removal's, and the previous complete record's where the newest
// deploy record is not complete on this target — which the newest complete
// record already answers, an incomplete one taking no part in the
// comparison. It is pure code over the sequence its caller assembles from the
// target's own row of every deploy of the service into one environment, the
// way [Excused] decides the rollout exemption over its own caller's
// assembly.
func RecordedRelease(deploys []RecordedDeploy) (releaseID, buildID string) {
	var newest RecordedDeploy
	found := false
	for _, d := range deploys {
		if !d.Complete {
			continue
		}
		if !found || d.Number > newest.Number {
			newest, found = d, true
		}
	}
	if !found || newest.Removal {
		return "", ""
	}
	return newest.ReleaseID, newest.BuildID
}
