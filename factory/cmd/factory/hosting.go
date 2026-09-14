package main

import (
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/service"
)

func instanceHourRate(svc service.Service) deploy.Priced {
	return deploy.Priced{Rate: svc.InstanceHourRate.Number, InForce: svc.InstanceHourRate.Present}
}

func environmentRate(svc service.Service) environment.Rate {
	return environment.Rate{PerHour: svc.EnvironmentHourRate.Number, InForce: svc.EnvironmentHourRate.Present}
}
