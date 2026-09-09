// recordedrelease_test.go is beside exemption_test.go and stale_test.go, the
// third file in this package that touches no database:
// [driftdetector.RecordedRelease] is pure code over plain inputs.
package driftdetector_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/driftdetector"
)

func TestRecordedRelease(t *testing.T) {
	tests := []struct {
		name        string
		deploys     []driftdetector.RecordedDeploy
		wantRelease string
		wantBuild   string
	}{
		{
			name:        "no deploys at all is nothing recorded",
			deploys:     nil,
			wantRelease: "",
			wantBuild:   "",
		},
		{
			name: "the newest complete record's release and build",
			deploys: []driftdetector.RecordedDeploy{
				{ReleaseID: "rel_1", BuildID: "bl_1", Number: 1, Complete: true},
				{ReleaseID: "rel_2", BuildID: "bl_2", Number: 2, Complete: true},
			},
			wantRelease: "rel_2",
			wantBuild:   "bl_2",
		},
		{
			name: "the previous complete record where the newest deploy is not complete on this target",
			deploys: []driftdetector.RecordedDeploy{
				{ReleaseID: "rel_1", BuildID: "bl_1", Number: 1, Complete: true},
				{ReleaseID: "rel_2", BuildID: "bl_2", Number: 2, Complete: false},
			},
			wantRelease: "rel_1",
			wantBuild:   "bl_1",
		},
		{
			name: "nothing where the newest complete record is a removal's",
			deploys: []driftdetector.RecordedDeploy{
				{ReleaseID: "rel_1", BuildID: "bl_1", Number: 1, Complete: true},
				{Number: 2, Complete: true, Removal: true},
			},
			wantRelease: "",
			wantBuild:   "",
		},
		{
			name: "an incomplete removal newer than a complete release does not hide it",
			deploys: []driftdetector.RecordedDeploy{
				{ReleaseID: "rel_1", BuildID: "bl_1", Number: 1, Complete: true},
				{Number: 2, Complete: false, Removal: true},
			},
			wantRelease: "rel_1",
			wantBuild:   "bl_1",
		},
		{
			name: "no complete record at all is nothing recorded",
			deploys: []driftdetector.RecordedDeploy{
				{ReleaseID: "rel_1", BuildID: "bl_1", Number: 1, Complete: false},
			},
			wantRelease: "",
			wantBuild:   "",
		},
		{
			name: "order in the slice does not matter, only Number",
			deploys: []driftdetector.RecordedDeploy{
				{ReleaseID: "rel_2", BuildID: "bl_2", Number: 2, Complete: true},
				{ReleaseID: "rel_1", BuildID: "bl_1", Number: 1, Complete: true},
			},
			wantRelease: "rel_2",
			wantBuild:   "bl_2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			releaseID, buildID := driftdetector.RecordedRelease(tt.deploys)
			if releaseID != tt.wantRelease || buildID != tt.wantBuild {
				t.Errorf("RecordedRelease() = %q, %q, want %q, %q", releaseID, buildID, tt.wantRelease, tt.wantBuild)
			}
		})
	}
}
