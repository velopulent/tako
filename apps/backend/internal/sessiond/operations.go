package sessiond

import (
	"encoding/hex"
	"errors"
	"strings"

	"github.com/velopulent/tako/internal/platform"
)

type serviceOperation = platform.ServiceOperation

const maxServiceUnitLength = platform.MaxServiceUnitLength

func parseServiceOperation(scope, unit, action string) (serviceOperation, error) {
	return platform.ParseServiceOperation(scope, unit, action)
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
