package main

import (
	"context"
	"errors"
	"strings"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/service"
)

// Dependencies is the command's database-backed adapter. The deployer owns
// what composition does with these reads.
func (p *path) Dependencies(ctx context.Context, itemID, serviceID, productionID string) ([]deploy.Dependency, error) {
	reaches, err := p.contracts.ComposedFrom(ctx, itemID, serviceID, productionID)
	if err != nil {
		return nil, err
	}
	dependencies := make([]deploy.Dependency, 0, len(reaches))
	for _, producer := range reaches {
		producerService, err := service.Get(ctx, p.d.pool, producer.ServiceID)
		if err != nil {
			return nil, err
		}
		dependency := deploy.Dependency{
			ServiceID: producer.ServiceID, ReleaseID: producer.ReleaseID,
			ServiceName: producerService.Name, Addresses: producer.Addresses,
		}
		for _, target := range serviceTargets(p.production, producerService) {
			dependency.Reaches = append(dependency.Reaches, deploy.Reach{Address: target.Address, Target: p.d.targets.at(target.Address)})
		}
		dependencies = append(dependencies, dependency)
	}
	return dependencies, nil
}

func (p *path) SeedVersions(ctx context.Context, serviceID string) ([]deploy.Version, error) {
	versions, err := service.SeedVersions(ctx, p.d.pool, serviceID)
	if errors.Is(err, service.ErrVersionNotFound) {
		return nil, deploy.ErrNoVersion
	}
	return deployVersions(versions), err
}

func (p *path) ValueSetVersions(ctx context.Context, serviceID string) ([]deploy.Version, error) {
	versions, err := service.ValueSetVersions(ctx, p.d.pool, serviceID)
	if errors.Is(err, service.ErrVersionNotFound) {
		return nil, deploy.ErrNoVersion
	}
	return deployVersions(versions), err
}

func deployVersions(versions []service.Version) []deploy.Version {
	converted := make([]deploy.Version, 0, len(versions))
	for _, version := range versions {
		converted = append(converted, deploy.Version{ID: version.ID, Content: version.Content})
	}
	return converted
}

// candidateWaitLog adapts the pass's already-read log and writer to the
// deployer's candidate-run wait operations.
type candidateWaitLog struct{ path *path }

func (l candidateWaitLog) Rows(ctx context.Context) ([]deploy.WaitRow, error) {
	read, err := l.path.readLog(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]deploy.WaitRow, 0, len(read.rows))
	for _, row := range read.rows {
		rows = append(rows, deploy.WaitRow{ID: row.ID, Shape: string(row.Shape), Payload: row.Payload})
	}
	return rows, nil
}

func (l candidateWaitLog) Open(ctx context.Context, actor record.Actor, payload string) (string, error) {
	row, err := l.path.log.AppendWaitOpen(ctx, decisionlog.Entry{Actor: actor, Principal: deployerPrincipal, Payload: payload, FormatVersion: "wait/1"})
	return row.ID, err
}

func (l candidateWaitLog) Close(ctx context.Context, actor record.Actor, row, payload string) error {
	_, err := l.path.log.AppendWaitClose(ctx, decisionlog.Entry{Actor: actor, Principal: deployerPrincipal, FormatVersion: "wait/1", Closes: row, Payload: payload})
	return err
}

func describeComposition(composed []environment.Composed) string {
	if len(composed) == 0 {
		return "nothing, its build's consumer contract naming no producer"
	}
	named := make([]string, 0, len(composed))
	for _, dependency := range composed {
		named = append(named, dependency.ServiceID+" at "+dependency.ReleaseID)
	}
	return strings.Join(named, ", ")
}

var _ deploy.CandidateSource = (*path)(nil)
var _ deploy.WaitLog = candidateWaitLog{}
