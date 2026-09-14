package deploy

import (
	"context"

	"github.com/dulguun0225/borg/factory/record"
)

func previousReleaseTarget(ctx context.Context, w *Writer, p Performance, d Deploy, address string) (Deploy, Target, error) {
	if !p.IntoProduction {
		return Deploy{}, Target{}, nil
	}
	previous, found, err := PreviousOnTarget(ctx, w.Pool(), p.ServiceID, p.EnvironmentID, address, d.Number)
	if err != nil || !found {
		return Deploy{}, Target{}, err
	}
	targets, err := Targets(ctx, w.Pool(), previous.ID)
	if err != nil {
		return Deploy{}, Target{}, err
	}
	for _, target := range targets {
		if target.Address == address {
			return previous, target, nil
		}
	}
	return Deploy{}, Target{}, nil
}

func tearDownPreviousRelease(ctx context.Context, w *Writer, p Performance, previous Deploy, target Target) error {
	if previous.ID == "" || target.Fleets.Release.Instances == 0 || target.Fleets.Release.TornDownAt != "" {
		return nil
	}
	hours, err := Hours(previous.At, record.Now(), target.Fleets.Release.Instances)
	if err != nil {
		return err
	}
	return w.TearDownRelease(ctx, previous.ID, target.Address, hours, p.InstanceHourRate)
}
