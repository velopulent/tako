package platform

import (
	"errors"
	"strings"
)

const MaxServiceUnitLength = 256

var (
	ErrInvalidServiceScope  = errors.New("invalid service scope")
	ErrInvalidServiceUnit   = errors.New("invalid service unit")
	ErrInvalidServiceAction = errors.New("invalid service action")
)

// ServiceOperation is the small, allowlisted vocabulary shared by the HTTP
// gateway and the privileged session boundary. It deliberately contains no
// executable command or shell representation.
type ServiceOperation struct {
	Scope  string
	Unit   string
	Action string
}

type ServiceImpactRelation struct {
	Name         string `json:"name"`
	Relationship string `json:"relationship"`
}

type ServiceImpact struct {
	Scope           string                  `json:"scope"`
	Unit            string                  `json:"unit"`
	Action          string                  `json:"action"`
	CurrentState    string                  `json:"currentState"`
	CurrentSubState string                  `json:"currentSubState"`
	Affected        []ServiceImpactRelation `json:"affected"`
	Warnings        []string                `json:"warnings"`
}

func ValidateServiceTarget(scope, unit string) error {
	if scope != "system" && scope != "user" {
		return ErrInvalidServiceScope
	}
	if unit == "" || len(unit) > MaxServiceUnitLength || strings.ContainsAny(unit, "/\x00") {
		return ErrInvalidServiceUnit
	}
	return nil
}

func ParseServiceOperation(scope, unit, action string) (ServiceOperation, error) {
	if err := ValidateServiceTarget(scope, unit); err != nil {
		return ServiceOperation{}, err
	}
	switch action {
	case "start", "stop", "restart", "reload", "enable", "disable", "mask", "unmask":
		return ServiceOperation{Scope: scope, Unit: unit, Action: action}, nil
	default:
		return ServiceOperation{}, ErrInvalidServiceAction
	}
}

func ServiceImpactForAction(detail UnitDetail, action string) ServiceImpact {
	impact := ServiceImpact{
		Scope:           detail.Scope,
		Unit:            detail.Name,
		Action:          action,
		CurrentState:    detail.ActiveState,
		CurrentSubState: detail.SubState,
		Affected:        make([]ServiceImpactRelation, 0, 16),
		Warnings:        make([]string, 0, 2),
	}
	seen := make(map[string]struct{})
	add := func(relationship string, names []string) {
		for _, name := range names {
			if name == "" || name == detail.Name || len(impact.Affected) >= 128 {
				continue
			}
			key := relationship + "\x00" + name
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			impact.Affected = append(impact.Affected, ServiceImpactRelation{Name: name, Relationship: relationship})
		}
	}
	add("requires", detail.Requires)
	add("wants", detail.Wants)
	add("wanted-by", detail.WantedBy)
	add("conflicts", detail.Conflicts)
	add("before", detail.Before)
	add("after", detail.After)
	if action == "stop" || action == "restart" || action == "mask" {
		impact.Warnings = append(impact.Warnings, "This action may stop or disrupt workloads that depend on this unit.")
	}
	if action == "enable" || action == "disable" {
		impact.Warnings = append(impact.Warnings, "This action changes whether the unit starts automatically.")
	}
	if len(impact.Affected) > 0 {
		impact.Warnings = append(impact.Warnings, "Review affected relationships before confirming.")
	}
	return impact
}
