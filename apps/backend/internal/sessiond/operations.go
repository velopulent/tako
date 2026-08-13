package sessiond

import (
	"encoding/hex"
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

type hostConfigurationOperation struct {
	Hostname            string
	Timezone            string
	NTPEnabled          bool
	ExpectedFingerprint string
}

func parseHostConfigurationOperation(hostname, timezone string, ntpEnabled bool, expectedFingerprint string) (hostConfigurationOperation, error) {
	if !validHostname(hostname) || timezone == "" || len(timezone) > 128 || strings.ContainsAny(timezone, "\x00\r\n") || strings.Contains(timezone, "..") || expectedFingerprint == "" {
		return hostConfigurationOperation{}, errors.New("invalid host configuration")
	}
	if len(expectedFingerprint) != 64 {
		return hostConfigurationOperation{}, errors.New("invalid host configuration fingerprint")
	}
	if _, err := hex.DecodeString(expectedFingerprint); err != nil {
		return hostConfigurationOperation{}, errors.New("invalid host configuration fingerprint")
	}
	return hostConfigurationOperation{Hostname: hostname, Timezone: timezone, NTPEnabled: ntpEnabled, ExpectedFingerprint: expectedFingerprint}, nil
}

func validHostname(value string) bool {
	if value == "" || len(value) > 253 || strings.ContainsAny(value, "\x00\r\n/ \t") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}
