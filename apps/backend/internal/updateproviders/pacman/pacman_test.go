package pacman

import "testing"

func TestParseInventoryAndHistory(t *testing.T) {
	items := parseInventory("linux 6.1 -> 6.2\n")
	if len(items) != 1 || items[0].CurrentVersion != "6.1" || items[0].CandidateVersion != "6.2" {
		t.Fatalf("unexpected inventory: %#v", items)
	}
	history := parseHistory("[2026-01-02T03:04:05+0000] [ALPM] upgraded linux (6.1 -> 6.2)\n")
	if len(history) != 1 || history[0].Packages["linux"] != "6.2" {
		t.Fatalf("unexpected history: %#v", history)
	}
}
