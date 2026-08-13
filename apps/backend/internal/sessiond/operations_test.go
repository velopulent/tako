package sessiond

import (
	"strings"
	"testing"
)

func TestParseServiceOperationUsesExactAllowlist(t *testing.T) {
	valid, err := parseServiceOperation("system", "sshd.service", "restart")
	if err != nil || valid.Scope != "system" || valid.Unit != "sshd.service" || valid.Action != "restart" {
		t.Fatalf("valid operation = %+v, %v", valid, err)
	}
	for _, test := range []struct {
		name   string
		scope  string
		unit   string
		action string
	}{
		{name: "unknown scope", scope: "global", unit: "sshd.service", action: "restart"},
		{name: "path traversal", scope: "system", unit: "../tako.service", action: "restart"},
		{name: "empty unit", scope: "system", unit: "", action: "restart"},
		{name: "oversized unit", scope: "system", unit: strings.Repeat("x", maxServiceUnitLength+1), action: "restart"},
		{name: "unknown action", scope: "system", unit: "sshd.service", action: "run"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseServiceOperation(test.scope, test.unit, test.action); err == nil {
				t.Fatal("unsafe service operation was accepted")
			}
		})
	}
}

func TestParseHostConfigurationOperationValidatesIdentityAndFingerprint(t *testing.T) {
	configuration, err := parseHostConfigurationOperation("tako.example", "Asia/Kolkata", true, strings.Repeat("a", 64))
	if err != nil || configuration.Hostname != "tako.example" || !configuration.NTPEnabled {
		t.Fatalf("valid host configuration = %#v, %v", configuration, err)
	}
	for _, test := range []struct {
		name        string
		hostname    string
		timezone    string
		fingerprint string
	}{
		{name: "empty hostname", hostname: "", timezone: "UTC", fingerprint: strings.Repeat("a", 64)},
		{name: "hostname path", hostname: "../tako", timezone: "UTC", fingerprint: strings.Repeat("a", 64)},
		{name: "timezone traversal", hostname: "tako", timezone: "../etc", fingerprint: strings.Repeat("a", 64)},
		{name: "short fingerprint", hostname: "tako", timezone: "UTC", fingerprint: "deadbeef"},
		{name: "non-hex fingerprint", hostname: "tako", timezone: "UTC", fingerprint: strings.Repeat("z", 64)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseHostConfigurationOperation(test.hostname, test.timezone, false, test.fingerprint); err == nil {
				t.Fatal("unsafe host configuration was accepted")
			}
		})
	}
}
