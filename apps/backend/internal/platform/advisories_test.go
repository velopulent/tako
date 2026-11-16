package platform

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRPMVersionsPreserveEpochAndNativeOrdering(t *testing.T) {
	for _, test := range []struct {
		a, b string
		want int
	}{
		{"1:1.0-1", "0:99.0-1", 1}, {"0:1.0-1", "1.0-1", 0},
		{"1.10-1", "1.9-9", 1}, {"1.0~rc1-1", "1.0-1", -1},
		{"1.0^git1-1", "1.0-1", 1}, {"1.0^git1-1", "1.0.1-1", -1},
		{"1.0-2.el9", "1.0-10.el9", -1}, {"1.01-1", "1.1-1", 0},
		{"1a-1", "1.1-1", -1}, {"1.0-1", "1.0-1", 0},
	} {
		if got := CompareRPMVersions(test.a, test.b); got != test.want {
			t.Errorf("%s vs %s: got %d, want %d", test.a, test.b, got, test.want)
		}
		if got := CompareRPMVersions(test.b, test.a); got != -test.want {
			t.Errorf("reverse: %d", got)
		}
	}
}

func TestAdvisoriesMatchAllCrossedFixesWithoutNameOnlyFallback(t *testing.T) {
	values := []UpdateAdvisory{}
	for _, v := range []string{"1:1.0-1", "1:1.1-1", "1:1.2-1", "1:2.0-1", "0:99.0-1"} {
		values = append(values, UpdateAdvisory{ID: v, Type: "security", Packages: []UpdateAdvisoryPackage{{Name: "openssl", Architecture: "x86_64", Version: v}}})
	}
	items := []UpdatePackage{
		{Name: "openssl", Architecture: "x86_64", CurrentVersion: "1:1.0-1", CandidateVersion: "1:1.2-1"},
		{Name: "openssl", Architecture: "aarch64", CurrentVersion: "1:1.0-1", CandidateVersion: "1:1.2-1"},
		{Name: "openssl", Architecture: "x86_64", CandidateVersion: "1:1.2-1"},
	}
	ApplyUpdateAdvisories(items, values)
	if len(items[0].Advisories) != 2 || items[0].Advisories[0].ID != "1:1.1-1" || items[0].Advisories[1].ID != "1:1.2-1" {
		t.Fatalf("wrong crossed fixes: %#v", items[0].Advisories)
	}
	if len(items[1].Advisories) != 0 || len(items[2].Advisories) != 0 || items[2].AdvisoryState != "partial" {
		t.Fatal("matched unsupported architecture or unknown installed version")
	}
	values[0].Packages[0].Repository = "different"
	values[0].Packages[0].Version = "1:1.1-1"
	items[0].Repository = "updates"
	ApplyUpdateAdvisories(items[:1], values[:1])
	if len(items[0].Advisories) != 0 || items[0].AdvisoryID != "" || len(items[0].CVEUrls) != 0 {
		t.Fatal("matched a different repository")
	}
}

func TestCVEReferencesUseValidatedIDsAndDeduplicateAcrossPackages(t *testing.T) {
	value := UpdateAdvisory{ID: "TEST-1", Type: "security", Severity: "High", Source: "rpm-md", References: []UpdateReference{
		{ID: "cve-2026-12345", Type: "cve"}, {Type: "cve", URL: "https://vendor.test/CVE-2026-12345/"},
		{ID: "CVE-2026-12346", Type: "cve", URL: "javascript:alert(1)"}, {ID: "CVE-2026-1", Type: "cve"},
		{Type: "vendor", URL: "https://user:secret@example.com"}, {Type: "vendor", URL: "javascript:alert(1)"},
		{Type: "vendor", URL: "https://vendor.test/advisory"},
	}, Packages: []UpdateAdvisoryPackage{{Name: "a", Architecture: "x86_64", Version: "2"}, {Name: "b", Architecture: "x86_64", Version: "2"}}}
	items := []UpdatePackage{{Name: "a", Architecture: "x86_64", CurrentVersion: "1", CandidateVersion: "3"}, {Name: "b", Architecture: "x86_64", CurrentVersion: "1", CandidateVersion: "3"}}
	ApplyUpdateAdvisories(items, []UpdateAdvisory{value, value})
	status := UpdateStatus{Packages: items}
	collectUpdateAdvisories(&status)
	if len(status.Advisories) != 1 {
		t.Fatalf("duplicate advisories: %d", len(status.Advisories))
	}
	got := status.Advisories[0]
	if len(got.CVEIDs) != 2 || len(got.CVEUrls) != 2 || len(got.PackageKeys) != 2 || len(got.VendorUrls) != 1 || got.Severity != "important" || got.VendorSeverity != "High" {
		t.Fatalf("bad normalization: %#v", got)
	}
	if got.CVEUrls[0] != "https://www.cve.org/CVERecord?id=CVE-2026-12345" {
		t.Fatal(got.CVEUrls)
	}
}

func TestAdvisoryResponseIsBoundedAndReportsPartialCoverage(t *testing.T) {
	items := []UpdatePackage{{Name: "one", Architecture: "x86_64", CurrentVersion: "1", CandidateVersion: "2", AdvisoryState: "available"}}
	for i := 0; i < 400; i++ {
		items[0].Advisories = append(items[0].Advisories, UpdateAdvisoryDetail{ID: strings.Repeat("x", i+1), Description: strings.Repeat("a", maxAdvisoryText)})
	}
	status := UpdateStatus{Packages: items}
	collectUpdateAdvisories(&status)
	data, _ := json.Marshal(status.Advisories)
	if len(data) > maxAdvisoryResponseBytes+maxUpdateAdvisories+2 || status.AdvisoryState != "partial" {
		t.Fatalf("unbounded: %d %s", len(data), status.AdvisoryState)
	}
}

func TestAdvisoryMetadataDoesNotChangeInventoryFingerprint(t *testing.T) {
	status := UpdateStatus{Backend: "dnf", Packages: []UpdatePackage{{Name: "a", CurrentVersion: "1", CandidateVersion: "2"}}}
	before := UpdateFingerprint(status)
	status.Packages[0].Description = "new advisory information"
	if UpdateFingerprint(status) != before {
		t.Fatal("informational change invalidated approval")
	}
	status.Packages[0].CandidateVersion = "3"
	if UpdateFingerprint(status) == before {
		t.Fatal("candidate change failed to invalidate approval")
	}
}
