package dnf

import "testing"

func TestParseJSONAndTable(t *testing.T) {
	jsonItems := parseJSON(`{"packages":[{"name":"openssl","version":"3.2","arch":"x86_64"}]}`)
	if len(jsonItems) != 1 || jsonItems[0].Name != "openssl" {
		t.Fatalf("unexpected JSON inventory: %#v", jsonItems)
	}
	table := parseTable("openssl.x86_64 3.2 updates\n")
	if len(table) != 1 || table[0].CandidateVersion != "3.2" {
		t.Fatalf("unexpected table inventory: %#v", table)
	}
}

func TestParsePlanIncludesRiskyActions(t *testing.T) {
	changes := parsePlan("Upgrading:\n openssl x86_64 3.2 updates 1 M\n\nRemoving:\n obsolete x86_64 1.0 installed 1 M\n")
	if len(changes) != 2 || changes[0].Action != "remove" || changes[1].Action != "upgrade" {
		t.Fatalf("unexpected plan: %#v", changes)
	}
}

func TestParseHistory(t *testing.T) {
	items := parseHistory("2026-01-02T03:04:05+0000 Upgraded: openssl-3.2-1.x86_64\n")
	if len(items) != 1 {
		t.Fatalf("unexpected history: %#v", items)
	}
}
