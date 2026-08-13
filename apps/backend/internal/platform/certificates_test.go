package platform

import "testing"

func TestInspectCertificateDegradesWhenUnconfigured(t *testing.T) {
	status, err := InspectCertificate("")
	if err != nil || status.Configured || status.Valid || status.Warning == "" {
		t.Fatalf("unexpected unconfigured certificate status: %#v, %v", status, err)
	}
}
