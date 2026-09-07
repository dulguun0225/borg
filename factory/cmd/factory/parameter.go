package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
)

// authoring is one parameter an owner authors, as both callers say it: the
// parameter and its value as strings, and the subject names the parameter
// needs, the rest left empty. Which of them are read follows from the
// parameter, because the record a parameter is a field of is a fact of the
// parameter and not a choice.
type authoring struct {
	parameter   string
	value       string
	serviceName string
	areaName    string
	projectName string
	gateRow     string
	stage       string
	quantity    string
}

// authored is what one authoring write produced: the policy version it
// appended, and the shortening it wrote pending where the value asked for a
// shorter decision-log retention than the one in force. A shortening is not in
// force on write — the row that decides one is routed away from whoever
// authored the value — so the two answers are told apart here rather than by
// the caller reading the version's own fields.
type authored struct {
	version policy.Version
	pending string
}

// authorParameter authors one parameter, and is the whole of the dispatch on
// which subject the parameter is a field of. Two callers make the act: the
// author subcommand and Factory's own AuthorParameter call.
func authorParameter(ctx context.Context, pool *pgxpool.Pool, factory *policy.Factory,
	actor record.Actor, a authoring) (authored, error) {
	if a.parameter == "" || a.value == "" {
		return authored{}, errors.New("factory: the parameter and its value are both required")
	}
	parameter := gatepolicy.Parameter(a.parameter)
	if _, err := gatepolicy.Define(parameter); err != nil {
		return authored{}, err
	}
	if a.projectName == "" {
		a.projectName = defaultProjectName
	}
	if a.gateRow == "" {
		a.gateRow = gate.MergeToMaster.String()
	}
	if a.stage == "" {
		a.stage = string(item.StageImplementation)
	}
	if a.quantity == "" {
		a.quantity = string(gatepolicy.QuantityErrorRate)
	}

	if parameter == gatepolicy.AllowedPredicateKinds {
		version, err := factory.AuthorAllowedPredicateKinds(ctx, actor, strings.Split(a.value, ","))
		return authored{version: version}, err
	}

	number, err := strconv.ParseFloat(a.value, 64)
	if err != nil {
		return authored{}, fmt.Errorf("factory: %s takes a number, not %q", parameter, a.value)
	}

	var version policy.Version
	switch parameter {
	case gatepolicy.RiskThreshold:
		if a.gateRow == gate.RolePromptOrSkill.String() {
			version, err = factory.AuthorRolePromptOrSkillThreshold(ctx, actor, number)
			break
		}
		prj, err2 := namedProject(ctx, pool, a.projectName)
		if err2 != nil {
			return authored{}, err2
		}
		production, found, err2 := environment.Production(ctx, pool, prj.ID)
		if err2 != nil {
			return authored{}, err2
		}
		if !found {
			return authored{}, errors.New("factory: production's environment record does not exist yet — run the path once, which installs it")
		}
		version, err = factory.AuthorGateThreshold(ctx, actor, production.ID, a.gateRow, number)
	case gatepolicy.AttemptLimit:
		version, err = factory.AuthorAttemptLimit(ctx, actor, item.Stage(a.stage), int(number))
	case gatepolicy.DecisionLogRetention:
		// Lengthening is in force on write. Shortening is decided at a gate
		// row of its own, routed away from whoever authored the value, so
		// what this writes there is the value pending and the row that
		// approves it is where it comes into force.
		version, err = factory.AuthorDecisionLogRetention(ctx, actor, int64(number))
		if errors.Is(err, policy.ErrShorteningIsDecided) {
			written, shortening, err2 := factory.WriteRetentionShortening(ctx, actor, int64(number))
			if err2 != nil {
				return authored{}, err2
			}
			return authored{version: shortening, pending: written.ID}, nil
		}
	case gatepolicy.ItemSizeTarget:
		ar, err2 := namedArea(ctx, pool, a.areaName)
		if err2 != nil {
			return authored{}, err2
		}
		version, err = factory.AuthorItemSizeTarget(ctx, actor, ar.ID, number)
	default:
		svc, err2 := namedService(ctx, pool, a.serviceName)
		if err2 != nil {
			return authored{}, err2
		}
		switch parameter {
		case gatepolicy.WindowSize:
			version, err = factory.AuthorWindowSize(ctx, actor, svc.ID, gatepolicy.Quantity(a.quantity), number)
		case gatepolicy.WindowConfidence:
			version, err = factory.AuthorWindowConfidence(ctx, actor, svc.ID, number)
		case gatepolicy.WindowCap:
			version, err = factory.AuthorWindowCap(ctx, actor, svc.ID, number)
		case gatepolicy.WindowLimit:
			version, err = factory.AuthorWindowLimit(ctx, actor, svc.ID, number)
		}
	}
	return authored{version: version}, err
}
