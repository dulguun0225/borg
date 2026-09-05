package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/service"
)

func namedService(ctx context.Context, pool *pgxpool.Pool, name string) (service.Service, error) {
	if name == "" {
		return service.Service{}, errors.New("factory: this parameter is a field of the service record, so -service is required")
	}
	svc, found, err := service.ByName(ctx, pool, name)
	if err != nil {
		return service.Service{}, err
	}
	if !found {
		return service.Service{}, fmt.Errorf("factory: no service is named %q", name)
	}
	return svc, nil
}

func namedArea(ctx context.Context, pool *pgxpool.Pool, name string) (area.Area, error) {
	if name == "" {
		return area.Area{}, errors.New("factory: this parameter is a field of the area record, so -area is required")
	}
	ar, found, err := area.ByName(ctx, pool, name)
	if err != nil {
		return area.Area{}, err
	}
	if !found {
		return area.Area{}, fmt.Errorf("factory: no area is named %q — declare it with `factory area %s`", name, name)
	}
	return ar, nil
}
