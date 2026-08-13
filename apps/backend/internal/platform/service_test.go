package platform

import (
	"errors"
	"strings"
	"testing"
)

func TestParseServiceOperationAllowlist(t *testing.T) {
	operation, err := ParseServiceOperation("user", "demo.service", "restart")
	if err != nil {
		t.Fatal(err)
	}
	if operation.Scope != "user" || operation.Unit != "demo.service" || operation.Action != "restart" {
		t.Fatalf("unexpected operation: %#v", operation)
	}

	for _, test := range []struct {
		name   string
		scope  string
		unit   string
		action string
		want   error
	}{
		{name: "scope", scope: "global", unit: "demo.service", action: "start", want: ErrInvalidServiceScope},
		{name: "path", scope: "system", unit: "../demo.service", action: "start", want: ErrInvalidServiceUnit},
		{name: "nul", scope: "system", unit: "demo\x00.service", action: "start", want: ErrInvalidServiceUnit},
		{name: "length", scope: "system", unit: strings.Repeat("x", MaxServiceUnitLength+1), action: "start", want: ErrInvalidServiceUnit},
		{name: "action", scope: "system", unit: "demo.service", action: "exec", want: ErrInvalidServiceAction},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseServiceOperation(test.scope, test.unit, test.action)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestUnitDetailsRejectsNonInventoryTargetsBeforeDBus(t *testing.T) {
	if _, err := UnitDetails(t.Context(), "other", "demo.service"); !errors.Is(err, ErrInvalidServiceScope) {
		t.Fatalf("invalid scope error = %v", err)
	}
	if _, err := UnitDetails(t.Context(), "system", "../demo.service"); !errors.Is(err, ErrInvalidServiceUnit) {
		t.Fatalf("invalid unit error = %v", err)
	}
}
