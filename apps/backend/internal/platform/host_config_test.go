package platform

import "testing"

func TestHostConfigurationFingerprintChangesWithAnyField(t *testing.T) {
	base := NewHostConfiguration("tako", "UTC", true)
	if len(base.Fingerprint) != 64 {
		t.Fatalf("unexpected fingerprint %q", base.Fingerprint)
	}
	for _, changed := range []HostConfiguration{
		NewHostConfiguration("other", "UTC", true),
		NewHostConfiguration("tako", "Asia/Kolkata", true),
		NewHostConfiguration("tako", "UTC", false),
	} {
		if changed.Fingerprint == base.Fingerprint {
			t.Fatalf("configuration change did not alter fingerprint: %#v", changed)
		}
	}
}
