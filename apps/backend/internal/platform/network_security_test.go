package platform

import (
	"testing"
)

func TestNetworkOperationValidationIsFailClosed(t *testing.T) {
	if err := ValidateNetworkOperation(NetworkOperation{Backend: "NetworkManager", Action: "static", Address: "not-an-address"}); err == nil {
		t.Fatal("invalid static address accepted")
	}
	if err := ValidateNetworkOperation(NetworkOperation{Backend: "NetworkManager", Action: "dhcp", Interface: "eno1"}); err == nil {
		t.Fatal("mutation without fingerprint accepted")
	}
	if err := ValidateNetworkOperation(NetworkOperation{Backend: "NetworkManager", Action: "dhcp", Interface: "eno1", ExpectedFingerprint: "fingerprint", Confirmation: "CONFIRM NETWORK CHANGE"}); err != nil {
		t.Fatalf("valid DHCP operation rejected: %v", err)
	}
}

func TestFirewallOperationValidationProtectsAccess(t *testing.T) {
	operation := FirewallOperation{Backend: "UFW", Action: "disable", ExpectedFingerprint: "fingerprint", Confirmation: "CONFIRM FIREWALL CHANGE"}
	if err := ValidateFirewallOperation(operation); err != ErrFirewallAccessRisk {
		t.Fatalf("lockout warning = %v", err)
	}
	operation.Confirmation = "CONFIRM FIREWALL ACCESS"
	if err := ValidateFirewallOperation(operation); err != nil {
		t.Fatalf("protected disable rejected: %v", err)
	}
}

func TestSecurityOperationRejectsBroadRemediation(t *testing.T) {
	if err := ValidateSecurityOperation(SecurityOperation{Framework: "SELinux", Action: "selinux-restorecon", Path: "/", ExpectedFingerprint: "fingerprint", Confirmation: "CONFIRM NARROW SECURITY CHANGE"}); err != ErrSecurityUnsafe {
		t.Fatalf("root restorecon was not rejected: %v", err)
	}
	if err := ValidateSecurityOperation(SecurityOperation{Framework: "SELinux", Action: "selinux-boolean", Boolean: "bad name", ExpectedFingerprint: "fingerprint", Confirmation: "CONFIRM NARROW SECURITY CHANGE"}); err == nil {
		t.Fatal("invalid boolean accepted")
	}
}
