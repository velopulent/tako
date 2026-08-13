package sessiond

import (
	"errors"
	"strings"
)

const maxServiceUnitLength = 256

type serviceOperation struct {
	Scope  string
	Unit   string
	Action string
}

func parseServiceOperation(scope, unit, action string) (serviceOperation, error) {
	if scope != "system" && scope != "user" {
		return serviceOperation{}, errors.New("invalid service scope")
	}
	if unit == "" || len(unit) > maxServiceUnitLength || strings.ContainsAny(unit, "/\x00") {
		return serviceOperation{}, errors.New("invalid service unit")
	}
	switch action {
	case "start", "stop", "restart", "reload", "enable", "disable", "mask", "unmask":
		return serviceOperation{Scope: scope, Unit: unit, Action: action}, nil
	default:
		return serviceOperation{}, errors.New("invalid service action")
	}
}
