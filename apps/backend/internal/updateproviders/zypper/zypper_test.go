package zypper

import "testing"

func TestParseXMLAndHistory(t *testing.T) {
	items := parseXML(`<stream><update-list><update name="openssl" edition="3.2" edition-old="3.1" arch="x86_64"/></update-list></stream>`)
	if len(items) != 1 || items[0].Name != "openssl" || items[0].CurrentVersion != "3.1" {
		t.Fatalf("unexpected inventory: %#v", items)
	}
	history := parseHistory("2026-01-02 03:04:05|update|openssl|3.1|3.2|x86_64|repo\n")
	if len(history) != 1 || history[0].Packages["openssl"] != "3.2" {
		t.Fatalf("unexpected history: %#v", history)
	}
}

func TestParsePlanXMLIncludesRiskyActions(t *testing.T) {
	changes := parsePlanXML(`<stream><solvable status="to-be-uninstalled" name="old" edition-old="1"/><solvable status="to-be-upgraded" name="new" edition="2"/></stream>`)
	if len(changes) != 2 || changes[0].Action != "upgrade" || changes[1].Action != "remove" {
		t.Fatalf("unexpected plan: %#v", changes)
	}
}

func TestOperationDependsOnlyOnReleaseKind(t *testing.T) {
	if operation(Provider{}) != "update" || operation(Provider{tumbleweed: true}) != "dup" {
		t.Fatal("unexpected zypper operation")
	}
}
