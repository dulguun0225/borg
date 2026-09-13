package main

import (
	"github.com/dulguun0225/borg/factory/build"
)

func buildCoverageGaps(bl build.Build) []string {
	var gaps []string
	if bl.ResolvedSetCouldNotDerive != "" {
		gaps = append(gaps, "the resolved set could not be derived: "+bl.ResolvedSetCouldNotDerive)
	}
	for _, coverage := range bl.Coverage {
		if coverage.FetchWithoutRunningReason != "" {
			gaps = append(gaps, coverage.FetchWithoutRunningReason)
		} else if !coverage.FetchWithoutRunning {
			gaps = append(gaps, coverage.Ecosystem+" cannot separate fetching from running")
		}
		if !coverage.Digests {
			gaps = append(gaps, coverage.Ecosystem+" has no content digests")
		}
	}
	return gaps
}
