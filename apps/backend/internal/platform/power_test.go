package platform

import "testing"

func TestPowerActionStatesAreFailClosed(t *testing.T) {
	for _, test := range []struct {
		state     string
		action    string
		wantState string
		available bool
	}{
		{state: "yes", action: "reboot", wantState: "available", available: true},
		{state: "challenge", action: "shutdown", wantState: "challenged"},
		{state: "no", action: "reboot", wantState: "denied"},
		{state: "na", action: "shutdown", wantState: "unavailable"},
		{state: "unexpected", action: "reboot", wantState: "unavailable"},
	} {
		got := PowerActionStatusForState(test.state, test.action)
		if got.State != test.wantState || got.Available != test.available {
			t.Fatalf("state %q -> %#v, want %s/%v", test.state, got, test.wantState, test.available)
		}
	}
}

func TestPowerFingerprintChangesWithInhibitors(t *testing.T) {
	base := PowerStatus{Available: true, Reboot: PowerActionStatus{State: "available", Available: true}, Shutdown: PowerActionStatus{State: "available", Available: true}}
	base.Fingerprint = powerFingerprint(base)
	changed := base
	changed.Inhibitors = []PowerInhibitor{{What: "shutdown", Who: "backup", Why: "Snapshot", Mode: "block", PID: 42}}
	changed.Fingerprint = powerFingerprint(changed)
	if base.Fingerprint == changed.Fingerprint {
		t.Fatal("inhibitor changes did not alter power fingerprint")
	}
}

func TestPowerInhibitorsMakeActionUnavailable(t *testing.T) {
	status := PowerActionStatus{State: "available", Available: true}
	blocked := applyPowerInhibitors(status, "shutdown", []PowerInhibitor{{What: "shutdown:sleep", Who: "backup", Why: "Snapshot", Mode: "block"}})
	if blocked.State != "inhibited" || blocked.Available || blocked.Reason == "" {
		t.Fatalf("blocking inhibitor was not represented: %#v", blocked)
	}
	allowed := applyPowerInhibitors(status, "shutdown", []PowerInhibitor{{What: "shutdown", Who: "backup", Why: "Snapshot", Mode: "delay"}})
	if allowed.State != "available" || !allowed.Available {
		t.Fatalf("delay inhibitor unexpectedly blocked action: %#v", allowed)
	}
}
