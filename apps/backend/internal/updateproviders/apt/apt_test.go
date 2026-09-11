package apt

import (
	"testing"

	"github.com/velopulent/tako/internal/platform"
)

func TestParseInventoryAndPlan(t *testing.T) {
	items := parseInventory("openssl/bookworm 3.0.14 amd64 [upgradable from: 3.0.11]\n")
	if len(items) != 1 || items[0].Name != "openssl" || items[0].CurrentVersion != "3.0.11" {
		t.Fatalf("unexpected inventory: %#v", items)
	}
	changes := parsePlan("Inst openssl [3.0.11] (3.0.14 Debian:stable [amd64])\nRemv obsolete [1.0]\n")
	if len(changes) != 2 || changes[0].Name != "obsolete" || changes[0].Action != "remove" || changes[1].Action != "upgrade" {
		t.Fatalf("unexpected plan: %#v", changes)
	}
}

func TestParseDocumentedStatusFD(t *testing.T) {
	progress, ok := parseStatus(platform.UpdateStreamEvent{Kind: "output", Output: platform.UpdateOutput{Stream: "stdout", Line: "dlstatus:openssl:42.5:Downloading"}})
	if !ok || progress.Phase != "downloading" || progress.Percent != 42 || progress.Cancelable {
		t.Fatalf("unexpected progress: %#v", progress)
	}
}

func TestParseHistory(t *testing.T) {
	items := parseHistory("Start-Date: 2026-01-02  03:04:05\nUpgrade: openssl:amd64 (1, 2)\nEnd-Date: 2026-01-02  03:04:06\n")
	if len(items) != 1 || items[0].Packages["openssl"] != "2" {
		t.Fatalf("unexpected history: %#v", items)
	}
}
