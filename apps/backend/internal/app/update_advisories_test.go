package app

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/velopulent/tako/internal/platform"
)

func TestUpdatesExposeAllMatchedAdvisoriesWithoutElevatedCredentials(t *testing.T) {
	server, broker := newTestServer(t)
	current := signIn(t, server, false)
	broker.respond = func(operation string, _ json.RawMessage) (any, error) {
		if operation != "updates.status" {
			t.Fatalf("unexpected operation %s", operation)
		}
		return platform.UpdateStatus{Available: true, Backend: "dnf", Contract: "native-distro-provider", Packages: []platform.UpdatePackage{{Name: "openssl", Architecture: "x86_64", CurrentVersion: "1", CandidateVersion: "3"}}, AdvisoryState: "partial", Advisories: []platform.UpdateAdvisoryDetail{
			{ID: "ONE", Type: "security", Source: "dnf", CVEIDs: []string{"CVE-2026-12345"}, CVEUrls: []string{"https://www.cve.org/CVERecord?id=CVE-2026-12345"}, PackageKeys: []string{"openssl.x86_64"}},
			{ID: "TWO", Type: "bugfix", Source: "dnf", PackageKeys: []string{"openssl.x86_64"}},
		}}, nil
	}
	response := send(server, current, http.MethodGet, "/api/v1/updates", "")
	if response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	var got platform.UpdateStatus
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Advisories) != 2 || got.AdvisoryState != "partial" || got.Advisories[0].CVEIDs[0] != "CVE-2026-12345" {
		t.Fatalf("lost advisory details: %#v", got)
	}
	if broker.last().credentials.AdminToken != "" {
		t.Fatal("read-only inventory used admin credentials")
	}
}
