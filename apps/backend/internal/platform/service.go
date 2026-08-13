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
